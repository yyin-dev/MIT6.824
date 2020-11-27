package kvraft

import (
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
	"../raft"
)

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

	store         map[string]string
	clientSeqMap  map[int64]int
	applyResMap   map[int64](map[int]bool)
	applyCond     *sync.Cond
	waitApplyTime time.Duration
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	kv.mu.Lock()
	if args.Seq < kv.clientSeqMap[args.Cid] {
		// A greater Seq has been seen. So args.Seq already finished.
		kv.mu.Unlock()
		return
	}

	// args.Seq >= kv.clientSeqMap[args.Cid]
	op := Op{
		Type: GET,
		Key:  args.Key,
		Cid:  args.Cid,
		Seq:  args.Seq,
	}
	_, _, isLeader := kv.rf.Start(op)
	if !isLeader {
		reply.Err = ErrWrongLeader
		kv.mu.Unlock()
		return
	}

	// isLeader = true
	waitApplyCh := make(chan bool)
	go func(cid int64, seq int) {
		// Wait until kv.applyResMap[cid][seq] exists and send on waitApplyCh
		kv.mu.Lock()

		_, applied := kv.applyResMap[cid][seq]
		for !applied {
			kv.applyCond.Wait()
			_, applied = kv.applyResMap[cid][seq]
		}

		kv.mu.Unlock()
		waitApplyCh <- true
	}(args.Cid, args.Seq)
	kv.mu.Unlock()

	select {
	case <-waitApplyCh:
		kv.mu.Lock()
		if v, exists := kv.store[args.Key]; exists {
			reply.Err = OK
			reply.Value = v
		} else {
			reply.Err = ErrNoKey
		}
		kv.mu.Unlock()
	case <-time.After(kv.waitApplyTime):
		reply.Err = ErrWrongLeader
	}
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	kv.mu.Lock()
	if args.Seq < kv.clientSeqMap[args.Cid] {
		kv.mu.Unlock()
		return
	}

	op := Op{
		Type:  args.Op,
		Key:   args.Key,
		Value: args.Value,
		Cid:   args.Cid,
		Seq:   args.Seq,
	}
	_, _, isLeader := kv.rf.Start(op)
	if !isLeader {
		reply.Err = ErrWrongLeader
		kv.mu.Unlock()
		return
	}

	waitApplyCh := make(chan bool)
	go func(cid int64, seq int) {
		kv.mu.Lock()

		_, applied := kv.applyResMap[cid][seq]
		for !applied {
			kv.applyCond.Wait()
			_, applied = kv.applyResMap[cid][seq]
		}

		kv.mu.Unlock()
		waitApplyCh <- true
	}(args.Cid, args.Seq)
	kv.mu.Unlock()

	select {
	case <-waitApplyCh:
		reply.Err = OK
	case <-time.After(kv.waitApplyTime):
		reply.Err = ErrWrongLeader
	}
}

func (kv *KVServer) applyCommitted() {
	for msg := range kv.applyCh {
		kv.mu.Lock()

		DPrintf("=%v= %v <- applyCh", kv.me, msg)
		op := msg.Command.(Op)

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
			if _, exists := kv.applyResMap[op.Cid]; !exists {
				kv.applyResMap[op.Cid] = make(map[int]bool)
			}
			kv.applyResMap[op.Cid][op.Seq] = true
			DPrintf("=%v= clientSeqMap=%v, applyResMap=%v", kv.me, kv.clientSeqMap, kv.applyResMap)
			kv.applyCond.Broadcast()
		}

		kv.mu.Unlock()
	}
}

//
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})

	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate
	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	kv.store = make(map[string]string)
	kv.clientSeqMap = make(map[int64]int)
	kv.applyResMap = make(map[int64](map[int]bool))
	kv.applyCond = sync.NewCond(&kv.mu)
	kv.waitApplyTime = 300 * time.Millisecond

	go kv.applyCommitted()
	return kv
}

//
// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
//
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}
