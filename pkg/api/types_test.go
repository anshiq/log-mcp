package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestJSONRoundTrip verifies the new request/response types survive a
// JSON encode/decode round trip without losing fields.
func TestJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   any
		out  any
	}{
		{
			name: "start request with env_file",
			in:   &StartRequest{App: "web", Env: []string{"A=1"}, EnvFile: ".env.local"},
			out:  &StartRequest{},
		},
		{
			name: "status result with pgid",
			in:   &StatusResult{ProcessID: "p1", Status: "running", PID: 42, PGID: 42},
			out:  &StatusResult{},
		},
		{
			name: "signal process request",
			in:   &SignalProcessRequest{ProcessID: "p1", Signal: "SIGTERM"},
			out:  &SignalProcessRequest{},
		},
		{
			name: "signal process result",
			in:   &SignalProcessResult{ProcessID: "p1", Signal: "SIGTERM"},
			out:  &SignalProcessResult{},
		},
		{
			name: "process env request",
			in:   &ProcessEnvRequest{ProcessID: "p1", Live: true},
			out:  &ProcessEnvRequest{},
		},
		{
			name: "process env result",
			in:   &ProcessEnvResult{ProcessID: "p1", Live: true, PID: 42, Env: []string{"A=1"}, Source: map[string]string{"A": "spec"}, Redacted: 1},
			out:  &ProcessEnvResult{},
		},
		{
			name: "open shell request",
			in:   &OpenShellRequest{ProcessID: "p1", Shell: "/bin/bash"},
			out:  &OpenShellRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := json.Unmarshal(data, tt.out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(tt.in, tt.out) {
				t.Fatalf("round trip mismatch:\n in:  %#v\n out: %#v", tt.in, tt.out)
			}
		})
	}
}
