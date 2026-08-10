// Package logstore implements logs.Sink on top of SQLite (modernc.org/sqlite,
// pure Go, no cgo). It provides a durable archive of log entries and process
// instance records for the runtime. The in-memory ring buffers remain the
// source of truth for live waiting; the archive is used for back-fill after
// ring eviction and for crash-survival across restarts.
//
// Write paths are asynchronous: Append and RecordInstance enqueue onto a
// bounded channel and never block the caller on disk I/O. A single writer
// goroutine drains the queue, batches inserts into transactions, and flushes
// on a timer or when the batch reaches its size threshold. Reads (Query) go
// straight to SQLite and are always bounded.
package logstore

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"agent-runtime/internal/logs"
)

const (
	defaultQueueSize       = 4096
	defaultBatchSize       = 500
	defaultFlushInterval   = 200 * time.Millisecond
	defaultQueryLimit      = 10000
	deleteBatchRows        = 100
	defaultJanitorInterval = time.Hour
)

// ErrClosed is returned by operations on a sink after Close has been called.
// Close is terminal: the writer goroutine has exited and the database is
// closed, so nothing can be read, retained, deleted or recorded afterwards.
var ErrClosed = errors.New("logstore: sink closed")

const schemaSQL = `
CREATE TABLE IF NOT EXISTS entries(
  process_id TEXT NOT NULL,
  id         INTEGER NOT NULL,
  ts         INTEGER NOT NULL,      -- unix nanos
  stream     TEXT NOT NULL,
  line       TEXT NOT NULL,
  PRIMARY KEY(process_id, id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS instances(
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
`

const insertEntrySQL = `INSERT INTO entries(process_id,id,ts,stream,line) VALUES (?,?,?,?,?)`

const insertInstanceSQL = `INSERT INTO instances(process_id,instance_id,command,workdir,profile,started_at) VALUES (?,?,?,?,?,?)`

const upsertInstanceExitSQL = `INSERT INTO instances(process_id,instance_id,command,workdir,profile,started_at)
VALUES (?,?,?,?,?,?)
ON CONFLICT(process_id, instance_id) DO UPDATE SET
  exited_at=excluded.exited_at, exit_code=excluded.exit_code`

const querySQL = `
SELECT id, ts, stream, line FROM entries
 WHERE process_id = ? AND stream IN (?, ?)
   AND (? = '' OR line LIKE ? ESCAPE '\')
   AND (? = 0 OR id < ?)
 ORDER BY id DESC LIMIT ?`

const deleteEntriesSQL = `DELETE FROM entries WHERE process_id = ?`
const deleteInstancesSQL = `DELETE FROM instances WHERE process_id = ?`

// itemKind discriminates the work items a writer goroutine drains.
type itemKind int

const (
	itemEntry itemKind = iota
	itemInstance
	itemDelete
)

// instanceInfo carries the fields of a RecordInstance call.
type instanceInfo struct {
	instanceID string
	command    string
	workdir    string
	profile    string
	startedAt  int64
	exitedAt   *int64
	exitCode   *int64
}

// queueItem is one unit of write work.
type queueItem struct {
	kind      itemKind
	processID string
	e         logs.Entry
	inst      instanceInfo
	done      chan error // set for itemDelete; the writer replies with the delete result
}

// SQLite is a logs.Sink backed by a single SQLite database file. Safe for
// concurrent use.
type SQLite struct {
	db     *sql.DB
	logger *slog.Logger

	queue         chan queueItem
	flushInterval time.Duration
	batchSize     int

	dropped atomic.Uint64

	stopCh     chan struct{} // closed by Close to stop the writer
	writerDone chan struct{} // closed by the writer when it has exited
	stopped    chan struct{} // closed by Close after the writer has exited and db is closed

	closeOnce sync.Once
	closeErr  error

	// maxAge/maxMB cache the retention policy passed to Retain so the Janitor
	// loop can re-apply it without re-supplying parameters.
	maxAge time.Duration
	maxMB  int64

	// testWriterGate is a test-only hook: when non-nil, the writer blocks on it
	// after receiving each item, allowing tests to force queue-full drops.
	testWriterGate chan struct{}
}

// Open opens (creating if necessary) the SQLite database at path and starts
// the asynchronous writer goroutine. logger may be nil.
func Open(path string, logger *slog.Logger) (*SQLite, error) {
	return open(path, logger, defaultQueueSize, defaultFlushInterval, defaultBatchSize)
}

// open is Open with tunable batching parameters, used by tests.
func open(path string, logger *slog.Logger, queueSize int, flushInterval time.Duration, batchSize int) (*SQLite, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("logstore: mkdir %s: %w", filepath.Dir(path), err)
	}
	// auto_vacuum is a file-level property and must be set before any tables
	// exist; only attempt it when the database file is new/empty.
	isNew := false
	if fi, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			isNew = true
		}
	} else if fi.Size() == 0 {
		isNew = true
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("logstore: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// auto_vacuum is a file-level property and must be set before any tables
	// exist; only attempt it when the database file is new/empty. It must also
	// be set before journal_mode=WAL: switching an empty database to WAL writes
	// the header first, after which SQLite silently ignores auto_vacuum and
	// incremental_vacuum becomes a no-op (Retain's size budget would then
	// never reclaim pages).
	if isNew {
		if _, err := db.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			logger.Warn("logstore: enabling incremental auto_vacuum failed", "error", err)
		}
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=3000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("logstore: %s: %w", pragma, err)
		}
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("logstore: create schema: %w", err)
	}

	s := &SQLite{
		db:            db,
		logger:        logger,
		queue:         make(chan queueItem, queueSize),
		flushInterval: flushInterval,
		batchSize:     batchSize,
		stopCh:        make(chan struct{}),
		writerDone:    make(chan struct{}),
		stopped:       make(chan struct{}),
	}
	go s.writerLoop()
	return s, nil
}

// Append enqueues an entry for asynchronous persistence. It never blocks:
// when the write queue is full the entry is dropped and the drop counter is
// incremented. Readers are therefore never stalled by disk back-pressure.
func (s *SQLite) Append(processID string, e logs.Entry) {
	s.enqueue(queueItem{kind: itemEntry, processID: processID, e: e})
}

// RecordInstance enqueues an instance lifecycle record. startedAt is unix
// nanos; when exitedAt/exitCode are non-nil the record is an exit update.
func (s *SQLite) RecordInstance(procID, instanceID, command, workdir, profile string, startedAt int64, exitedAt, exitCode *int64) error {
	if s.isClosed() {
		return ErrClosed
	}
	s.enqueue(queueItem{
		kind:      itemInstance,
		processID: procID,
		inst: instanceInfo{
			instanceID: instanceID,
			command:    command,
			workdir:    workdir,
			profile:    profile,
			startedAt:  startedAt,
			exitedAt:   exitedAt,
			exitCode:   exitCode,
		},
	})
	return nil
}

// isClosed reports whether Close has completed (the writer has exited and the
// database is closed).
func (s *SQLite) isClosed() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// drop increments the drop counter and logs the first drop for a message.
func (s *SQLite) drop(msg string) {
	n := s.dropped.Add(1)
	if n == 1 {
		s.logger.Warn(msg, "dropped", n)
	}
}

func (s *SQLite) enqueue(item queueItem) {
	// After Close the writer goroutine has exited, so an item enqueued now
	// could never be persisted. Drop it rather than leak it on the undrained
	// queue. The queue channel is never closed, so Append can never panic on a
	// send-to-closed-channel; this guard is purely about not leaking items.
	select {
	case <-s.stopped:
		s.drop("logstore: dropping entry; sink closed")
		return
	default:
	}
	select {
	case s.queue <- item:
	default:
		s.drop("logstore: dropping entries; queue full")
	}
}

// Query returns the matching tail of a process's archived entries, newest-last
// by ID (matching logs.Query semantics). Lines <= 0 means a large default.
// BeforeID bounds the result to entries older than a retained ring-buffer ID.
func (s *SQLite) Query(processID string, q logs.Query) ([]logs.Entry, error) {
	if s.isClosed() {
		return nil, ErrClosed
	}
	var streams [2]string
	switch q.Stream {
	case logs.FilterStdout:
		streams = [2]string{"stdout", "stdout"}
	case logs.FilterStderr:
		streams = [2]string{"stderr", "stderr"}
	default:
		streams = [2]string{"stdout", "stderr"}
	}
	limit := q.Lines
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	pattern := ""
	if q.Contains != "" {
		pattern = "%" + escapeLike(q.Contains) + "%"
	}
	rows, err := s.db.Query(querySQL, processID, streams[0], streams[1], pattern, pattern, q.BeforeID, q.BeforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("logstore: query: %w", err)
	}
	defer rows.Close()

	entries := make([]logs.Entry, 0, limit)
	for rows.Next() {
		var e logs.Entry
		var ts int64
		var stream string
		if err := rows.Scan(&e.ID, &ts, &stream, &e.Line); err != nil {
			return nil, fmt.Errorf("logstore: scan: %w", err)
		}
		e.Timestamp = time.Unix(0, ts)
		e.Stream = logs.Stream(stream)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("logstore: rows: %w", err)
	}
	// The query is ordered newest-first; flip to ascending (newest-last).
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// DeleteProcess removes every archived row for a process. The delete is
// enqueued behind any pending appends for the process and executed by the
// writer in order, so a remove_process cannot leave trailing rows behind.
func (s *SQLite) DeleteProcess(processID string) error {
	if s.isClosed() {
		return ErrClosed
	}
	done := make(chan error, 1)
	item := queueItem{kind: itemDelete, processID: processID, done: done}
	select {
	case s.queue <- item:
	case <-s.stopped:
		return ErrClosed // sink closed while the delete was queued
	}
	select {
	case err := <-done:
		return err
	case <-s.stopped:
		return ErrClosed // sink closed before the delete executed
	}
}

func (s *SQLite) deleteProcessNow(processID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("logstore: delete begin: %w", err)
	}
	if _, err := tx.Exec(deleteEntriesSQL, processID); err != nil {
		tx.Rollback()
		return fmt.Errorf("logstore: delete entries: %w", err)
	}
	if _, err := tx.Exec(deleteInstancesSQL, processID); err != nil {
		tx.Rollback()
		return fmt.Errorf("logstore: delete instances: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("logstore: delete commit: %w", err)
	}
	return nil
}

// Retain enforces the retention policy: entries older than maxAge are deleted,
// and when the database file exceeds maxMB, the oldest entries are deleted in
// batches until the file is back under budget. A non-positive value disables
// the corresponding dimension. The passed policy is cached for the Janitor
// loop. It also records the policy for Janitor.
func (s *SQLite) Retain(maxAge time.Duration, maxMB int64) error {
	if s.isClosed() {
		return ErrClosed
	}
	s.maxAge = maxAge
	s.maxMB = maxMB
	if maxAge > 0 {
		cutoff := time.Now().Add(-maxAge).UnixNano()
		if _, err := s.db.Exec(`DELETE FROM entries WHERE ts < ?`, cutoff); err != nil {
			return fmt.Errorf("logstore: retain age: %w", err)
		}
	}
	if maxMB > 0 {
		budget := maxMB * 1024 * 1024
		for {
			size, err := s.dbSize()
			if err != nil {
				return fmt.Errorf("logstore: retain size: %w", err)
			}
			if size <= budget {
				break
			}
			// (process_id, id) is the primary key of the WITHOUT ROWID table,
			// so the row-value IN selects exact rows.
			res, err := s.db.Exec(
				`DELETE FROM entries WHERE (process_id, id) IN
				 (SELECT process_id, id FROM entries ORDER BY ts ASC, id ASC LIMIT ?)`,
				deleteBatchRows,
			)
			if err != nil {
				return fmt.Errorf("logstore: retain delete: %w", err)
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				break
			}
			// Reclaim freed pages inside the loop: page_count includes pages
			// that have been freed but not yet vacuumed, so without this the
			// size check below would keep deleting until the table is empty.
			if _, err := s.db.Exec("PRAGMA incremental_vacuum"); err != nil {
				s.logger.Warn("logstore: incremental_vacuum failed", "error", err)
			}
		}
	}
	// Final vacuum pass for the age-based path.
	if _, err := s.db.Exec("PRAGMA incremental_vacuum"); err != nil {
		s.logger.Warn("logstore: incremental_vacuum failed", "error", err)
	}
	return nil
}

// Janitor runs Retain on the cached retention policy every interval until
// stopCh is closed. interval <= 0 means one hour.
func (s *SQLite) Janitor(stopCh <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = defaultJanitorInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			if err := s.Retain(s.maxAge, s.maxMB); err != nil {
				s.logger.Warn("logstore: janitor retain failed", "error", err)
			}
		}
	}
}

// Close stops the writer goroutine, flushes all queued items and closes the
// database. Safe to call multiple times; subsequent calls return the first
// error.
func (s *SQLite) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		<-s.writerDone
		s.closeErr = s.db.Close()
		close(s.stopped)
	})
	return s.closeErr
}

// writerLoop is the single writer goroutine. It batches queued entries and
// instance records, flushes them in a transaction every flushInterval or once
// batchSize items are queued (whichever comes first), and processes ordered
// deletes. On Close it drains whatever remains and exits.
func (s *SQLite) writerLoop() {
	defer close(s.writerDone)
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	var batch []queueItem

	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.flush(batch)
		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-s.queue:
			if !ok {
				// The queue is never closed; this arm exists for safety.
				return
			}
			if item.kind == itemDelete {
				// Preserve ordering: flush pending writes, then delete.
				flush()
				err := s.deleteProcessNow(item.processID)
				item.done <- err
				continue
			}
			batch = append(batch, item)
			if s.testWriterGate != nil {
				<-s.testWriterGate
			}
			if len(batch) >= s.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.stopCh:
			// Graceful stop: drain whatever is still queued, flush, and exit.
			for {
				select {
				case item := <-s.queue:
					if item.kind == itemDelete {
						flush()
						err := s.deleteProcessNow(item.processID)
						item.done <- err
						continue
					}
					batch = append(batch, item)
				default:
					flush()
					return
				}
			}
		}
	}
}

// flush writes one batch inside a single transaction.
func (s *SQLite) flush(batch []queueItem) {
	tx, err := s.db.Begin()
	if err != nil {
		s.logger.Warn("logstore: flush begin failed", "error", err, "items", len(batch))
		return
	}
	insEntry, err := tx.Prepare(insertEntrySQL)
	if err != nil {
		tx.Rollback()
		s.logger.Warn("logstore: prepare insert failed", "error", err)
		return
	}
	insInst, err := tx.Prepare(insertInstanceSQL)
	if err != nil {
		insEntry.Close()
		tx.Rollback()
		s.logger.Warn("logstore: prepare instance failed", "error", err)
		return
	}
	upsExit, err := tx.Prepare(upsertInstanceExitSQL)
	if err != nil {
		insEntry.Close()
		insInst.Close()
		tx.Rollback()
		s.logger.Warn("logstore: prepare upsert failed", "error", err)
		return
	}

	for _, item := range batch {
		switch item.kind {
		case itemEntry:
			if _, err := insEntry.Exec(item.processID, item.e.ID, item.e.Timestamp.UnixNano(), string(item.e.Stream), item.e.Line); err != nil {
				s.logger.Warn("logstore: insert entry failed", "error", err, "process_id", item.processID, "id", item.e.ID)
			}
		case itemInstance:
			if item.inst.exitedAt != nil {
				if _, err := upsExit.Exec(item.processID, item.inst.instanceID, item.inst.command, item.inst.workdir, item.inst.profile, item.inst.startedAt, *item.inst.exitedAt, *item.inst.exitCode); err != nil {
					s.logger.Warn("logstore: upsert exit failed", "error", err, "process_id", item.processID, "instance_id", item.inst.instanceID)
				}
			} else {
				if _, err := insInst.Exec(item.processID, item.inst.instanceID, item.inst.command, item.inst.workdir, item.inst.profile, item.inst.startedAt); err != nil {
					s.logger.Warn("logstore: insert instance failed", "error", err, "process_id", item.processID, "instance_id", item.inst.instanceID)
				}
			}
		}
	}
	insEntry.Close()
	insInst.Close()
	upsExit.Close()
	if err := tx.Commit(); err != nil {
		s.logger.Warn("logstore: flush commit failed", "error", err)
	}
}

func (s *SQLite) dbSize() (int64, error) {
	var pages, pageSize int64
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		return 0, err
	}
	if err := s.db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	return pages * pageSize, nil
}

// escapeLike escapes the LIKE wildcards so user input is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
