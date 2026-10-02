//go:build !linux

package main

import "os"

// childPeakMemory is 0 off Linux, where the OS reports no peak RSS for a
// finished subprocess.
func childPeakMemory(*os.ProcessState) int64 { return 0 }
