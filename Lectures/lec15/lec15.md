# Lec15 Spark

Another evolutional step, after MapReduce, for big data computing. It's more general/expressive, and handles iterative computations better than MapReduce.



## Programming model

Lines like `val lines = spark.read.textFile("in").rdd`, `val links2 = links1.distinct()` are only building the lineage graph (a **DAG**, not cyclic) rather than doing any actual computation.

Each transformation is executed only when calling `collect`. Whenever `collect` is called, the entire graph is recomputed from the very beginning (unless we specify `cache` or `persist`). `cache` tells Spark to persist the result in memory. 

Transformations like `map` and `filter` have narrow dependencies, while `distinct` and `groupByKey` have wide dependencies. Narrow dependencies doesn't require data movement between machines, while wide dependencies do. Spark is able to understand data distribution on different servers and make possible optimizations. 

The Scala code runs in a "driver" machine. The driver constructs the lineage graph and coordinates servers. The driver assumes that data is stored in HDFS, which automatically partitions large files like GFS. 



## Execution strategy

In MapReduce, the outcome of each step is written to GFS, causing high disk I/O for iterative applications. Spark avoids this cost. 

Wide dependency is implemented like the Map step in MapReduce. Let *upstream transformations* be any narrow dependency before the wide dependency, and *downstream transformations* be the wide dependencies. After finishing upstream transformations, each machine arranges its outcome into buckets, one per downstream partitions (This is like the `Map-X-1`, `Map-X-2`, ... in MapReduce for upsteam worker `X`). Before the downsteam transformations start, each worker fetches its bucket from each upstream worker (This is like merging `Map-X-1`, `Map-Y-1`, ..., into one file for  reduce worker `1`). Wide dependencies require inter-machine communication and is expensive. 

`cache` tells Spark to keep the result in memory, while `persist` tells Spark to save the result to HDFS. 

As Spark has the complete view of the lineage graph, many optimizations are possible: pipelining consecutive narrow transformations, increasing locality, reducing data communication, etc. 



## Fault tolerance

When a machine crashes, its memory and computation state are lost. Also, if not specified, intermediate result from previous transformations are discarded. Thus, if machines crash, the drive must re-run the transformations. 

Re-execution is cheap for narrow dependencies but expensive for wide dependencies. 

```
    A                B                 C
#1 Narrow           Narrow            Narrow
#2 Narrow           Narrow            Narrow
#3 Narrow           Narrow            Narrow
#4 Wide             Wide [X]          Wide
```

For example, suppose B crashes when executing the wide transformation in #4. To re-execute, intermediate result from all servers in #3 are needed - so both A, B, C needs to re-execute #1, #2, #3, before B can re-execute its #4. This is extremely inefficient. 

To avoids the problem, Spark supports checkpoints to HDFS and re-execution starts from the last checkpoint. This requires immutabe data and deterministic computation in Spark. 



## Limitations

Not for streaming data.