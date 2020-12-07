# Lec12 Distributed Transaction

Distributed transactions = concurrency control + atomic commit.



### Background

Distributed transaction is needed when data is sharded among many servers, and an operation involves records on different servers. This is traditional database concern, but the idea is used in many distributed systems. 

The goal is to hide the complexities. The programmer just specify the beginning and end of a transaction. The correct behavior of a transaction is ACID. Atomic: all-or-nothing; Concistent: obey application-specific invariants; Isolated: no interference between transactions (serializable); Durable: once committed, the change is permanent across failures. In this lecture, we focus on Atomic, Isolated, and Durable.

Serializability definition. Refer to FAQ for the difference between linearizability and serializability.

A transaction can abort if something goes wrong, and must clean up the work. 



### Concurrency Control

Pessimistic vs Optimistic. This lecture discusses pessimistic concurrency control.

Two-phase Locking: acquire a lock before using the data; hold all locks until *after* commit/abort. Two-phase locking ensures serializability, but can forbid some serializable executions. Make sure you understand why we cannot release lock earlier. Two-phase locking can be more efficient than simple locking. 



### Atomic Commit

Two-Phase Commit (2PC): operations + PREPARE + COMMIT. 

2PC is clearly correct without failures. We achieved all-or-nothing. What if there are crashes ([video](https://www.youtube.com/watch?v=aDp99WDIM_4&feature=youtu.be))? 

Participant crash:

-  Suppose the participant B crashed before sending YES for PREPARE/COMMIT. When it restarts and receives request again, the volatile state about the transaction is gone, so it cannot recognize the transaction, and just tell the TC to abort.
- Suppose B crashes after replying YES for PREPARE, but before replying YES for COMMIT, indicating that it's ready to execute the transaction. The server is allowed to send COMMIT message to A, which would make the changes permanent. Thus, B must make sure that it persists the information (about transactions and locks held) about the transaction before replying YES for PREPARE. In this way, when B restarts, it can re-construct the information and get ready to receive COMMIT.
- Suppose B crashes after replying YES for COMMIT, then it has done all necessary work, and can just restart.

TC crash:

- Suppose TC crashes before sending any COMMIT messages, then it's ok no participant would actually make the changes. The TC can just forget about the transaction.
- Suppose TC crashes after sending at least one COMMIT, telling some participant to make the changes permanent, the TC must be able to continue after crash. This means before sending any COMMIT, the TC must make the information about the transaction persistent. This also means that participants must be able to handle duplicate COMMITs.

What if network problems occur ([video](https://youtu.be/aDp99WDIM_4?t=3389))?

PREPARE: 

- If a participant doesn't get request for PREPARE: it's allowed to abort - removes the state and releases the lock. If it receives PREPARE for some transaction it doesn't know, it can just reply NO.

- If TC doesn't get reply for PREPARE: Resending + Timeout and ABORT.

COMMIT:

- If a participant replied YES to PREPARE, but doesn't hear COMMIT: it's NOT allowed to abort and release the lock. It must blocks waiting, holding the lock. 

This "waiting with lock" makes 2PC unfavorable and inefficient. 

After getting ACKs for all COMMITs, the TC can erase the logs previously persisted. After getting COMMIT and made the changes, they can erase the logs. Later when they receive messages for transactions that they don't recognize, they can just ACK again. 

Disadvantages of 2PC: many communications, persistence, waiting with lock make it slow; Not fault-tolerant.



### Raft

The coordinator-participant organization looks similar to Raft (leader-follower), but the outcome is very different. In Raft, all machines are doing the same thing; but in 2PC, all machines are doing different things. 

You can combine Raft with 2PC, to make it more available.

