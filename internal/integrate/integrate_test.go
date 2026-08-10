package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selfPath(t *testing.T) string {
	t.Helper()
	p, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBlocksContainServerCommand(t *testing.T) {
	p := selfPath(t)
	for _, agent := range Supported() {
		block, err := Block(agent, p)
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		if !strings.Contains(block, p) {
			t.Fatalf("%s block missing binary path:\n%s", agent, block)
		}
		if !strings.Contains(block, "serve") {
			t.Fatalf("%s block missing serve arg:\n%s", agent, block)
		}
	}
}

func TestWriteClaudeMerge(t *testing.T) {
	dir := t.TempDir()
	p := selfPath(t)
	path, err := Write(Claude, p, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != ".mcp.json" {
		t.Fatalf("path = %s", path)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "agent-runtime") {
		t.Fatalf("missing entry:\n%s", data)
	}
	// Merging again (simulating an existing .mcp.json with other servers) must
	// not clobber pre-existing entries.
	other := `{"mcpServers":{"claude-code":{"command":"claude","args":["code"]}}}`
	if err := os.WriteFile(path, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(Claude, p, dir); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "claude-code") || !strings.Contains(s, "agent-runtime") {
		t.Fatalf("merge clobbered entries:\n%s", s)
	}
}

func TestWriteCodexAppendIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := selfPath(t)
	path, err := Write(Codex, p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "[mcp_servers.agent-runtime]") {
		t.Fatalf("missing section:\n%s", data)
	}
	// Second write must not duplicate.
	if _, err := Write(Codex, p, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if got := strings.Count(string(data), "[mcp_servers.agent-runtime]"); got != 1 {
		t.Fatalf("section duplicated %d times:\n%s", got, data)
	}
}

func TestWriteOpenCodeMerge(t *testing.T) {
	dir := t.TempDir()
	p := selfPath(t)
	path, err := Write(OpenCode, p, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "opencode.json" {
		t.Fatalf("path = %s", path)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	for _, want := range []string{`"$schema"`, `"type": "local"`, `"enabled": true`, "serve", p} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	// Merging again (simulating an existing opencode.json with other servers)
	// must not clobber pre-existing entries or the schema field.
	if _, err := Write(OpenCode, p, dir); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	s = string(data)
	if strings.Count(s, `"agent-runtime"`) != 1 {
		t.Fatalf("agent-runtime duplicated:\n%s", s)
	}
	if !strings.Contains(s, `"$schema"`) {
		t.Fatalf("schema lost on merge:\n%s", s)
	}
}

func TestBlockGeneric(t *testing.T) {
	block, err := Block(Generic, "/usr/bin/agent-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "stdio") {
		t.Fatalf("generic block should mention stdio:\n%s", block)
	}
}
