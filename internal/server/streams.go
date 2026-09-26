// Shared streaming helpers: every stream opens with a snapshot, then
// deltas, each message carrying an opaque cursor. Slow consumers never
// block producers: per-stream bounded queues (1024) coalesce or emit a
// Gap{dropped} marker. Heartbeats every 15s keep proxies alive (§5.4).
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// streamWriter is a newline-delimited JSON stream with flush + heartbeat.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	enc     *json.Encoder
	done    <-chan struct{}
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
	sw := &streamWriter{w: w, flusher: flusher, enc: json.NewEncoder(w), done: r.Context().Done()}
	// Heartbeats keep webviews/proxies from idling out.
	if heartbeatMsg != nil {
		go func() {
			t := time.NewTicker(15 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-sw.done:
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
	select {
	case <-s.done:
		return fmt.Errorf("stream closed")
	default:
	}
	if err := s.enc.Encode(v); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// gapMessage marks dropped messages for a slow consumer.
func gapMessage(dropped int, reason string) map[string]any {
	return map[string]any{"kind": "gap", "gap": map[string]any{"dropped": dropped, "reason": reason}}
}

// heartbeatMessage is the 15s keepalive payload.
func heartbeatMessage() any {
	return map[string]any{"kind": "heartbeat", "at": time.Now().UnixNano()}
}
