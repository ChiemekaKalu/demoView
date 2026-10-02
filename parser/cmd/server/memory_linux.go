package main

import (
	"os"
	"syscall"
)

// childPeakMemory is a finished subprocess's peak RSS in bytes. Linux
// reports ru_maxrss in kilobytes.
func childPeakMemory(ps *os.ProcessState) int64 {
	if ps == nil {
		return 0
	}
	if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
		return ru.Maxrss * 1024
	}
	return 0
}
