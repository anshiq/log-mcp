//go:build linux
// +build linux

package servicemgr

import (
	"fmt"
	"os"
	"path/filepath"
)

type systemdManager struct {
	exe        string
	socketPath string
}

func newPlatform(exePath, socketPath string) (Manager, error) {
	return &systemdManager{exe: exePath, socketPath: socketPath}, nil
}

func unitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user"), nil
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func (m *systemdManager) socketUnit() string {
	return `[Unit]
Description=agent-runtime daemon socket (per-user)
Documentation=https://github.com/anomalyco/opencode

[Socket]
ListenStream=` + m.socketPath + `
SocketMode=0600
DirectoryMode=0700

[Install]
WantedBy=sockets.target
`
}

func (m *systemdManager) serviceUnit() string {
	return `[Unit]
Description=agent-runtime daemon (1 per OS user)
Requires=agentd.socket
After=agentd.socket

[Service]
Type=notify
KillMode=process
Delegate=yes
Restart=on-failure
RestartSec=1s
ExecStart=` + m.exe + ` --foreground
NotifyAccess=main

[Install]
WantedBy=default.target
`
}

// Install writes the user units and enables the socket. It never enables
// linger silently; linger guidance is printed instead.
func (m *systemdManager) Install() error {
	dir, err := unitDir()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "agentd.socket"), []byte(m.socketUnit()), 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "agentd.service"), []byte(m.serviceUnit()), 0o644); err != nil {
		return err
	}
	if _, err := run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if _, err := run("systemctl", "--user", "enable", "--now", "agentd.socket"); err != nil {
		return err
	}
	fmt.Println("installed agentd.socket. For survive-logout: loginctl enable-linger $USER (opt-in, never automatic)")
	return nil
}

func (m *systemdManager) Uninstall() error {
	_, _ = run("systemctl", "--user", "disable", "--now", "agentd.socket")
	_, _ = run("systemctl", "--user", "disable", "--now", "agentd.service")
	dir, err := unitDir()
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, "agentd.socket"))
	_ = os.Remove(filepath.Join(dir, "agentd.service"))
	_, _ = run("systemctl", "--user", "daemon-reload")
	return nil
}

func (m *systemdManager) Status() (ServiceStatus, error) {
	out, err := run("systemctl", "--user", "is-active", "agentd.service", "agentd.socket")
	_ = out
	return ServiceStatus{Running: err == nil}, nil
}

func (m *systemdManager) Start() error {
	_, err := run("systemctl", "--user", "start", "agentd.socket")
	return err
}

func (m *systemdManager) Stop() error {
	_, err := run("systemctl", "--user", "stop", "agentd.service", "agentd.socket")
	return err
}
