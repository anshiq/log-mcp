package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestLoadFromFindsConfigUpward(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
runtime:
  log_buffer_lines: 500
  shutdown_timeout: 10s
apps:
  backend:
    type: spring-boot
    workdir: ./backend
    command: ["./mvnw", "spring-boot:run"]
  frontend:
    type: nextjs
`
	if err := os.WriteFile(filepath.Join(root, "agent-runtime.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadFrom(sub)
	if err != nil {
		t.Fatal(err)
	}
	if l.ProjectDir != root {
		t.Fatalf("project dir = %q, want %q", l.ProjectDir, root)
	}
	if l.Config.Runtime.LogBufferLines != 500 {
		t.Fatalf("log_buffer_lines = %d", l.Config.Runtime.LogBufferLines)
	}
	if l.Config.Runtime.ShutdownTimeout.Time() != 10*time.Second {
		t.Fatalf("shutdown_timeout = %v", l.Config.Runtime.ShutdownTimeout)
	}
	if !reflect.DeepEqual(l.Config.Apps["backend"].Command, []string{"./mvnw", "spring-boot:run"}) {
		t.Fatalf("backend command = %v", l.Config.Apps["backend"].Command)
	}
	if l.Config.Apps["frontend"].Type != "nextjs" {
		t.Fatalf("frontend type = %q", l.Config.Apps["frontend"].Type)
	}
}

func TestDefaults(t *testing.T) {
	l, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rc := l.Config.Runtime
	if rc.LogBufferLines != 10000 {
		t.Fatalf("default log_buffer_lines = %d", rc.LogBufferLines)
	}
	if rc.ShutdownTimeout.Time() != 5*time.Second {
		t.Fatalf("default shutdown_timeout = %v", rc.ShutdownTimeout)
	}
	if rc.MaxLogLines != 2000 {
		t.Fatalf("default max_log_lines = %d", rc.MaxLogLines)
	}
	if rc.MaxLogBytes != 512*1024 {
		t.Fatalf("default max_log_bytes = %d", rc.MaxLogBytes)
	}
	if rc.MaxExitedProcesses != 50 {
		t.Fatalf("default max_exited_processes = %d", rc.MaxExitedProcesses)
	}
	if rc.LogStore != "memory" {
		t.Fatalf("default log_store = %q, want memory", rc.LogStore)
	}
}

func TestMaxExitedProcessesOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte("runtime:\n  max_exited_processes: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Config.Runtime.MaxExitedProcesses; got != 7 {
		t.Fatalf("max_exited_processes = %d, want 7", got)
	}
}

func TestInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-runtime.yaml")
	if err := os.WriteFile(path, []byte("runtime:\n  shutdown_timeout: notaduration\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestNamesSorted(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("apps:\n")
	names := []string{"zebra", "alpha", "mango", "delta", "bravo", "echo", "charlie", "foxtrot", "golf", "lima"}
	for _, n := range names {
		fmt.Fprintf(&b, "  %s:\n    command: [\"true\"]\n", n)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := l.Names()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("Names() not sorted: %v", got)
	}
	if len(got) != len(names) {
		t.Fatalf("Names() length = %d, want %d", len(got), len(names))
	}
}

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `
# comment
API_KEY=secret123
DATABASE_URL="postgres://localhost/db"
EMPTY=
QUOTED='single'
SIMPLE=value
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := ParseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"API_KEY":      "secret123",
		"DATABASE_URL": "postgres://localhost/db",
		"EMPTY":        "",
		"QUOTED":       "single",
		"SIMPLE":       "value",
	}
	got := map[string]string{}
	for _, kv := range env {
		i := indexOf(kv, "=")
		got[kv[:i]] = kv[i+1:]
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("env[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseEnvFileMissing(t *testing.T) {
	if _, err := ParseEnvFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error for missing env file")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i < len(s)-len(sub)+1; i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestMergeEnvPrecedence(t *testing.T) {
	layers := []EnvLayer{
		{Name: "shell", Vars: []string{"A=1", "B=2", "C=3"}},
		{Name: "runtime.env", Vars: []string{"B=20", "D=4"}},
		{Name: "env_file", Vars: []string{"A=10", "E=5"}},
		{Name: "app.env", Vars: []string{"C=30", "B=200"}},
		{Name: "request.env", Vars: []string{"A=100", "F=6"}},
	}
	merged, source := MergeEnv(layers...)

	// First-seen key order: A (shell), B (shell), C (shell), D (runtime.env),
	// E (env_file), F (request.env).
	wantOrder := []string{"A", "B", "C", "D", "E", "F"}
	if len(merged) != len(wantOrder) {
		t.Fatalf("merged length = %d, want %d: %v", len(merged), len(wantOrder), merged)
	}
	for i, want := range wantOrder {
		got := merged[i][:indexOf(merged[i], "=")]
		if got != want {
			t.Fatalf("merged[%d] key = %q, want %q", i, got, want)
		}
	}

	// Last layer wins for duplicate keys.
	wantVals := map[string]string{
		"A": "100", "B": "200", "C": "30", "D": "4", "E": "5", "F": "6",
	}
	gotVals := map[string]string{}
	for _, kv := range merged {
		i := indexOf(kv, "=")
		gotVals[kv[:i]] = kv[i+1:]
	}
	for k, v := range wantVals {
		if gotVals[k] != v {
			t.Fatalf("merged[%q] = %q, want %q", k, gotVals[k], v)
		}
	}

	// Provenance points at the winning layer for each key.
	wantSrc := map[string]string{
		"A": "request.env", "B": "app.env", "C": "app.env",
		"D": "runtime.env", "E": "env_file", "F": "request.env",
	}
	for k, v := range wantSrc {
		if source[k] != v {
			t.Fatalf("source[%q] = %q, want %q", k, source[k], v)
		}
	}
	if len(source) != len(wantSrc) {
		t.Fatalf("source length = %d, want %d", len(source), len(wantSrc))
	}

	// Duplicate keys appear exactly once.
	seen := map[string]bool{}
	for _, kv := range merged {
		k := kv[:indexOf(kv, "=")]
		if seen[k] {
			t.Fatalf("duplicate key %q in merged output", k)
		}
		seen[k] = true
	}
}

func TestMergeEnvRobustness(t *testing.T) {
	layers := []EnvLayer{
		{Name: "empty", Vars: nil},
		{Name: "junk", Vars: []string{"=x", "", "OK=1", "NOEQUALS", "CASE=upper"}},
		{Name: "case", Vars: []string{"case=lower"}},
	}
	merged, source := MergeEnv(layers...)
	// "=x" (empty key), "" and "NOEQUALS" (no '=') are skipped; "CASE" and
	// "case" are distinct keys (case-sensitive).
	want := []string{"OK=1", "CASE=upper", "case=lower"}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("merged = %v, want %v", merged, want)
	}
	if source["OK"] != "junk" || source["CASE"] != "junk" || source["case"] != "case" {
		t.Fatalf("unexpected provenance: %v", source)
	}
}

func TestMergeEnvVars(t *testing.T) {
	got := MergeEnvVars(
		[]string{"A=1", "B=2"},
		[]string{"B=3", "C=4"},
		[]string{},
	)
	want := []string{"A=1", "B=3", "C=4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergeEnvVars = %v, want %v", got, want)
	}
}

func TestParseNulEnv(t *testing.T) {
	data := []byte("FOO=bar\x00PATH=/usr/bin\x00\x00BAZ=qux\x00EMBED=a=b=c\x00NOEQUALS\x00\x00")
	got, err := ParseNulEnv(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"FOO=bar", "PATH=/usr/bin", "BAZ=qux", "EMBED=a=b=c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseNulEnv = %v, want %v", got, want)
	}
}

func TestCaptureShellEnv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "capture-env.sh")
	// printf \000 is a NUL byte; includes empty chunks and a no-'=' chunk.
	body := "#!/bin/sh\nprintf 'FOO=bar\\000PATH=/usr/bin\\000\\000BAZ=qux\\000NOEQUALS\\000'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := CaptureShellEnv(script, 5*time.Second)
	if err != nil {
		t.Fatalf("CaptureShellEnv: %v", err)
	}
	want := []string{"FOO=bar", "PATH=/usr/bin", "BAZ=qux"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CaptureShellEnv = %v, want %v", got, want)
	}
}

func TestCaptureShellEnvTimeout(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow-env.sh")
	// exec replaces the shell with sleep so the killed child is sleep itself.
	body := "#!/bin/sh\nexec sleep 10\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if got, err := CaptureShellEnv(script, 100*time.Millisecond); err == nil {
		t.Fatalf("expected timeout error, got %v", got)
	} else if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took too long: %v", time.Since(start))
	}
}

func TestRedactEnv(t *testing.T) {
	vars := []string{
		"API_KEY=abc123",
		"apikey=no-underscore",
		"DATABASE_URL=postgres://localhost/db",
		"PASSWORD=hunter2",
		"my_token=xyz",
		"ACCESS_KEY=AKIAEXAMPLE",
		"PRIVATE_KEY=-----BEGIN",
		"AUTH_TOKEN=tok",
		"secret_value=topsecret",
		"PATH=/usr/bin",
	}
	redacted, n := RedactEnv(vars, false)
	if n != 8 {
		t.Fatalf("redacted count = %d, want 8", n)
	}
	if len(redacted) != len(vars) {
		t.Fatalf("redacted length = %d, want %d", len(redacted), len(vars))
	}
	for i, kv := range redacted {
		if kv == vars[i] {
			continue
		}
		if !strings.HasSuffix(kv, "=***") {
			t.Fatalf("redacted[%d] = %q, want *** value", i, kv)
		}
	}
	// Redacted entries have "***"; non-matching keys keep their value.
	redactedKeys := map[int]bool{0: true, 1: true, 3: true, 4: true, 5: true, 6: true, 7: true, 8: true}
	for i, kv := range redacted {
		val := kv[indexOf(kv, "=")+1:]
		if redactedKeys[i] {
			if val != "***" {
				t.Fatalf("redacted[%d] value = %q, want \"***\"", i, val)
			}
		} else if val != vars[i][indexOf(vars[i], "=")+1:] {
			t.Fatalf("redacted[%d] value = %q, want %q", i, val, vars[i][indexOf(vars[i], "=")+1:])
		}
	}
	// Non-matching keys pass through unchanged.
	if redacted[2] != vars[2] || redacted[9] != vars[9] {
		t.Fatalf("non-secret entries were modified: %v", redacted)
	}

	// reveal=true returns the slice unchanged with count 0.
	out, n := RedactEnv(vars, true)
	if n != 0 {
		t.Fatalf("reveal count = %d, want 0", n)
	}
	if !reflect.DeepEqual(out, vars) {
		t.Fatal("reveal=true modified the slice")
	}
}
