package logs

import "testing"

func TestParseLevel(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"level=info", "info"},
		{"time=2024-01-01T00:00:00Z level=WARN msg=started", "warn"},
		{`{"ts":123,"level":"error","msg":"boom"}`, "error"},
		{`level="debug" foo=bar`, "debug"},
		{`{"level": "error", "msg": "x"}`, "error"},
		{"[INFO] server listening", "info"},
		{"ERROR: failed to connect", "error"},
		{"warning: disk low", "warn"},
		{"debug message", "debug"},
		{"level=critical", "error"},
		{"level=fatal something went wrong", "error"},
		{"plain prose with no level", ""},
		{"", ""},
		{"https://example.com", ""},
		{"msg=hello world", ""},
	}
	for _, tt := range tests {
		if got := ParseLevel(tt.line); got != tt.want {
			t.Errorf("ParseLevel(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFilterByLevel(t *testing.T) {
	entries := []Entry{
		{ID: 1, Line: "level=info started"},
		{ID: 2, Line: `{"level":"error"} boom`},
		{ID: 3, Line: "level=warn slow"},
		{ID: 4, Line: "debug detail"},
		{ID: 5, Line: "plain line"},
	}

	tests := []struct {
		level string
		want  []uint64 // expected surviving IDs
	}{
		{"error", []uint64{2}},
		{"info", []uint64{1}},
		{"warn", []uint64{3}},
		{"debug", []uint64{4}},
		{"WARN", []uint64{3}},         // case-insensitive
		{"critical", []uint64{2}},     // alias normalization: critical == error
		{"", []uint64{1, 2, 3, 4, 5}}, // no filter
		{"all", []uint64{1, 2, 3, 4, 5}},
		{"bogus", []uint64{1, 2, 3, 4, 5}}, // unknown requested level: unchanged
	}
	for _, tt := range tests {
		got := FilterByLevel(entries, tt.level)
		if len(got) != len(tt.want) {
			t.Fatalf("FilterByLevel(%q) returned %d entries, want %d: %+v", tt.level, len(got), len(tt.want), got)
		}
		for i, id := range tt.want {
			if got[i].ID != id {
				t.Fatalf("FilterByLevel(%q) = %+v, want IDs %v", tt.level, got, tt.want)
			}
		}
	}
}
