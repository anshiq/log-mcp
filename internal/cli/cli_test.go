package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-runtime/internal/integrate"
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
			name:     "remove agent",
			args:     []string{"claude", "--remove"},
			wantOpts: integrateOptions{remove: true},
			wantPos:  []string{"claude"},
		},
		{
			name:     "remove skill project scope",
			args:     []string{"skill", "--remove", "--scope", "project"},
			wantOpts: integrateOptions{remove: true, scope: "project"},
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

func TestParseShellArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantShell string
		wantPID   int
		wantPos   []string
		wantErr   string
	}{
		{
			name:      "flag after target",
			args:      []string{"proc_abc", "--shell", "/bin/sh"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "flag before target",
			args:      []string{"--shell", "/bin/sh", "proc_abc"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "inline double dash",
			args:      []string{"proc_abc", "--shell=/bin/sh"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "inline single dash",
			args:      []string{"-shell=/bin/bash", "proc_abc"},
			wantShell: "/bin/bash",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "single dash space separated",
			args:      []string{"-shell", "/bin/sh", "proc_abc"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_abc"},
		},
		{
			name:      "multiple targets still returned",
			args:      []string{"proc_a", "proc_b", "--shell", "/bin/sh"},
			wantShell: "/bin/sh",
			wantPos:   []string{"proc_a", "proc_b"},
		},
		{
			name:      "pid space separated double dash",
			args:      []string{"--pid", "1234", "--shell", "/bin/sh"},
			wantShell: "/bin/sh",
			wantPID:   1234,
		},
		{
			name:    "pid space separated single dash",
			args:    []string{"-pid", "1234"},
			wantPID: 1234,
		},
		{
			name:    "pid equals form",
			args:    []string{"--pid=1234"},
			wantPID: 1234,
		},
		{
			name:    "pid after target still parses both",
			args:    []string{"web", "--pid", "1234"},
			wantPID: 1234,
			wantPos: []string{"web"},
		},
		{
			name:    "empty args",
			args:    nil,
			wantPos: []string{},
		},
		{
			name:    "unknown flag",
			args:    []string{"proc_abc", "--bogus"},
			wantErr: `unknown flag "--bogus"`,
		},
		{
			name:    "help treated as unknown flag",
			args:    []string{"--help"},
			wantErr: `unknown flag "--help"`,
		},
		{
			name:    "shell flag at end without value",
			args:    []string{"proc_abc", "--shell"},
			wantErr: "--shell requires an argument",
		},
		{
			name:    "single dash shell flag at end without value",
			args:    []string{"-shell"},
			wantErr: "--shell requires an argument",
		},
		{
			name:    "pid flag at end without value",
			args:    []string{"--pid"},
			wantErr: "--pid requires an argument",
		},
		{
			name:    "single dash pid flag at end without value",
			args:    []string{"-pid"},
			wantErr: "--pid requires an argument",
		},
		{
			name:    "invalid pid value",
			args:    []string{"--pid=abc"},
			wantErr: `invalid --pid value "abc"`,
		},
		{
			name:    "invalid pid value space form",
			args:    []string{"--pid", "abc"},
			wantErr: `invalid --pid value "abc"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotShell, gotPID, gotPos, err := parseShellArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got shell=%q pid=%d pos=%v", tt.wantErr, gotShell, gotPID, gotPos)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotShell != tt.wantShell {
				t.Fatalf("shell = %q, want %q", gotShell, tt.wantShell)
			}
			if gotPID != tt.wantPID {
				t.Fatalf("pid = %d, want %d", gotPID, tt.wantPID)
			}
			if len(gotPos) != len(tt.wantPos) {
				t.Fatalf("pos = %v, want %v", gotPos, tt.wantPos)
			}
			for i := range tt.wantPos {
				if gotPos[i] != tt.wantPos[i] {
					t.Fatalf("pos = %v, want %v", gotPos, tt.wantPos)
				}
			}
		})
	}
}

// TestShellCommandRejectsPidWithTarget verifies the combination check in
// ShellCommand: --pid and a positional target are mutually exclusive. The
// check fires before any runtime is constructed, so nil loaded/logger are fine.
func TestShellCommandRejectsPidWithTarget(t *testing.T) {
	err := ShellCommand([]string{"--pid", "1234", "web"}, nil, nil)
	if err == nil {
		t.Fatal("expected error combining --pid with a target")
	}
	if !strings.Contains(err.Error(), "cannot combine --pid with a target") {
		t.Fatalf("error = %v, want combination error", err)
	}
}

func TestShellTarget(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantProcID string
		wantApp    string
	}{
		{
			name:       "proc id",
			target:     "proc_abc",
			wantProcID: "proc_abc",
			wantApp:    "",
		},
		{
			name:       "app name",
			target:     "web",
			wantProcID: "",
			wantApp:    "web",
		},
		{
			name:       "empty target",
			target:     "",
			wantProcID: "",
			wantApp:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procID, app := shellTarget(tt.target)
			if procID != tt.wantProcID || app != tt.wantApp {
				t.Fatalf("shellTarget(%q) = (%q, %q), want (%q, %q)", tt.target, procID, app, tt.wantProcID, tt.wantApp)
			}
		})
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
		"agent-runtime-logging/SKILL.md",
		"agent-runtime-project-config/SKILL.md",
		"agent-runtime-ready/SKILL.md",
		".claude/skills/agent-runtime-ready",
		".config/opencode/skills/agent-runtime-ready",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("output missing %q:\n%s", want, data)
		}
	}
}

// TestIntegrateSkillRemove installs the skills into a temp project then removes
// them via `integrate skill --remove --scope project`. It must not prompt
// (stdin is not a TTY in tests) and must leave no skill directories behind.
func TestIntegrateSkillRemove(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(oldwd)
	}()

	// Nothing installed: a no-op, no error.
	if err := Integrate([]string{"skill", "--remove"}); err != nil {
		t.Fatalf("remove with nothing installed = %v", err)
	}

	dirs := integrate.SkillTargetDirs(integrate.ScopeProject, proj)
	for _, d := range dirs {
		if _, _, _, err := integrate.InstallSkillTree(d); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "out.txt")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	err = Integrate([]string{"skill", "--remove", "--scope", "project"})
	f.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("Integrate(skill --remove) = %v", err)
	}
	data, _ := os.ReadFile(out)
	for _, want := range []string{"removed", ".claude/skills/agent-runtime-ready", ".opencode/skills/agent-runtime-ready"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("output missing %q:\n%s", want, data)
		}
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after remove", d)
		}
	}
}
