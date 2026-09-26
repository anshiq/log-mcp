//go:build windows
// +build windows

package shim

import (
	"os/exec"
)

// setupChild on Windows: process-group isolation comes from Job Objects
// (internal/process/jobobject_windows.go), not setsid. The daemon assigns
// the child to the instance Job Object after start; tree kill is
// TerminateJobObject per §10.
func setupChild(cmd *exec.Cmd) {
	// No SysProcAttr tweaks: default creation is fine; the Job Object
	// provides the isolation and accounting.
}

// BecomeSubreaper is a no-op on Windows (Job Objects inherit).
func BecomeSubreaper() error { return nil }
