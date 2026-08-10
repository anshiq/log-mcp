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
