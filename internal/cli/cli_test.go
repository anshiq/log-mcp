package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-runtime/internal/logs"
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
			name:     "skill write",
			args:     []string{"skill", "--write"},
			wantOpts: integrateOptions{write: true},
			wantPos:  []string{"skill"},
		},
		{
			name:     "skill write project scope",
			args:     []string{"skill", "--write", "--scope", "project"},
			wantOpts: integrateOptions{write: true, scope: "project"},
			wantPos:  []string{"skill"},
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

// TestDrainLogsPrintsPendingOnce exercises the shared drain helper: given a
// cursor that has already consumed some entries (simulating what the tail tick
// loop printed), it prints exactly the remaining pending entries once, advances
// the cursor, and prints nothing on a second call.
func TestDrainLogsPrintsPendingOnce(t *testing.T) {
	pl := logs.NewProcessLogs(1000, nil)
	pl.Append(logs.StreamStdout, "already printed by tail")
	pl.Append(logs.StreamStdout, "server listening on http://127.0.0.1:8080")
	pl.Append(logs.StreamStderr, "shutting down")

	next := uint64(1) // past the first entry, as tail left it

	var buf bytes.Buffer
	drainLogs(pl, &buf, &next)
	want := "[stdout] server listening on http://127.0.0.1:8080\n[stderr] shutting down\n"
	if buf.String() != want {
		t.Fatalf("drainLogs output = %q, want %q", buf.String(), want)
	}
	if next != 3 {
		t.Fatalf("cursor after drain = %d, want 3", next)
	}

	buf.Reset()
	drainLogs(pl, &buf, &next)
	if buf.String() != "" {
		t.Fatalf("second drain printed %q, want nothing (no double-print)", buf.String())
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

// TestIntegrateSkillPrintsHint exercises the print-only path of `integrate
// skill`: it must not write anything (only print), and the output must show
// the install hint, the target dirs, and the embedded file list.
func TestIntegrateSkillPrintsHint(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.txt")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	err = Integrate([]string{"skill"})
	f.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("Integrate([]string{\"skill\"}) = %v, want nil", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# To install: agent-runtime integrate skill --write",
		"SKILL.md",
		"reference/logging.md",
		"reference/agent-runtime-yaml.md",
		"reference/agent-workflow.md",
		"templates/agent-runtime.yaml",
		".claude/skills/agent-runtime-ready",
		".config/opencode/skills/agent-runtime-ready",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("output missing %q:\n%s", want, data)
		}
	}
}
