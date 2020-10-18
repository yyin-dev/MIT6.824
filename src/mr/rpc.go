package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import (
	"os"
	"strconv"
)

const (
	mapTaskType    = "Map"
	reduceTaskType = "Reduce"
	sleepTaskType  = "Sleep"
	exitTaskType   = "Exit"
)

// Request job (map/reduce) from master
type RequestTaskArgs struct {
}

type RequestTaskReply struct {
	// common
	TaskType   string
	NumMaps    int
	NumReduces int

	// For map task only
	FileName      string
	MapInputIndex int

	// For reduce task only
	ReduceIndex int
}

type NotifyTaskDoneArgs struct {
	TaskType string

	// for map task only
	MapIndex      int
	Filename      string
	TempFilenames []string

	// for reduce task only
	ReduceIndex        int
	TempOutputFilename string
}

type NotifyTaskDoneReply struct {
}

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the master.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func masterSock() string {
	s := "/var/tmp/824-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
