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

// signalGroup signals only the direct child on Windows.
func signalGroup(p *os.Process, sig syscall.Signal) error {
	if p == nil {
		return nil
	}
	return p.Signal(sig)
}

// sigTerm signals only the direct child on Windows.
func sigTerm(p *os.Process) error {
	return signalGroup(p, syscall.SIGTERM)
}

// sigKill kills only the direct child on Windows.
func sigKill(p *os.Process) error {
	return signalGroup(p, syscall.SIGKILL)
}
