package cli

import (
	"strings"
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

func TestStringList(t *testing.T) {
	var l stringList
	if got := l.String(); got != "[]" {
		t.Fatalf("String() of empty list = %q, want %q", got, "[]")
	}
	for _, v := range []string{"A=1", "B=2", "C=3"} {
		if err := l.Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
	}
	want := []string{"A=1", "B=2", "C=3"}
	if len(l) != len(want) {
		t.Fatalf("len = %d, want %d", len(l), len(want))
	}
	for i := range want {
		if l[i] != want[i] {
			t.Fatalf("l[%d] = %q, want %q", i, l[i], want[i])
		}
	}
	if got := l.String(); got != "[A=1 B=2 C=3]" {
		t.Fatalf("String() = %q, want %q", got, "[A=1 B=2 C=3]")
	}
}

func TestParseRunArgs(t *testing.T) {
	opts, err := parseRunArgs([]string{
		"--app", "web",
		"--workdir", "/tmp/w",
		"--env", "A=1",
		"--env", "B=2",
		"--env-file", "x.env",
		"cmd", "arg1", "arg2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.app != "web" || opts.workdir != "/tmp/w" {
		t.Fatalf("app/workdir = %q/%q, want web//tmp/w", opts.app, opts.workdir)
	}
	if len(opts.env) != 2 || opts.env[0] != "A=1" || opts.env[1] != "B=2" {
		t.Fatalf("env = %v, want [A=1 B=2]", []string(opts.env))
	}
	if opts.envFile != "x.env" {
		t.Fatalf("envFile = %q, want x.env", opts.envFile)
	}
	if opts.command != "cmd" || len(opts.args) != 2 || opts.args[0] != "arg1" || opts.args[1] != "arg2" {
		t.Fatalf("command/args = %q/%v, want cmd/[arg1 arg2]", opts.command, opts.args)
	}
}

func TestParseRunArgsRejectsBadFlag(t *testing.T) {
	if _, err := parseRunArgs([]string{"--no-such-flag"}); err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestShellCommandMissingTarget(t *testing.T) {
	err := ShellCommand(nil, nil, nil)
	if err == nil {
		t.Fatal("expected usage error for missing target, got nil")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Fatalf("error = %v, want usage error", err)
	}
}
