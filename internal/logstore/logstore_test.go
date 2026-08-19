package logstore

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-runtime/internal/logs"
)

// newTestSink opens a SQLite archive in a temp dir with the default batching
// parameters.
func newTestSink(t *testing.T) *SQLite {
	t.Helper()
	s, err := Open(t.TempDir()+"/logs.db", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// appendLine appends one stdout entry with the given ID and line.
func appendLine(s *SQLite, procID string, id uint64, line string) {
	s.Append(procID, logs.Entry{
		ID:        id,
		Timestamp: time.Now(),
		Stream:    logs.StreamStdout,
		Line:      line,
	})
}

func TestRoundTrip(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const proc = "proc_1"
	for i := 0; i < 50; i++ {
		appendLine(s, proc, uint64(i), fmt.Sprintf("line-%d", i))
	}
	// Closing flushes the writer and is terminal; the rows must be persisted
	// to the file. Reopen the same path with a fresh sink to query them.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	entries, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 50 {
		t.Fatalf("got %d entries, want 50", len(entries))
	}
	// Newest-last by ID.
	if entries[0].ID != 0 || entries[49].ID != 49 {
		t.Fatalf("IDs not preserved in order: first=%d last=%d", entries[0].ID, entries[49].ID)
	}
	if entries[0].Line != "line-0" || entries[49].Line != "line-49" {
		t.Fatalf("lines wrong: %q .. %q", entries[0].Line, entries[49].Line)
	}
}

func TestReopenPersistence(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const proc = "proc_persist"
	appendLine(s, proc, 0, "hello")
	appendLine(s, proc, 1, "world")
	ex := int64(1234)
	code := int64(0)
	if err := s.RecordInstance(proc, "run_1", "echo", "/tmp", "generic", 100, 1234, &ex, &code); err != nil {
		t.Fatalf("RecordInstance: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen the same file: the rows must survive (crash-survival story).
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	entries, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 10})
	if err != nil {
		t.Fatalf("Query after reopen: %v", err)
	}
	if len(entries) != 2 || entries[0].Line != "hello" || entries[1].Line != "world" {
		t.Fatalf("reopened rows wrong: %+v", entries)
	}
}

func TestOpenInstancesReturnsUnclosedRecords(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ex := int64(1234)
	code := int64(0)
	// run_1: started, then exited.
	if err := s.RecordInstance("proc_a", "run_1", "echo", "/tmp", "generic", 100, 111, nil, nil); err != nil {
		t.Fatalf("RecordInstance start: %v", err)
	}
	if err := s.RecordInstance("proc_a", "run_1", "echo", "/tmp", "generic", 100, 111, &ex, &code); err != nil {
		t.Fatalf("RecordInstance exit: %v", err)
	}
	// run_2: started, still open (daemon crashed here).
	if err := s.RecordInstance("proc_a", "run_2", "echo", "/tmp", "generic", 200, 222, nil, nil); err != nil {
		t.Fatalf("RecordInstance: %v", err)
	}
	// proc_b: started, still open.
	if err := s.RecordInstance("proc_b", "run_1", "sleep", "/var", "generic", 300, 333, nil, nil); err != nil {
		t.Fatalf("RecordInstance: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen (as a fresh runtime would) and read open instances: only the two
	// that never recorded an exit, ordered by started_at, with their pids.
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	recs, err := s2.OpenInstances()
	if err != nil {
		t.Fatalf("OpenInstances: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("open instances = %d, want 2: %+v", len(recs), recs)
	}
	if recs[0].ProcessID != "proc_a" || recs[0].InstanceID != "run_2" || recs[0].PID != 222 {
		t.Fatalf("recs[0] = %+v", recs[0])
	}
	if recs[1].ProcessID != "proc_b" || recs[1].InstanceID != "run_1" || recs[1].PID != 333 {
		t.Fatalf("recs[1] = %+v", recs[1])
	}
}

func TestMigrationAddsPidColumn(t *testing.T) {
	// Create a database with the pre-adoption schema (no pid column), insert an
	// instance row, then open it with the current build: migrateSchema must add
	// the column and OpenInstances must still work.
	path := t.TempDir() + "/logs.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	oldSchema := `CREATE TABLE instances(
		process_id  TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		command     TEXT NOT NULL,
		workdir     TEXT NOT NULL,
		profile     TEXT NOT NULL,
		started_at  INTEGER NOT NULL,
		exited_at   INTEGER,
		exit_code   INTEGER,
		PRIMARY KEY(process_id, instance_id)
	) WITHOUT ROWID;
	CREATE TABLE entries(
		process_id TEXT NOT NULL,
		id         INTEGER NOT NULL,
		ts         INTEGER NOT NULL,
		stream     TEXT NOT NULL,
		line       TEXT NOT NULL,
		PRIMARY KEY(process_id, id)
	) WITHOUT ROWID;`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO instances(process_id,instance_id,command,workdir,profile,started_at) VALUES ('proc_x','run_1','echo','/tmp','generic',100)`); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	cols, err := tableColumns(s.db, "instances")
	if err != nil {
		t.Fatalf("tableColumns: %v", err)
	}
	if !slices.Contains(cols, "pid") {
		t.Fatalf("instances table missing pid after migration: %v", cols)
	}
	recs, err := s.OpenInstances()
	if err != nil {
		t.Fatalf("OpenInstances: %v", err)
	}
	if len(recs) != 1 || recs[0].ProcessID != "proc_x" || recs[0].PID != 0 {
		t.Fatalf("migrated row wrong: %+v", recs)
	}
}

func TestQueryFilters(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const proc = "proc_filter"
	s.Append(proc, logs.Entry{ID: 0, Timestamp: time.Now(), Stream: logs.StreamStdout, Line: "Server started"})
	s.Append(proc, logs.Entry{ID: 1, Timestamp: time.Now(), Stream: logs.StreamStderr, Line: "Error: boom"})
	s.Append(proc, logs.Entry{ID: 2, Timestamp: time.Now(), Stream: logs.StreamStdout, Line: "request handled"})
	s.Append(proc, logs.Entry{ID: 3, Timestamp: time.Now(), Stream: logs.StreamStderr, Line: "retrying 50%"})
	// Close is terminal; reopen the same path with a fresh sink and query it.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	// Stream filter.
	stdout, err := s2.Query(proc, logs.Query{Stream: logs.FilterStdout, Lines: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(stdout) != 2 || stdout[0].Stream != logs.StreamStdout || stdout[1].Stream != logs.StreamStdout {
		t.Fatalf("stdout filter wrong: %+v", stdout)
	}
	stderr, err := s2.Query(proc, logs.Query{Stream: logs.FilterStderr, Lines: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(stderr) != 2 {
		t.Fatalf("stderr filter wrong: %+v", stderr)
	}

	// Contains filter (exact substring, wildcards escaped).
	contains, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 10, Contains: "Error"})
	if err != nil {
		t.Fatal(err)
	}
	if len(contains) != 1 || contains[0].Line != "Error: boom" {
		t.Fatalf("contains filter wrong: %+v", contains)
	}
	// '%' and '_' in user input must match literally, not as wildcards.
	pct, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 10, Contains: "50%"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pct) != 1 || pct[0].Line != "retrying 50%" {
		t.Fatalf("percent escape wrong: %+v", pct)
	}
	underscore, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 10, Contains: "request_h"})
	if err != nil {
		t.Fatal(err)
	}
	if len(underscore) != 0 {
		t.Fatalf("underscore must match literally: %+v", underscore)
	}

	// BeforeID bounds the result to older entries.
	before, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 10, BeforeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 || before[0].ID != 0 || before[1].ID != 1 {
		t.Fatalf("BeforeID wrong: %+v", before)
	}
}

func TestConcurrentAppend(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const proc = "proc_race"
	const writers = 8
	const perWriter = 200
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				stream := logs.StreamStdout
				if i%2 == 0 {
					stream = logs.StreamStderr
				}
				s.Append(proc, logs.Entry{
					ID:        uint64(w*perWriter + i),
					Timestamp: time.Now(),
					Stream:    stream,
					Line:      fmt.Sprintf("w%d-l%d", w, i),
				})
			}
		}(w)
	}
	wg.Wait()
	// Close flushes all concurrent appends and is terminal; reopen the same
	// path with a fresh sink to verify the persisted rows.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	entries, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 100000})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != writers*perWriter {
		t.Fatalf("got %d entries, want %d", len(entries), writers*perWriter)
	}
	// IDs must be unique and in ascending order.
	seen := make(map[uint64]bool, len(entries))
	for i, e := range entries {
		if seen[e.ID] {
			t.Fatalf("duplicate ID %d", e.ID)
		}
		seen[e.ID] = true
		if i > 0 && entries[i-1].ID >= e.ID {
			t.Fatalf("IDs not ascending at %d", i)
		}
	}
}

func TestQueueFullDrops(t *testing.T) {
	// A tiny queue with a gated writer forces drops while Append never blocks.
	s, err := open(t.TempDir()+"/logs.db", nil, 4, time.Hour, 1<<30)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gate := make(chan struct{})
	s.testWriterGate = gate
	closed := false
	defer func() {
		if !closed {
			close(gate)
		}
		s.Close()
	}()

	const proc = "proc_drop"
	const total = 200
	for i := 0; i < total; i++ {
		appendLine(s, proc, uint64(i), fmt.Sprintf("l%d", i))
	}
	// All Appends returned promptly (none blocked). Give the gated writer time
	// to consume one item and stall; the queue then overflows.
	time.Sleep(100 * time.Millisecond)
	if s.dropped.Load() == 0 {
		t.Fatal("expected dropped > 0 with a full queue")
	}
	close(gate)
	closed = true
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The surviving entries are the ones that fit in the queue plus the batch
	// the gated writer held; nothing more is required, but the write path must
	// have processed what it accepted.
	if n := s.dropped.Load(); n == 0 {
		t.Fatal("dropped counter must be positive")
	}
}

func TestRetain(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const proc = "proc_retain"
	old := time.Now().Add(-48 * time.Hour)
	newer := time.Now()
	for i := 0; i < 10; i++ {
		s.Append(proc, logs.Entry{ID: uint64(i), Timestamp: old, Stream: logs.StreamStdout, Line: fmt.Sprintf("old-%d", i)})
	}
	for i := 0; i < 5; i++ {
		s.Append(proc, logs.Entry{ID: uint64(100 + i), Timestamp: newer, Stream: logs.StreamStdout, Line: fmt.Sprintf("new-%d", i)})
	}
	// Close is terminal; reopen the same path with a fresh sink and retain on
	// the reopened handle.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	// Age-based retention: delete anything older than 24h.
	if err := s2.Retain(24*time.Hour, 0); err != nil {
		t.Fatalf("Retain: %v", err)
	}
	entries, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("after age retain got %d entries, want 5", len(entries))
	}
	for _, e := range entries {
		if e.ID != 100 && e.ID != 101 && e.ID != 102 && e.ID != 103 && e.ID != 104 {
			t.Fatalf("old entry survived retain: %+v", e)
		}
	}
}

func TestRetainSizeBudget(t *testing.T) {
	path := t.TempDir() + "/logs.db"
	s, err := Open(path, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const proc = "proc_size"
	// 400 entries x ~4KB = ~1.6MB of content; a 1MB budget must trim the
	// oldest. strings.Repeat builds the padded line in O(n); a char-by-char
	// append loop would be O(n^2) and pathologically slow under -race.
	for i := 0; i < 400; i++ {
		line := fmt.Sprintf("line-%d-", i) + strings.Repeat("x", 4000)
		s.Append(proc, logs.Entry{ID: uint64(i), Timestamp: time.Now(), Stream: logs.StreamStdout, Line: line})
	}
	// Close flushes the entries and is terminal; reopen to run the size budget.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if err := s2.Retain(0, 1); err != nil { // 1 MB budget
		t.Fatalf("Retain: %v", err)
	}
	size, err := s2.dbSize()
	if err != nil {
		t.Fatal(err)
	}
	if size > 1*1024*1024 {
		t.Fatalf("db size %d exceeds budget", size)
	}
	entries, err := s2.Query(proc, logs.Query{Stream: logs.FilterAll, Lines: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected some entries to survive the size budget")
	}
	// The oldest entries must have been dropped first.
	if entries[0].ID == 0 {
		t.Fatal("oldest entry should have been trimmed by size retention")
	}
}

func TestDeleteProcess(t *testing.T) {
	s := newTestSink(t)
	const procA = "proc_a"
	const procB = "proc_b"
	for i := 0; i < 5; i++ {
		appendLine(s, procA, uint64(i), fmt.Sprintf("a-%d", i))
		appendLine(s, procB, uint64(i), fmt.Sprintf("b-%d", i))
	}
	// DeleteProcess is synchronous: the writer drains the appends queued ahead
	// of it, flushes them, then removes every row for proc_a before returning.
	// So queries after it returns are deterministic and run before Close.
	if err := s.DeleteProcess(procA); err != nil {
		t.Fatalf("DeleteProcess: %v", err)
	}
	a, err := s.Query(procA, logs.Query{Stream: logs.FilterAll, Lines: 10})
	if err != nil {
		t.Fatalf("Query proc_a: %v", err)
	}
	if len(a) != 0 {
		t.Fatalf("proc_a rows remain: %+v", a)
	}
	b, err := s.Query(procB, logs.Query{Stream: logs.FilterAll, Lines: 10})
	if err != nil {
		t.Fatalf("Query proc_b: %v", err)
	}
	if len(b) != 5 {
		t.Fatalf("proc_b rows removed too: %+v", b)
	}
}

func TestNoopSink(t *testing.T) {
	var n Noop
	n.Append("p", logs.Entry{})
	if got, err := n.Query("p", logs.Query{}); err != nil || len(got) != 0 {
		t.Fatalf("Noop.Query = %v, %v", got, err)
	}
	if err := n.DeleteProcess("p"); err != nil {
		t.Fatal(err)
	}
	if err := n.RecordInstance("p", "i", "c", "w", "prof", 1, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
}
