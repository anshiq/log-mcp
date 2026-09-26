//go:build windows
// +build windows

package shim

import (
	"fmt"
	"os"
	"syscall"
)

func killPID(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// Windows has no POSIX signals; SIGKILL maps to Kill, anything else
	// to a ctrl event is unsupported — refuse loudly instead of faking it.
	if sig == syscall.SIGKILL {
		return p.Kill()
	}
	return fmt.Errorf("shim: signal %v unsupported on Windows (use Stop for trees via Job Object)", sig)
}

func killGroup(pgid int, sig syscall.Signal) error {
	return fmt.Errorf("shim: process-group signal unsupported on Windows (use Stop: TerminateJobObject)")
}
