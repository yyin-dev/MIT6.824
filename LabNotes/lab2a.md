## Lab2A

Sometimes deadlock cannot be detected by `-race`, since it only checks if **all** goroutines are stuck. 

```go
func (rf *Raft) periodicAppendEntries() {
	for {
		if rf.killed() {
			return
		}

		rf.mu.Lock()
		DPrintf("[%v] acquires the lock in periodicAppendEntries", rf.me)

		if rf.state != leader {
			// Release the lock before return! Otherwise you get deadlock
			// cannot be detected by Go's race detector. Since other servers
			// are still functioning.
             rf.mu.Unlock()
			return
		}

		// still leader
		DPrintf("[%v] is still the leader", rf.me)
		if time.Since(rf.prevTimeAppendEntries) > rf.heartbeatInterval {
			DPrintf("[%v] about to call sendAppendEntriesToPeers", rf.me)
			rf.sendAppendEntriesToPeers()
			DPrintf("[%v] finishes sendAppendEntriesToPeers", rf.me)
		}
		DPrintf("[%v] releases the lock in periodicAppendEntries", rf.me)
		rf.mu.Unlock()

		time.Sleep(50 * time.Millisecond)
	}
}
```

It's crucial to release the lock at line 14. Otherwise, this server would hangs, while other servers are still running. No deadlock would be reported by Go's race detector in this case.

