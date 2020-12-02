package kvraft

import (
	"crypto/rand"
	"math/big"

	"../labrpc"
)

type Clerk struct {
	servers    []*labrpc.ClientEnd
	cid        int64
	nextSeq    int
	prevLeader int
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

// Assume that a client will make only one call into a Clerk at a time.
func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	ck.cid = nrand()
	ck.nextSeq = 1
	ck.prevLeader = 0
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
	DPrintf("Client: GET(%v) [%v] starts", key, ck.cid)

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
				DPrintf("Client: GET(%v) [%v] done -> %v.", key, ck.cid, reply.Value)
				return reply.Value
			case ErrNoKey:
				ck.prevLeader = i
				DPrintf("Client: GET(%v) [%v] done -> %v.", key, ck.cid, "")
				return ""
			case ErrWrongLeader:
				continue
			}
		}
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
	DPrintf("Client: %v(%v, %v) [%v] starts", op, key, value, ck.cid)
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
				DPrintf("Client: %v(%v, %v) [%v] done", op, key, value, ck.cid)
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
