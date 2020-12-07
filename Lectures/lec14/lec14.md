# Lec14 FaRM

Performance tricks:

- Sharding
- All data in RAM
- Non-volatile RAM
- one-sided RDMA
- Kernel bypassing
- Transaction protocol that exploits RDMA

Make sure you understand Figure 4.

Note that one-sided RDMA writes are used to append to the logs/queues of servers (which is faster than RPCs).

*Validate* and *commit-backup* are related with reads, while *lock* and *commit-primary* are related with writes. As you can see from Figure4, *validate* just involves one-sided RDMA reads, while *lock* involves acquiring the lock. In other words, read-only operations only involve RDMAs and never requires attention of the remote CPU - so it's extremely fast.

After one ACK for *commit-primary* is received (for each shard?), TC can reply to application.  



Useful Q&As:

> Q: Section 3 seems to say that a single transaction's reads may see
> inconsistent data. That doesn't seem like it would be serializable!
>
> A: Farm only guarantees serializability for transactions that commit.
> If a transaction sees the kind of inconsistency Section 3 is talking
> about, FaRM will abort the transaction. Applications must handle
> inconsistency in the sense that they should not crash, so that they
> can get as far as asking to commit, so that FaRM can abort them.

