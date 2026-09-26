//go:build !linux
// +build !linux

package shim

import (
	"os/exec"
	"syscall"
)

// setupChild makes the child a process-group leader in its own session.
func setupChild(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setpgid = true
}

// BecomeSubreaper is a no-op outside Linux (no subreaper; tracking via
// pgid + proc_listchildpids on mac, Job Objects on Windows per §10).
func BecomeSubreaper() error { return nil }
