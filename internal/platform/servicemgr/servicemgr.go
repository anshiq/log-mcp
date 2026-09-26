// Package servicemgr implements the platform.ServiceManager interface:
// systemd user units on Linux, LaunchAgents on macOS, Task Scheduler
// XML on Windows (imported via schtasks).
package servicemgr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ServiceStatus describes the state of a managed service.
type ServiceStatus struct {
	Running bool
	Enabled bool
	Pid     int
}

// Manager manages the per-user daemon service.
type Manager interface {
	Install() error
	Uninstall() error
	Status() (ServiceStatus, error)
	Start() error
	Stop() error
}

// New returns the platform manager. exePath is the agentd binary;
// socketPath selects the socket-activated unit where supported.
func New(exePath, socketPath string) (Manager, error) {
	return newPlatform(exePath, socketPath)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w: %s", name, args, err, out)
	}
	return string(out), nil
}
