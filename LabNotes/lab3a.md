# Lab3A: Key/value service without log compaction

Note: this note is written before I refactored Lab3. For most up-to-date note, refer to `./lab3.md`.

## Client
The client implementation is straight-forward. Use `prevLeader` to make it a bit more efficient.

## Server
- Structure overview

    A go-routine `applyCommitted` continuously reads from `kv.applyCh` for commited commands, and apply them with the help of a duplicate detection scheme.

    When the server's RPC handler handles a request, it checks for obselete request, checks for leadership, submits the request to Raft, and waits for the command to be committed. When the command is applied (by the go-routine above), the handler should be notified about this and reply to client. In other words, information needs to be passed between the handler and `applyCommitted`. 

- Time-out 

    A time-out scheme needs to be implemented on the server-side, which can be easily done using 
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

    In RPC handler, if we receieve a request such that `args.SeqNum < kv.clientSeqMap[args.Cid]`, we know this must be an obselete request and can be skipped. In `applyCommitted`, we only apply commands that satisfy `op.Seq > kv.clientSeqMap[op.Cid]`.

- Passing the information

    After a command is applied in `applyCommitted`, any waiting RPC handler needs to be notified. This is done using `kv.applyCond`. 
    ```go
    func (kv *KVServer) applyCommitted() {
        for msg := range kv.applyCh {
            kv.mu.Lock()
            op := msg.Command.(Op)
            if op.Seq > kv.clientSeqMap[op.Cid] {
                apply command ...

                kv.clientSeqMap[op.Cid] = op.Seq
                if _, exists := kv.applyResMap[op.Cid]; !exists {
                    kv.applyResMap[op.Cid] = make(map[int]bool)
                }
                kv.applyResMap[op.Cid][op.Seq] = true
                kv.applyCond.Broadcast()
            }
            kv.mu.Unlock()
        }
    }
    ```

    We use a `map[int64](map[int]bool)` to store applied commands. Notice that we just need a `(clientID, seqNum)` pair to identify a command. 
    
    One alternative implementation is to use a `map[int64](map[int]string)`, where the result of applying a command is stored into the map by `applyCommited`, and later read by RPC handlers from the map. However, this is not necessary for 2 reaons. Firstly, for `PutAppend`, the handler just needs to know whether a command is applied or not. Secondly, for `Get`, if the handler knows the command is committed, it can directly read from `kv.store` - if the command is the latest, this is correct; if it's not the latest command, the reply information won't be used. 

- Linearizability

    To ensure linearizability, only the leader should call `Start`. However, when the command is committed, any server can reply to client.

- Checking the Term?

    In my initial implementation of my RPC handlers, I record the `startTerm` when handing the command to Raft, and record the `applyTerm` when the command is applied. And only reply `OK` to client if `startTerm == applyTerm`. 

    This is unnecessary but even WRONG! Consider the following case: 5 servers {1, 2, 3, 4, 5} are in two partitions {1, 2, 3} and {4, 5}, with 1 being the leader in the 1st partition. A command C is passed to 1 at term t1. Now the partition heals, 1 becomes candidate but is re-elected as leader for a larger term t2, and C is passed to 1 again. Now, 1's log contains C(t1) and C(t2), and RPC handlers are waiting for C to be applied in t2. Suppose everything goes well and both entries get committed. When C is applied in `applyCommitted`, the server updates `kv.clientSeqMap` and notifies RPC handlers that it's committed in t1. When C(t2) is received on `applyCh`, it's considered duplicate and discarded. In this way, RPC handlers wait forever for C to be committed in t2. The system is in livelock (program running but making no useful progress). This fails test `partitions, one client (3A)`.

    Remove this term-check and the code passes all tests. We shouldn't check the term, as `clientID-seqNum` should be used to identify a command. 


Final result:
```
$ go test -run 3A
Test: one client (3A) ...
  ... Passed --  15.4  5  3456  148
Test: many clients (3A) ...
  ... Passed --  16.6  5  5308  745
Test: unreliable net, many clients (3A) ...
  ... Passed --  17.3  5  2520  618
Test: concurrent append to same key, unreliable (3A) ...
  ... Passed --   2.3  3   179   52
Test: progress in majority (3A) ...
  ... Passed --   0.7  5    65    2
Test: no progress in minority (3A) ...
  ... Passed --   1.2  5   121    3
Test: completion after heal (3A) ...
  ... Passed --   1.0  5    59    3
Test: partitions, one client (3A) ...
  ... Passed --  22.7  5 14755  119
Test: partitions, many clients (3A) ...
  ... Passed --  23.9  5 54292  680
Test: restarts, one client (3A) ...
  ... Passed --  19.6  5  9396  148
Test: restarts, many clients (3A) ...
  ... Passed --  21.2  5 30929  745
Test: unreliable net, restarts, many clients (3A) ...
  ... Passed --  22.1  5  3627  617
Test: restarts, partitions, many clients (3A) ...
  ... Passed --  28.6  5 56681  630
Test: unreliable net, restarts, partitions, many clients (3A) ...
  ... Passed --  28.2  5  3964  422
Test: unreliable net, restarts, partitions, many clients, linearizability checks (3A) ...
  ... Passed --  25.8  7 12184 1011
PASS
ok      _/mnt/c/Users/yy0125/Desktop/MIT6.824/src/kvraft        247.239s
```

After adding `rf.sendAppendEntriesToPeers` in `Start()`, the code still passes the tests and is slightly faster:

```
$ go test -run 3A
Test: one client (3A) ...
  ... Passed --  15.1  5 15199 2377
Test: many clients (3A) ...
  ... Passed --  15.2  5 20688 2650
Test: unreliable net, many clients (3A) ...
  ... Passed --  15.8  5 10530 1553
Test: concurrent append to same key, unreliable (3A) ...
  ... Passed --   1.0  3   251   52
Test: progress in majority (3A) ...
  ... Passed --   2.7  5   284    2
Test: no progress in minority (3A) ...
  ... Passed --   1.0  5   127    3
Test: completion after heal (3A) ...
  ... Passed --   1.0  5    61    3
Test: partitions, one client (3A) ...
  ... Passed --  22.4  5 19634 2268
Test: partitions, many clients (3A) ...
  ... Passed --  23.2  5 35520 2617
Test: restarts, one client (3A) ...
  ... Passed --  20.0  5 26168 2678
Test: restarts, many clients (3A) ...
  ... Passed --  20.1  5 50980 2697
Test: unreliable net, restarts, many clients (3A) ...
  ... Passed --  21.8  5 11623 1437
Test: restarts, partitions, many clients (3A) ...
  ... Passed --  27.1  5 94461 2509
Test: unreliable net, restarts, partitions, many clients (3A) ...
  ... Passed --  30.0  5  9885 1004
Test: unreliable net, restarts, partitions, many clients, linearizability checks (3A) ...
  ... Passed --  25.3  7 24323 1511
PASS
ok      _/mnt/c/Users/yy0125/Desktop/MIT6.824/src/kvraft        242.368s
```

