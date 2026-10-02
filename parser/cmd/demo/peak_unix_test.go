//go:build linux || darwin

package main

import (
	"os/exec"
	"runtime"
	"syscall"
)

// runPeak runs cmd and returns its peak resident set size in bytes, from the
// rusage of the finished child.
func runPeak(cmd *exec.Cmd) (int64, error) {
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return -1, nil
	}
	if runtime.GOOS == "darwin" {
		return int64(ru.Maxrss), nil // bytes
	}
	return int64(ru.Maxrss) * 1024, nil // kilobytes
}
