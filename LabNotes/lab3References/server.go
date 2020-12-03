package kvraft

import (
	"bytes"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
	"../raft"
)

type Op struct {
	ID     int64
	Client int    // which client does the request come from?
	SeqNum int64  // sequence number for Put/Append
	Cmd    string // "Put", "Append" or "Get"
	Key    string
	Value  string
}

func (a Op) sameAs(b Op) bool {
	return a.ID == b.ID
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big

	data    map[string]string // k/v storage
	seq4cli map[int]int64     // the latest sequence number processed for each client

	// chans will be created at each index in the Raft log,
	// as a notification for reaching-agreement
	agreeChs map[int]chan Op
}

func (kv *KVServer) snapshot() []byte {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(kv.data)
	e.Encode(kv.seq4cli)
	return w.Bytes()
}

func (kv *KVServer) readSnapshot(data []byte) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	if data == nil || len(data) < 1 {
		return
	}

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)

	var kvdata map[string]string
	var seq4cli map[int]int64

	if d.Decode(&kvdata) != nil ||
		d.Decode(&seq4cli) != nil {
		log.Fatal("KVServer.readSnapshot")
	} else {
		kv.data = kvdata
		kv.seq4cli = seq4cli
	}
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	op := Op{}
	op.ID = nrand()
	op.Cmd = "Get"
	op.Key = args.Key

	index, _, ok := kv.rf.Start(op)
	if ok {
		ch := kv.agreeCh(index)
		select {
		case op2 := <-ch:
			// 在等待Raft达成一致的过程中, 原本的leader可能会失效, 旧的日志项可能被
			// 新的leader覆盖掉, 旧的请求(op)也就失效了.
			if op.sameAs(op2) {
				reply.Value = op2.Value
				if reply.Value == "" {
					reply.Err = ErrNoKey
				} else {
					reply.Err = OK
				}
				return
			}
		case <-time.After(1 * time.Second):
		}
	}
	reply.Err = ErrWrongLeader
	reply.Value = ""
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	op := Op{}
	op.ID = nrand()
	op.Client = args.Client
	op.SeqNum = args.SeqNum
	op.Cmd = args.Op
	op.Key = args.Key
	op.Value = args.Value

	index, _, ok := kv.rf.Start(op)
	if ok {
		ch := kv.agreeCh(index)
		select {
		case op2 := <-ch:
			if op.sameAs(op2) {
				reply.Err = OK
				return
			}
		case <-time.After(1 * time.Second):
		}
	}
	reply.Err = ErrWrongLeader
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

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
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
	kv.data = make(map[string]string)
	kv.seq4cli = make(map[int]int64)
	kv.agreeChs = make(map[int]chan Op)

	kv.readSnapshot(persister.ReadSnapshot())

	go func() {
		for !kv.killed() {
			select {
			case msg := <-kv.applyCh: // 大多数节点达成一致时Raft才会apply
				if msg.CommandValid {
					op, ok := msg.Command.(Op)
					if !ok {
						continue
					}

					kv.mu.Lock()
					if !kv.seenLocked(op.Client, op.SeqNum) { // 这是不是一个旧的(重复的)请求?
						kv.seq4cli[op.Client] = op.SeqNum
						switch op.Cmd {
						case "Put":
							kv.data[op.Key] = op.Value
						case "Append":
							kv.data[op.Key] += op.Value
						}
					}
					if op.Cmd == "Get" {
						op.Value = kv.data[op.Key]
					}
					// 每次收到来自 applyCh 的消息就说明 Raft 的日志又增长了,
					// 所以把对 Raft state size 的检查放在此处进行
					kv.checkSnapshot(msg.CommandIndex)
					kv.mu.Unlock()

					// KVServer.Put/Append/Get 通过 Raft.Start 发出请求后, 需等待Raft达成一致,
					// 现在通知他们: Raft在索引为 CommandIndex 的日志项上已经达成一致
					kv.agreeCh(msg.CommandIndex) <- op
				} else {
					// CommandValid is false -> snapshot
					snapshot := msg.Command.([]byte)
					kv.readSnapshot(snapshot)
				}
			}
		}
	}()

	return kv
}

func (kv *KVServer) agreeCh(index int) chan Op {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	ch, ok := kv.agreeChs[index]
	if !ok {
		ch = make(chan Op, 1)
		kv.agreeChs[index] = ch
	}
	return ch
}

func (kv *KVServer) seenLocked(client int, seqNum int64) bool {
	val, ok := kv.seq4cli[client]
	if ok && val >= seqNum {
		return true
	}
	return false
}

func (kv *KVServer) seenUnlocked(client int, seqNum int64) bool {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	return kv.seenLocked(client, seqNum)
}

func (kv *KVServer) checkSnapshot(lastIncludedIndex int) {
	if kv.maxraftstate == -1 ||
		float32(kv.rf.RaftStateSize()) < float32(kv.maxraftstate)*0.8 {
		return
	}

	ss := kv.snapshot()
	go kv.rf.TakeSnapshot(lastIncludedIndex, ss)
}
