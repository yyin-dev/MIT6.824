package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isLeader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	crand "crypto/rand"
	"log"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
)

const (
	ElectionTimeoutLower = 300
	ElectionTimeoutUpper = 500
	HeartbeatTimeout     = 100 // heartbeat的周期要短于election timeout, 否则无法选出稳定的Leader
)

const (
	Follower  int32 = 0
	Candidate int32 = 1
	Leader    int32 = 2
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

type LogEntry struct {
	Term    int
	Command interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	timeoutMsgCh chan struct{}
	voteCh       chan struct{}
	applyCh      chan ApplyMsg
	state        int32
	grantedVotes int32

	// persistent state on all servers
	currentTerm int
	votedFor    int
	logs        []LogEntry

	// volatile state on all servers
	commitIndex int
	lastApplied int

	// volatile state on leaders
	nextIndex  []int
	matchIndex []int

	// snapshot
	lastIncludedIndex int

	// for debugging Lab4B
	gid int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	state := atomic.LoadInt32(&rf.state)
	term := rf.currentTerm
	isLeader := (state == Leader)
	return term, isLeader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.logs)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)

	var currentTerm int
	var votedFor int
	var lastIncludedIndex int
	var logs []LogEntry

	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&lastIncludedIndex) != nil ||
		d.Decode(&logs) != nil {
		log.Fatal("Raft.readPersist")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.lastIncludedIndex = lastIncludedIndex
		rf.logs = logs
	}
}

func (rf *Raft) persistStateAndSnapshot(snapshot []byte) {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.logs)
	state := w.Bytes()
	rf.persister.SaveStateAndSnapshot(state, snapshot)
}

func (rf *Raft) RaftStateSize() int {
	return rf.persister.RaftStateSize()
}

func (rf *Raft) ReadSnapshot() []byte {
	return rf.persister.ReadSnapshot()
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool

	ConflictIndex int
	ConflictTerm  int
}

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

type InstallSnapshotReply struct {
	Term int
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	if args.Term < rf.currentTerm {
		return
	}

	if args.Term > rf.currentTerm {
		rf.stepDown(args.Term)
	}

	rf.resetElectionTimer()

	if args.Term == rf.currentTerm {
		lastLogIndex := rf.getLastLogIndex()
		lastLogTerm := rf.getLogEntry(lastLogIndex).Term

		if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
			if args.LastLogTerm > lastLogTerm ||
				args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex {
				rf.votedFor = args.CandidateId
				reply.VoteGranted = true
				atomic.StoreInt32(&rf.state, Follower)
			}
		}
	}
	reply.Term = rf.currentTerm
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	reply.Term = rf.currentTerm
	reply.Success = false

	if args.Term < rf.currentTerm {
		rf.setConflictIndexAndTerm(args, reply)
		return
	}

	rf.resetElectionTimer()

	if args.Term > rf.currentTerm || atomic.LoadInt32(&rf.state) == Candidate {
		rf.stepDown(args.Term)
	}

	if args.PrevLogIndex < rf.lastIncludedIndex {
		//            prevLogIndex(4)    lastIncludedIndex(13)
		//                 ↓                 ↓
		// logs      □ □ □ □ □ □ □ □ □ □ □ □ □ ■ ■ ■ ■ ■ ■ ■ ■
		//                   ↑
		// entries           □ □ □ □ □ □ □ □ □ ■ ■ ■ ■
		//
		// entries 应该从 prevLogIndex + 1 开始追加, 但现在 lastIncludedIndex 之前的 logs 被删去了,
		// 所以只应该把图中 entries 实心部分从 lastIncludedIndex + 1 开始追加

		reply.Success = true

		offset := rf.lastIncludedIndex - args.PrevLogIndex
		if offset >= len(args.Entries) {
			return
		}

		index := rf.lastIncludedIndex + 1
		for i := offset; i < len(args.Entries); i++ {
			if rf.getLogEntry(index).Term != args.Entries[i].Term {
				rindex := rf.relativeIndex(index) - 1 // relative index begins from 1, minus 1 to access rf.logs directly
				if rindex >= 0 && rindex < len(rf.logs) {
					rf.logs = rf.logs[:rindex]
				}
				rf.logs = append(rf.logs, args.Entries[i])
			}
			index++
		}

		return
	}

	if rf.getLogEntry(args.PrevLogIndex).Term != args.PrevLogTerm {
		rf.setConflictIndexAndTerm(args, reply)
		return
	}

	reply.Success = true
	index := args.PrevLogIndex + 1
	for _, entry := range args.Entries {
		if rf.getLogEntry(index).Term != entry.Term {
			rindex := rf.relativeIndex(index) - 1 // same as above
			if rindex >= 0 && rindex < len(rf.logs) {
				rf.logs = rf.logs[:rindex]
			}
			rf.logs = append(rf.logs, entry)
		}
		index++
	}

	if rf.commitIndex < args.LeaderCommit {
		rf.commitIndex = min(args.LeaderCommit, rf.getLastLogIndex())
		rf.apply()
	}
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		return
	}

	rf.resetElectionTimer()

	if args.Term > rf.currentTerm {
		rf.stepDown(args.Term)
	}

	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		return
	}

	if rf.getLogEntry(args.LastIncludedIndex).Term == args.LastIncludedTerm {
		// If existing log entry has same index and term as snapshot's last included entry,
		// retain log entries following it
		index := rf.relativeIndex(args.LastIncludedIndex)
		rf.logs = rf.logs[index-1:] // index starts from 1
	} else {
		// Discard the entire log
		rf.logs = []LogEntry{{Term: args.LastIncludedTerm, Command: nil}}
	}

	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastApplied = max(rf.lastApplied, rf.lastIncludedIndex)
	rf.commitIndex = max(rf.commitIndex, rf.lastIncludedIndex)

	rf.persistStateAndSnapshot(args.Data)

	if rf.lastApplied > rf.lastIncludedIndex {
		// 如果lastApplied 大于 lastIncludedIndex, 那么 KVServer 端的 DB 状态可能会
		// 比 snapshot 更新, 此时再用 snapshot 去覆盖就会造成 linearizability 测试失败.
		return
	}

	// 如果把这段代码放在 goroutine 里面就会因为乱序而无法通过 linearizability 测试
	rf.applyCh <- ApplyMsg{
		CommandValid: false,
		Command:      args.Data,
		CommandIndex: -1,
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the peer server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	index := 0
	term := rf.currentTerm
	isLeader := false

	// Your code here (2B).

	if atomic.LoadInt32(&rf.state) == Leader {
		isLeader = true
		index = rf.getLastLogIndex() + 1

		rf.logs = append(rf.logs, LogEntry{
			Term:    term,
			Command: command,
		})
		rf.matchIndex[rf.me] = rf.getLastLogIndex()

		// Lab3B TestSnapshotSize 所需的性能优化, 将复制日志的操作提前,
		// 否则对于每一个请求, client 至少要等待一个 heartbeat 时长.
		rf.broadcastHeartbeats()
	}

	return index, term, isLeader
}

func (rf *Raft) TakeSnapshot(lastIncludedIndex int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if atomic.LoadInt32(&rf.state) != Leader {
		return
	}

	index := rf.relativeIndex(lastIncludedIndex)
	if index <= 1 {
		return
	}

	rf.logs = rf.logs[index-1:] // index starts from 1
	rf.lastIncludedIndex = lastIncludedIndex

	rf.persistStateAndSnapshot(snapshot)
	rf.sendInstallSnapshotRPCs()
}

//
// the tester calls Kill() when a Raft instance won't
// b needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg, gid ...int) *Raft {
	rf := &Raft{
		peers:             peers,
		persister:         persister,
		me:                me,
		dead:              0,
		timeoutMsgCh:      make(chan struct{}),
		voteCh:            make(chan struct{}),
		applyCh:           applyCh,
		state:             Follower,
		grantedVotes:      0,
		currentTerm:       0,
		votedFor:          -1,
		commitIndex:       0,
		lastApplied:       0,
		nextIndex:         make([]int, len(peers)),
		matchIndex:        make([]int, len(peers)),
		lastIncludedIndex: 0,
	}

	if len(gid) == 0 {
		rf.gid = -1
	} else {
		rf.gid = gid[0]
	}

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// lastIncludedIndex 是导致 raft state size 刚刚超过阈值而触发 KVServer 进行
	// snapshot 的那条日志, 它已经被 apply 到 KVServer 了, 是最近一次 snapshot 时
	// 被 apply 到 KVServer 的日志, 所以重启时应该把 lastApplied 恢复为 lastIncludedIndex.
	// 另外, apply 的范围是 (lastApplied,commitIndex], commitIndex不应该恢复到一个
	// 大于 lastApplied 的值, 否则就会有日志被 apply —— 对于刚启动的节点这是不合理的.
	rf.lastApplied = rf.lastIncludedIndex
	rf.commitIndex = rf.lastIncludedIndex

	for srv := range rf.peers {
		if srv != rf.me {
			rf.nextIndex[srv] = rf.getLastLogIndex() + 1
			rf.matchIndex[srv] = rf.lastIncludedIndex
		}
	}

	go func() {
		for !rf.killed() {
			state := atomic.LoadInt32(&rf.state)
			switch state {
			case Follower:
				select {
				// 当select所有case里的channel都不可读时, time.After(d)计时开始,
				// 超时后当前时间对象被写入time.After(d)的channel, 阻塞被解除.
				case <-time.After(electionTimeout()):
					atomic.StoreInt32(&rf.state, Candidate)
					rf.startElection()
				case <-rf.timeoutMsgCh:
				}
			case Candidate:
				select {
				case <-time.After(electionTimeout()):
					rf.startElection()
				case <-rf.voteCh:
					votes := int(atomic.LoadInt32(&rf.grantedVotes))
					if votes > len(rf.peers)/2 {
						rf.becomeLeader(votes)
					}
				}
			case Leader:
				rf.mu.Lock()
				rf.broadcastHeartbeats()
				// rf.advanceCommitIndex()
				rf.mu.Unlock()
				time.Sleep(HeartbeatTimeout * time.Millisecond)
			}
		}
	}()

	// 两种设计
	// 1. 周期性地检查"commitIndex > lastApplied"
	// 2. 每次更新commitIndex时再进行检查
	// go func() {
	// 	for !rf.killed() {
	// 		rf.apply()
	// 		time.Sleep(100 * time.Millisecond)
	// 	}
	// }()

	return rf
}

func (rf *Raft) sendRequestVoteRPCs() {
	args := RequestVoteArgs{}
	args.Term = rf.currentTerm
	args.CandidateId = rf.me
	args.LastLogIndex = rf.getLastLogIndex()
	args.LastLogTerm = rf.getLogEntry(args.LastLogIndex).Term

	for srv := range rf.peers {
		if srv == rf.me {
			continue
		}
		if atomic.LoadInt32(&rf.state) != Candidate {
			return
		}

		go func(srv int) {
			var reply RequestVoteReply
			if rf.sendRequestVote(srv, &args, &reply) {
				rf.mu.Lock()
				if reply.Term > rf.currentTerm {
					rf.stepDown(reply.Term)
					rf.mu.Unlock()
					return
				}
				if atomic.LoadInt32(&rf.state) != Candidate ||
					rf.currentTerm != args.Term { // term comfusion
					rf.mu.Unlock()
					return
				}
				if reply.VoteGranted {
					rf.mu.Unlock()
					atomic.AddInt32(&rf.grantedVotes, 1)
					rf.voteCh <- struct{}{}
					return
				}
				rf.mu.Unlock()
			}
		}(srv)
	}
}

func (rf *Raft) broadcastHeartbeats() {
	if atomic.LoadInt32(&rf.state) != Leader {
		return
	}
	rf.persist()

	args1 := InstallSnapshotArgs{}
	args1.Term = rf.currentTerm
	args1.LeaderId = rf.me
	args1.LastIncludedIndex = rf.lastIncludedIndex
	args1.LastIncludedTerm = rf.getLogEntry(rf.lastIncludedIndex).Term
	args1.Data = rf.persister.ReadSnapshot()

	args2 := AppendEntriesArgs{}
	args2.Term = rf.currentTerm
	args2.LeaderId = rf.me
	args2.LeaderCommit = rf.commitIndex

	for srv := range rf.peers {
		if srv == rf.me {
			continue
		}
		if atomic.LoadInt32(&rf.state) != Leader {
			return
		}
		if rf.nextIndex[srv] <= rf.lastIncludedIndex {
			go rf.syncWithSnapshot(srv, args1)
			continue
		}

		args2.PrevLogIndex = rf.nextIndex[srv] - 1
		args2.PrevLogTerm = rf.getLogEntry(args2.PrevLogIndex).Term

		// if last log index >= nextIndex for a follower:
		// send AppendEntries RPC with log entries starting at nextIndex
		args2.Entries = []LogEntry{}
		lastLogIndex := rf.getLastLogIndex()
		if lastLogIndex >= rf.nextIndex[srv] {
			args2.Entries = rf.retrieveLogEntries(rf.nextIndex[srv], len(rf.logs))
		}

		go rf.syncWithLogs(srv, args2)
	}
}

func (rf *Raft) sendInstallSnapshotRPCs() {
	args := InstallSnapshotArgs{}
	args.Term = rf.currentTerm
	args.LeaderId = rf.me
	args.LastIncludedIndex = rf.lastIncludedIndex
	args.LastIncludedTerm = rf.getLogEntry(rf.lastIncludedIndex).Term
	args.Data = rf.persister.ReadSnapshot()

	for srv := range rf.peers {
		if srv == rf.me {
			continue
		}
		if atomic.LoadInt32(&rf.state) != Leader {
			return
		}
		go rf.syncWithSnapshot(srv, args)
	}
}

// REQUIRES: rf.state == Leader
func (rf *Raft) syncWithSnapshot(srv int, args InstallSnapshotArgs) {
	var reply InstallSnapshotReply
	if rf.sendInstallSnapshot(srv, &args, &reply) {
		rf.mu.Lock()
		defer rf.mu.Unlock()

		if reply.Term > rf.currentTerm {
			rf.stepDown(reply.Term)
			return
		}
		if atomic.LoadInt32(&rf.state) != Leader ||
			rf.currentTerm != args.Term { // term comfusion
			return
		}

		rf.matchIndex[srv] = max(rf.matchIndex[srv], args.LastIncludedIndex)
		rf.nextIndex[srv] = rf.matchIndex[srv] + 1
	}
}

// REQUIRES: rf.state == Leader
func (rf *Raft) syncWithLogs(srv int, args AppendEntriesArgs) {
	var reply AppendEntriesReply
	if rf.sendAppendEntries(srv, &args, &reply) {
		rf.mu.Lock()
		defer rf.mu.Unlock()

		if reply.Term > rf.currentTerm {
			rf.stepDown(reply.Term)
			return
		}
		if atomic.LoadInt32(&rf.state) != Leader ||
			rf.currentTerm != args.Term { // term comfusion
			return
		}

		if reply.Success {
			// Term confusion
			// -------------------------
			// A related, but not identical problem is that of assuming that your
			// state has not changed between when you sent the RPC, and when you
			// received the reply. A good example of this is setting matchIndex = nextIndex - 1,
			// or matchIndex = len(log) when you receive a response to an RPC.
			// This is not safe, because both of those values could have been updated
			// since when you sent the RPC. Instead, the correct thing to do is update
			// matchIndex to be prevLogIndex + len(entries[]) from the arguments you
			// sent in the RPC originally.
			rf.matchIndex[srv] = args.PrevLogIndex + len(args.Entries)
			rf.nextIndex[srv] = rf.matchIndex[srv] + 1

			// 为了优化 Lab3B-TestSnapshotSize, 把 commitIndex 的更新放到每个 AE RPC 成功返回
			// 后进行. 每个 RPC 的处理都是在一个单独的线程里进行, 在 rf.Make() 里面调用 broadcastHeartbeats()
			// 前加锁, 返回后解锁并进入心跳等待, 在主线程挂起的期间内这些 AE RPC 并发执行, 同时
			// 更新 commitIndex 触发 apply, 所以如果不在 rf.Start() 里提前广播心跳, 每个请求可以
			// 在一个心跳周期内达成一致并 apply 到上层应用.
			// ^
			// 若按照之前的设计, broadcastHeartbeats() 返回后再 advanceCommitIndex(), 那么 commitIndex
			// 的更新被放到了主线程里进行, 主线程尚未释放锁, 那些处理 AE RPC 的线程也就不能获得锁,
			// 也就不能更新 matchIndex 来触发 commitIndex 的更新, 所以在第一个心跳周期内 commitIndex
			// 是得不到更新的, 即: 这个无效的 advanceCommitIndex() 返回后主线程释放锁并进入心跳
			// 等待的挂起阶段后, AE RPC 并发执行, 虽然它们更新了 matchIndex 却并不更新 commitIndex,
			// 只有在第二个心跳周期内 advanceCommitIndex() 才能更新 commitIndex 触发 apply.
			rf.advanceCommitIndex()
		} else {
			rf.nextIndex[srv] = max(rf.nextIndex[srv]-1, 1)
			rf.conflictCheck(srv, reply.ConflictIndex, reply.ConflictTerm)

			if rf.nextIndex[srv] < 1 {
				log.Fatalf("rf.nextIndex[%v]=%v", srv, rf.nextIndex[srv])
			}
		}
	}
}

func (rf *Raft) relativeIndex(absoluteIndex int) int {
	if rf.lastIncludedIndex == 0 {
		return absoluteIndex
	}

	rindex := absoluteIndex - rf.lastIncludedIndex + 1 // starting from 1
	return rindex
}

func (rf *Raft) getLogEntry(index int) LogEntry {
	index = rf.relativeIndex(index)
	// if index < 0 {
	// 	DPrintf("lastincluded=%v, index=%v", rf.lastIncludedIndex, index)
	// 	debug.PrintStack()
	// }
	if index > 0 && index <= len(rf.logs) {
		return rf.logs[index-1]
	}
	return LogEntry{Term: 0, Command: nil}
}

func (rf *Raft) getLastLogIndex() int {
	if rf.lastIncludedIndex == 0 {
		return len(rf.logs)
	}
	return rf.lastIncludedIndex + len(rf.logs) - 1
}

func (rf *Raft) retrieveLogEntries(index, n int) []LogEntry {
	index = rf.relativeIndex(index)
	if index > 0 && index <= len(rf.logs) {
		end := min(index+n-1, len(rf.logs))
		src := rf.logs[index-1 : end]
		dst := make([]LogEntry, len(src))
		copy(dst, src) // we must make a deep copy
		return dst
	}
	return []LogEntry{}
}

func (rf *Raft) startElection() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if atomic.LoadInt32(&rf.state) != Candidate {
		return
	}

	rf.currentTerm++
	rf.votedFor = rf.me
	atomic.StoreInt32(&rf.grantedVotes, 1)

	rf.persist()
	rf.resetElectionTimer()
	rf.sendRequestVoteRPCs()
}

func (rf *Raft) stepDown(term int) {
	atomic.StoreInt32(&rf.state, Follower)
	atomic.StoreInt32(&rf.grantedVotes, 0)
	rf.currentTerm = term
	rf.votedFor = -1
	rf.persist()
}

func (rf *Raft) becomeLeader(votes int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	atomic.StoreInt32(&rf.state, Leader)
	for srv := range rf.peers {
		rf.nextIndex[srv] = rf.getLastLogIndex() + 1
	}
}

func (rf *Raft) advanceCommitIndex() {
	if atomic.LoadInt32(&rf.state) != Leader {
		return
	}

	// if there exists an N such that N > commitIndex, a majority
	// of matcheIndex[i] >= N, and log[N].term == currentTerm:
	// set commitIndex = N

	// 直接遍历

	/*for n := rf.getLastLogIndex(); n > rf.commitIndex; n-- {
		count := 0
		for i := range rf.matchIndex {
			if rf.matchIndex[i] >= n {
				count++
			}
		}

		if count > len(rf.peers)/2 &&
			rf.getLogEntry(n).Term == rf.currentTerm {
			rf.commitIndex = n
			break
		}
	}*/

	// 至少要有 (n/2)+1 个节点的 matchIndex 大于等于 commitIndex, 即,
	// 只有当大多数节点成功复制了Leader的日志, Leader的commitIndex才能
	// 向前推进. 所以如果 matches[n/2] > commitIndex, 那么matches[n/2]
	// 后面的值一定都大于commitIndex, 从而保证了"大多数"

	matches := make([]int, len(rf.matchIndex))
	copy(matches, rf.matchIndex)
	sort.Ints(matches)
	N := matches[len(matches)/2]
	if N >= rf.lastIncludedIndex &&
		rf.getLogEntry(N).Term == rf.currentTerm {
		rf.commitIndex = max(rf.commitIndex, N)
		rf.apply()
	}
}

func (rf *Raft) resetElectionTimer() {
	go func() {
		rf.timeoutMsgCh <- struct{}{}
	}()
}

func (rf *Raft) apply() {
	// if lastApplied < commitIndex: increment lastApplied, apply
	// log[lastApplied] to state machine.

	// apply all logs in (lastApplied, commitIndex]
	for rf.lastApplied < rf.commitIndex {
		rf.lastApplied++
		rf.applyCh <- ApplyMsg{
			CommandValid: true,
			Command:      rf.getLogEntry(rf.lastApplied).Command,
			CommandIndex: rf.lastApplied,
		}

		// debug lab4B
		if rf.gid > 0 {
			DPrintf("%v-%v raft apply %v", rf.gid, rf.me, rf.lastApplied)
		}
	}
}

func (rf *Raft) setConflictIndexAndTerm(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	reply.ConflictIndex = 0
	reply.ConflictTerm = 0

	last := rf.getLastLogIndex()
	if last < args.PrevLogIndex {
		// If a follower does not have prevLogIndex in its log, it should return
		// with conflictIndex = len(log) and conflictTerm = None.
		// 注: 此处的"len(log)"的含义是: 最后一条日志的索引+1
		reply.ConflictIndex = last + 1
		return
	}

	// If a follower does have prevLogIndex in its log, but the term does not match,
	// it should return conflictTerm = log[prevLogIndex].Term, and then search its log
	// for the first index whose entry has term equal to conflictTerm.
	term := rf.getLogEntry(args.PrevLogIndex).Term
	if term != args.PrevLogTerm {
		reply.ConflictTerm = term
		for i := rf.lastIncludedIndex; i <= last; i++ {
			if rf.getLogEntry(i).Term == term {
				reply.ConflictIndex = i
				return
			}
		}
	}
}

func (rf *Raft) conflictCheck(srv int, conflictIndex int, conflictTerm int) {
	// Upon receiving a conflict response, the leader should first search
	// its log for conflictTerm. If it finds an entry in its log with that
	// term, it should set nextIndex to be the one beyond the index of the
	// last entry in that term in its log.
	last := rf.getLastLogIndex()
	for i := rf.lastIncludedIndex; i <= last; i++ {
		if rf.getLogEntry(i).Term == conflictTerm {
			for i <= last && rf.getLogEntry(i).Term == conflictTerm {
				i++
			}
			rf.nextIndex[srv] = i
			return
		}
	}
	// If it does not find an entry with that term, it should set nextIndex = conflictIndex.
	rf.nextIndex[srv] = conflictIndex
}

func electionTimeout() time.Duration {
	d := duration(ElectionTimeoutLower, ElectionTimeoutUpper)
	return d
}

func duration(lower, upper int64) time.Duration {
	max := big.NewInt(upper - lower)
	rr, _ := crand.Int(crand.Reader, max)
	d := time.Duration(rr.Int64()+lower) * time.Millisecond
	return d
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
