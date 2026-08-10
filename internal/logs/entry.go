package logs

import "time"

// Stream identifies the output stream a log entry was captured from.
type Stream string

const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

// StreamFilter selects which streams to include in a query.
type StreamFilter string

const (
	FilterAll    StreamFilter = "all"
	FilterStdout StreamFilter = "stdout"
	FilterStderr StreamFilter = "stderr"
)

// Entry is a single captured log line.
//
// ID is a monotonically increasing, process-local identifier assigned by the
// runtime when the line is received. IDs are shared across stdout and stderr so
// entries can be merged into a single chronological sequence.
type Entry struct {
	ID        uint64    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Stream    Stream    `json:"stream"`
	Line      string    `json:"line"`
}
