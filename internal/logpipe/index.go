// Per-workspace FTS5 log index (logs/<ws>/index.db, §6.3).
//
// Segments are authoritative; the index accelerates SearchLogs. If the
// indexer falls behind it drops indexing, not logs, and a re-index job
// fills the gap later. Cursors track per-instance ingest positions so
// daemon restarts resume without gaps.
package logpipe

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// WorkspaceIndex is the FTS5 index for one workspace.
type WorkspaceIndex struct {
	db   *sql.DB
	path string
}

// OpenWorkspaceIndex opens (creating) the index.db for a workspace.
func OpenWorkspaceIndex(path string) (*WorkspaceIndex, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, err
		}
	}
	schema := `
CREATE TABLE IF NOT EXISTS lines (
  rowid       INTEGER PRIMARY KEY,
  process_id  TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  ts          INTEGER NOT NULL,
  stream      INTEGER NOT NULL,
  level       INTEGER,
  seg_file    INTEGER NOT NULL DEFAULT 0,
  seg_off     INTEGER NOT NULL DEFAULT 0,
  line        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS lines_proc_seq ON lines(process_id, seq);
CREATE INDEX IF NOT EXISTS lines_proc_ts ON lines(process_id, ts);
-- Idempotency key for imports and re-index jobs: (process_id, seq).
CREATE UNIQUE INDEX IF NOT EXISTS lines_proc_seq_uniq ON lines(process_id, seq);
CREATE VIRTUAL TABLE IF NOT EXISTS lines_fts USING fts5(line, content='lines', content_rowid='rowid',
  tokenize='unicode61 remove_diacritics 2');
CREATE TABLE IF NOT EXISTS cursors (instance_id TEXT PRIMARY KEY, seq INTEGER NOT NULL);
-- External-content FTS5 does not index content-table writes by itself;
-- these triggers keep lines_fts in step with lines.
CREATE TRIGGER IF NOT EXISTS lines_ai AFTER INSERT ON lines BEGIN
  INSERT INTO lines_fts(rowid, line) VALUES (new.rowid, new.line);
END;
CREATE TRIGGER IF NOT EXISTS lines_ad AFTER DELETE ON lines BEGIN
  INSERT INTO lines_fts(lines_fts, rowid, line) VALUES('delete', old.rowid, old.line);
END;
CREATE TRIGGER IF NOT EXISTS lines_au AFTER UPDATE ON lines BEGIN
  INSERT INTO lines_fts(lines_fts, rowid, line) VALUES('delete', old.rowid, old.line);
  INSERT INTO lines_fts(rowid, line) VALUES (new.rowid, new.line);
END;
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("logpipe: index schema: %w", err)
	}
	// FTS5 availability check (modernc builds enable it; fail fast otherwise).
	var fts int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_fts5(?)", "x").Scan(&fts); err != nil {
		// pragma_fts5 doesn't exist as such; verify via sqlite_compileoption.
		var used int
		_ = db.QueryRow("SELECT COUNT(*) FROM pragma_compile_options WHERE compile_options LIKE '%FTS5%'").Scan(&used)
		if used == 0 {
			db.Close()
			return nil, fmt.Errorf("logpipe: sqlite without FTS5")
		}
	}
	return &WorkspaceIndex{db: db, path: path}, nil
}

// Close closes the index.
func (x *WorkspaceIndex) Close() error { return x.db.Close() }

// IndexedLine is one line to index.
type IndexedLine struct {
	ProcessID  string
	InstanceID string
	Seq        uint64
	TS         int64
	Stream     uint8
	Level      *int
	Line       string
}

// AppendBatch inserts lines idempotently by (process_id, seq): re-imports
// and re-index jobs skip rows that already exist.
func (x *WorkspaceIndex) AppendBatch(lines []IndexedLine) error {
	if len(lines) == 0 {
		return nil
	}
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO lines
		(process_id, instance_id, seq, ts, stream, level, seg_file, seg_off, line)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, l := range lines {
		ts := l.TS
		if ts == 0 {
			ts = time.Now().UnixNano()
		}
		var level any
		if l.Level != nil {
			level = *l.Level
		}
		if _, err := stmt.Exec(l.ProcessID, l.InstanceID, l.Seq, ts, l.Stream, level, l.Line); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetCursor records the ingest cursor for an instance.
func (x *WorkspaceIndex) SetCursor(instanceID string, seq uint64) error {
	_, err := x.db.Exec(`INSERT INTO lines (instance_id, seq) VALUES (?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET seq=excluded.seq`, instanceID, seq)
	_ = err
	_, err = x.db.Exec(`INSERT INTO cursors (instance_id, seq) VALUES (?, ?)
		ON CONFLICT(instance_id) DO UPDATE SET seq=excluded.seq`, instanceID, seq)
	return err
}

// Cursor returns the ingest cursor (0 when unknown).
func (x *WorkspaceIndex) Cursor(instanceID string) uint64 {
	var seq uint64
	_ = x.db.QueryRow("SELECT seq FROM cursors WHERE instance_id=?", instanceID).Scan(&seq)
	return seq
}

// SearchResult is one FTS hit with highlights.
type SearchHit struct {
	ProcessID  string
	InstanceID string
	Seq        uint64
	TS         int64
	Stream     uint8
	Line       string
	Rank       float64
}

// Search runs an FTS5 query with process/stream/time filters. maxRows caps
// at 1000; the response flags truncation.
func (x *WorkspaceIndex) Search(query string, processes []string, streams []uint8, from, to int64, maxRows int) ([]SearchHit, bool, error) {
	if maxRows <= 0 || maxRows > 1000 {
		maxRows = 200
	}
	q := `SELECT l.process_id, l.instance_id, l.seq, l.ts, l.stream, l.line, rank
		FROM lines_fts f JOIN lines l ON l.rowid = f.rowid
		WHERE lines_fts MATCH ?`
	args := []any{query}
	if len(processes) > 0 {
		q += ` AND l.process_id IN (` + placeholders(len(processes)) + `)`
		for _, p := range processes {
			args = append(args, p)
		}
	}
	if len(streams) > 0 {
		q += ` AND l.stream IN (` + placeholders(len(streams)) + `)`
		for _, st := range streams {
			args = append(args, st)
		}
	}
	if from > 0 {
		q += ` AND l.ts >= ?`
		args = append(args, from)
	}
	if to > 0 {
		q += ` AND l.ts <= ?`
		args = append(args, to)
	}
	q += ` ORDER BY rank LIMIT ?`
	args = append(args, maxRows+1)
	rows, err := x.db.Query(q, args...)
	if err != nil {
		// FTS syntax errors (unbalanced quotes) degrade to a LIKE scan.
		if strings.Contains(err.Error(), "syntax error") {
			return x.likeSearch(query, processes, maxRows)
		}
		return nil, false, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ProcessID, &h.InstanceID, &h.Seq, &h.TS, &h.Stream, &h.Line, &h.Rank); err != nil {
			return nil, false, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := false
	if len(out) > maxRows {
		out = out[:maxRows]
		truncated = true
	}
	return out, truncated, nil
}

func (x *WorkspaceIndex) likeSearch(query string, processes []string, maxRows int) ([]SearchHit, bool, error) {
	q := `SELECT process_id, instance_id, seq, ts, stream, line FROM lines WHERE line LIKE ?`
	args := []any{"%" + query + "%"}
	if len(processes) > 0 {
		q += ` AND process_id IN (` + placeholders(len(processes)) + `)`
		for _, p := range processes {
			args = append(args, p)
		}
	}
	q += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, maxRows+1)
	rows, err := x.db.Query(q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ProcessID, &h.InstanceID, &h.Seq, &h.TS, &h.Stream, &h.Line); err != nil {
			return nil, false, err
		}
		out = append(out, h)
	}
	truncated := false
	if len(out) > maxRows {
		out = out[:maxRows]
		truncated = true
	}
	return out, truncated, nil
}

func placeholders(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ","
		}
		s += "?"
	}
	return s
}

// DeleteBeforeSeq drops index rows below the first retained segment seq
// (retention janitor keeps index and segments in step).
func (x *WorkspaceIndex) DeleteBeforeSeq(processID string, seq uint64) error {
	_, err := x.db.Exec("DELETE FROM lines WHERE process_id=? AND seq < ?", processID, seq)
	return err
}
