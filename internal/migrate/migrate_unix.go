//go:build !windows

package migrate

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || err == unix.EPERM
}

// isOurBinary checks /proc/<pid>/exe matches this binary (stale pid guard).
func isOurBinary(pid int) bool {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	self, _ := os.Executable()
	ai, err1 := os.Stat(exe)
	bi, err2 := os.Stat(self)
	if err1 == nil && err2 == nil {
		return os.SameFile(ai, bi)
	}
	return exe == self
}

func signalTerm(pid int) error {
	return unix.Kill(pid, unix.SIGTERM)
}
