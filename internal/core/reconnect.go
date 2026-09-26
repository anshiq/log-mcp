// Boot reconnection: after a daemon crash, restart or upgrade, re-attach
// to every running shim with full observability (no log gap, no unknown
// exit codes). For each live instance: Hello the shim socket, record
// exits that happened while down, and resume ingest from the index
// cursor. Dead shim sockets degrade to orphan polling (pid-poll adopt).
package core

import (
	"os"
	"path/filepath"
	"time"

	"agent-runtime/internal/logpipe"
	"agent-runtime/internal/shim"
)

// ReconnectReport summarizes boot reconnection.
type ReconnectReport struct {
	Reconnected []string
	Exited      []string
	Orphaned    []string
}

// ReconnectShims scans live instances and re-attaches to their shims.
// It runs at daemon boot before serving.
func (e *Engine) ReconnectShims(runtimeDir string) ReconnectReport {
	var rep ReconnectReport
	lives, err := e.store.LiveInstances()
	if err != nil {
		return rep
	}
	seen := map[string]bool{}
	for _, inst := range lives {
		sock := filepath.Join(inst.ShimDir, "shim.sock")
		if inst.ShimDir == "" {
			sock = filepath.Join(runtimeDir, "shims", inst.ID, "shim.sock")
		}
		seen[sock] = true
		cl, err := shim.Dial(sock, 3*time.Second)
		if err != nil {
			// Dead shim socket: orphan (signal + pid-poll fallback).
			_ = e.store.UpdateInstanceStatus(inst.ID, "orphaned")
			rep.Orphaned = append(rep.Orphaned, inst.ID)
			continue
		}
		hello, err := cl.Hello()
		cl.Close()
		if err != nil {
			_ = e.store.UpdateInstanceStatus(inst.ID, "orphaned")
			rep.Orphaned = append(rep.Orphaned, inst.ID)
			continue
		}
		if hello.Exit != nil {
			// Exited while the daemon was down: the shim held the exit.
			ex := hello.Exit
			_ = e.store.UpdateInstanceExit(inst.ID, int64(ex.Code), ex.Signal, ex.OomKilled, "daemon-down")
			proc, _ := e.store.GetProcess(inst.ProcessID)
			ws, proj := "", ""
			if proc != nil {
				ws, proj = proc.WorkspaceID, ""
				if w, err := e.store.GetWorkspace(proc.WorkspaceID); err == nil {
					proj = w.ProjectID
				}
			}
			_, _ = e.store.AppendEvent(time.Now().UnixNano(), "process.exited",
				proj, ws, inst.ProcessID, inst.ID, "", "reconnect")
			rep.Exited = append(rep.Exited, inst.ID)
			continue
		}
		// Running: resume ingest from the index cursor (no gaps).
		if proc, err := e.store.GetProcess(inst.ProcessID); err == nil {
			idxPath := filepath.Join(e.dataDir, "logs", proc.WorkspaceID, "index.db")
			if idx, err := logpipe.OpenWorkspaceIndex(idxPath); err == nil {
				_ = idx.SetCursor(inst.ID, uint64(hello.LastSeq))
				idx.Close()
			}
		}
		rep.Reconnected = append(rep.Reconnected, inst.ID)
	}
	// Shim dirs on disk unknown to the DB: orphaned pre-v3 strays.
	entries, _ := os.ReadDir(filepath.Join(runtimeDir, "shims"))
	for _, en := range entries {
		sock := filepath.Join(runtimeDir, "shims", en.Name(), "shim.sock")
		if !seen[sock] {
			if _, err := os.Stat(sock); err == nil {
				rep.Orphaned = append(rep.Orphaned, en.Name())
			}
		}
	}
	return rep
}
