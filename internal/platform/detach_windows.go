//go:build windows
// +build windows

package platform

import (
	"os/exec"
)

// detach on Windows: the shim joins a Job Object after start (tree
// tracking via TerminateJobObject); no creation flags needed here.
func detach(cmd *exec.Cmd) {}
