//go:build linux
// +build linux

package shim

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// setupChild makes the child a process-group leader in its own session
// (setsid) so tree-kill via pgid never touches the daemon. The shim
// itself is expected to run with PR_SET_CHILD_SUBREAPER so grandchildren
// that daemonize re-parent to it and get reaped.
func setupChild(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setpgid = true
}

// BecomeSubreaper marks this process as a child subreaper (daemonize
// grandchildren re-parent here, not to init).
func BecomeSubreaper() error {
	_, _, errno := syscall.Syscall(syscall.SYS_PRCTL, unix.PR_SET_CHILD_SUBREAPER, 1, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
