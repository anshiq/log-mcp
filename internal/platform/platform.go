// Package platform defines the cross-platform abstraction layer
// for agent-runtime v3. Linux implementations live in platform_linux.go;
// macOS and Windows stubs are in platform_darwin.go and platform_windows.go.
package platform

import (
	"context"
	"net"
)

// Paths provides XDG-based path resolution.
type Paths interface {
	Runtime() string
	Data() string
	Config() string
	State() string
	Cache() string
}

// IPC provides Unix domain socket and peer-credential functionality.
type IPC interface {
	Listen(addr string) (net.Listener, error)
	Dial(ctx context.Context, addr string) (net.Conn, error)
	PeerUID(conn net.Conn) (uint32, error)
}

// Spawner spawns and reconnects to shims.
type Spawner interface {
	Spawn(ctx context.Context, bundle []byte) (ShimHandle, error)
	Reconnect(ctx context.Context, instanceDir string) (ShimHandle, error)
}

// ShimHandle represents a running shim process.
type ShimHandle interface {
	PID() int
	PGID() int
	ControlAddr() string
}

// TreeKiller sends signals to process trees.
type TreeKiller interface {
	Signal(h ShimHandle, sig Signal, group bool) error
}

// Signal is a POSIX signal number.
type Signal int

// ServiceManager manages daemon lifecycle via systemd/launchd/task scheduler.
type ServiceManager interface {
	Install() error
	Uninstall() error
	Status() (ServiceStatus, error)
	Start() error
	Stop() error
}

// ServiceStatus describes the state of a managed service.
type ServiceStatus struct {
	Running bool
	Active  bool
	Enabled bool
	Pid     int
}

// ResourceMonitor samples process resource usage.
type ResourceMonitor interface {
	Sample(pid int, cgroup string) (Usage, error)
}

// Usage holds CPU and memory usage.
type Usage struct {
	CPU    float64 // nanoseconds
	Memory int64   // bytes
}
