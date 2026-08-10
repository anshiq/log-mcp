//go:build unix

package process

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttr starts the child in its own process group so that the whole
// process tree (npm -> next-server, mvn -> java, ...) can be signalled as a
// unit.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// sigTerm sends SIGTERM to the entire process group of p.
func sigTerm(p *os.Process) error {
	if p == nil {
		return nil
	}
	return syscall.Kill(-p.Pid, syscall.SIGTERM)
}

// sigKill sends SIGKILL to the entire process group of p.
func sigKill(p *os.Process) error {
	if p == nil {
		return nil
	}
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}
