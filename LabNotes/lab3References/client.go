package kvraft

import (
	"crypto/rand"
	"math/big"
	"time"

	"../labrpc"
)

var nextClient int = 1

type Clerk struct {
	servers []*labrpc.ClientEnd
	leader  int   // server集群中最近一次存活的leader
	client  int   // 通过clerk向server集群发出请求的客户端id
	seqNum  int64 // sequence number for Put/Append request
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	ck.leader = 0
	ck.client = nextClient
	ck.seqNum = 1
	nextClient++
	return ck
}

//
// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
func (ck *Clerk) Get(key string) string {
	args := GetArgs{}
	args.Key = key
	// server集群中只有leader能响应client的请求. leader可能尚未被选举
	// 出来, 或者发生变更. Clerk.leader记录最近一次存活的leader, 如果
	// 本次失败就尝试别的server, 直到发现新的leader
	for {
		var reply GetReply
		ok := ck.servers[ck.leader].Call("KVServer.Get", &args, &reply)
		if ok && reply.Err != ErrWrongLeader {
			return reply.Value
		}

		ck.leader = (ck.leader + 1) % len(ck.servers)
		time.Sleep(50 * time.Millisecond)
	}
}

//
// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
func (ck *Clerk) PutAppend(key string, value string, op string) {
	args := PutAppendArgs{}
	args.Client = ck.client
	args.SeqNum = ck.seqNum
	args.Key = key
	args.Value = value
	args.Op = op

	// Put/Append不是幂等操作, Clerk.seqNum记录client每次Put/Append请求的序列号,
	// 每次发出新请求的时候就自增seqNum, 保证每个请求有唯一的序列号. KVServer会记录
	// 每个client的最新请求序列号, 防止重复执行旧命令
	ck.seqNum++

	for {
		var reply PutAppendReply
		ok := ck.servers[ck.leader].Call("KVServer.PutAppend", &args, &reply)
		if ok && reply.Err != ErrWrongLeader {
			return
		}
		ck.leader = (ck.leader + 1) % len(ck.servers)
		time.Sleep(50 * time.Millisecond)
	}
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}

func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}
