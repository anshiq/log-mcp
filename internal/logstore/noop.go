package logstore

import "agent-runtime/internal/logs"

// Noop is a logs.Sink that discards everything. It is used when
// log_store: memory so the runtime has a single, uniform wiring path.
type Noop struct{}

// Append discards the entry.
func (Noop) Append(processID string, e logs.Entry) {}

// Query returns no entries.
func (Noop) Query(processID string, q logs.Query) ([]logs.Entry, error) { return nil, nil }

// DeleteProcess does nothing.
func (Noop) DeleteProcess(processID string) error { return nil }

// RecordInstance does nothing.
func (Noop) RecordInstance(procID, instanceID, command, workdir, profile string, startedAt int64, exitedAt, exitCode *int64) error {
	return nil
}

// Close does nothing.
func (Noop) Close() error { return nil }
