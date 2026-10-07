//go:build windows

package daemon

import (
	"fmt"
	"os"
)

// Windows stub for the v3 per-user daemon. Full support (named pipes,
// Job Objects, Task Scheduler) is Phase 9 per §10; these stubs keep the
// tree cross-compiling and fail with actionable errors at runtime.

type DaemonState int

const (
	StateNone DaemonState = iota
	StateBooting
	StateReady
	StateDraining
)

func (s DaemonState) String() string { return "unsupported" }

type Daemon struct{}

func New(_, _ string) *Daemon { return &Daemon{} }

type Status struct{}

func (d *Daemon) AcquireLock() (*os.File, error) {
	return nil, fmt.Errorf("agentd: not supported on Windows yet")
}
func (d *Daemon) WritePid() error { return fmt.Errorf("agentd: not supported on Windows yet") }
func (d *Daemon) PidPath() string { return "" }
func (d *Daemon) ReadPid() int    { return 0 }
func (d *Daemon) IsRunning() bool { return false }
func (d *Daemon) WaitReady(_ interface{}) error {
	return fmt.Errorf("agentd: not supported on Windows yet")
}
func (d *Daemon) Start() error             { return fmt.Errorf("agentd: not supported on Windows yet") }
func (d *Daemon) WithTCP(_ string) *Daemon { return d }
func (d *Daemon) TCPWantPath() string      { return "" }
func (d *Daemon) WriteTCPWant(_ string) error {
	return fmt.Errorf("agentd: not supported on Windows yet")
}
func (d *Daemon) Stop(bool) error  { return fmt.Errorf("agentd: not supported on Windows yet") }
func (d *Daemon) Restart() error   { return fmt.Errorf("agentd: not supported on Windows yet") }
func (d *Daemon) Install() error   { return fmt.Errorf("agentd: not supported on Windows yet") }
func (d *Daemon) Uninstall() error { return fmt.Errorf("agentd: not supported on Windows yet") }

type LiveInstance struct {
	InstanceID string
	ShimDir    string
}

type ReconnectReport struct{}

func (d *Daemon) ReconnectShims(_ string, _ []LiveInstance) ReconnectReport { return ReconnectReport{} }
func NotifyReady()                                                          {}
func WaitForSignal() any                                                    { return nil }
