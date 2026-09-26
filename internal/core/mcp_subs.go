// MCP-compatible pull subscriptions: capped per-client buffers over the
// daemon event forwarders, so MCP stays pull-only while GUI/TUI stream.
package core

import (
	"fmt"
	"sync"
	"time"
)

// MCPEvent is one buffered pull-model event.
type MCPEvent struct {
	Type      string `json:"type"`
	ProcessID string `json:"processId"`
	At        int64  `json:"at"`
}

// MCPSubscription is a capped buffer owned by one MCP session.
type MCPSubscription struct {
	ID        string
	SessionID string
	Types     []string
	ProcessID string
	mu        sync.Mutex
	buf       []MCPEvent
	dropped   int
}

// MCPSubscriptions tracks pull-model subscriptions.
type MCPSubscriptions struct {
	mu   sync.Mutex
	subs map[string]*MCPSubscription
}

// Subscribe creates a capped subscription.
func (m *MCPSubscriptions) Subscribe(sessionID string, types []string, processID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subs == nil {
		m.subs = map[string]*MCPSubscription{}
	}
	id := fmt.Sprintf("sub_%d", time.Now().UnixNano())
	m.subs[id] = &MCPSubscription{ID: id, SessionID: sessionID, Types: types, ProcessID: processID}
	return id
}

// Drain returns buffered events (up to limit; <=0 drains all).
func (m *MCPSubscriptions) Drain(id string, limit int) ([]MCPEvent, int) {
	m.mu.Lock()
	sub, ok := m.subs[id]
	m.mu.Unlock()
	if !ok {
		return nil, 0
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	out := sub.buf
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	dropped := sub.dropped
	sub.buf = nil
	sub.dropped = 0
	if out == nil {
		out = []MCPEvent{}
	}
	return out, dropped
}

// Unsubscribe closes a subscription.
func (m *MCPSubscriptions) Unsubscribe(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subs, id)
}
