package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
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

func TestDiscoverOpenCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := t.TempDir()

	cands := DiscoverOpenCode(proj)
	if len(cands) != 5 {
		t.Fatalf("got %d candidates, want 5", len(cands))
	}
	if cands[0].Scope != ScopeProject || cands[0].Exists {
		t.Fatalf("first candidate wrong: %+v", cands[0])
	}
	if cands[4].Scope != ScopeGlobal || cands[4].Exists {
		t.Fatalf("last candidate wrong: %+v", cands[4])
	}

	// Project opencode.json and global opencode.jsonc both exist.
	if err := os.WriteFile(filepath.Join(proj, "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	gdir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "opencode.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cands = DiscoverOpenCode(proj)
	if !cands[0].Exists || !cands[4].Exists {
		t.Fatalf("discovery missed existing configs: %+v", cands)
	}
	existing := 0
	for _, c := range cands {
		if c.Exists {
			existing++
		}
	}
	if existing != 2 {
		t.Fatalf("existing candidates = %d, want 2", existing)
	}
}

func TestMergeOpenCodeConfigPreservesEverything(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.jsonc")
	original := `{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"], // keep this comment
  "skills": {
    "paths": [".opencode/skills"]
  }, /* block comment */
  "mcp": {
    "codegraph": {
      "type": "local",
      "command": ["codegraph", "serve", "--mcp"],
      "enabled": true
    } // server comment
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := MergeOpenCodeConfig(path, []string{"/usr/bin/agent-runtime", "serve"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	out, _ := os.ReadFile(path)
	s := string(out)

	// Comments and every pre-existing line must survive.
	for _, want := range []string{
		"// keep this comment",
		"/* block comment */",
		"// server comment",
		`"codegraph"`,
		`"instructions"`,
		`"AGENTS.md"`,
		`"skills"`,
		".opencode/skills",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("lost %q:\n%s", want, s)
		}
	}
	// Entry present and shaped correctly.
	for _, want := range []string{
		`"agent-runtime"`,
		`"type": "local"`,
		`"/usr/bin/agent-runtime"`,
		`"serve"`,
		`"enabled": true`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	// No double commas, still valid JSONC.
	if strings.Contains(s, ",,") {
		t.Fatalf("double comma produced:\n%s", s)
	}
	if _, err := hujson.Parse(out); err != nil {
		t.Fatalf("result not valid JSONC: %v\n%s", err, s)
	}

	// Idempotent: a second merge is a no-op.
	changed, err = MergeOpenCodeConfig(path, []string{"/usr/bin/agent-runtime", "serve"})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second merge should be a no-op")
	}
	if got := strings.Count(string(mustRead(t, path)), `"agent-runtime"`); got != 1 {
		t.Fatalf("agent-runtime present %d times", got)
	}
}

func TestMergeOpenCodeConfigAddsMCPWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte("{\n  \"instructions\": [\"AGENTS.md\"]\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeOpenCodeConfig(path, []string{"/bin/agent-runtime", "serve"}); err != nil {
		t.Fatal(err)
	}
	s := string(mustRead(t, path))
	for _, want := range []string{`"mcp"`, `"agent-runtime"`, `"instructions"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	if _, err := hujson.Parse([]byte(s)); err != nil {
		t.Fatalf("not valid JSONC: %v\n%s", err, s)
	}
}

func TestMergeOpenCodeConfigRequiresJSONC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeOpenCodeConfig(path, []string{"/bin/agent-runtime", "serve"}); err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
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
