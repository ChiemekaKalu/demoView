//go:build !linux && !darwin && !windows

package main

import "os/exec"

// runPeak runs cmd. It returns -1 for the peak, which is not measured here.
func runPeak(cmd *exec.Cmd) (int64, error) {
	return -1, cmd.Run()
}
