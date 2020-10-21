## Lab2B

The student guide points out common bugs. Very helpful. In particular, following points helped my development:

- Reset election timer

> If election timeout elapses without receiving `AppendEntries` RPC *from current leader* or *granting* vote to candidate: convert to candidate.

- From the guide: "A leader will occasionally (at least once per heartbeat interval) send out an `AppendEntries` RPC to all peers to prevent them from starting a new election. If the leader has no new entries to send to a particular peer, the `AppendEntries` RPC contains no entries, and is considered a heartbeat. 

    So heartbeat is not special! It's just an `AppendEntries` RPC call. The leader should send out `AppendEntries` to all peers at least once per heartbeat interval. For each peer, if the peer's log is not identical with the leader's, then `args.Entries` would be non-empty. Otherwise, `args.Entries` would be empty and the `AppendEntries` is called a "hearbeat". Thus, no separate routine is needed for heartbeat and it's implicitly handled in the routine that sends `AppendEntries` to sync the log of leaders and its peers.

    - Mistake 1: returning success on receiving heartbeats, without doing any checking. 

    - Mistake 2: Upon receiving heartbeat, truncate the follower's log following `prevLogIndex` and append any entries included in the `AppendEntries` argument. This is wrong. From Figure 2:

    > *If* an existing entry conflicts with a new one (same index but different terms), delete the existing entry and all that follow it.

    The *if* here is crucial. If the follower has all entries the leader sent, it **MUST NOT** truncate the log. Any elements *following* `args.Entries` **MUST** be kept. This is because we could receive outdated `AppendEntries` from the leader. 

- You should restart election timer *only* if a) get an `AppendEntries` from the *current* leader (check `term` argument); b) you're starting an election; c) you *grant* a vote to another peer.

- Before handling incoming RPC:

    > If RPC request or response contains term `T > currentTerm`: set `currentTerm = T`, convert to follower (§5.1)

    For example, if you have already voted in the current term, and an incoming `RequestVote` PRC has higher term, you should *first* step down and adopt the term (and resetting `votedFor`) and *then* handles the RPC, which means granting the vote!

- If a step says “reply false”, this means you should *reply immediately*, and not perform any of the subsequent steps.

- One common source of confusion is the difference between `nextIndex` and `matchIndex`. In particular, you may observe that `matchIndex = nextIndex - 1`, and simply not implement `matchIndex`. This is not safe. While `nextIndex` and `matchIndex` are generally updated at the same time to a similar value (specifically, `nextIndex = matchIndex + 1`), the two serve quite different purposes. `nextIndex` is a *guess* as to what prefix the leader shares with a given follower. It is generally quite optimistic (we share everything), and is moved backwards only on negative responses. For example, when a leader has just been elected, `nextIndex` is set to be index index at the end of the log. In a way, `nextIndex` is used for performance – you only need to send these things to this peer.

    `matchIndex` is used for safety. It is a con­ser­v­a­tive *mea­sure­ment* of what pre­fix of the log the leader shares with a given fol­lower. `matchIndex` can­not ever be set to a value that is too high, as this may cause the `commitIndex` to be moved too far for­ward. This is why `matchIndex` is ini­tial­ized to -1 (i.e., we agree on no pre­fix), and only up­dated when a fol­lower *pos­i­tively ac­knowl­edges* an `AppendEntries` RPC.

- Term confusion refers to servers getting confused by RPCs that come from old terms. In general, this is not a problem when receiving an RPC, since the rules in Figure 2 say exactly what you should do when you see an old term. However, Figure 2 generally doesn’t discuss what you should do when you get old RPC *replies*. From experience, we have found that by far the simplest thing to do is to first record the term in the reply (it may be higher than your current term), and then to compare the current term with the term you sent in your original RPC. If the two are different, drop the reply and return. *Only* if the two terms are the same should you continue processing the reply. There may be further optimizations you can do here with some clever protocol reasoning, but this approach seems to work well. And *not* doing it leads down a long, winding path of blood, sweat, tears and despair.

- A related, but not identical problem is that of assuming that your state has not changed between when you sent the RPC, and when you received the reply. A good example of this is setting `matchIndex = nextIndex - 1`, or `matchIndex = len(log)` when you receive a response to an RPC. This is *not* safe, because both of those values could have been updated since when you sent the RPC. Instead, the correct thing to do is update `matchIndex` to be `prevLogIndex + len(entries[])` from the arguments you sent in the RPC originally.