//go:build !windows

package process_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agent-runtime/internal/process"
)

func TestGroupKillTerminatesGrandchildren(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("process-group semantics vary in CI sandboxes")
	}
	m := process.New(context.Background(), process.Options{DefaultGrace: 200 * time.Millisecond})
	proc, err := m.Start(context.Background(), helperCommand(t, "spawn"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	var childPid int
	waitFor(t, 5*time.Second, "CHILD= line", func() bool {
		for _, e := range allStdout(proc) {
			if strings.HasPrefix(e.Line, "CHILD=") {
				childPid, _ = strconv.Atoi(strings.TrimPrefix(e.Line, "CHILD="))
				return true
			}
		}
		return false
	})
	if childPid <= 0 {
		t.Fatal("no grandchild pid captured")
	}

	if err := m.Stop(context.Background(), proc.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "grandchild reaped", func() bool {
		err := syscall.Kill(childPid, 0)
		return err == syscall.ESRCH
	})
}

func TestSignalByName(t *testing.T) {
	cases := []struct {
		name string
		want syscall.Signal
	}{
		{"SIGINT", syscall.SIGINT},
		{"INT", syscall.SIGINT},
		{"sigterm", syscall.SIGTERM},
		{"TERM", syscall.SIGTERM},
		{"SIGHUP", syscall.SIGHUP},
		{"QUIT", syscall.SIGQUIT},
		{"SIGUSR1", syscall.SIGUSR1},
		{"usr2", syscall.SIGUSR2},
		{"SIGKILL", syscall.SIGKILL},
		{"kill", syscall.SIGKILL},
	}
	for _, c := range cases {
		got, err := process.SignalByName(c.name)
		if err != nil {
			t.Fatalf("SignalByName(%q): %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("SignalByName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
	bad := []string{"", "SIG", "SIGFOO", "SEGV", "ALRM", "sigstop"}
	for _, name := range bad {
		if _, err := process.SignalByName(name); err == nil {
			t.Fatalf("SignalByName(%q): expected error", name)
		}
	}
}

// TestSignalKillTerminates verifies that a SIGKILL delivered via Signal kills
// a running (even SIGTERM-ignoring) helper without Stop's escalation machinery.
func TestSignalKillTerminates(t *testing.T) {
	m := newManager(t)
	proc, err := m.Start(context.Background(), helperCommand(t, "ignore-term"), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "running", func() bool { return proc.Info().Status == process.StatusRunning })

	if err := m.Signal(context.Background(), proc.ID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not die after SIGKILL via Signal")
	}
	info := proc.Info()
	if info.Status != process.StatusExited {
		t.Fatalf("status = %q, want exited", info.Status)
	}
	if info.ExitCode == nil || *info.ExitCode != -1 {
		t.Fatalf("killed exit code = %v, want -1 (signal)", info.ExitCode)
	}
}
