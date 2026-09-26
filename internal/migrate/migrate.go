// Package migrate moves existing users to v3 (§14): resolve → trust
// prompt → config import → legacy logs.db/audit.log import → v2 daemon
// handover (legacy adopt path, processes badged until restarted).
package migrate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
	"agent-runtime/internal/logpipe"
	"agent-runtime/internal/store"

	_ "modernc.org/sqlite"
)

// Options controls a migration run.
type Options struct {
	DryRun   bool
	AllKnown bool
	Cleanup  bool
	Yes      bool // trust repo configs non-interactively
}

// Report summarizes a migration run.
type Report struct {
	Dirs            []string
	ProjectsCreated int
	ConfigsImported int
	LogsImported    int64
	AuditImported   int
	DaemonsStopped  int
	Skipped         []string
}

// Migrate migrates one workspace directory into the v3 store.
func Migrate(eng *core.Engine, dataDir, dir string, opts Options) (*Report, error) {
	rep := &Report{Dirs: []string{dir}}
	pid, wid, err := eng.ResolveWorkspace(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	ws, err := eng.Store().GetWorkspace(string(wid))
	if err != nil {
		return nil, err
	}
	// 1. Repo config import (trust-gated).
	if imported, err := importRepoConfig(eng, dataDir, string(pid), ws, opts); err != nil {
		rep.Skipped = append(rep.Skipped, fmt.Sprintf("config: %v", err))
	} else if imported {
		rep.ConfigsImported++
	}
	// 2. Legacy archive import.
	n, err := importLegacyLogs(eng, dataDir, string(wid), dir, opts)
	if err != nil {
		rep.Skipped = append(rep.Skipped, fmt.Sprintf("logs: %v", err))
	} else {
		rep.LogsImported += n
	}
	// 3. Legacy audit import.
	m, err := importLegacyAudit(eng, dir, opts)
	if err != nil {
		rep.Skipped = append(rep.Skipped, fmt.Sprintf("audit: %v", err))
	} else {
		rep.AuditImported += m
	}
	// 4. v2 daemon handover.
	stopped, err := handoverV2Daemon(dir, opts)
	if err != nil {
		rep.Skipped = append(rep.Skipped, fmt.Sprintf("handover: %v", err))
	} else if stopped {
		rep.DaemonsStopped++
	}
	return rep, nil
}

func importRepoConfig(eng *core.Engine, dataDir, projectID string, ws *store.Workspace, opts Options) (bool, error) {
	ep := config.DiscoverEffective(dataDir, projectID, ws.ID, ws.Path)
	if ep.Repo == "" {
		return false, nil
	}
	data, err := os.ReadFile(ep.Repo)
	if err != nil {
		return false, err
	}
	sha := config.SHA256(data)
	if trusted, _ := eng.Store().IsTrusted(ws.ID, ep.Repo); trusted {
		if have, found, _ := eng.Store().RepoTrustSHA(ws.ID, ep.Repo); found && have == sha {
			return false, nil // already imported + trusted
		}
	}
	if !opts.Yes && !opts.DryRun {
		return false, fmt.Errorf("repo config %s needs trust: run `agent-runtime project trust` or migrate --yes", ep.Repo)
	}
	if opts.DryRun {
		return true, nil
	}
	target := ep.Project
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return false, err
	}
	by := "migrate"
	if _, err := eng.Store().AddRevision(projectID, "project", string(data), sha, true, "", "import", "", "imported-from-repo"); err != nil {
		return false, err
	}
	if err := eng.Store().TrustRepo(ws.ID, ep.Repo, sha, by); err != nil {
		return false, err
	}
	return true, nil
}

// legacyEntry mirrors one v2 logstore entries row.
type legacyEntry struct {
	processID string
	id        int64
	ts        int64
	stream    string
	line      string
}

// importLegacyLogs copies <root>/.agent-runtime/logs.db entries into the
// workspace FTS index (bounded, resumable, idempotent by (process_id,
// seq)). Adoptable open instances are registered as legacy processes.
func importLegacyLogs(eng *core.Engine, dataDir, workspaceID, root string, opts Options) (int64, error) {
	legacy := filepath.Join(root, ".agent-runtime", "logs.db")
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return 0, nil
	}
	db, err := sql.Open("sqlite", legacy)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT process_id, id, ts, stream, line FROM entries ORDER BY process_id, id LIMIT 100000")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	idx, err := logpipe.OpenWorkspaceIndex(filepath.Join(dataDir, "logs", workspaceID, "index.db"))
	if err != nil {
		return 0, err
	}
	defer idx.Close()
	var batch []logpipe.IndexedLine
	var total int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if !opts.DryRun {
			if err := idx.AppendBatch(batch); err != nil {
				return err
			}
		}
		total += int64(len(batch))
		batch = nil
		return nil
	}
	procs := map[string]bool{}
	for rows.Next() {
		var e legacyEntry
		if err := rows.Scan(&e.processID, &e.id, &e.ts, &e.stream, &e.line); err != nil {
			break
		}
		batch = append(batch, logpipe.IndexedLine{
			ProcessID: e.processID, InstanceID: "legacy", Seq: uint64(e.id),
			TS: e.ts, Stream: streamNum(e.stream), Line: e.line,
		})
		procs[e.processID] = true
		if len(batch) >= 1000 {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := flush(); err != nil {
		return total, err
	}
	// Register legacy process rows so adopted pids have identity.
	if !opts.DryRun {
		for pid := range procs {
			_, _ = eng.Store().CreateProcess(pid, workspaceID, "", `{"legacy":true}`,
				"persistent", "", "", 0)
		}
	}
	return total, nil
}

func streamNum(s string) uint8 {
	switch s {
	case "stderr":
		return 2
	case "pty":
		return 3
	case "system":
		return 4
	}
	return 1
}

// importLegacyAudit copies <root>/.agent-runtime/audit.log (JSONL) into
// the audit table.
func importLegacyAudit(eng *core.Engine, root string, opts Options) (int, error) {
	path := filepath.Join(root, ".agent-runtime", "audit.log")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e struct {
			Tool   string         `json:"tool"`
			Args   map[string]any `json:"args"`
			Result string         `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if !opts.DryRun {
			args, _ := json.Marshal(e.Args)
			_ = eng.Store().AppendAudit("", "", "", "", e.Tool, string(args), e.Result, 0)
		}
		n++
	}
	return n, nil
}

// handoverV2Daemon adopts a running v2 per-project daemon's processes via
// the legacy pid-poll path and SIGTERMs it. Adopted processes show the
// "legacy: restart for full capture" badge until restarted.
func handoverV2Daemon(root string, opts Options) (bool, error) {
	pidPath := filepath.Join(root, ".agent-runtime", "daemon.pid")
	data, err := os.ReadFile(pidPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, nil
	}
	if !pidAlive(pid) {
		return false, nil
	}
	if !isOurBinary(pid) {
		return false, nil // not an agent-runtime daemon
	}
	if opts.DryRun {
		return true, nil
	}
	// Handover marker for patched v2.x daemons (leave processes running),
	// then SIGTERM.
	_ = os.WriteFile(filepath.Join(root, ".agent-runtime", "handover"), []byte("v3\n"), 0o600)
	if err := signalTerm(pid); err != nil {
		return false, err
	}
	return true, nil
}

// FindKnownDirs scans harness configs + recent dirs for v3-migratable
// projects (agent-runtime.yaml files to import).
func FindKnownDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}
	home, _ := os.UserHomeDir()
	// Harness config files that reference project paths.
	candidates := []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".config", "opencode", "opencode.json"),
	}
	for _, f := range candidates {
		for _, dir := range extractDirs(f) {
			if hasRepoConfig(dir) {
				add(dir)
			}
		}
	}
	// $HOME top-level guesses are intentionally not walked: migration
	// stays explicit (migrate <dir>) or harness-driven.
	_ = time.Now
	return out
}

func hasRepoConfig(dir string) bool {
	for _, n := range []string{"agent-runtime.yaml", "agent-runtime.yml"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// extractDirs pulls quoted absolute paths out of a config file.
func extractDirs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, tok := range strings.FieldsFunc(string(data), func(c rune) bool {
		return c == '"' || c == '\'' || c == '\n' || c == ',' || c == ' ' || c == '\t'
	}) {
		if filepath.IsAbs(tok) {
			if st, err := os.Stat(tok); err == nil && st.IsDir() {
				out = append(out, tok)
			}
		}
	}
	return out
}
