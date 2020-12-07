# Lec16 Memcache



## Infrastructure architectures

Typical evolutions for websites:

1. single machine w/ web server + DB
    DB provides persistent storage, crash recovery, transactions, SQL. Application queries DB, formats HTML, etc.
    but: as load grows, application takes too much CPU time
2. many web FEs, one shared DB
    FEs are stateless, all sharing (and concurrency control) via DB
      stateless -> any FE can serve any request, no harm from FE crash
    but: as load grows, need more FEs, soon single DB server is bottleneck
3. many web FEs, data sharded over cluster of DBs
    partition data by key over the DBs. App looks at key (e.g. user), chooses the right DB.
    Good DB parallelism if no data is super-popular (no hot data).
    painful -- cross-shard transactions and queries probably don't work w/o 2PC.
    Not useful if some data is very hot.
    but: DBs are slow, even for reads, why not cache read requests?
4. many web FEs, many caches for reads, many DBs for writes
    Cost-effective b/c use requests are typically read-heavy and memcached 10x faster than a DB. Memcached just an in-memory hash table, very simple. 
    complex b/c DB and memcacheds can get out of sync
    fragile b/c cache misses can easily overload the DB
    (next bottleneck will be DB writes -- hard to solve)



## Facebooks infrastructure picture

One primary datacenter in the west coast, and a secondary datacenter in the east cost. All writes go the primary and reads go to the nearest datacenter. Writes are asynchronously transmitted from the primary to secondary. Within each datacenter, there are servers, memcache, DBs. 

Memcache is a look-aside cache, not a look-through cache. It's not aware of the DBs. The primary goal is not reduce latency, but shielding the DB from huge load. Two key concerns: a little bit of staleness of OK but unbounded staleness is unacceptable; read-my-write is important.



## Performance

Two basic strategies for storage: parallel vs replication. 

- Patition: divide keys over memcache servers. 

    Efficient use of memory.

    Works well if no key is extremely hot.

    Many TCP connections from clieent to server (each client has connections to all memcache servers -> O(n^2)).

- Replication: divide clients over memcache servers. Each memcache servers work independently. 

    Works the case when some keys are very hot.

    Fewer TCP connections (one per client -> O(n) connections).

    Less total data can be cached.

Why multiple clusters per region?

Regional pool, shared by all clusters in a region, for less-popular item to save RAM. 

Cold cluster warmup.

Thundering herd: solved by lease. 

Gutter servers for failures.



## Consistency

Goal: bounded staleness + read-my-write

The lease mechanism solves certain staleness problems. 











































