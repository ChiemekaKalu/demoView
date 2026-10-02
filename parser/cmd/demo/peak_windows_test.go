package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

// processMemoryCounters is PROCESS_MEMORY_COUNTERS from psapi.h.
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

var getProcessMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// runPeak runs cmd and returns its peak working set in bytes. The handle it
// opens while the child runs keeps the counters readable after it exits.
func runPeak(cmd *exec.Cmd) (int64, error) {
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	const processQueryLimitedInformation = 0x1000
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(cmd.Process.Pid))
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return 0, fmt.Errorf("OpenProcess: %w", err)
	}
	defer syscall.CloseHandle(h)
	if err := cmd.Wait(); err != nil {
		return 0, err
	}
	pmc := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	if ok, _, err := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.cb)); ok == 0 {
		return 0, fmt.Errorf("GetProcessMemoryInfo: %w", err)
	}
	return int64(pmc.peakWorkingSetSize), nil
}
