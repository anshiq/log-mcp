//go:build windows
// +build windows

package servicemgr

import (
	"fmt"
	"os"
	"path/filepath"
)

type taskManager struct {
	exe        string
	socketPath string
}

func newPlatform(exePath, socketPath string) (Manager, error) {
	return &taskManager{exe: exePath, socketPath: socketPath}, nil
}

const taskName = "agent-runtime-agentd"

func (m *taskManager) taskXML() string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Actions Context="Author"><Exec><Command>` + m.exe + `</Command><Arguments>--foreground</Arguments></Exec></Actions>
</Task>
`
}

// Install registers a logon task via schtasks.
func (m *taskManager) Install() error {
	xml := filepath.Join(os.TempDir(), "agentd-task.xml")
	if err := writeFileAtomic(xml, []byte(m.taskXML()), 0o600); err != nil {
		return err
	}
	defer os.Remove(xml)
	_, err := run("schtasks", "/Create", "/TN", taskName, "/XML", xml, "/F")
	return err
}

func (m *taskManager) Uninstall() error {
	_, err := run("schtasks", "/Delete", "/TN", taskName, "/F")
	return err
}

func (m *taskManager) Status() (ServiceStatus, error) {
	out, err := run("schtasks", "/Query", "/TN", taskName)
	_ = out
	return ServiceStatus{Running: err == nil}, nil
}

func (m *taskManager) Start() error {
	_, err := run("schtasks", "/Run", "/TN", taskName)
	return err
}

func (m *taskManager) Stop() error {
	return fmt.Errorf("stop the agentd process directly (Task Manager); then schtasks /End /TN %s", taskName)
}
