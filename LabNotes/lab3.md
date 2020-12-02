# Lab 3: Fault-tolerant Key/Value Service

The purpose of this note is to serve as a detailed guide for MIT 6.824 Lab3 KV Raft. This note presents implementation details and common pitfalls.

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
    
    func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
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