# Lec17 COPS

We have seen Spanner and Memcache, similar in some sense. Reads are fast by reading from local replica, but writes are expensive. In Spanner, writes must go through Paxos group; In Memcache, all writes must go to the primary and synced to the secondaries. Goal: both reads and writes proceed without talking to other datacenters. In other words, both reads and writes are handled "locally".

- Strawman 1

    Shards in each datacenter. Reads and writes are handled by local shards, and pushed to other datacenters asynchronously. This greatly increases parallelism. 

    How to determine the order of writes? Solution1: wall-clock time. Use unique ID/IP/datacenter name to break ties when timestamps are the same. What if clocks drift? It creates a window where any writes in the window produces no effect. Solution2: Lamport clock, aka "logical clock". 

    ```
    Tmax = highest version # seen (from self and others)
    version # = max(Tmax + 1, wall-clock time)
    ```

    If some server has a fast clock, the others can accomodate.

    However, there's no order guarantee. For example, client issues write to ShardA then ShardB, but the order these writes appear in other datacenters are non-deterministic. It only achieves *eventual consistency*:

    - Clients may see updates in different orders
    - If no writes are issued for long enough, all clients see the same data across replicas

- Strawman 2

    Provides a `sync` operation with version number, acting like barriers. `Sync` doesn't return until all replicas have at least the version number specified. 

    This is a working solution - very fast reads and reasonably fast writes.

    But can we have the semantics of `sync` without the cost of synchronous waiting?

    Possibility solution: log server. But doesn't scale well. 

    Updated target:

    - forward puts asynchronously (no sync() or waiting)
    - each shard to forward puts independently (no central log server)
    - enough ordering to make it relatively easy to reason about

- COPS: causal dependency

    Each COPS client maintains a "context" to reflect orders. Dependencies are sent in requests and followed. 

    A client establishes dependencies between versions in two ways:

    1. its own sequence of puts and gets ("Execution Thread" in Section 3)
    2. reading data written by another client

    It's nice that when updates are not causally related, COPS has no guarantee and improves paralleism.



Causal consistency is a popular research idea, but rarely used in deployed systems. What's actually used?

- no geographic replication at all
- primary-site (PNUTS, Memcache)
- eventual consistency (Dynamo, Cassandra)
- strongly consistent (Spanner)



