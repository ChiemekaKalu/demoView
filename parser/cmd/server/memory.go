package main

import (
	"bytes"
	"os"
	"runtime"
	"strconv"
)

// resetPeakMemory restarts the VmHWM high-water mark, so the next
// peakMemory covers one parse rather than the life of the process. It
// reports whether that worked: where /proc/self/clear_refs is missing or
// read-only, VmHWM keeps counting from process start.
func resetPeakMemory() bool {
	return os.WriteFile("/proc/self/clear_refs", []byte("5"), 0) == nil
}

// peakMemory reports the process's peak resident set size in bytes. On Linux
// that is VmHWM from /proc/self/status, since the last resetPeakMemory.
// Elsewhere it falls back to the memory the Go runtime has obtained from the
// OS, which is an approximation that rarely shrinks.
func peakMemory() int64 {
	if status, err := os.ReadFile("/proc/self/status"); err == nil {
		if n, ok := parseVmHWM(status); ok {
			return n
		}
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.Sys)
}

// parseVmHWM extracts VmHWM, given in kB, from /proc/self/status.
func parseVmHWM(status []byte) (int64, bool) {
	for line := range bytes.Lines(status) {
		rest, ok := bytes.CutPrefix(line, []byte("VmHWM:"))
		if !ok {
			continue
		}
		fields := bytes.Fields(rest)
		if len(fields) != 2 || string(fields[1]) != "kB" {
			return 0, false
		}
		kb, err := strconv.ParseInt(string(fields[0]), 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}
