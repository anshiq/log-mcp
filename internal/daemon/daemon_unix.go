//go:build !windows

package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"agent-runtime/internal/config"
	"agent-runtime/internal/runtime"
)

// pidAlive probes whether a pid is a live process (signal 0).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || err == unix.EPERM
}

// IsRunning reports whether a live daemon owns this project.
func IsRunning(projectDir string) bool {
	pid := ReadPid(projectDir)
	if !pidAlive(pid) {
		return false
	}
	// Confirm it is actually our daemon (a stale pid could be recycled).
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

// Start spawns a detached daemon for the project and returns true when started,
// false when one is already running.
func Start(loaded *config.Loaded, logger *slog.Logger) (bool, error) {
	if IsRunning(loaded.ProjectDir) {
		return false, nil
	}
	if err := os.MkdirAll(Dir(loaded.ProjectDir), 0o755); err != nil {
		return false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	logF, err := os.OpenFile(LogPath(loaded.ProjectDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer logF.Close()
	cmd := exec.Command(exe, "daemon", "run")
	cmd.Dir = loaded.ProjectDir
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return false, err
	}
	logger.Info("daemon spawned", "pid", cmd.Process.Pid, "project", loaded.ProjectDir)
	return true, nil
}

// Stop gracefully stops the daemon (SIGTERM) and waits for it to exit.
func Stop(projectDir string, timeout time.Duration) error {
	pid := ReadPid(projectDir)
	if !pidAlive(pid) {
		return fmt.Errorf("daemon is not running")
	}
	if err := unix.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("daemon (pid %d) did not stop within %v", pid, timeout)
}

// Run is the daemon's main loop (invoked by `daemon run` in the detached
// child). It acquires the project lock, hosts the runtime + RPC server, and
// shuts everything down gracefully on SIGTERM/SIGINT.
func Run(loaded *config.Loaded, logger *slog.Logger) error {
	if err := os.MkdirAll(Dir(loaded.ProjectDir), 0o755); err != nil {
		return err
	}
	lock, err := acquireLock(LockPath(loaded.ProjectDir))
	if err != nil {
		return fmt.Errorf("another daemon owns this project: %w", err)
	}
	defer lock.Close()

	if err := os.WriteFile(PidPath(loaded.ProjectDir), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(PidPath(loaded.ProjectDir))

	rt := runtime.New(loaded, logger)
	// Re-attach to processes from a crashed previous daemon before serving. The
	// archive's open instance records carry the pids needed to resume managing
	// them (signal, watch, events, archive-backed logs). Best-effort: any
	// failure just logs and the daemon starts fresh.
	if n := rt.AdoptOrphans(); n > 0 {
		logger.Info("daemon adopted orphaned processes", "count", n)
	}
	srv, err := NewServer(rt, SocketPath(loaded.ProjectDir))
	if err != nil {
		return err
	}
	logger.Info("daemon ready", "project", loaded.ProjectDir, "socket", SocketPath(loaded.ProjectDir))

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	<-sigc
	// v3 handover: migrate writes <project>/.agent-runtime/handover before
	// SIGTERM so a patched v2.x skips stopping processes (or honours
	// AGENT_RUNTIME_HANDOVER=1 when set); the v3 daemon re-attaches via
	// the legacy adopt path instead of supervising fresh.
	if os.Getenv("AGENT_RUNTIME_HANDOVER") == "1" || handoverRequested(loaded.ProjectDir) {
		logger.Info("daemon handing over processes to v3; leaving children running")
		srv.Close()
		return nil
	}
	logger.Info("daemon shutting down")
	srv.Close()
	return rt.Shutdown()
}

// acquireLock takes an exclusive flock on the daemon lock file so only one
// daemon runs per project.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
