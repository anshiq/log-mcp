//go:build unix

package process

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// setProcAttr starts the child in its own process group so that the whole
// process tree (npm -> next-server, mvn -> java, ...) can be signalled as a
// unit.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends sig to the whole process group of p (pgid == pid due to
// Setpgid).
func signalGroup(p *os.Process, sig syscall.Signal) error {
	if p == nil {
		return nil
	}
	return syscall.Kill(-p.Pid, sig)
}

// pidAlive reports whether an OS pid is a live, signalable process. A zombie
// (exited child not yet reaped) is NOT alive: it cannot be signaled and will
// never run again, so liveness loops and adoption must not keep it alive.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// On Linux, inspect the process state via /proc: a 'Z' (zombie) is dead even
	// though kill(pid, 0) succeeds.
	if state, ok := procState(pid); ok {
		return state != 'Z' && state != 'X'
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// procState reads the process state char from /proc/<pid>/stat. The state is
// the first field after the (comm) which may contain spaces/parens, so it is
// found by scanning from the last ')'.
func procState(pid int) (byte, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	if i := strings.LastIndexByte(string(data), ')'); i >= 0 && i+2 < len(data) {
		return data[i+2], true
	}
	return 0, false
}

// sigTerm sends SIGTERM to the entire process group of p.
func sigTerm(p *os.Process) error {
	return signalGroup(p, syscall.SIGTERM)
}

// sigKill sends SIGKILL to the entire process group of p.
func sigKill(p *os.Process) error {
	return signalGroup(p, syscall.SIGKILL)
}
