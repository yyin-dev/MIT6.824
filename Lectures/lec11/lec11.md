# Lec11 Frangipani

The lecture makes everything clear. Very informative and inspiring. 

Just imagine Petal as a virtualized disk, which is distributed, fault-tolerant, strong-consistency - ideally virtualized. 

Three big topics of Frangipani:

- Cache coherence
- Distributed transactions
- Distributed cache recovery

and the interactions among the above.

The cache coherence protocol, interplaying with locking service and write-back policy, is similar to cache consistency in OS. This is relatively simple. 

Distributed transactions is enabled by locking. This is also straight-forward.

The distributed cache recovery uses Write-Ahead Log (WAL). Two interesting designs: (1) the log is initially stored in the memory of Frangipani servers, but later stored on Petal; (2) Each Frangipani maintains its own log, instead of all servers using the same log. 

The lecture explains how to use Log Seqence Number (LSN) to detect the end of the log and how version number helps in recovery. You might be interested in several recovery cases ([cases](https://youtu.be/-pKNCjUhPjQ?t=4001)) discussed in the lecture. Also, you should understand why it's safe for the recovery software to replay logs without locking (and it cannot relies on locking).

The lecture also talks about why Frangipani didn't have big influence on the evolution of distributed storage. Reasons include: (1) file system is not a good API for many applications; (2) The intended use caes is among trusted partites.



Contents not discussed in the lecture but covered in the lecture note:

- Network partition and recovery

    What if:

    ```
    WS1 holds a lock
    Network partition
    WS2 decides WS1 is dead, recovers, releases WS1's locks
    If WS1 is actually alive, could it subsequently try to write data covered by the lock?
    ```

    If lock is associated with a lease. Lock servers doesn't start recovery until the lease expires. 

- Paxos/Raft usage

    Paxos-based managers in used for lock servers and Petal servers. 

    