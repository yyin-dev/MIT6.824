package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
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
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
)

const (
	leader    = "Leader"
	follower  = "Follower"
	candidate = "Candidate"

	noVote         = -1
	elecTimeoutMin = 400
	elecTimeoutMax = 600

	heartbeatInterval = 100 * time.Millisecond
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
	CommandTerm  int
}

type logEntry struct {
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

	// 2A: leader election + heartbeat
	state    string
	currTerm int
	votedFor int
	log      []logEntry

	commitIndex int
	lastApplied int

	nextIndex  []int // reinitialized after election
	matchIndex []int // reinitialized after election

	elecTimeout            time.Duration
	prevTimeElecSuppressed time.Time // The prev time when suppressed from starting election: receiving an AppendEntries from CURRENT leader, or granting vote to candidate
	votesReceived          int
	majorityVotes          int

	heartbeatInterval     time.Duration
	prevTimeAppendEntries time.Time // prev time AppendEntries is fired

	applyCondVar *sync.Cond
	applyCh      chan ApplyMsg
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	term := rf.currTerm
	isLeader := rf.state == leader

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

	// Should be called only when holding the lock
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
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
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currTerm int
	var votedFor int
	var log []logEntry
	if d.Decode(&currTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil {
		DPrintf("Cannot read persisted state")
	} else {
		rf.mu.Lock()
		defer rf.mu.Unlock()

		rf.currTerm = currTerm
		rf.votedFor = votedFor
		rf.log = log
	}
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateID  int
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

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term > rf.currTerm {
		rf.currTerm = args.Term
		rf.votedFor = noVote
		rf.state = follower
		rf.persist()
		DPrintf("[%v] reverts to follower when receiving RequestVote call from [%v]", rf.me, args.CandidateID)
	}

	upToDate1 := args.LastLogTerm > rf.log[len(rf.log)-1].Term
	upToDate2 := args.LastLogTerm == rf.log[len(rf.log)-1].Term && args.LastLogIndex >= len(rf.log)-1
	upToDate := upToDate1 || upToDate2
	DPrintf("[%v] receives RequestVote from [%v], votedFor = %v, args.Term = %v, rf.currTerm = %v", rf.me, args.CandidateID, rf.votedFor, args.Term, rf.currTerm)

	reply.Term = rf.currTerm
	reply.VoteGranted = false
	if (rf.votedFor == noVote || rf.votedFor == args.CandidateID) && upToDate {
		reply.VoteGranted = true
		rf.votedFor = args.CandidateID
		rf.persist()

		// granting vote, reset election timer
		rf.prevTimeElecSuppressed = time.Now()
		DPrintf("[%v] votes for [%v]", rf.me, args.CandidateID)
	}
}

//
// Long-running goroutine for periodic election timeout
//
func (rf *Raft) periodicElection() {
	for {
		if rf.killed() {
			return
		}

		rf.mu.Lock()
		timeout := time.Since(rf.prevTimeElecSuppressed) > rf.elecTimeout
		if rf.state != leader && timeout {
			DPrintf("[%v] becomes candidate at, term = %v", rf.me, rf.currTerm+1)
			// Restart another round of election, become candidate
			rf.state = candidate
			rf.currTerm++
			rf.votedFor = rf.me
			rf.persist()
			rf.votesReceived = 1
			rf.prevTimeElecSuppressed = time.Now()
			rf.elecTimeout = genRandomElecTimeout()

			// send RequestVote RPCs to all other servers
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					continue
				}

				// seperate goroutine for each RPC call, non-blocking
				go func(server int, term int, candidateID int, lastLogIndex int, lastLogTerm int) {
					args := RequestVoteArgs{
						Term:         term,
						CandidateID:  candidateID,
						LastLogIndex: lastLogIndex,
						LastLogTerm:  lastLogTerm,
					}
					reply := RequestVoteReply{}
					ok := rf.peers[server].Call("Raft.RequestVote", &args, &reply)

					if !ok {
						DPrintf("[%v] cannot get reply from RequestVote to [%v]", candidateID, server)
						return
					}

					rf.mu.Lock()
					defer rf.mu.Unlock()

					if rf.currTerm < reply.Term {
						rf.currTerm = args.Term
						rf.votedFor = noVote
						rf.state = follower
						rf.persist()
						DPrintf("[%v] reverts to follower when receiving RequestVote reply from [%v]", rf.me, server)
						return
					}
					if rf.state == candidate && reply.VoteGranted {
						DPrintf("[%v] receives vote from [%v]", candidateID, server)
						rf.votesReceived++
						if rf.votesReceived >= rf.majorityVotes && term == rf.currTerm {
							// become leader
							rf.state = leader
							rf.nextIndex = make([]int, len(rf.peers))
							rf.matchIndex = make([]int, len(rf.peers))
							for i := 0; i < len(rf.peers); i++ {
								rf.nextIndex[i] = len(rf.log)
								rf.matchIndex[i] = 0
							}
							DPrintf("[%v] receives majority vote and becomes leader (term = %v)", rf.me, rf.currTerm)

							// immediately send one round of heartbeat
							rf.sendAppendEntriesToPeers()

							// start background routine for periodic heartbeat
							go rf.periodicAppendEntries()
						}
					}
				}(i, rf.currTerm, rf.me, len(rf.log)-1, rf.log[len(rf.log)-1].Term)
			}
		}
		rf.mu.Unlock()

		time.Sleep(100 * time.Millisecond)
	}
}

type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []logEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool

	// Fast rollback
	XTerm  int // term of the conflicting entry (-1 if none)
	XIndex int // index of the first entry with XTerm (-1 if none)
	XLen   int // log length
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	DPrintf("[%v] receives AppendEntries call from [%v]", rf.me, args.LeaderID)

	if args.Term > rf.currTerm {
		rf.currTerm = args.Term
		rf.votedFor = noVote
		rf.state = follower
		rf.persist()
		DPrintf("[%v] reverts to follower when receiving AppendEntries call from [%v]", rf.me, args.LeaderID)
	}

	reply.Term = rf.currTerm
	reply.Success = false

	// obsolete AppendEntries
	if args.Term < rf.currTerm {
		// Fast rollback
		// No need to fill the reply, since leader wouldn't use this info
		return
	}

	// args.Term >= rf.currTerm, so must be current leader
	// Reset election timer
	rf.prevTimeElecSuppressed = time.Now()

	// "If the leader has no new entries to send to a particular peer, the
	// AppendEntries RPC contains no entries, and is considered a heartbeat."
	// A heartbeat is just a normal AppendEntries call.

	// log not matching
	if len(rf.log) <= args.PrevLogIndex || rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		// Fast rollback
		if len(rf.log) <= args.PrevLogIndex {
			// len(rf.log) <= args.PrevLogIndex ==> Leader's log is too short
			//    0 1 2 3
			// S1 4
			// S2 4 6 6 6, prevLogIndex = 1.
			// Only XLen matters. Set XTerm = XIndex = -1.
			reply.XTerm = -1
			reply.XIndex = -1
			reply.XLen = len(rf.log)
		} else {
			// rf.log[args.PrevLogIndex].Term != args.PrevLogTerm => leader's log is not too short
			//    0 1 2 3
			// S1 4 5 5
			// S2 4 6 6 6, prevLogIndex = 1/2. => XTerm: 5, XIndex = 1
			//
			// S1 4 4 4
			// S2 4 6 6 6, prevLogIndex = 1/2. => XTerm: 4, XIndex = 0

			// XTerm >= 0, XIndex >= 1
			reply.XTerm = rf.log[args.PrevLogIndex].Term
			for i := args.PrevLogIndex; i >= 1; i-- {
				if rf.log[i].Term == rf.log[args.PrevLogIndex].Term {
					reply.XIndex = i
				} else {
					break
				}
			}
			reply.XLen = len(rf.log)
		}
		return
	}

	// matching up to prevLogIndex
	DPrintf("[%v] original log: %v", rf.me, rf.log)
	DPrintf("[%v] received prevLogIndex = %v, entries = %v", rf.me, args.PrevLogIndex, args.Entries)
	// 1. If an existing entry conflicts with a new one, delete the existing
	// entry and all that follow it
	i := 0
	for ; i < len(args.Entries); i++ {
		currIndex := args.PrevLogIndex + 1 + i

		// no conflict, but runs out of log
		if currIndex >= len(rf.log) {
			break
		}

		// a conflict
		if rf.log[currIndex].Term != args.Entries[i].Term {
			rf.log = rf.log[:currIndex]
			break
		}
	}

	// 2. Append any new entries not already in the log
	for ; i < len(args.Entries); i++ {
		rf.log = append(rf.log, args.Entries[i])
	}
	DPrintf("[%v] updated log: %v", rf.me, rf.log)
	rf.persist()

	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, len(rf.log)-1)
		rf.applyCondVar.Broadcast()
		DPrintf("[%v] updates commitIndex to %v", rf.me, rf.commitIndex)
	}
	reply.Success = true
}

func (rf *Raft) periodicAppendEntries() {
	for {
		if rf.killed() {
			return
		}

		rf.mu.Lock()

		if rf.state != leader {
			// Release the lock before return! Otherwise you get deadlock
			// cannot be detected by Go's race detector. Since other servers
			// are still functioning.
			rf.mu.Unlock()
			return
		}

		// still leader
		if time.Since(rf.prevTimeAppendEntries) > rf.heartbeatInterval {
			rf.sendAppendEntriesToPeers()
		}
		rf.mu.Unlock()

		time.Sleep(50 * time.Millisecond)
	}
}

//
// Send AppendEntries to all other servers.
// The caller of this function should hold rf.mu when calling.
//
func (rf *Raft) sendAppendEntriesToPeers() {
	DPrintf("[%v] calls sendAppendEntriesToPeers", rf.me)
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}

		prevLogIndex := rf.matchIndex[i]
		prevLogTerm := rf.log[prevLogIndex].Term
		entries := rf.log[prevLogIndex+1:]
		go func(server int, term int, leaderID int, prevLogIndex int, prevLogTerm int, entries []logEntry, leaderCommit int) {
			args := AppendEntriesArgs{
				Term:         term,
				LeaderID:     leaderID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: leaderCommit,
			}
			reply := AppendEntriesReply{}

			DPrintf("[%v] tries to send AppendEntries to [%v]", leaderID, server)
			ok := rf.peers[server].Call("Raft.AppendEntries", &args, &reply)
			if !ok {
				DPrintf("[%v] AppendEntries call to [%v] fails to get response", leaderID, server)
				return
			}

			rf.mu.Lock()
			defer rf.mu.Unlock()
			if reply.Term > rf.currTerm {
				rf.currTerm = args.Term
				rf.votedFor = noVote
				rf.state = follower
				rf.persist()
				DPrintf("[%v] reverts to follower after receving reply from AppendEntries", rf.me)
			}

			if term != rf.currTerm || rf.state != leader {
				// term confusion (student's guide). Drop reply and return
				return
			}

			DPrintf("[%v] AppendEntries reply from [%v] is %v. prevLogIndex = %v. Entries = %v", leaderID, server, reply.Success, prevLogIndex, entries)
			if reply.Success {
				rf.nextIndex[server] = prevLogIndex + len(args.Entries) + 1
				rf.matchIndex[server] = prevLogIndex + len(args.Entries)

				// Check for commited entry
				for N := len(rf.log) - 1; N > rf.commitIndex; N-- {
					replicatedCount := 1
					for i := 0; i < len(rf.peers); i++ {
						if i == rf.me {
							continue
						}

						if rf.matchIndex[i] >= N {
							replicatedCount++
						}
					}
					if replicatedCount >= len(rf.peers)/2+1 && rf.log[N].Term == rf.currTerm {
						rf.commitIndex = N
						DPrintf("[%v] updates commitIndex to %v", rf.me, rf.commitIndex)
						rf.applyCondVar.Broadcast()
					}
				}
			} else {
				// Reasons for false reply:
				// Case 1. term < follower's term
				// Case 2. log mismatch
				// If case 1 is true, then we would exit already. So here, the only
				// reason for negative reply is log inconsistency.

				// slow rollback
				// rf.nextIndex[server]--

				// fast rollback
				if reply.XTerm == -1 && reply.XIndex == -1 {
					// case 3
					rf.nextIndex[server] = reply.XLen
				} else {
					foundIndex := -1
					for i := len(rf.log) - 1; i >= 1; i-- {
						if rf.log[i].Term == reply.XTerm {
							foundIndex = i
							break
						} else if rf.log[i].Term < reply.XTerm {
							break
						}
					}
					if foundIndex == -1 {
						// case 1
						rf.nextIndex[server] = reply.XIndex
					} else {
						// case 2
						rf.nextIndex[server] = foundIndex
					}
				}
			}
		}(i, rf.currTerm, rf.me, prevLogIndex, prevLogTerm, entries, rf.commitIndex)
	}
	rf.prevTimeAppendEntries = time.Now()
}

func (rf *Raft) applyCommitted() {
	for {
		if rf.killed() {
			return
		}

		rf.mu.Lock()

		for rf.lastApplied >= rf.commitIndex {
			rf.applyCondVar.Wait()
		}

		// rf.lastApplied < rf.commitIndex
		rf.lastApplied++
		logEntry := rf.log[rf.lastApplied]
		msg := ApplyMsg{
			CommandValid: true,
			Command:      logEntry.Command,
			CommandIndex: rf.lastApplied,
			CommandTerm:  logEntry.Term,
		}
		DPrintf("[%v] updates lastApplied to %v, sending on applyCh", rf.me, rf.lastApplied)
		rf.mu.Unlock()

		rf.applyCh <- msg
	}
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
	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// prepare return value
	index := len(rf.log)
	term := rf.currTerm
	isLeader := rf.state == leader

	if isLeader {
		// Add to leader's log
		entry := logEntry{
			Term:    rf.currTerm,
			Command: command,
		}
		rf.log = append(rf.log, entry)
		rf.persist()
		DPrintf("[%v] receives from server %v, current log: %v", rf.me, command, rf.log)
	}

	return index, term, isLeader
}

func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		peers:     peers,
		persister: persister,
		me:        me,

		state:    follower,
		currTerm: 0,
		votedFor: noVote,
		log:      []logEntry{logEntry{}}, // one dummy entry to avoid edge case in RequestVote

		commitIndex: 0,
		lastApplied: 0,

		elecTimeout: genRandomElecTimeout(),
		// prevTimeElecSuppressed would have zero value

		votesReceived:     0,
		majorityVotes:     len(peers)/2 + 1,
		heartbeatInterval: heartbeatInterval,
		// prevTimeAppendEntries would have zero value

		// applyCondVar would be initialized later
		applyCh: applyCh,
	}
	rf.applyCondVar = sync.NewCond(&rf.mu)

	// goroutine for election timeout
	go rf.periodicElection()

	// goroutine for apply commited entry
	go rf.applyCommitted()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	return rf
}

func genRandomElecTimeout() time.Duration {
	return time.Duration(IntRange(elecTimeoutMin, elecTimeoutMax)) * time.Millisecond
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}
