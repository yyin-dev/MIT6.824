package mr

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

const (
	idle     = "idle"     // not assigned
	assigned = "assigned" // assigned
	finished = "finished" // finished

	taskTimeout = 10 * time.Second
)

type MapTaskState struct {
	index          int
	status         string
	tempFilenames  []string
	prevAssignTime time.Time
}

type ReduceTaskState struct {
	status         string
	prevAssignTime time.Time
}

type Master struct {
	mutex sync.Mutex
	done  bool

	numMaps    int // num of map tasks
	numReduces int // num of reduce tasks

	mapTaskStates    map[string]MapTaskState // (inputFileName, state)
	reduceTaskStates []ReduceTaskState       // (index, state)
}

// Your code here -- RPC handlers for the worker to call.

//
// Assign a task to a worker.
//
func (m *Master) RequestTask(args *RequestTaskArgs, reply *RequestTaskReply) error {
	// Try assign map tasks
	m.mutex.Lock()
	defer m.mutex.Unlock()

	reply.NumMaps = m.numMaps
	reply.NumReduces = m.numReduces

	mapTasksDone := true
	for f, state := range m.mapTaskStates {
		if state.status != finished {
			mapTasksDone = false
		}

		timeout := state.prevAssignTime.Add(taskTimeout).Before(time.Now())
		if state.status == idle || (state.status == assigned && timeout) {
			// assign and return
			reply.TaskType = mapTaskType
			reply.FileName = f
			reply.MapInputIndex = state.index

			// Just for debug
			if !state.prevAssignTime.IsZero() && state.status == assigned && timeout {
				DPrintf("Map-%v is reassigned for timetout", state.index)
			}

			// Update struct in map: https://stackoverflow.com/a/17443950/9057530
			state.status = assigned
			state.prevAssignTime = time.Now()
			m.mapTaskStates[f] = state

			return nil
		}
	}

	// Runs out of map tasks
	if !mapTasksDone {
		reply.TaskType = sleepTaskType
		return nil
	}

	// All map tasks done. Assign reduce tasks
	reduceTasksDone := true
	for i, state := range m.reduceTaskStates {
		if state.status != finished {
			reduceTasksDone = false

			timeout := state.prevAssignTime.Add(taskTimeout).Before(time.Now())
			if state.status == idle || (state.status == assigned && timeout) {
				reply.TaskType = reduceTaskType
				reply.ReduceIndex = i

				m.reduceTaskStates[i].status = assigned
				m.reduceTaskStates[i].prevAssignTime = time.Now()

				return nil
			}
		}
	}

	// Runs out of reduce tasks
	if !reduceTasksDone {
		reply.TaskType = sleepTaskType
		return nil
	}

	// All maps and reduces are done
	m.done = true
	reply.TaskType = exitTaskType
	return nil
}

func (m *Master) NotifyTaskDone(args *NotifyTaskDoneArgs, reply *NotifyTaskDoneReply) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if args.TaskType == mapTaskType {
		if m.mapTaskStates[args.Filename].status == finished {
			return nil
		}

		state := m.mapTaskStates[args.Filename]
		state.status = finished
		m.mapTaskStates[args.Filename] = state

		// Atomic rename
		for i := 0; i < m.numReduces; i++ {
			name := fmt.Sprintf("mr-%v-%v", args.MapIndex, i)
			os.Rename(args.TempFilenames[i], name)
		}

		DPrintf("Master knows Map-%v is done", args.MapIndex)
	} else {
		if m.reduceTaskStates[args.ReduceIndex].status == finished {
			return nil
		}

		m.reduceTaskStates[args.ReduceIndex].status = finished

		// Atomic rename
		name := fmt.Sprintf("mr-out-%v", args.ReduceIndex)
		os.Rename(args.TempOutputFilename, name)

		DPrintf("Master knows Reduce-%v is done", args.ReduceIndex)
	}

	return nil
}

//
// start a thread that listens for RPCs from worker.go
//
func (m *Master) server() {
	rpc.Register(m)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := masterSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

//
// main/mrmaster.go calls Done() periodically to find out
// if the entire job has finished.
//
func (m *Master) Done() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.done
}

//
// create a Master.
// main/mrmaster.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeMaster(files []string, nReduce int) *Master {
	m := Master{
		numMaps:          len(files),
		numReduces:       nReduce,
		mapTaskStates:    make(map[string]MapTaskState),
		reduceTaskStates: make([]ReduceTaskState, nReduce),
		done:             false,
	}
	for i, f := range files {
		m.mapTaskStates[f] = MapTaskState{
			status: idle,
			index:  i,
		}
	}
	for i := 0; i < nReduce; i++ {
		m.reduceTaskStates[i] = ReduceTaskState{
			status: idle,
		}
	}

	m.server()
	return &m
}
