//go:build !windows
// +build !windows

package shim

import (
	"syscall"
)

func killPID(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

func killGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}
