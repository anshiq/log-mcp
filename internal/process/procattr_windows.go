//go:build windows

package process

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttr is a no-op on Windows: process-group signalling is not supported
// in v1. Only the direct child is signalled (documented limitation).
func setProcAttr(cmd *exec.Cmd) {}

// sigTerm signals only the direct child on Windows.
func sigTerm(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}

// sigKill kills only the direct child on Windows.
func sigKill(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
