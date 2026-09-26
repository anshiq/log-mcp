package session

import (
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	s, err := reg.Register(SessionKindMCP, HarnessClaudeCode, 1234, "ws_test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	got, ok := reg.Get(s.ID)
	if !ok {
		t.Error("session should be found")
	}
	if got.Kind != SessionKindMCP {
		t.Errorf("kind = %q, want %q", got.Kind, SessionKindMCP)
	}
}

func TestRegistry_Close(t *testing.T) {
	reg := NewRegistry()
	s, _ := reg.Register(SessionKindCLI, HarnessUnknown, 0, "")
	err := reg.Close(s.ID)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, ok := reg.Get(s.ID)
	if !ok {
		// Session still exists but is closed
	}
	if s.ClosedAt == nil {
		t.Error("session should be closed")
	}
}

func TestRegistry_Heartbeat(t *testing.T) {
	reg := NewRegistry()
	s, _ := reg.Register(SessionKindGUI, HarnessUnknown, 0, "")
	before := s.LastSeenAt
	reg.Heartbeat(s.ID)
	after := s.LastSeenAt
	if !after.After(before) {
		t.Error("heartbeat should update last_seen_at")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SessionKindMCP, HarnessClaudeCode, 1, "ws1")
	reg.Register(SessionKindCLI, HarnessUnknown, 2, "")
	reg.Register(SessionKindGUI, HarnessUnknown, 3, "")
	sessions := reg.List()
	if len(sessions) != 3 {
		t.Errorf("List() = %d, want 3", len(sessions))
	}
}

func TestRegistry_Count(t *testing.T) {
	reg := NewRegistry()
	reg.Register(SessionKindMCP, HarnessClaudeCode, 1, "ws1")
	reg.Register(SessionKindCLI, HarnessUnknown, 2, "")
	if reg.Count() != 2 {
		t.Errorf("Count() = %d, want 2", reg.Count())
	}
}
