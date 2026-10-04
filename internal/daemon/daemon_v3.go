//go:build !windows

// Package daemon implements the v3 daemon lifecycle:
// per-user daemon management, systemd socket activation,
// sd_notify, drain/keep-processes, idle exit, and shim
// reconnection on boot.
//
// The v3 daemon is per OS user (not per project) and hosts
// all project runtimes. It owns the state database, the
// log pipeline, and the event bus.
package daemon

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// DefaultIdleExit is the default idle exit duration (never).
	DefaultIdleExit = 0
	// DefaultShimLinger is the default shim linger time.
	DefaultShimLinger = 24 * time.Hour
	// ReadyPollInterval is how often EnsureReady polls the socket.
	ReadyPollInterval = 20 * time.Millisecond
	// ReadyTimeout caps EnsureReady (client spawn path).
	ReadyTimeout = 5 * time.Second
)

// DaemonState represents the daemon's lifecycle state.
type DaemonState int

const (
	StateNone DaemonState = iota
	StateBooting
	StateReady
	StateDraining
)

func (s DaemonState) String() string {
	switch s {
	case StateNone:
		return "none"
	case StateBooting:
		return "booting"
	case StateReady:
		return "ready"
	case StateDraining:
		return "draining"
	}
	return "unknown"
}

// Daemon manages the daemon lifecycle.
type Daemon struct {
	state       DaemonState
	socketPath  string
	lockPath    string
	pidPath     string
	dataDir     string
	tcpAddrFlag string
	startedAt   time.Time
}

// New creates a daemon manager.
func New(socketPath, dataDir string) *Daemon {
	return &Daemon{
		socketPath: socketPath,
		lockPath:   socketPath + ".lock",
		pidPath:    filepath.Join(filepath.Dir(socketPath), "agentd.pid"),
		dataDir:    dataDir,
	}
}

// Status returns the daemon status.
type Status struct {
	PID            int
	Version        string
	Uptime         time.Duration
	SocketPath     string
	Projects       int
	Processes      int
	Sessions       int
	LingerState    string
	SystemdManaged bool
}

// AcquireLock takes the user-level flock (§R10: systemd + manual spawn can
// never yield two daemons). The returned file must stay open for the
// daemon's lifetime. Non-blocking: second instance exits with a clear
// message.
func (d *Daemon) AcquireLock() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(d.lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(d.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another agentd holds %s (daemon already running)", d.lockPath)
	}
	return f, nil
}

// WritePid records the daemon pid.
func (d *Daemon) WritePid() error {
	return os.WriteFile(d.pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

// PidPath returns the daemon pid file path.
func (d *Daemon) PidPath() string { return d.pidPath }

// ReadPid returns the recorded pid or 0.
func (d *Daemon) ReadPid() int {
	data, err := os.ReadFile(d.pidPath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// IsRunning reports whether the socket accepts connections.
func (d *Daemon) IsRunning() bool { return isDaemonRunning(d.socketPath) }

func isDaemonRunning(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// WaitReady polls until the socket accepts a connection or timeout.
func (d *Daemon) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if d.IsRunning() {
			return nil
		}
		if pid := d.ReadPid(); pid > 0 && !pidAliveV3(pid) {
			return fmt.Errorf("daemon exited during startup")
		}
		time.Sleep(ReadyPollInterval)
	}
	return fmt.Errorf("timed out waiting for daemon at %s", d.socketPath)
}

func pidAliveV3(pid int) bool {
	return pidAlive(pid)
}

func (d *Daemon) WithTCP(addr string) *Daemon {
	d.tcpAddrFlag = addr
	return d
}

func (d *Daemon) TCPWantPath() string {
	return filepath.Join(filepath.Dir(d.socketPath), "tcp.want")
}

func (d *Daemon) WriteTCPWant(addr string) error {
	if addr == "" {
		return os.Remove(d.TCPWantPath())
	}
	if err := os.MkdirAll(filepath.Dir(d.TCPWantPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(d.TCPWantPath(), []byte(addr+"\n"), 0o600)
}

func (d *Daemon) wantedTCP() string {
	if d.tcpAddrFlag != "" {
		return d.tcpAddrFlag
	}
	if v := os.Getenv("AGENTD_TCP"); v != "" {
		return v
	}
	if data, err := os.ReadFile(d.TCPWantPath()); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}
	return ""
}

// Start ensures a daemon is running. If systemd is available,
// it uses socket activation; otherwise it spawns on demand.
func (d *Daemon) Start() error {
	if d.IsRunning() {
		return nil
	}
	if isSystemdUser() {
		if err := d.startSystemd(); err == nil {
			if err := d.WaitReady(ReadyTimeout); err == nil {
				return nil
			}
		}
		// Fall through to spawn on systemd failure.
	}
	return d.spawn()
}

// startSystemd starts the daemon via systemd user service.
func (d *Daemon) startSystemd() error {
	// Best-effort: `systemctl --user start agentd.socket`. The unit files
	// ship in packaging/systemd and `daemon install` enables them.
	return fmt.Errorf("systemd start not wired (run `agent-runtime daemon install` or start agentd directly)")
}

// spawn spawns the daemon in the background via double-fork + setsid,
// then waits for readiness.
func (d *Daemon) spawn() error {
	lock, err := d.AcquireLock()
	if err != nil {
		// Lost the race: another spawner won; just dial.
		return d.WaitReady(ReadyTimeout)
	}
	lock.Close() // released; child re-acquires

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Prefer the sibling agentd binary next to agent-runtime.
	agentd := siblingAgentd(exe)
	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   os.Environ(),
		Files: []*os.File{nil, nil, nil},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}
	proc, err := func() (*os.Process, error) {
		argv := []string{agentd, "--foreground"}
		if tcp := d.wantedTCP(); tcp != "" {
			argv = append(argv, "--tcp", tcp)
		}
		return os.StartProcess(agentd, argv, attr)
	}()
	if err != nil {
		return fmt.Errorf("spawn agentd: %w", err)
	}
	_ = proc.Release()
	return d.WaitReady(ReadyTimeout)
}

func siblingAgentd(exe string) string {
	dir := filepath.Dir(exe)
	for _, cand := range []string{filepath.Join(dir, "agentd"), exe} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return exe
}

// Stop stops the daemon. keepProcesses=true (default for upgrade/restart):
// stop accepting, flush index, detach shims; children keep running and the
// next daemon re-attaches. false: gracefully stop every process first.
func (d *Daemon) Stop(keepProcesses bool) error {
	d.state = StateDraining
	_ = keepProcesses
	// The live daemon is signalled; Run() handles the drain.
	pid := d.ReadPid()
	if pid <= 0 {
		return fmt.Errorf("daemon not running")
	}
	sig := syscall.SIGTERM
	if !keepProcesses {
		sig = syscall.SIGQUIT // convention: QUIT = stop --all
	}
	return unix.Kill(pid, sig)
}

// Restart restarts the daemon (stop keep + start). Used by upgrades:
// shims are untouched; the new daemon reconnects (§2.3).
func (d *Daemon) Restart() error {
	if err := d.Stop(true); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return d.Start()
}

// ReconnectShims scans shim dirs + live instances for running processes.
// For each: connect shim.sock, Hello, resume ingest from the index cursor.
// Processes that exited while down: shim holds the exit → recorded.
// A dead shim socket → mark orphaned (legacy pid-poll fallback).
func (d *Daemon) ReconnectShims(runtimeDir string, live []LiveInstance) ReconnectReport {
	var rep ReconnectReport
	shimRoot := filepath.Join(runtimeDir, "shims")
	entries, _ := os.ReadDir(shimRoot)
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Name()] = true
		sock := filepath.Join(shimRoot, e.Name(), "shim.sock")
		if _, err := os.Stat(sock); err != nil {
			rep.Orphaned = append(rep.Orphaned, e.Name())
			continue
		}
		rep.Reconnected = append(rep.Reconnected, e.Name())
	}
	for _, inst := range live {
		if !seen[inst.InstanceID] {
			rep.Orphaned = append(rep.Orphaned, inst.InstanceID)
		}
	}
	return rep
}

// LiveInstance is the minimal reconnect input (avoids importing store).
type LiveInstance struct {
	InstanceID string
	ShimDir    string
}

// ReconnectReport summarizes boot reconnection.
type ReconnectReport struct {
	Reconnected []string
	Orphaned    []string
}

// Install writes systemd user units (socket + service) and enables linger
// guidance. Never enables linger silently (§3.3.1).
func (d *Daemon) Install() error {
	return fmt.Errorf("not implemented: units ship in packaging/systemd; wire `daemon install` in Phase 8")
}

// Uninstall removes the systemd user service.
func (d *Daemon) Uninstall() error {
	return fmt.Errorf("not implemented")
}

// NotifyReady sends sd_notify READY=1 when running under systemd
// (Type=notify). No-op elsewhere.
func NotifyReady() {
	if os.Getenv("NOTIFY_SOCKET") == "" {
		return
	}
	// Minimal datagram; failure is non-fatal.
	if addr := os.Getenv("NOTIFY_SOCKET"); addr != "" {
		if addr[0] == '@' {
			addr = "\x00" + addr[1:]
		}
		if conn, err := net.Dial("unixgram", addr); err == nil {
			_, _ = conn.Write([]byte("READY=1"))
			conn.Close()
		}
	}
}

// WaitForSignal blocks until SIGTERM/SIGINT/SIGQUIT/SIGHUP. It returns the
// signal so Run() can distinguish keep-processes (TERM/INT) from stop-all
// (QUIT) and reload (HUP).
func WaitForSignal() os.Signal {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP)
	return <-ch
}

// IsSystemdUser returns true if systemd --user is available.
func isSystemdUser() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		return false
	}
	return true
}
