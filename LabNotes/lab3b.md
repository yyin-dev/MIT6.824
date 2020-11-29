# Lab3b

### Git commit record
With immediate `AppendEntries` in `rf.Start`, current code reliably passes `TestSnapshotRPC3B`, `TestSnapshotSize3B`, `TestSnapshotRecover3B`, but fails for `TestSnapshotRecoverManyClients3B`. 

The code reliably passes 3A tests, so the problem here must be related with snapshot and recovery. 

