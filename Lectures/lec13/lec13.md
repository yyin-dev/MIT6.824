# Lec13 Spanner

Why reading the paper: globally-distributed, externally consistency (linearizability), 2PC w/ Paxos, synchronized time.

- Sharding and Paxos group

    ```
    DS1          DS2           DS3
    A-shard1    A-shard2    A-shard3  <-- Paxos Group A
    B-shard1    B-shard2    B-shard3  <-- Paxos Group B
    ...         ...         ...           ...
    ```

    Sharding & Independent Paxos groups: More parallellism => Better performance. 

    The Paxos variant has a leader and is similar to Raft. The client-server interaction is similar to Lab3.

- Challenge: correct read from nearby

    Google wanted read operation to be served by nearby datacenters, which means not through the leader. But as in Raft, the follower might be lagging and serves stale data. But Google also required external consistency (linearizability).

- Challlenge: distributed transaction

    Each transaction can involve multiple shards and multiple Paxos groups.

- Read-write transaction

    Very standard: two-phase locking & two-phase commit, only Paxos-based.

    Note that the client picks one Paxos leader to be the TC.

    Remeber that 2PC is notorious for low-availability and waiting with lock. Spanner mitigates the problem by replicating the participants with Paxos.

    However, the communication is expensive. Many messages are cross-datacenter communication in the wide area. The time to complete one transaction is 14~100 ms.

- Read-only transaction

    Two ways to improve performance: (1) Read from local datacenter; (2) Read-only transaction doesn't require locking. The challenge is how to ensure serializability and external consistency (linearizability).

    Naive solution: Simply reading the latest value from the local datacenter doesn't yield serializability. 

    Spanner's solution: snapshot isolated. Assume each machine has a synchronoized clock, and each transaction is tagged with a timestamp. The timestamp for read-write transaction is the commit time, the timestmap for read-only transaction is the start time. Each replica maintains different versions of data, for different timestamps. When serving a read-only request, it finds the version with largest timestamp lower than the read-only request. 

    The snapshot-isolation approach provides linearizability, but at the cost of additional storage space.

    Another problem: what if the local replica is just lagging (in the minority)? Delay the read (SAFETIME).

- Synchronized clock?

    What if clocks are not synchronized? No problem for read-write transaction. If timestamp is too large, slow but still correct. If timestamp is too small, miss committed writes and not linearizable.

    UTC => GPS => GPS receiver => Time master. Time uncertainty: propagation, network, local clock drifts, ...

    TrueTime. TTInterval = [earliest, latest]. Two rules to ensure linearizable:

    - Start rule: timestamp = TT.now().latest. 

    - Commit wait (only for read-write): delay until timestamp < TS.now().earliest.

    Snapshot isolation provides serializable read-only transactions, and synchronized timestamps yield linearizability. 

    

    

    

