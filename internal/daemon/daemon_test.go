package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// newTestRuntime builds a runtime with a deterministic (shell_env: none)
// config and registers a shutdown cleanup.
func newTestRuntime(t *testing.T) (*runtime.Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	yaml := "runtime:\n  shell_env: none\n"
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(filepath.Join(dir, "agent-runtime.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := runtime.New(loaded, logger)
	t.Cleanup(func() { rt.Shutdown() })
	return rt, dir
}

func TestRPCClientServerRoundTrip(t *testing.T) {
	rt, dir := newTestRuntime(t)
	sock := filepath.Join(dir, "test.sock")
	srv, err := NewServer(rt, sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	client := NewClient(sock)

	// List is empty before any start.
	lst, err := client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lst.Processes) != 0 {
		t.Fatalf("expected empty list, got %d", len(lst.Processes))
	}

	// Start a long-lived process through the client.
	start, err := client.Start(context.Background(), api.StartRequest{
		Command: "sleep", Args: []string{"30"}, WorkDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if start.ProcessID == "" {
		t.Fatal("empty process id")
	}

	// List now shows it, and status is running.
	lst, err = client.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lst.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(lst.Processes))
	}
	st, err := client.Status(start.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "running" {
		t.Fatalf("status = %q, want running", st.Status)
	}

	// GetLogs returns an (empty) result, not an error.
	if _, err := client.GetLogs(api.GetLogsRequest{ProcessID: start.ProcessID}); err != nil {
		t.Fatal(err)
	}

	// Stop through the client; status becomes stopped.
	if err := client.Stop(context.Background(), start.ProcessID); err != nil {
		t.Fatal(err)
	}
	st, err = client.Status(start.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "stopped" && st.Status != "exited" {
		t.Fatalf("status after stop = %q, want stopped/exited", st.Status)
	}
}

// TestClientUnknownMethodAndMissingDaemon verifies error surfacing.
func TestClientErrors(t *testing.T) {
	rt, dir := newTestRuntime(t)
	sock := filepath.Join(dir, "test.sock")
	srv, err := NewServer(rt, sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	client := NewClient(sock)
	if _, err := client.Status("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown process")
	}

	// A client pointed at a dead socket must report "daemon not running".
	dead := NewClient(filepath.Join(dir, "missing.sock"))
	if _, err := dead.List(); err == nil {
		t.Fatal("expected error for missing daemon socket")
	}
}

// TestServerConcurrentClients exercises multiple simultaneous clients on one
// server (Phase 3 multi-client requirement).
func TestServerConcurrentClients(t *testing.T) {
	rt, dir := newTestRuntime(t)
	sock := filepath.Join(dir, "test.sock")
	srv, err := NewServer(rt, sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	const n = 4
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			c := NewClient(sock)
			start, err := c.Start(context.Background(), api.StartRequest{Command: "sleep", Args: []string{"5"}, WorkDir: dir})
			if err != nil {
				done <- err
				return
			}
			time.Sleep(50 * time.Millisecond)
			st, err := c.Status(start.ProcessID)
			if err != nil {
				done <- err
				return
			}
			if st.Status != "running" {
				done <- &unexpectedStatus{st.Status}
				return
			}
			_ = c.Stop(context.Background(), start.ProcessID)
			done <- nil
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("client %d: %v", i, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for concurrent clients")
		}
	}
}

type unexpectedStatus struct{ s string }

func (u *unexpectedStatus) Error() string { return "unexpected status " + u.s }
