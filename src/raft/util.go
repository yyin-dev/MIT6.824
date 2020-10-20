package raft

import (
	"log"
	"math/rand"
)

// Debugging
const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}

func IntRange(lower int, upper int) int {
	return rand.Intn(upper-lower+1) + lower
}

func min(x int, y int) int {
	if x < y {
		return x
	}
	return y
}
