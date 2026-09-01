//go:build windows

package daemon

import (
	"errors"
	"log/slog"
	"time"

	"agent-runtime/internal/config"
)

// Daemon mode is Unix-only (it relies on flock, setsid and /proc). On Windows
// these lifecycle operations are refused with a clear error; the Unix-socket
// RPC itself (rpc.go) still works when a daemon is hosted elsewhere.
var errDaemonUnsupported = errors.New("daemon mode is not supported on Windows")

func pidAlive(pid int) bool { return false }

func IsRunning(projectDir string) bool { return false }

func Start(loaded *config.Loaded, logger *slog.Logger) (bool, error) {
	return false, errDaemonUnsupported
}

func Stop(projectDir string, timeout time.Duration) error { return errDaemonUnsupported }

func Run(loaded *config.Loaded, logger *slog.Logger) error { return errDaemonUnsupported }
