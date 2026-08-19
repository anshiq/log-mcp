package mcp

import (
	"encoding/json"
	"testing"
)

// startInputJSON mirrors the json tags of the start_process input struct so we
// can fuzz the json decode path without pulling the go-sdk decoder into scope.
type startInputJSON struct {
	App     string   `json:"app,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	WorkDir string   `json:"workdir,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// getLogsInputJSON mirrors the json tags of the get_logs input struct.
type getLogsInputJSON struct {
	ProcessID string `json:"process_id"`
	Stream    string `json:"stream,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	Contains  string `json:"contains,omitempty"`
}

// FuzzStartInput ensures json.Unmarshal of a start_process-shaped payload never
// panics on arbitrary bytes.
func FuzzStartInput(f *testing.F) {
	seeds := []string{
		`{"app":"backend","command":"npm","args":["run","dev"],"workdir":"./be","env":["A=1"]}`,
		`{"command":"go","args":["run","."]}`,
		`{}`,
		`{"args":["unterminated`,
		`not json at all`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var in startInputJSON
		_ = json.Unmarshal(data, &in)
	})
}

// FuzzGetLogsInput ensures json.Unmarshal of a get_logs-shaped payload never
// panics on arbitrary bytes.
func FuzzGetLogsInput(f *testing.F) {
	seeds := []string{
		`{"process_id":"p1","stream":"stderr","lines":100,"contains":"error"}`,
		`{"process_id":"p1"}`,
		`{}`,
		`{"lines":-5}`,
		`{"process_id":`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var in getLogsInputJSON
		_ = json.Unmarshal(data, &in)
	})
}
