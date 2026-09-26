// Shared streaming helpers: every stream opens with a snapshot, then
// deltas, each message carrying an opaque cursor. Slow consumers never
// block producers: per-stream bounded queues (1024) coalesce or emit a
// Gap{dropped} marker. Heartbeats every 15s keep proxies alive (§5.4).
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var heartbeatInterval = 15 * time.Second

// streamWriter is a newline-delimited JSON stream with flush + heartbeat.
type streamWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	enc     *json.Encoder
	done    <-chan struct{}
	stop    chan struct{}
	closed  atomic.Bool
}

func newStream(w http.ResponseWriter, r *http.Request, heartbeatMsg func() any) (*streamWriter, bool) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
		return nil, false
	}
	sw := &streamWriter{
		w: w, flusher: flusher, enc: json.NewEncoder(w),
		done: r.Context().Done(), stop: make(chan struct{}),
	}
	// Heartbeats keep webviews/proxies from idling out.
	if heartbeatMsg != nil {
		go func() {
			defer func() { _ = recover() }()
			t := time.NewTicker(heartbeatInterval)
			defer t.Stop()
			for {
				select {
				case <-sw.done:
					return
				case <-sw.stop:
					return
				case <-t.C:
					_ = sw.send(heartbeatMsg())
				}
			}
		}()
	}
	return sw, true
}

func (s *streamWriter) send(v any) error {
	if s.closed.Load() {
		return fmt.Errorf("stream closed")
	}
	select {
	case <-s.done:
		return fmt.Errorf("stream closed")
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return fmt.Errorf("stream closed")
	}
	if err := s.enc.Encode(v); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *streamWriter) Close() {
	if s.closed.CompareAndSwap(false, true) {
		close(s.stop)
	}
}

// gapMessage marks dropped messages for a slow consumer.
func gapMessage(dropped int, reason string) map[string]any {
	return map[string]any{"kind": "gap", "gap": map[string]any{"dropped": dropped, "reason": reason}}
}

// heartbeatMessage is the 15s keepalive payload.
func heartbeatMessage() any {
	return map[string]any{"kind": "heartbeat", "at": time.Now().UnixNano()}
}
