// Package session implements the session registry for agent-runtime v3.
// A session represents one connected client: an MCP bridge instance,
// a CLI invocation, the GUI, a TUI, or a web tab.
//
// Ownership is per project, not per session: any session in the same
// workspace sees the same processes. Processes with lifetime=session
// are tied to their starting session via a lease (§3.7).
package session

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// SessionKind is the type of connected client.
type SessionKind string

const (
	SessionKindMCP SessionKind = "mcp"
	SessionKindCLI SessionKind = "cli"
	SessionKindGUI SessionKind = "gui"
	SessionKindTUI SessionKind = "tui"
	SessionKindWeb SessionKind = "web"
)

// Harness is the AI coding agent type.
type Harness string

const (
	HarnessClaudeCode    Harness = "claude-code"
	HarnessCodex         Harness = "codex"
	HarnessGemini        Harness = "gemini"
	HarnessOpenCode      Harness = "opencode"
	HarnessCursor        Harness = "cursor"
	HarnessWindsurf      Harness = "windsurf"
	HarnessVSCodeCopilot Harness = "vscode-copilot"
	HarnessZed           Harness = "zed"
	HarnessCline         Harness = "cline"
	HarnessUnknown       Harness = "unknown"
)

// SessionLeaseGrace is the grace period after a session closes before
// lifetime=session processes are stopped. It covers agent restarts.
const SessionLeaseGrace = 30 * time.Second

// Session is one connected client.
type Session struct {
	ID             string
	Kind           SessionKind
	Harness        Harness
	HarnessVersion string
	ClientPID      int
	WorkspaceID    string
	StartedAt      time.Time
	LastSeenAt     time.Time
	ClosedAt       *time.Time
}

// Registry tracks all connected sessions.
type Registry struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	byWorkspace map[string]map[string]bool // workspaceID -> session IDs
}

// NewRegistry creates a new session registry.
func NewRegistry() *Registry {
	return &Registry{
		sessions:    make(map[string]*Session),
		byWorkspace: make(map[string]map[string]bool),
	}
}

// Register creates a new session. workspaceID may be empty for
// global clients (GUI without a selected project).
func (r *Registry) Register(kind SessionKind, harness Harness, clientPID int, workspaceID string) (*Session, error) {
	if kind == "" {
		return nil, fmt.Errorf("session: kind required")
	}
	if harness == "" {
		harness = HarnessUnknown
	}
	id := "sess_" + newID()
	now := time.Now()
	s := &Session{
		ID:          id,
		Kind:        kind,
		Harness:     harness,
		ClientPID:   clientPID,
		WorkspaceID: workspaceID,
		StartedAt:   now,
		LastSeenAt:  now,
	}
	r.mu.Lock()
	r.sessions[id] = s
	if workspaceID != "" {
		if r.byWorkspace[workspaceID] == nil {
			r.byWorkspace[workspaceID] = make(map[string]bool)
		}
		r.byWorkspace[workspaceID][id] = true
	}
	r.mu.Unlock()
	return s, nil
}

// Get returns a session by ID.
func (r *Registry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Close marks a session as closed. Idempotent. Returns the closed session
// so callers can schedule lease expiry for lifetime=session processes.
func (r *Registry) Close(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	if s.ClosedAt != nil {
		return nil
	}
	now := time.Now()
	s.ClosedAt = &now
	if s.WorkspaceID != "" {
		delete(r.byWorkspace[s.WorkspaceID], id)
	}
	return nil
}

// Heartbeat updates the last_seen_at timestamp.
func (r *Registry) Heartbeat(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	if s.ClosedAt != nil {
		return fmt.Errorf("session %s is closed", id)
	}
	s.LastSeenAt = time.Now()
	return nil
}

// List returns all open sessions.
func (r *Registry) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Session
	for _, s := range r.sessions {
		if s.ClosedAt == nil {
			out = append(out, s)
		}
	}
	return out
}

// ListByWorkspace returns open sessions for a workspace.
func (r *Registry) ListByWorkspace(workspaceID string) []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Session
	for id := range r.byWorkspace[workspaceID] {
		if s, ok := r.sessions[id]; ok && s.ClosedAt == nil {
			out = append(out, s)
		}
	}
	return out
}

// Count returns the number of open sessions.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, s := range r.sessions {
		if s.ClosedAt == nil {
			n++
		}
	}
	return n
}

// ReapExpired closes sessions idle longer than maxIdle (heartbeat streams
// normally prevent this; it is a backstop for dead bridges). It returns
// the reaped IDs so the engine can expire session-leased processes.
func (r *Registry) ReapExpired(maxIdle time.Duration) []string {
	if maxIdle <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-maxIdle)
	r.mu.Lock()
	defer r.mu.Unlock()
	var reaped []string
	for id, s := range r.sessions {
		if s.ClosedAt != nil {
			continue
		}
		if s.LastSeenAt.Before(cutoff) {
			now := time.Now()
			s.ClosedAt = &now
			delete(r.byWorkspace[s.WorkspaceID], id)
			reaped = append(reaped, id)
		}
	}
	return reaped
}

func newID() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	out := make([]byte, 16)
	v := uint64(time.Now().UnixMilli()) & 0xffffffffffff
	for i := 11; i >= 0; i-- {
		out[i] = alphabet[v&31]
		v >>= 5
	}
	acc := 0
	bits := 0
	j := 12
	for _, c := range b {
		acc = (acc << 8) | int(c)
		bits += 8
		for bits >= 5 && j < 16 {
			bits -= 5
			out[j] = alphabet[(acc>>bits)&31]
			j++
		}
	}
	return string(out[:j])
}
