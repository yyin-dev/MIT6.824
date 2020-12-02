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
			DPrintf("=%v= %v <- applyCh, store=%v:%v", kv.me, msg, op.Key, kv.store[op.Key])
		} else {
			DPrintf("=%v= %v <- applyCh, duplicate", kv.me, msg)
		}

		if op.Type == GET {
			op.Value = kv.store[op.Key]
		}
		kv.mu.Unlock()

		kv.getWaitCh(msg.CommandIndex) <- op
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
	kv.persister = persister

	kv.store = make(map[string]string)
	kv.clientSeqMap = make(map[int64]int)
	kv.waitChans = make(map[int](chan Op))
	kv.waitApplyTime = 1000 * time.Millisecond

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
