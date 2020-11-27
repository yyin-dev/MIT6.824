package kvraft

import "log"

const (
	OK             = "OK"
	ErrNoKey       = "ErrNoKey"
	ErrWrongLeader = "ErrWrongLeader"
)

const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}

type Err string

// Put or Append
type PutAppendArgs struct {
	Key   string
	Value string
	Op    string // "Put" or "Append"

	Cid int64
	Seq int
}

type PutAppendReply struct {
	Err Err
}

type GetArgs struct {
	Key string

	Cid int64
	Seq int
}

type GetReply struct {
	Err   Err
	Value string
}
