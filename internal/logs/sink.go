package logs

// Sink is an optional durable destination for log entries. Implementations
// must be safe for concurrent use; Append must never block the caller on I/O
// (implementations enqueue and flush asynchronously).
type Sink interface {
	Append(processID string, e Entry)
	Query(processID string, q Query) ([]Entry, error) // newest-last by ID
	DeleteProcess(processID string) error
	RecordInstance(procID, instanceID, command, workdir, profile string, startedAt int64, pid int64, exitedAt, exitCode *int64) error
	Close() error // flushes and closes; safe to call multiple times
}

// InstanceRecord is a durable lifecycle record for one run of a process. PID
// is the OS pid at start (0 when unknown), the handle daemon adoption uses to
// re-attach to a process that outlived the runtime that spawned it.
type InstanceRecord struct {
	ProcessID  string
	InstanceID string
	Command    string
	WorkDir    string
	Profile    string
	StartedAt  int64 // unix nanos
	PID        int64 // os pid at start; 0 when not recorded
}

// InstanceArchive is an optional Sink capability: it can enumerate lifecycle
// records that have no recorded exit (exited_at IS NULL) — i.e. processes that
// were still running when the runtime/daemon that owned them died. Used for
// daemon adoption of orphaned processes.
type InstanceArchive interface {
	OpenInstances() ([]InstanceRecord, error)
}
