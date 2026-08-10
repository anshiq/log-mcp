package cli

import (
	"testing"
)

func TestParseIntegrateArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantOpts integrateOptions
		wantPos  []string
		wantErr  bool
	}{
		{
			name:    "print only",
			args:    []string{"opencode"},
			wantPos: []string{"opencode"},
		},
		{
			name:     "write after agent",
			args:     []string{"opencode", "--write"},
			wantOpts: integrateOptions{write: true},
			wantPos:  []string{"opencode"},
		},
		{
			name:     "write before agent",
			args:     []string{"--write", "claude"},
			wantOpts: integrateOptions{write: true},
			wantPos:  []string{"claude"},
		},
		{
			name:     "full flag set",
			args:     []string{"--scope", "global", "opencode", "--yes", "--create", "--no-verify", "--write"},
			wantOpts: integrateOptions{write: true, yes: true, create: true, noVerify: true, scope: "global"},
			wantPos:  []string{"opencode"},
		},
		{
			name:     "scope equals form",
			args:     []string{"opencode", "--scope=project"},
			wantOpts: integrateOptions{scope: "project"},
			wantPos:  []string{"opencode"},
		},
		{
			name:    "dangling scope",
			args:    []string{"opencode", "--scope"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pos, err := parseIntegrateArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got opts=%+v pos=%v", got, pos)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(pos) != 1 || pos[0] != tt.wantPos[0] {
				t.Fatalf("pos = %v, want %v", pos, tt.wantPos)
			}
			if got != tt.wantOpts {
				t.Fatalf("opts = %+v, want %+v", got, tt.wantOpts)
			}
		})
	}
}
