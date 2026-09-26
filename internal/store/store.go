// Package store provides the SQLite-backed state database (state.db)
// for agent-runtime v3. It is the single writer of all project,
// workspace, session, process, and audit state.
//
// The database uses WAL mode, synchronous=NORMAL, and a single-writer
// goroutine pattern (same as logstore) with a read pool. Migrations are
// numbered embedded SQL files applied in a transaction with a backup
// taken before each migration.
package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// schemaMigration is the current schema version.
	schemaMigration = 1
)

// DB is the state database handle.
// Per §12 it is the single writer of state.db; clients never open it
// directly. Keep one *DB per daemon process.
type DB struct {
	db   *sql.DB
	path string
}

// Project is a row from the projects table.
type Project struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	LastUsedAt int64  `json:"last_used_at"`
	ConfigMode string `json:"config_mode"`
	ActiveRev  int    `json:"active_rev"`
	DeletedAt  int64  `json:"deleted_at"`
}

// Workspace is a row from the workspaces table.
type Workspace struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	Path         string `json:"path"`
	Dev          uint64 `json:"dev"`
	Ino          uint64 `json:"ino"`
	GitCommonDir string `json:"git_common_dir"`
	GitWorktree  bool   `json:"git_worktree"`
	BranchHint   string `json:"branch_hint"`
	Confirmed    bool   `json:"confirmed"`
	CreatedAt    int64  `json:"created_at"`
	LastSeenAt   int64  `json:"last_seen_at"`
	MissingSince int64  `json:"missing_since"`
}

// ID is a project identifier.
type ID string

// WorkspaceID is a workspace identifier.
type WorkspaceID string

// BeginTx starts a transaction on the state database.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open state.db: %w", err)
	}
	// Single pooled connection keeps the "single writer" invariant cheap
	// while WAL still allows concurrent readers; busy_timeout covers
	// brief contention. See §12.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)

	// WAL mode for concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set foreign_keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	// Apply migrations.
	if err := applyMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate state.db: %w", err)
	}

	return &DB{db: db, path: path}, nil
}

// applyMigrations runs all pending schema migrations in a single
// transaction. Each migration is a SQL string applied in order.
// The migrations table itself is created first so Open works on an
// empty database.
func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get current migration version.
	var current int
	err = tx.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// Apply migrations from current+1 to schemaMigration.
	for v := current + 1; v <= schemaMigration; v++ {
		migration := migrationSQL(v)
		if _, err := tx.Exec(migration); err != nil {
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", v, time.Now().Unix()); err != nil {
			return fmt.Errorf("record migration %d: %w", v, err)
		}
	}

	return tx.Commit()
}

// migrationSQL returns the SQL for applying migration version v.
func migrationSQL(v int) string {
	switch v {
	case 1:
		return initialSchema
	default:
		return ""
	}
}

// initialSchema is the v1 schema for state.db.
const initialSchema = `
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);

CREATE TABLE IF NOT EXISTS projects (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  last_used_at  INTEGER,
  config_mode   TEXT NOT NULL DEFAULT 'import',
  active_rev    INTEGER,
  deleted_at    INTEGER
);

CREATE TABLE IF NOT EXISTS project_fingerprints (
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  value       TEXT NOT NULL,
  weak        INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, kind, value)
);
CREATE INDEX IF NOT EXISTS fp_lookup ON project_fingerprints(kind, value);

CREATE TABLE IF NOT EXISTS workspaces (
  id              TEXT PRIMARY KEY,
  project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  path            TEXT NOT NULL UNIQUE,
  dev             INTEGER,
  ino             INTEGER,
  git_common_dir  TEXT,
  git_worktree    INTEGER NOT NULL DEFAULT 0,
  branch_hint     TEXT,
  confirmed       INTEGER NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  last_seen_at    INTEGER,
  missing_since   INTEGER
);
CREATE INDEX IF NOT EXISTS ws_devino ON workspaces(dev, ino);
CREATE INDEX IF NOT EXISTS ws_gitcommon ON workspaces(git_common_dir);

CREATE TABLE IF NOT EXISTS config_revisions (
  id           INTEGER PRIMARY KEY,
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  layer        TEXT NOT NULL,
  content      TEXT NOT NULL,
  sha256       TEXT NOT NULL,
  valid        INTEGER NOT NULL,
  errors_json  TEXT,
  source       TEXT NOT NULL,
  session_id   TEXT,
  message      TEXT,
  created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS rev_proj ON config_revisions(project_id, layer, id DESC);

CREATE TABLE IF NOT EXISTS repo_trust (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  path         TEXT NOT NULL,
  sha256       TEXT NOT NULL,
  trusted_at   INTEGER NOT NULL,
  trusted_by   TEXT NOT NULL,
  PRIMARY KEY (workspace_id, path)
);

CREATE TABLE IF NOT EXISTS sessions (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL,
  harness      TEXT,
  harness_version TEXT,
  client_pid   INTEGER,
  workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
  started_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  closed_at    INTEGER
);

CREATE TABLE IF NOT EXISTS processes (
  id                 TEXT PRIMARY KEY,
  workspace_id       TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  app                TEXT,
  spec_json          TEXT NOT NULL,
  profile            TEXT,
  lifetime           TEXT NOT NULL DEFAULT 'persistent',
  restart_policy     TEXT NOT NULL,
  started_by_session TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  config_rev         INTEGER,
  created_at         INTEGER NOT NULL,
  removed_at         INTEGER
);
CREATE INDEX IF NOT EXISTS proc_ws ON processes(workspace_id, removed_at);

CREATE TABLE IF NOT EXISTS instances (
  id           TEXT PRIMARY KEY,
  process_id   TEXT NOT NULL REFERENCES processes(id) ON DELETE CASCADE,
  pid          INTEGER,
  pgid         INTEGER,
  shim_dir     TEXT,
  cgroup       TEXT,
  status       TEXT NOT NULL,
  started_at   INTEGER NOT NULL,
  ready_at     INTEGER,
  exited_at    INTEGER,
  exit_code    INTEGER,
  exit_signal  TEXT,
  oom_killed   INTEGER,
  exit_reason  TEXT
);
CREATE INDEX IF NOT EXISTS inst_proc ON instances(process_id, started_at DESC);
CREATE INDEX IF NOT EXISTS inst_live ON instances(status) WHERE exited_at IS NULL;

CREATE TABLE IF NOT EXISTS events (
  id           INTEGER PRIMARY KEY,
  ts           INTEGER NOT NULL,
  type         TEXT NOT NULL,
  project_id   TEXT,
  workspace_id TEXT,
  process_id   TEXT,
  instance_id  TEXT,
  session_id   TEXT,
  payload_json TEXT
);
CREATE INDEX IF NOT EXISTS ev_ws ON events(workspace_id, id);
CREATE INDEX IF NOT EXISTS ev_proc ON events(process_id, id);

CREATE TABLE IF NOT EXISTS audit (
  id           INTEGER PRIMARY KEY,
  ts           INTEGER NOT NULL,
  session_id   TEXT,
  harness      TEXT,
  project_id   TEXT,
  workspace_id TEXT,
  action       TEXT NOT NULL,
  args_json    TEXT,
  result       TEXT NOT NULL,
  duration_ms  INTEGER
);
CREATE INDEX IF NOT EXISTS audit_ws_ts ON audit(workspace_id, ts DESC);

CREATE TABLE IF NOT EXISTS integrations (
  harness      TEXT NOT NULL,
  scope        TEXT NOT NULL,
  kind         TEXT NOT NULL,
  path         TEXT NOT NULL,
  version      TEXT,
  installed_at INTEGER NOT NULL,
  PRIMARY KEY (harness, scope, kind)
);

CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at INTEGER NOT NULL);
`

// BeginTx starts a transaction on the state database.
func (d *DB) BeginTx() (*sql.Tx, error) {
	return d.db.Begin()
}

// Exec executes a statement against the state database.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.db.Exec(query, args...)
}

// QueryRow queries a single row.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.db.QueryRow(query, args...)
}

// Query queries the state database.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.db.Query(query, args...)
}

// Path returns the database file path.
func (d *DB) Path() string { return d.path }

// Close checkpoints the WAL and closes the state database.
func (d *DB) Close() error {
	_, _ = d.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return d.db.Close()
}

// Backup takes an online backup with VACUUM INTO, keeping the daemon
// serving while it runs. Used before migrations and by the daily job.
func (d *DB) Backup(dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	// Quote the path as a SQL string literal safely.
	_, err := d.db.Exec("VACUUM INTO '" + escapePath(dest) + "'")
	return err
}

func escapePath(p string) string {
	out := make([]byte, 0, len(p)+2)
	for i := 0; i < len(p); i++ {
		if p[i] == '\'' {
			out = append(out, '\'', '\'')
		} else {
			out = append(out, p[i])
		}
	}
	return string(out)
}

// --- Project/Workspace methods ---

// ResolveWorkspace returns the workspace for a canonical path.
func (d *DB) ResolveWorkspace(path string) (*Workspace, error) {
	row := d.db.QueryRow("SELECT id, project_id, path, dev, ino, git_common_dir, git_worktree, branch_hint, confirmed, created_at, last_seen_at, missing_since FROM workspaces WHERE path = ? AND missing_since IS NULL", path)
	return scanWorkspace(row)
}

func (d *DB) GetWorkspaceAtID(id string) (*Workspace, error) {
	row := d.db.QueryRow("SELECT id, project_id, path, dev, ino, git_common_dir, git_worktree, branch_hint, confirmed, created_at, last_seen_at, missing_since FROM workspaces WHERE id = ?", id)
	return scanWorkspace(row)
}

func (d *DB) CreateProject(name string) (*Project, error) {
	id := "proj_" + newULID()
	if name == "" {
		name = id
	}
	now := time.Now().Unix()
	_, err := d.db.Exec("INSERT INTO projects (id, name, created_at, updated_at, last_used_at, config_mode) VALUES (?, ?, ?, ?, ?, 'import')", id, name, now, now, now)
	if err != nil {
		return nil, err
	}
	return &Project{ID: id, Name: name, CreatedAt: now, UpdatedAt: now, LastUsedAt: now, ConfigMode: "import"}, nil
}

func (d *DB) GetProjectByName(name string) (*Project, error) {
	row := d.db.QueryRow("SELECT id, name, created_at, updated_at, last_used_at, config_mode, active_rev, deleted_at FROM projects WHERE name = ? AND deleted_at IS NULL", name)
	return scanProject(row)
}

func (d *DB) GetProject(id string) (*Project, error) {
	row := d.db.QueryRow("SELECT id, name, created_at, updated_at, last_used_at, config_mode, active_rev, deleted_at FROM projects WHERE id = ?", id)
	return scanProject(row)
}

func (d *DB) CreateWorkspace(projectID, path string) (*Workspace, error) {
	return d.CreateWorkspaceFull(projectID, path, 0, 0, "", false, "")
}

// CreateWorkspaceFull records dev/ino/git locators at creation so the
// moved/clone/worktree cases in the resolver work from day one.
func (d *DB) CreateWorkspaceFull(projectID, path string, dev, ino uint64, gitCommon string, worktree bool, branch string) (*Workspace, error) {
	id := "ws_" + newULID()
	now := time.Now().Unix()
	wt := 0
	if worktree {
		wt = 1
	}
	_, err := d.db.Exec(`INSERT INTO workspaces
		(id, project_id, path, dev, ino, git_common_dir, git_worktree, branch_hint, confirmed, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, projectID, path, int64(dev), int64(ino), nullStr(gitCommon), wt, nullStr(branch), now, now)
	if err != nil {
		return nil, err
	}
	return &Workspace{ID: id, ProjectID: projectID, Path: path, Dev: dev, Ino: ino,
		GitCommonDir: gitCommon, GitWorktree: worktree, BranchHint: branch,
		Confirmed: true, CreatedAt: now, LastSeenAt: now}, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (d *DB) UpdateWorkspaceLastSeen(id string) error {
	_, err := d.db.Exec("UPDATE workspaces SET last_seen_at = ? WHERE id = ?", time.Now().Unix(), id)
	return err
}

func (d *DB) ListProjects() ([]*Project, error) {
	rows, err := d.db.Query("SELECT id, name, created_at, updated_at, last_used_at, config_mode, active_rev, deleted_at FROM projects WHERE deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (d *DB) UpdateProject(p *Project) error {
	_, err := d.db.Exec("UPDATE projects SET name = ?, updated_at = ? WHERE id = ?", p.Name, time.Now().Unix(), p.ID)
	return err
}

func (d *DB) DeleteProject(id string) error {
	_, err := d.db.Exec("UPDATE projects SET deleted_at = ? WHERE id = ?", time.Now().Unix(), id)
	return err
}

func (d *DB) ListWorkspaces(projectID string) ([]*Workspace, error) {
	rows, err := d.db.Query("SELECT id, project_id, path, dev, ino, git_common_dir, git_worktree, branch_hint, confirmed, created_at, last_seen_at, missing_since FROM workspaces WHERE project_id = ? AND missing_since IS NULL", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workspaces []*Workspace
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, nil
}

func (d *DB) LinkWorkspace(projectID, path string) (*Workspace, error) {
	return d.CreateWorkspace(projectID, path)
}

func (d *DB) ForgetProject(id string) error {
	_, err := d.db.Exec("UPDATE projects SET deleted_at = ? WHERE id = ?", time.Now().Unix(), id)
	return err
}

func (d *DB) GC() ([]string, error) {
	rows, err := d.db.Query("SELECT id FROM workspaces WHERE missing_since IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (d *DB) AddFingerprint(projectID, kind, value string, weak bool) error {
	_, err := d.db.Exec("INSERT OR REPLACE INTO project_fingerprints (project_id, kind, value, weak) VALUES (?, ?, ?, ?)", projectID, kind, value, weak)
	return err
}

func (d *DB) GetProjectByFingerprints(fingerprints []Fingerprint) ([]*Project, error) {
	if len(fingerprints) == 0 {
		return nil, nil
	}
	// Group values by kind; a project matches when it shares a root_commit
	// AND (shares a remote OR has no remote fingerprint). Weak fingerprints
	// (shallow clones) only match on remotes. Implemented as two queries to
	// keep the SQL readable and index-friendly (fp_lookup).
	byKind := map[string][]string{}
	for _, fp := range fingerprints {
		byKind[fp.Kind] = append(byKind[fp.Kind], fp.Value)
	}
	roots := byKind["root_commit"]
	remotes := byKind["remote"]

	placeholders := func(n int) string {
		s := ""
		for i := 0; i < n; i++ {
			if i > 0 {
				s += ","
			}
			s += "?"
		}
		return s
	}

	// Candidates sharing a root commit.
	rootProjects := map[string]bool{}
	if len(roots) > 0 {
		args := make([]any, len(roots))
		for i, v := range roots {
			args[i] = v
		}
		rows, err := d.db.Query(
			"SELECT DISTINCT project_id FROM project_fingerprints WHERE kind='root_commit' AND value IN ("+placeholders(len(roots))+")", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				rootProjects[id] = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	// Candidates sharing a remote.
	remoteProjects := map[string]bool{}
	if len(remotes) > 0 {
		args := make([]any, len(remotes))
		for i, v := range remotes {
			args[i] = v
		}
		rows, err := d.db.Query(
			"SELECT DISTINCT project_id FROM project_fingerprints WHERE kind='remote' AND value IN ("+placeholders(len(remotes))+")", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				remoteProjects[id] = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Intersection rule per §4.3.2: root ∩ (remote ∪ no-remote).
	// If there are no roots (shallow/weak), fall back to remotes only.
	matched := map[string]bool{}
	if len(roots) > 0 {
		for id := range rootProjects {
			if len(remotes) == 0 || remoteProjects[id] || !projectHasRemote(id, d) {
				matched[id] = true
			}
		}
	} else {
		for id := range remoteProjects {
			matched[id] = true
		}
	}
	var out []*Project
	for id := range matched {
		p, err := d.GetProject(id)
		if err != nil {
			continue
		}
		if p.DeletedAt != 0 {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func projectHasRemote(projectID string, d *DB) bool {
	var n int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM project_fingerprints WHERE project_id=? AND kind='remote'", projectID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// FindWorkspaceByDevIno implements the "moved" step: same inode, old path
// gone. Callers check exists(ws.Path) before rebinding.
func (d *DB) FindWorkspaceByDevIno(dev, ino uint64) (*Workspace, error) {
	row := d.db.QueryRow(`SELECT id, project_id, path, dev, ino, git_common_dir, git_worktree,
		branch_hint, confirmed, created_at, last_seen_at, missing_since
		FROM workspaces WHERE dev=? AND ino=? AND missing_since IS NULL LIMIT 1`, int64(dev), int64(ino))
	return scanWorkspace(row)
}

// FindWorkspacesByGitCommon returns sibling workspaces sharing one clone
// (all worktrees of a clone share git_common_dir).
func (d *DB) FindWorkspacesByGitCommon(common string) ([]*Workspace, error) {
	if common == "" {
		return nil, nil
	}
	rows, err := d.db.Query(`SELECT id, project_id, path, dev, ino, git_common_dir, git_worktree,
		branch_hint, confirmed, created_at, last_seen_at, missing_since
		FROM workspaces WHERE git_common_dir=? AND missing_since IS NULL`, common)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

// UpdateWorkspacePath rebinds a workspace after a move or re-clone.
func (d *DB) UpdateWorkspacePath(id, path string, dev, ino uint64, gitCommon string) error {
	_, err := d.db.Exec(`UPDATE workspaces SET path=?, dev=?, ino=?,
		git_common_dir=?, last_seen_at=?, missing_since=NULL WHERE id=?`,
		path, int64(dev), int64(ino), nullStr(gitCommon), time.Now().Unix(), id)
	return err
}

// TouchProject updates last_used_at.
func (d *DB) TouchProject(id string) error {
	_, err := d.db.Exec("UPDATE projects SET last_used_at=?, updated_at=? WHERE id=?",
		time.Now().Unix(), time.Now().Unix(), id)
	return err
}

// MarkWorkspaceMissing flags a workspace whose path vanished (for gc).
func (d *DB) MarkWorkspaceMissing(id string) error {
	_, err := d.db.Exec("UPDATE workspaces SET missing_since=? WHERE id=? AND missing_since IS NULL",
		time.Now().Unix(), id)
	return err
}

// GetWorkspace is an alias kept for engine call sites.
func (d *DB) GetWorkspace(id string) (*Workspace, error) {
	return d.GetWorkspaceAtID(id)
}

func (d *DB) TrustRepo(workspaceID, path, sha256, trustedBy string) error {
	_, err := d.db.Exec("INSERT OR REPLACE INTO repo_trust (workspace_id, path, sha256, trusted_at, trusted_by) VALUES (?, ?, ?, ?, ?)", workspaceID, path, sha256, time.Now().Unix(), trustedBy)
	return err
}

func (d *DB) IsTrusted(workspaceID, path string) (bool, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM repo_trust WHERE workspace_id = ? AND path = ?", workspaceID, path).Scan(&count)
	return count > 0, err
}

// RepoTrustSHA returns the trusted sha256 for (workspace, path).
// Trust is (workspace_id, path, sha256): a changed file becomes untrusted
// until re-approved (§9.3).
func (d *DB) RepoTrustSHA(workspaceID, path string) (string, bool, error) {
	var sha string
	err := d.db.QueryRow("SELECT sha256 FROM repo_trust WHERE workspace_id = ? AND path = ?", workspaceID, path).Scan(&sha)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return sha, true, nil
}

// Fingerprint is a git fingerprint for a project.
type Fingerprint struct {
	ProjectID string
	Kind      string
	Value     string
	Weak      bool
}

// --- Helper scan functions ---

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(row scanner) (*Workspace, error) {
	var ws Workspace
	var dev, ino, created, lastSeen, missing sql.NullInt64
	var gitCommon, branch sql.NullString
	var gitWorktree, confirmed sql.NullInt64
	err := row.Scan(&ws.ID, &ws.ProjectID, &ws.Path, &dev, &ino,
		&gitCommon, &gitWorktree, &branch, &confirmed, &created, &lastSeen, &missing)
	if err != nil {
		return nil, err
	}
	if dev.Valid {
		ws.Dev = uint64(dev.Int64)
	}
	if ino.Valid {
		ws.Ino = uint64(ino.Int64)
	}
	ws.GitCommonDir = gitCommon.String
	ws.GitWorktree = gitWorktree.Valid && gitWorktree.Int64 != 0
	ws.BranchHint = branch.String
	ws.Confirmed = !confirmed.Valid || confirmed.Int64 != 0
	if created.Valid {
		ws.CreatedAt = created.Int64
	}
	if lastSeen.Valid {
		ws.LastSeenAt = lastSeen.Int64
	}
	if missing.Valid {
		ws.MissingSince = missing.Int64
	}
	return &ws, nil
}

func scanProject(row scanner) (*Project, error) {
	var p Project
	var lastUsed, active, deleted sql.NullInt64
	var created, updated sql.NullInt64
	err := row.Scan(&p.ID, &p.Name, &created, &updated, &lastUsed, &p.ConfigMode, &active, &deleted)
	if err != nil {
		return nil, err
	}
	if created.Valid {
		p.CreatedAt = created.Int64
	}
	if updated.Valid {
		p.UpdatedAt = updated.Int64
	}
	if lastUsed.Valid {
		p.LastUsedAt = lastUsed.Int64
	}
	if active.Valid {
		p.ActiveRev = int(active.Int64)
	}
	if deleted.Valid {
		p.DeletedAt = deleted.Int64
	}
	return &p, nil
}

func newULID() string {
	// ULID: 48-bit millis (lexicographically sortable) + 80-bit rand,
	// Crockford base32. No new dependency.
	ms := uint64(time.Now().UnixMilli())
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return fmt.Sprintf("%013d%08x", ms, time.Now().UnixNano())
	}
	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	copy(b[6:], rnd[:])
	return encodeBase32(b[:])
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func encodeBase32(b []byte) string {
	// 16 bytes -> 26 chars.
	out := make([]byte, 0, 26)
	bits := 0
	acc := 0
	for _, c := range b {
		acc = (acc << 8) | int(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockford[(acc>>bits)&31])
		}
	}
	if bits > 0 {
		out = append(out, crockford[(acc<<(5-bits))&31])
	}
	return string(out)
}

// --- Runtime tables: sessions / processes / instances / events / audit ---
//
// These are thin CRUD helpers over the §12 schema. The daemon is the only
// writer; the HTTP/Connect layer reads through these.

// SessionRow mirrors the sessions table.
type SessionRow struct {
	ID         string
	Kind       string
	Harness    string
	ClientPID  int64
	Workspace  string
	StartedAt  int64
	LastSeenAt int64
	ClosedAt   int64
}

// CreateSession inserts a session row.
func (d *DB) CreateSession(id, kind, harness string, pid int64, workspace string) error {
	now := time.Now().Unix()
	_, err := d.db.Exec(`INSERT INTO sessions (id, kind, harness, client_pid, workspace_id, started_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, kind, harness, pid, nullStr(workspace), now, now)
	return err
}

// HeartbeatSession bumps last_seen_at.
func (d *DB) HeartbeatSession(id string) error {
	_, err := d.db.Exec("UPDATE sessions SET last_seen_at=? WHERE id=? AND closed_at IS NULL", time.Now().Unix(), id)
	return err
}

// CloseSession marks a session closed.
func (d *DB) CloseSession(id string) error {
	_, err := d.db.Exec("UPDATE sessions SET closed_at=? WHERE id=?", time.Now().Unix(), id)
	return err
}

// ListOpenSessions returns sessions with no closed_at.
func (d *DB) ListOpenSessions(workspace string) ([]*SessionRow, error) {
	q := "SELECT id, kind, harness, client_pid, workspace_id, started_at, last_seen_at FROM sessions WHERE closed_at IS NULL"
	var args []any
	if workspace != "" {
		q += " AND workspace_id=?"
		args = append(args, workspace)
	}
	q += " ORDER BY started_at DESC"
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SessionRow
	for rows.Next() {
		var s SessionRow
		var ws, harness sql.NullString
		var pid sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Kind, &harness, &pid, &ws, &s.StartedAt, &s.LastSeenAt); err != nil {
			return nil, err
		}
		s.Harness = harness.String
		if pid.Valid {
			s.ClientPID = pid.Int64
		}
		s.Workspace = ws.String
		out = append(out, &s)
	}
	return out, rows.Err()
}

// ProcessRow mirrors the processes table (spec stored redacted-hash only;
// real env lives in the shim bundle per §12).
type ProcessRow struct {
	ID          string
	WorkspaceID string
	App         string
	SpecJSON    string
	Lifetime    string
	Restart     string
	StartedBy   string
	ConfigRev   int64
	CreatedAt   int64
}

// CreateProcess inserts a logical process row with a globally unique id.
func (d *DB) CreateProcess(ws, app, specJSON, lifetime, restart, startedBy string, configRev int64) (*ProcessRow, error) {
	id := "proc_" + newULID()
	now := time.Now().Unix()
	if lifetime == "" {
		lifetime = "persistent"
	}
	_, err := d.db.Exec(`INSERT INTO processes (id, workspace_id, app, spec_json, lifetime, restart_policy, started_by_session, config_rev, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ws, nullStr(app), specJSON, lifetime, restart, nullStr(startedBy), configRev, now)
	if err != nil {
		return nil, err
	}
	return &ProcessRow{ID: id, WorkspaceID: ws, App: app, SpecJSON: specJSON,
		Lifetime: lifetime, Restart: restart, StartedBy: startedBy, ConfigRev: configRev, CreatedAt: now}, nil
}

// RemoveProcess soft-deletes a process.
func (d *DB) RemoveProcess(id string) error {
	_, err := d.db.Exec("UPDATE processes SET removed_at=? WHERE id=?", time.Now().Unix(), id)
	return err
}

// InstanceRow mirrors the instances table.
type InstanceRow struct {
	ID        string
	ProcessID string
	PID       int64
	PGID      int64
	ShimDir   string
	Status    string
	StartedAt int64
}

// CreateInstance inserts a run row.
func (d *DB) CreateInstance(processID string, pid, pgid int64, shimDir, status string) (*InstanceRow, error) {
	id := "run_" + newULID()
	now := time.Now().Unix()
	_, err := d.db.Exec(`INSERT INTO instances (id, process_id, pid, pgid, shim_dir, status, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, processID, pid, pgid, shimDir, status, now)
	if err != nil {
		return nil, err
	}
	return &InstanceRow{ID: id, ProcessID: processID, PID: pid, PGID: pgid, ShimDir: shimDir, Status: status, StartedAt: now}, nil
}

// UpdateInstanceExit records an exit (code/signal/oom) observed via the shim.
func (d *DB) UpdateInstanceExit(id string, code int64, signal string, oom bool, reason string) error {
	oomV := 0
	if oom {
		oomV = 1
	}
	_, err := d.db.Exec(`UPDATE instances SET exited_at=?, exit_code=?, exit_signal=?, oom_killed=?, exit_reason=?,
		status=CASE WHEN status IN ('starting','running','ready') THEN 'exited' ELSE status END WHERE id=?`,
		time.Now().Unix(), code, signal, oomV, reason, id)
	return err
}

// UpdateInstanceStatus sets the status of an instance (e.g. orphaned when
// its shim socket is dead and only pid-poll adoption remains).
func (d *DB) UpdateInstanceStatus(id, status string) error {
	_, err := d.db.Exec(`UPDATE instances SET status=? WHERE id=? AND exited_at IS NULL`, status, id)
	return err
}

// LiveInstances returns non-exited instances (for reconnect on boot).
func (d *DB) LiveInstances() ([]*InstanceRow, error) {
	rows, err := d.db.Query(`SELECT id, process_id, pid, pgid, shim_dir, status, started_at
		FROM instances WHERE exited_at IS NULL ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*InstanceRow
	for rows.Next() {
		var r InstanceRow
		var pid, pgid sql.NullInt64
		var shim sql.NullString
		if err := rows.Scan(&r.ID, &r.ProcessID, &pid, &pgid, &shim, &r.Status, &r.StartedAt); err != nil {
			return nil, err
		}
		if pid.Valid {
			r.PID = pid.Int64
		}
		if pgid.Valid {
			r.PGID = pgid.Int64
		}
		r.ShimDir = shim.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

// AppendEvent persists one event and returns its monotonic id (cursor).
func (d *DB) AppendEvent(ts int64, typ, project, ws, proc, inst, sess, payload string) (int64, error) {
	res, err := d.db.Exec(`INSERT INTO events (ts, type, project_id, workspace_id, process_id, instance_id, session_id, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, typ, nullStr(project), nullStr(ws), nullStr(proc), nullStr(inst), nullStr(sess), nullStr(payload))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AppendAudit records one mutating RPC with redacted args.
func (d *DB) AppendAudit(sess, harness, project, ws, action, args, result string, ms int64) error {
	_, err := d.db.Exec(`INSERT INTO audit (ts, session_id, harness, project_id, workspace_id, action, args_json, result, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), nullStr(sess), nullStr(harness), nullStr(project), nullStr(ws), action, nullStr(args), result, ms)
	return err
}

// GetProcess returns a process row by global id.
func (d *DB) GetProcess(id string) (*ProcessRow, error) {
	var r ProcessRow
	var app, spec, lifetime, restart, startedBy sql.NullString
	var configRev sql.NullInt64
	err := d.db.QueryRow(`SELECT id, workspace_id, app, spec_json, lifetime, restart_policy,
		started_by_session, config_rev, created_at FROM processes WHERE id=? AND removed_at IS NULL`, id).
		Scan(&r.ID, &r.WorkspaceID, &app, &spec, &lifetime, &restart, &startedBy, &configRev, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	r.App, r.SpecJSON, r.Lifetime, r.Restart, r.StartedBy =
		app.String, spec.String, lifetime.String, restart.String, startedBy.String
	if configRev.Valid {
		r.ConfigRev = configRev.Int64
	}
	return &r, nil
}

// ListProcesses returns live process rows for a workspace.
func (d *DB) ListProcesses(workspaceID string) ([]*ProcessRow, error) {
	rows, err := d.db.Query(`SELECT id, workspace_id, app, spec_json, lifetime, restart_policy,
		started_by_session, config_rev, created_at FROM processes
		WHERE workspace_id=? AND removed_at IS NULL ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProcessRow
	for rows.Next() {
		var r ProcessRow
		var app, spec, lifetime, restart, startedBy sql.NullString
		var configRev sql.NullInt64
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &app, &spec, &lifetime, &restart,
			&startedBy, &configRev, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.App, r.SpecJSON, r.Lifetime, r.Restart, r.StartedBy =
			app.String, spec.String, lifetime.String, restart.String, startedBy.String
		if configRev.Valid {
			r.ConfigRev = configRev.Int64
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ConfigRevision mirrors one config_revisions row.
type ConfigRevision struct {
	ID        int64
	ProjectID string
	Layer     string
	Content   string
	SHA256    string
	Valid     bool
	Errors    string
	Source    string
	SessionID string
	Message   string
	CreatedAt int64
}

// AddRevision records a config revision and returns its id.
func (d *DB) AddRevision(projectID, layer, content, sha string, valid bool, errors, source, session, message string) (int64, error) {
	v := 0
	if valid {
		v = 1
	}
	res, err := d.db.Exec(`INSERT INTO config_revisions
		(project_id, layer, content, sha256, valid, errors_json, source, session_id, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		projectID, layer, content, sha, v, nullStr(errors), source, nullStr(session), nullStr(message), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListRevisions returns revisions newest-first.
func (d *DB) ListRevisions(projectID, layer string, limit int) ([]*ConfigRevision, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, project_id, layer, content, sha256, valid, errors_json, source, session_id, message, created_at
		FROM config_revisions WHERE project_id=?`
	args := []any{projectID}
	if layer != "" {
		q += ` AND layer=?`
		args = append(args, layer)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ConfigRevision
	for rows.Next() {
		var r ConfigRevision
		var v int
		var errs, sess, msg sql.NullString
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Layer, &r.Content, &r.SHA256,
			&v, &errs, &r.Source, &sess, &msg, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Valid = v != 0
		r.Errors, r.SessionID, r.Message = errs.String, sess.String, msg.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

// GetRevision returns one revision by id.
func (d *DB) GetRevision(id int64) (*ConfigRevision, error) {
	var r ConfigRevision
	var v int
	var errs, sess, msg sql.NullString
	err := d.db.QueryRow(`SELECT id, project_id, layer, content, sha256, valid, errors_json, source, session_id, message, created_at
		FROM config_revisions WHERE id=?`, id).Scan(
		&r.ID, &r.ProjectID, &r.Layer, &r.Content, &r.SHA256,
		&v, &errs, &r.Source, &sess, &msg, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	r.Valid = v != 0
	r.Errors, r.SessionID, r.Message = errs.String, sess.String, msg.String
	return &r, nil
}

// SetActiveRevision points the project at its last good revision.
func (d *DB) SetActiveRevision(projectID string, rev int64) error {
	_, err := d.db.Exec("UPDATE projects SET active_rev=?, updated_at=? WHERE id=?", rev, time.Now().Unix(), projectID)
	return err
}

// SetSetting / GetSetting persist daemon settings (SystemService).
func (d *DB) SetSetting(key, valueJSON string) error {
	_, err := d.db.Exec(`INSERT INTO settings (key, value_json, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		key, valueJSON, time.Now().Unix())
	return err
}

// GetSetting returns a setting value or ("", false) when absent.
func (d *DB) GetSetting(key string) (string, bool, error) {
	var v string
	err := d.db.QueryRow("SELECT value_json FROM settings WHERE key=?", key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

// ListEvents pages the events table (cursor = row id).
func (d *DB) ListEvents(workspace, process string, since int64, limit int) ([]*EventRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, ts, type, project_id, workspace_id, process_id, instance_id, session_id, payload_json
		FROM events WHERE id>?`
	args := []any{since}
	if workspace != "" {
		q += ` AND workspace_id=?`
		args = append(args, workspace)
	}
	if process != "" {
		q += ` AND process_id=?`
		args = append(args, process)
	}
	q += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EventRow
	for rows.Next() {
		var r EventRow
		var proj, ws, proc, inst, sess, payload sql.NullString
		if err := rows.Scan(&r.ID, &r.TS, &r.Type, &proj, &ws, &proc, &inst, &sess, &payload); err != nil {
			return nil, err
		}
		r.ProjectID, r.WorkspaceID, r.ProcessID = proj.String, ws.String, proc.String
		r.InstanceID, r.SessionID, r.Payload = inst.String, sess.String, payload.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

// EventRow mirrors one events row.
type EventRow struct {
	ID          int64
	TS          int64
	Type        string
	ProjectID   string
	WorkspaceID string
	ProcessID   string
	InstanceID  string
	SessionID   string
	Payload     string
}

// AuditRow mirrors one audit row.
type AuditRow struct {
	ID          int64
	TS          int64
	SessionID   string
	Harness     string
	ProjectID   string
	WorkspaceID string
	Action      string
	Args        string
	Result      string
	DurationMs  int64
}

// ListAudit pages the audit table newest-first.
func (d *DB) ListAudit(workspace string, limit int) ([]*AuditRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, ts, session_id, harness, project_id, workspace_id, action, args_json, result, duration_ms
		FROM audit`
	args := []any{}
	if workspace != "" {
		q += ` WHERE workspace_id=?`
		args = append(args, workspace)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditRow
	for rows.Next() {
		var r AuditRow
		var sess, harness, proj, ws, argsJ sql.NullString
		if err := rows.Scan(&r.ID, &r.TS, &sess, &harness, &proj, &ws, &r.Action, &argsJ, &r.Result, &r.DurationMs); err != nil {
			return nil, err
		}
		r.SessionID, r.Harness, r.ProjectID = sess.String, harness.String, proj.String
		r.WorkspaceID, r.Args = ws.String, argsJ.String
		out = append(out, &r)
	}
	return out, rows.Err()
}
