package logs

// Sink is an optional durable destination for log entries. Implementations
// must be safe for concurrent use; Append must never block the caller on I/O
// (implementations enqueue and flush asynchronously).
type Sink interface {
	Append(processID string, e Entry)
	Query(processID string, q Query) ([]Entry, error) // newest-last by ID
	DeleteProcess(processID string) error
	RecordInstance(procID, instanceID, command, workdir, profile string, startedAt int64, exitedAt, exitCode *int64) error
	Close() error // flushes and closes; safe to call multiple times
}
