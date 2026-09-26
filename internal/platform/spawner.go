// ShimSpawner: the platform.Spawner implementation that starts real
// per-process supervisors (agent-runtime-shim) and reconnects to them.
//
// The daemon's process lifecycle keeps its in-process supervision by
// default (solid, tested); routing exec through this spawner is the
// contained next step: bundle → shim child → Hello → ingest from the
// returned control socket. Reconnect (daemon restart) is fully wired
// today via Reconnect.
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"agent-runtime/internal/shim"
)

// ShimSpawner starts agent-runtime-shim binaries.
type ShimSpawner struct {
	// ShimExe is the shim binary; resolved next to the daemon or on PATH.
	ShimExe string
	// RuntimeDir is $XDG_RUNTIME_DIR/agent-runtime (shim dirs live under it).
	RuntimeDir string
}

// ResolveShimExe finds the shim binary next to the current executable,
// then on PATH.
func ResolveShimExe() string {
	if exe, err := os.Executable(); err == nil {
		for _, name := range []string{"agent-runtime-shim", "agent-runtime-shim.exe"} {
			if cand := filepath.Join(filepath.Dir(exe), name); isExec(cand) {
				return cand
			}
		}
	}
	if p, err := exec.LookPath("agent-runtime-shim"); err == nil {
		return p
	}
	return "agent-runtime-shim"
}

func isExec(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode().Perm()&0o111 != 0
}

// shimHandle is the live handle to one shim.
type shimHandle struct {
	pid      int
	pgid     int
	sock     string
	instance string
}

func (h *shimHandle) PID() int            { return h.pid }
func (h *shimHandle) PGID() int           { return h.pgid }
func (h *shimHandle) ControlAddr() string { return h.sock }

// Spawn writes the bundle, starts the shim detached (setsid), waits for
// the control socket + Hello, and returns the handle.
func (s *ShimSpawner) Spawn(ctx context.Context, bundle []byte) (ShimHandle, error) {
	var b shim.Bundle
	if err := json.Unmarshal(bundle, &b); err != nil {
		return nil, fmt.Errorf("spawner: bad bundle: %w", err)
	}
	if b.InstanceID == "" {
		return nil, fmt.Errorf("spawner: bundle has no instance_id")
	}
	exe := s.ShimExe
	if exe == "" {
		exe = ResolveShimExe()
	}
	dir := filepath.Join(s.RuntimeDir, "shims", b.InstanceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	specPath := filepath.Join(dir, "spec.json")
	if err := shim.SaveBundle(specPath, &b); err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "shim.sock")
	cmd := exec.CommandContext(ctx, exe, "--bundle", specPath, "--control", sock)
	cmd.Dir = "/"
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawner: start shim: %w", err)
	}
	// Don't Wait(): the shim outlives us (linger). Release the process
	// handle; the shim re-parents to init/subeeaper and serves shim.sock.
	// (cmd.Process.Release detaches without SIGKILL.)
	_ = cmd.Process.Release()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		cl, err := shim.Dial(sock, time.Second)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		hello, err := cl.Hello()
		cl.Close()
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return &shimHandle{pid: int(hello.Pid), pgid: int(hello.Pgid), sock: sock, instance: b.InstanceID}, nil
	}
	return nil, fmt.Errorf("spawner: shim %s never served %s", b.InstanceID, sock)
}

// Reconnect dials an existing shim dir and Hellos it.
func (s *ShimSpawner) Reconnect(ctx context.Context, instanceDir string) (ShimHandle, error) {
	sock := filepath.Join(instanceDir, "shim.sock")
	cl, err := shim.Dial(sock, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	hello, err := cl.Hello()
	if err != nil {
		return nil, err
	}
	return &shimHandle{pid: int(hello.Pid), pgid: int(hello.Pgid), sock: sock}, nil
}
