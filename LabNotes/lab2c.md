## Lab2C

The lab instruction says (and also from past experience) fast rollback algorithm is needed to pass `Figure 8 (unreliable)`. But this time, simply adding persistence is enough to pass the test. I guess the reason is that parameters are tuned well (election timetout, heartbeat interval, sleeping interval, etc.) and the performance is already good enough. Our code passes 2B even more quickly than the sample output. 

```
$ time go test -run 2A
Test (2A): initial election ...
  ... Passed --   3.1  3   60   13896    0
Test (2A): election after network failure ...
  ... Passed --   6.6  3  182   33682    0
PASS
ok      _/mnt/c/Users/yy0125/Desktop/MIT6.824/src/raft  9.657s

real    0m10.167s
user    0m0.749s
sys     0m0.656s

$ time go test -run 2B
Test (2B): basic agreement ...
  ... Passed --   1.1  3   22    4848    3
Test (2B): RPC byte count ...
  ... Passed --   2.7  3   56  113494   11
Test (2B): agreement despite follower disconnection ...
  ... Passed --   4.9  3  102   23004    7
Test (2B): no agreement if too many followers disconnect ...
  ... Passed --   4.3  5  208   40922    3
Test (2B): concurrent Start()s ...
  ... Passed --   1.4  3   22    4350    6
Test (2B): rejoin of partitioned leader ...
  ... Passed --   7.3  3  208   46367    4
Test (2B): leader backs up quickly over incorrect follower logs ...
  ... Passed --  28.1  5 2220 1899322  102
Test (2B): RPC counts aren't too high ...
  ... Passed --   2.7  3   52   12558   12
PASS
ok      _/mnt/c/Users/yy0125/Desktop/MIT6.824/src/raft  52.515s

real    0m52.972s
user    0m3.393s
sys     0m3.333s

$ time go test -run 2C
Test (2C): basic persistence ...
  ... Passed --   5.8  3  112   23328    6
Test (2C): more persistence ...
  ... Passed --  17.8  5 1006  202836   16
Test (2C): partitioned leader and one follower crash, leader restarts ...
  ... Passed --   2.4  3   44    9509    4
Test (2C): Figure 8 ...
  ... Passed --  32.4  5  817  181484   20
Test (2C): unreliable agreement ...
  ... Passed --   5.7  5  227   70452  246
Test (2C): Figure 8 (unreliable) ...
  ... Passed --  35.4  5 3192 9031584   91
Test (2C): churn ...
  ... Passed --  16.3  5  612  661890  303
Test (2C): unreliable churn ...
  ... Passed --  16.2  5  707  386654  242
PASS
ok      _/mnt/c/Users/yy0125/Desktop/MIT6.824/src/raft  131.775s

real    2m12.330s
user    0m16.002s
sys     0m13.453s
```

Total real time: ~3min. Total CPU time: ~20sec.