// Package daemon implements Phase 3 daemon mode: a long-lived process that owns
// the runtime's managed processes so they survive the MCP session that started
// them, and lets multiple sessions co-manage one stack.
//
// The daemon hosts the full runtime facade and exposes it over a Unix socket
// (RPC in rpc.go). The MCP stdio server becomes a thin client of the daemon
// when runtime.daemon is set; otherwise behavior is exactly today's
// session-scoped model (no regression).
//
// The lifecycle (Start/Stop/Run/IsRunning) is Unix-only — it relies on flock,
// setsid and /proc — and lives in daemon_unix.go with stubs in
// daemon_windows.go. The Unix-socket RPC (rpc.go) is portable.
package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Paths under the project's .agent-runtime/ directory.
func Dir(projectDir string) string        { return filepath.Join(projectDir, ".agent-runtime") }
func SocketPath(projectDir string) string { return filepath.Join(Dir(projectDir), "run.sock") }
func PidPath(projectDir string) string    { return filepath.Join(Dir(projectDir), "daemon.pid") }
func LogPath(projectDir string) string    { return filepath.Join(Dir(projectDir), "daemon.log") }
func LockPath(projectDir string) string   { return filepath.Join(Dir(projectDir), "daemon.lock") }

// HandoverPath is the marker migrate writes before SIGTERM so a patched
// v2.x daemon leaves its processes for v3 adoption.
func HandoverPath(projectDir string) string { return filepath.Join(Dir(projectDir), "handover") }

// handoverRequested reports whether migrate asked this daemon to hand over.
func handoverRequested(projectDir string) bool {
	_, err := os.Stat(HandoverPath(projectDir))
	return err == nil
}

// ErrAlreadyRunning is returned by Start when a live daemon already owns the
// project.
var ErrAlreadyRunning = errors.New("daemon is already running")

// ReadPid returns the daemon's recorded pid, or 0 when absent/unparseable.
func ReadPid(projectDir string) int {
	data, err := os.ReadFile(PidPath(projectDir))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// WaitReady polls until the daemon socket accepts a connection, the timeout
// elapses, or the daemon dies.
func WaitReady(projectDir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("unix", SocketPath(projectDir)); err == nil {
			conn.Close()
			return nil
		}
		if pid := ReadPid(projectDir); pid > 0 && !pidAlive(pid) {
			return errors.New("daemon exited during startup")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon to become ready at %s", SocketPath(projectDir))
}
