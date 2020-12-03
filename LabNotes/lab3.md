# Lab 3: Fault-tolerant Key/Value Service

The purpose of this note is to serve as a detailed guide for MIT 6.824 Lab3 KV Raft. This note presents implementation details and common pitfalls. If you have any questions, or you want to know more about Lab3, feel free to raise an issue and make this note better!



## Lab3A: Key/value service without log compaction

### Client

The client is straight-forward. You can remeber `prevLeader` to make it slightly more efficient.



### Server

- Structure overview

    A go-routine `applyCommitted` continuously reads from `kv.applyCh` for commited commands, and apply them with the help of a duplicate detection scheme.

    When the server's RPC handler handles a request, it checks for leadership, submits the request to Raft, and waits for the command to be committed. When the command is applied (by `applyCommitted`), the handler should be notified about this and reply to client. Thus, information needs to be passed from the handler to `applyCommitted`. 

- Linearizability

    To ensure linearizability, only the leader can submit command to Raft using `Start`. However, when the command is committed, any server can reply to client.

- Time-out 

    A time-out scheme needs to be implemented (it's easier to do on the server-side), which can be done using 

    ```go
    select {
    case <- signalDoneChan:
        ...
    case <- time.After(duration):
        ...
    }
    ```

- Duplicate detection scheme

    As mentioned in the [student guide](https://thesquareplanet.com/blog/students-guide-to-raft/#duplicate-detection), this can be implemented using a `ClientID` and a `SeqNum` for each request, based on the assumption that "It's OK to assume that a client will make only one call into a Clerk at a time". We use a `map[int64]int` to record the largest `SeqNum` for each client that has been **applied**.

    In `applyCommitted`, we only apply commands that satisfy `op.Seq > kv.clientSeqMap[op.Cid]`.

- Implementation details

    **Passing information** As mentioned above, `applyCommitted` needs to notify any waiting RPC handlers. This can be done using (1) shared memory & conditional variables; or (2) channels, and I tried both. Using channels makes the code shorter and more readable (and faster, as it requires less locking?). 

    **Checking the term?** In my initial RPC handlers, I record the `startTerm` when handing the command to Raft, and record the `applyTerm` when the command is applied. And only reply `OK` to client if `startTerm == applyTerm`. However, this is WRONG! Consider the following case: 5 servers {1, 2, 3, 4, 5} are in two partitions {1, 2, 3} and {4, 5}, with 1 being the leader in the 1st partition. A command C is passed to 1 at term t1. Now the partition heals, 1 becomes candidate but is re-elected as leader for a larger term t2, and C is passed to 1 again. Now, 1's log contains C(t1) and C(t2), and RPC handlers are waiting for C to be applied in t2. Suppose everything goes well and both entries get committed. When C is applied in `applyCommitted`, the server updates `kv.clientSeqMap` and notifies RPC handlers that it's committed in t1. When C(t2) is received on `applyCh`, it's considered duplicate and discarded. In this way, RPC handlers wait forever for C to be committed in t2. The system is in livelock (program running but making no useful progress). This fails test `partitions, one client (3A)`. 

    **Checking command** We should check the command instead of the term, as `clientID-seqNum` should be used to identify a command. 

    `client.go`:

    ```go
    type Clerk struct {
    	servers    []*labrpc.ClientEnd
    	cid        int64
    	nextSeq    int
    	prevLeader int
    }
    
    func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
    	ck := new(Clerk)
    	ck.servers = servers
    	ck.cid = nrand()
    	ck.nextSeq = 1
    	ck.prevLeader = 0
    	return ck
    }
    
    func (ck *Clerk) Get(key string) string {
    	args := GetArgs{
    		Key: key,
    		Cid: ck.cid,
    		Seq: ck.nextSeq,
    	}
    	ck.nextSeq++
    
    	for i := ck.prevLeader; ; i = (i + 1) % len(ck.servers) {
    		reply := GetReply{}
    		ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
    		if ok {
    			switch reply.Err {
    			case OK:
    				ck.prevLeader = i
    				return reply.Value
    			case ErrNoKey:
    				ck.prevLeader = i
    				return ""
    			case ErrWrongLeader:
    				continue
    			}
    		}
    	}
    }
    
    func (ck *Clerk) PutAppend(key string, value string, op string) {
    	args := PutAppendArgs{
    		Key:   key,
    		Value: value,
    		Op:    op,
    		Cid:   ck.cid,
    		Seq:   ck.nextSeq,
    	}
    	ck.nextSeq++
    
    	for i := ck.prevLeader; ; i = (i + 1) % len(ck.servers) {
    		reply := GetReply{}
    		ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
    		if ok {
    			switch reply.Err {
    			case OK:
    				ck.prevLeader = i
    				return
    			case ErrWrongLeader:
    				continue
    			}
    		}
    	}
    }
    
    func (ck *Clerk) Put(key string, value string) {
    	ck.PutAppend(key, value, "Put")
    }
    func (ck *Clerk) Append(key string, value string) {
    	ck.PutAppend(key, value, "Append")
    }
    ```

    `server.go`:

    ```go
    const (
    	GET    = "Get"
    	PUT    = "Put"
    	APPEND = "Append"
    )
    
    type Op struct {
    	Type  string
    	Key   string
    	Value string
    
    	Cid int64
    	Seq int
    }
    
    type KVServer struct {
    	mu           sync.Mutex
    	me           int
    	rf           *raft.Raft
    	applyCh      chan raft.ApplyMsg
    	dead         int32 // set by Kill()
    	maxraftstate int   // snapshot if log grows this big
    
    	store        map[string]string
    	clientSeqMap map[int64]int
    	waitChans    map[int](chan Op)
    
    	waitApplyTime time.Duration
    	persister     *raft.Persister
    }
    
    func (kv *KVServer) getWaitCh(index int) chan Op {
    	kv.mu.Lock()
    	defer kv.mu.Unlock()
    
    	ch, ok := kv.waitChans[index]
    	if !ok {
    		ch = make(chan Op, 1)
    		kv.waitChans[index] = ch
    	}
    	return ch
    }
    
    func (a Op) sameAs(b Op) bool {
    	return a.Cid == b.Cid && a.Seq == b.Seq
    }
    
    func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
    	op := Op{
    		Type: GET,
    		Key:  args.Key,
    		Cid:  args.Cid,
    		Seq:  args.Seq,
    	}
    	index, _, isLeader := kv.rf.Start(op)
    	if !isLeader {
    		reply.Err = ErrWrongLeader
    		return
    	}
    
    	ch := kv.getWaitCh(index)
    	select {
    	case appliedOp := <-ch:
    		if op.sameAs(appliedOp) {
    			reply.Value = appliedOp.Value
    			if reply.Value == "" {
    				reply.Err = ErrNoKey
    			} else {
    				reply.Err = OK
    			}
    		}
    	case <-time.After(kv.waitApplyTime):
    		reply.Err = ErrWrongLeader
    	}
    }
    
    func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
    	op := Op{
    		Type:  args.Op,
    		Key:   args.Key,
    		Value: args.Value,
    		Cid:   args.Cid,
    		Seq:   args.Seq,
    	}
    	index, _, isLeader := kv.rf.Start(op)
    	if !isLeader {
    		reply.Err = ErrWrongLeader
    		return
    	}
    
    	ch := kv.getWaitCh(index)
    	select {
    	case appliedOp := <-ch:
    		if op.sameAs(appliedOp) {
    			reply.Err = OK
    		}
    	case <-time.After(kv.waitApplyTime):
    		reply.Err = ErrWrongLeader
    	}
    }
    
    func (kv *KVServer) applyCommitted() {
    	for msg := range kv.applyCh {
    		if kv.killed() {
    			return
    		}
    
    		op := msg.Command.(Op)
    		kv.mu.Lock()
    
    		if op.Seq > kv.clientSeqMap[op.Cid] {
    			switch op.Type {
    			case GET:
    				// do nothing
    			case PUT:
    				kv.store[op.Key] = op.Value
    			case APPEND:
    				kv.store[op.Key] += op.Value
    			}
    
    			kv.clientSeqMap[op.Cid] = op.Seq
    		}
    		if op.Type == GET {
    			op.Value = kv.store[op.Key]
    		}
    		kv.mu.Unlock()
            
    		kv.getWaitCh(msg.CommandIndex) <- op
    	}
    }
    
    func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) 
        *KVServer {
    	// call labgob.Register on structures you want
    	// Go's RPC library to marshall/unmarshall.
    	labgob.Register(Op{})
    
    	kv := new(KVServer)
    	kv.me = me
    	kv.maxraftstate = maxraftstate
    	kv.applyCh = make(chan raft.ApplyMsg)
    	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
    	kv.persister = persister
    
    	kv.store = make(map[string]string)
    	kv.clientSeqMap = make(map[int64]int)
    	kv.waitChans = make(map[int](chan Op))
    	kv.waitApplyTime = 1000 * time.Millisecond
    
    	go kv.applyCommitted()
    	return kv
    }
    ```

    Note that in `applyCommitted`, a command is only applied if `op.Seq > kv.clientSeqMap[op.Cid]`, but message must **always** sent on channels (`kv.getWaitCh(msg.CommandIndex) <- op` is always executed). Consider the following case: Client whose clientID is `cid` sends command with seqNum of `1` to server. The command is submitted to Raft and received on `applyCh`. After passing the check `op.Seq > kv.clientSeqMap[op.Cid]`, `applyCommitted` applys the command and updates `kv.clientSeqMap[op.Cid]`. However, at this time, the RPC handler has already timed-out and is not waiting. Client `cid` would resend requests to servers and the command would be submitted to Raft again. However, when received on `applyCh` again, the check `op.Seq > kv.clientSeqMap[op.Cid]`  fails, but we still need to notify any waiting RPC handlers. Otherwise, the client would never get reply from server.

    

    ### Raft

    Almost no change is needed for `raft.go`. Just include `CommandTerm` in `ApplyMsg`.



## Part B: Key/value service with log compaction

Lab3B is hard, for two reasons: (1) compared with core Raft (Lab2), the paper is vague on this part; (2) log compaction requires close interaction between server and Raft. So instead of stricting following the instructions in the paper, we need to do some design work.



### Server

- Basics

    When kv-server decides to take snapshot, it creates the snapshot of current state and passes it to Raft. Raft discards log entries covered by the snapshot; When a Raft instance receives a snapshot (from `InstallSnapshot`), it passes the snapshot to kv-server (using `applyCh`) to reset its state.

- Application state

    Only `kv.store` and `kv.clientSeqMap` should be snapshotted, even if you are using shared memory & conditional variable for information passing in Lab3A.

- New variables

    As mentioned in paper, Raft needs two additional variables `lastIncludedIndex` and `lastIncludedTerm` for log compaction, and they are part of the Raft state that should be persisted. The kv-server doesn't need this information, as it can just call `kv.snapshotCheck(msg.CommandIndex)`.

So we make the following changes to `server.go`:

```go
func (kv *KVServer) applyCommitted() {
	for msg := range kv.applyCh {
		if kv.killed() {
			return
		}

		if msg.CommandValid {
			// logic for handling applied command
		} else {
			DPrintf("=%v= snapshot <- applyCh", kv.me)
			snapshot := msg.Command.([]byte)
			kv.readSnapshot(snapshot)
		}
	}
}

func (kv *KVServer) snapshotCheck(lastAppliedIndex int) {
	threshold := float32(0.7)
	maxRaftState := float32(kv.maxraftstate)
	currStateSize := float32(kv.persister.RaftStateSize())
	if maxRaftState > -1 && currStateSize > maxRaftState*threshold {
		go kv.rf.TakeSnapshot(lastAppliedIndex, kv.getSnapshot())
	}
}

func (kv *KVServer) getSnapshot() []byte {
	buffer := new(bytes.Buffer)
	encoder := labgob.NewEncoder(buffer)
	encoder.Encode(kv.store)
	encoder.Encode(kv.clientSeqMap)
	snapshot := buffer.Bytes()
	return snapshot
}

func (kv *KVServer) readSnapshot(data []byte) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if data == nil || len(data) < 1 {
		return
	}

	var store map[string]string
	var clientSeqMap map[int64]int
	buffer := bytes.NewBuffer(data)
	decoder := labgob.NewDecoder(buffer)
	if decoder.Decode(&store) != nil ||
		decoder.Decode(&clientSeqMap) != nil {
	} else {
		kv.store = store
		kv.clientSeqMap = clientSeqMap
	}
}
```



### Raft

- Log suffix

    Due to log compaction, we only have a suffix of the entire log. For discussion purpose, we call them "quantum" and "actual" properties, respectively. See the illustration below, where unfilled squares represent discarded logs and filled squares represent current log suffix.

    ```
    phantom:   0 1 2 3 4 5 6 7 8 9
               □ □ □ □ □ ■ ■ ■ ■ ■
    actual:              0 1 2 3 4
    ```

    As mentioned in the lab instructions, the first step to make Raft handles log suffix. I wrote some helper functions like `p2a`, `a2p`, `getLastLogIndex`, etc. to make it easier and more readable.

    One implementation detail is that in Lab2, I have a dummy entry `logEntry{}` at the beginning of the log, so log index starts from 1. The benefit is to avoid certain out-of-bound errors. However, in Lab3B, this dummy could be snapshotted. So I made changes accordingly. For example, in `Make`:

    ```go
    func Make(peers []*labrpc.ClientEnd, me int, persister *Persister, applyCh chan ApplyMsg) *Raft {
    	rf := &Raft{
    		// ...
             log:      []logEntry{logEntry{}},
    		commitIndex: 0,
    		lastApplied: 0,
    		lastIncludedIndex: -1,
    	}
    
    	rf.lastApplied = max(rf.lastApplied, rf.lastIncludedIndex) // lastApplied >= 0
    	rf.commitIndex = max(rf.commitIndex, rf.lastIncludedIndex) // commitIndex >= 0
        
    	// ...
    }
    ```

- AppendEntries

    The AppendEntries RPC requires careful changes. See the illustration below:

    ```
                       CaseA     CaseB				  CaseC
    args.prevLogIndex    ↓		  ↓					  ↓
           				    □ □ □ □ □ □ □ □ □ □ □ □ □
                           ↑
                    LastIncludedIndex
    ```

    Lab2 handles caseB and caseC, but caseA only happens with log compaction. Thus we have a new branch in `AppendEntries` handler that handles caseA:

    ```go
    if args.PrevLogIndex <= rf.lastIncludedIndex {
        reply.Success = true
        DPrintf("[%v] Entries starts before args.PrevLogIndex", rf.me)
    
        if args.PrevLogIndex+len(args.Entries) <= rf.lastIncludedIndex {
            return
        }
    
        appendStart := rf.lastIncludedIndex - args.PrevLogIndex
        if appendStart >= len(args.Entries) {
            return
        }
    
        i := appendStart
        for ; i < len(args.Entries); i++ {
            iPhantom := i + args.PrevLogIndex + 1
            iActual := rf.p2a(iPhantom)
            if iActual >= len(rf.log) {
                // no conflict, but runs out of log
                break
            }
    
            if args.Entries[i].Term != rf.log[iActual].Term {
                // conflict
                rf.log = rf.log[:iActual]
            }
        }
        rf.log = append(rf.log, args.Entries[i:]...)
        
        return
    }
    ```

    Note that if `args.PrevLogIndex <= rf.lastIncludedIndex`, we make `reply.Success = true`. The reason is that value of `reply.Success` depends on whether the follower's and server's log match up to `args.PrevLogIndex`. We know the follower's and the server's log match up to `rf.LastIncludedIndex`, otherwise it cannot be snapshotted. Thus, as `args.PrevLogIndex <= rf.lastInlucdedIndex`, the log matches up to `args.PrevLogIndex`. 

    And don't forget about the reminder in the student guide: 

    > Another issue many had (often immediately after fixing the issue above), was that, upon receiving a heartbeat, they would truncate the follower’s log following `prevLogIndex`, and then append any entries included in the `AppendEntries` arguments. This is *also* not correct. We can once again turn to Figure 2:
    >
    > *If* an existing entry conflicts with a new one (same index but different terms), delete the existing entry and all that follow it.
    >
    > The *if* here is crucial. If the follower has all the entries the leader sent, the follower **MUST NOT** truncate its log. Any elements *following* the entries sent by the leader **MUST** be kept. This is because we could be receiving an outdated `AppendEntries` RPC from the leader, and truncating the log would mean “taking back” entries that we may have already told the leader that we have in our log.

    You must have done this in Lab2, but just in case you forgot.

    In my source code, comments in `AppendEntries` carefully explained different cases. You might find it helpful.  

- InstallSnapshot

    ```go
    func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
    	rf.mu.Lock()
    	defer rf.mu.Unlock()
    
    	rf.revertToFollowerIfOutOfTerm(args.Term)
    	reply.Term = rf.currTerm
    
    	if args.Term < rf.currTerm {
            // This is an obselete RPC call from previous terms
    		return
    	}
    
    	rf.prevTimeElecSuppressed = time.Now()
    
    	if args.LastIncludedIndex <= rf.lastIncludedIndex {
            // This is an obselete call whose snapshot is useless
    		return
    	}
    
    	lastIncludedIndexActual := rf.p2a(args.LastIncludedIndex)
    	entryExists := 0 <= lastIncludedIndexActual && lastIncludedIndexActual < len(rf.log)
    	if entryExists && rf.log[lastIncludedIndexActual].Term == args.LastIncludedTerm {
    		rf.log = rf.log[lastIncludedIndexActual+1:]
    	} else {
    		rf.log = []logEntry{}
    	}
    
    	rf.lastIncludedIndex = args.LastIncludedIndex
    	rf.lastIncludedTerm = args.LastIncludedTerm
    	rf.persister.SaveStateAndSnapshot(rf.getRaftState(), args.Data)
    
    	rf.lastApplied = max(rf.lastApplied, rf.lastIncludedIndex)
    	rf.commitIndex = max(rf.commitIndex, rf.lastIncludedIndex)
    	DPrintf("[%v]  new Log: %v", rf.me, rf.log)
    
    	if rf.lastApplied > rf.lastIncludedIndex {
             // Current progress exceeds snapshot. The snapshot is obselete.
             // Skipping this check fails the linearizability check. 
    		return
    	}
    
    	snapshotMsg := ApplyMsg{
    		CommandValid: false,
    		Command:      args.Data,
    	}
    
    	rf.applyCh <- snapshotMsg
    }
    ```

    Nothing very tricky. But pay attention to the checks `if args.Term < rf.currTerm`, `if args.LastIncludedIndex <= rf.lastIncludedIndex`, `if rf.lastApplied > rf.lastIncludedIndex`. 

- Take snapshot

    ```go
    func (rf *Raft) TakeSnapshot(lastIncludedIndex int, snapshot []byte) {
    	rf.mu.Lock()
    	defer rf.mu.Unlock()
    
    	if lastIncludedIndex <= rf.lastIncludedIndex {
    		return
    	}
    
    	lastIncludedIndexActual := rf.p2a(lastIncludedIndex)
    	lastIncludedTerm := rf.index2term(lastIncludedIndex)
    
    	rf.lastIncludedIndex = lastIncludedIndex
    	rf.lastIncludedTerm = lastIncludedTerm
    	rf.log = rf.log[lastIncludedIndexActual+1:]
    
    	rf.persister.SaveStateAndSnapshot(rf.getRaftState(), snapshot)
    }
    ```

    Note that this is called by `kv.snapshotCheck` using `go kv.rf.TakeSnapshot(lastAppliedIndex, kv.getSnapshot())` to avoid deadlock. Thus, the check `if lastIncludedIndex <= rf.lastIncludedIndex` is important. 



### Coding advice

Sections above presented designs that I believe are relatively simple to implement and easy to reason about. During my implementation, I tried several less elegant designs. I briefly discuss them here.

- Server: Communication between `applyCommitted` and RPC handlers

    As mentioned, using `map[int](chan Op)` is better than using `map[int]Op` and `CondVar`, in my opinion.

-  Server: `LastIncludedIndex`

    In my first implementation, `lastIncludedIndex` is not only maintained by Raft, but also the server. The is not necessarily wrong, but adds additional complexity to the program. The simplest strategy is to call `snapshotCheck` after applying any command, and use `msg.CommandIndex` directly. 

- Server restore snapshot on restart

    In my first implementation, on restart, the Raft sends the snapshot on `applyCh` to server. This is not necessarily wrong, but adds additional complexity. The simplest way is do `kv.readSnapshot(kv.persister.ReadSnapshot())`.

- Raft: apply committed

    In Lab2 implementation, we use a separate go-routine for sending command to `applyCh`. When `rf.commitIndex` is increased, we notify this go-routine using some conditional variable. Again, this is not wrong but adds additional complexity, especially since we now have log compaction. A more straight-forward (and easier for debugging) way is to use function calls and do this synchronously. See `rf.tryCommit` and `rf.apply`. 



## Reference

In `./lab3References`.























