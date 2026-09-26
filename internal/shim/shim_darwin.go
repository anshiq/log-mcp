//go:build darwin
// +build darwin

package shim

import (
	"os/exec"
	"syscall"
)

// setupChild makes the child a session and process-group leader.
// Setsid alone implies pgid == pid; Setpgid alongside fails with EPERM.
func setupChild(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

// BecomeSubreaper is a no-op on macOS (no subreaper; tracking via pgid +
// proc_listchildpids per §10).
func BecomeSubreaper() error { return nil }
