package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
	"time"
)

//
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}

type KeyValueArray []KeyValue

func (a KeyValueArray) Len() int           { return len(a) }
func (a KeyValueArray) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a KeyValueArray) Less(i, j int) bool { return a[i].Key < a[j].Key }

//
// use ihash(key) % numReduces to choose the reduce
// task number for each KeyValue emitted by Map.
//
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

//
// main/mrworker.go calls this function.
//
func Worker(mapf func(string, string) []KeyValue, reducef func(string, []string) string) {
	for {
		reply := RequestTask()
		switch reply.TaskType {
		case mapTaskType:
			handleMapTask(reply, mapf)
		case reduceTaskType:
			handleReduceTask(reply, reducef)
		case sleepTaskType:
			time.Sleep(2 * time.Second)
		case exitTaskType:
			return
		}
	}
}

func handleMapTask(reply RequestTaskReply, mapf func(string, string) []KeyValue) {
	mapIndex := reply.MapInputIndex
	numReduces := reply.NumReduces
	filename := reply.FileName

	// Open and read file
	file, err := os.Open(filename)
	if err != nil {
		DPrintf("cannot open %v", filename)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		DPrintf("cannot read %v", filename)
	}
	file.Close()

	// Apply map function
	kva := mapf(filename, string(content))

	// Pre-open all needed files to save time
	pwd, _ := os.Getwd()
	files := make([](*os.File), numReduces)
	fileNames := make([]string, numReduces)
	encoders := make([]*json.Encoder, numReduces)
	for i := 0; i < numReduces; i++ {
		tempInterFileName := fmt.Sprintf("mr-%v-%v-*", mapIndex, i)
		tempInterFile, err := ioutil.TempFile(pwd, tempInterFileName)
		if err != nil {
			DPrintf("Cannot create temp inter file: %v\n", tempInterFileName)
		}
		encoder := json.NewEncoder(tempInterFile)

		files[i] = tempInterFile
		fileNames[i] = tempInterFile.Name()
		encoders[i] = encoder
	}

	// Write (k, v) into mr-X-Y
	for _, kv := range kva {
		reduceIndex := ihash(kv.Key) % numReduces

		// Write intermediate keys to file
		err = encoders[reduceIndex].Encode(kv)
		if err != nil {
			DPrintf("Cannot write %v: %v", kv, err)
		}
	}

	// Close all intermediate files
	for i := 0; i < len(files); i++ {
		err := files[i].Close()
		if err != nil {
			DPrintf("Cannot close %v: %v", files[i].Name(), err)
		}
	}

	// Notify master, which will rename temporary files (mentioned in paper)
	notifyMapTaskDone(mapIndex, filename, fileNames)
}

func handleReduceTask(reply RequestTaskReply, reducef func(string, []string) string) {
	// Read from different mr-X-Y's
	intermediate := []KeyValue{}
	for i := 0; i < reply.NumMaps; i++ {
		fileName := fmt.Sprintf("mr-%d-%d", i, reply.ReduceIndex)
		file, err := os.Open(fileName)
		if err != nil {
			DPrintf("Cannot open %s", fileName)
		}

		decoder := json.NewDecoder(file)
		for {
			var kv KeyValue
			if err := decoder.Decode(&kv); err != nil {
				break
			}
			intermediate = append(intermediate, kv)
		}
	}

	// sort
	sort.Sort(KeyValueArray(intermediate))

	// write to temp output file
	pwd, _ := os.Getwd()
	tempOutputFilename := fmt.Sprintf("mr-out-%v-*", reply.ReduceIndex)
	tempOutputFile, err := ioutil.TempFile(pwd, tempOutputFilename)
	if err != nil {
		DPrintf("Cannot create temp output file: %v\n", tempOutputFilename)
	}

	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(tempOutputFile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}
	tempOutputFile.Close()

	// The master would rename the file (mentioned by the paper)
	notifyReduceTaskDone(reply.ReduceIndex, tempOutputFilename)
}

func notifyMapTaskDone(mapIndex int, filename string, tempFilenames []string) {
	args := NotifyTaskDoneArgs{
		TaskType:      mapTaskType,
		MapIndex:      mapIndex,
		Filename:      filename,
		TempFilenames: tempFilenames,
	}
	reply := NotifyTaskDoneReply{}
	call("Master.NotifyTaskDone", &args, &reply)
}

func notifyReduceTaskDone(reduceIndex int, tempOutputFilename string) {
	args := NotifyTaskDoneArgs{
		TaskType:           reduceTaskType,
		ReduceIndex:        reduceIndex,
		TempOutputFilename: tempOutputFilename,
	}
	reply := NotifyTaskDoneReply{}
	call("Master.NotifyTaskDone", &args, &reply)
}

//
// Request task from master
//
func RequestTask() RequestTaskReply {
	args := RequestTaskArgs{}
	reply := RequestTaskReply{}
	call("Master.RequestTask", &args, &reply)
	DPrintf("Worker get %v from master", reply)
	return reply
}

//
// send an RPC request to the master, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := masterSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
