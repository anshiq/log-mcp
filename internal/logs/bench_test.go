package logs

import (
	"fmt"
	"testing"
)

// benchProcessLogs fills a ProcessLogs with n lines per stream (2n entries,
// interleaved by the shared ID counter).
func benchProcessLogs(n int) *ProcessLogs {
	p := NewProcessLogs(n+1, nil)
	for i := 0; i < n; i++ {
		p.Append(StreamStdout, fmt.Sprintf("stdout line %d with some padding 0123456789", i))
		p.Append(StreamStderr, fmt.Sprintf("stderr line %d with some padding 0123456789", i))
	}
	return p
}

// BenchmarkFrom exercises the From(id) read path at a mid-range ID: both rings
// are partially scanned and merged.
func BenchmarkFrom(b *testing.B) {
	p := benchProcessLogs(10000)
	mid := uint64(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := p.From(mid); len(got) == 0 {
			b.Fatal("empty From result")
		}
	}
}

// BenchmarkQuery exercises the full Query read path (merge + filter + tail).
func BenchmarkQuery(b *testing.B) {
	p := benchProcessLogs(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := p.Query(Query{Stream: FilterAll, Lines: 100, Contains: "padding"})
		if len(res.Entries) == 0 {
			b.Fatal("empty Query result")
		}
	}
}
