package integrate

import (
	"encoding/json"
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

func TestRemoveClaudeJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	cfg := `{"mcpServers":{"claude-code":{"command":"claude","args":["code"]},"agent-runtime":{"command":"/x","args":["serve"]}},"other":1}`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	gotPath, changed, err := Remove(Claude, dir)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path || !changed {
		t.Fatalf("path=%s changed=%v, want %s/true", gotPath, changed, path)
	}
	out := string(mustRead(t, path))
	if strings.Contains(out, "agent-runtime") {
		t.Fatalf("agent-runtime still present:\n%s", out)
	}
	for _, want := range []string{"claude-code", `"other"`, "claude"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q:\n%s", want, out)
		}
	}
	// Removing again is a no-op.
	_, changed, err = Remove(Claude, dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second remove should be a no-op")
	}
}

func TestRemoveJSONEntryRemovesEmptyTop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"agent-runtime":{"command":"/x","args":["serve"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Remove(Claude, dir); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatal("expected change")
	}
	out := string(mustRead(t, path))
	if strings.Contains(out, "mcpServers") {
		t.Fatalf("empty mcpServers not pruned:\n%s", out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("result not valid JSON: %s", out)
	}
}

func TestRemoveGeminiUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"agent-runtime":{"command":"/x","args":["serve"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gotPath, changed, err := Remove(Gemini, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path || !changed {
		t.Fatalf("path=%s changed=%v, want %s/true", gotPath, changed, path)
	}
}

func TestRemoveCodexTOMLKeepsOtherSections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `[model]
name = "gpt-5"

[mcp_servers.agent-runtime]
command = "/usr/bin/agent-runtime"
args = ["serve"]

[other]
value = 1
`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	gotPath, changed, err := Remove(Codex, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path || !changed {
		t.Fatalf("path=%s changed=%v, want %s/true", gotPath, changed, path)
	}
	out := string(mustRead(t, path))
	if strings.Contains(out, "mcp_servers.agent-runtime") {
		t.Fatalf("section still present:\n%s", out)
	}
	for _, want := range []string{"[model]", `name = "gpt-5"`, "[other]", "value = 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q:\n%s", want, out)
		}
	}
	// Second remove is a no-op.
	_, changed, err = Remove(Codex, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second remove should be a no-op")
	}
}

func TestRemoveCodexTOMLAtEOF(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[mcp_servers.agent-runtime]\ncommand = \"/usr/bin/agent-runtime\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Remove(Codex, t.TempDir()); err != nil {
		t.Fatal(err)
	} else if !changed {
		t.Fatal("expected change")
	}
	out := string(mustRead(t, path))
	if strings.Contains(out, "mcp_servers") {
		t.Fatalf("section still present:\n%s", out)
	}
}

func TestRemoveOpenCodeConfigPreservesEverything(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.jsonc")
	original := `{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"], // keep this comment
  "mcp": {
    "codegraph": {
      "type": "local",
      "command": ["codegraph", "serve", "--mcp"],
      "enabled": true
    }, // server comment
    "agent-runtime": {
      "type": "local",
      "command": ["/usr/bin/agent-runtime", "serve"],
      "enabled": true
    }
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := RemoveOpenCodeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	out := string(mustRead(t, path))
	for _, want := range []string{
		"// keep this comment",
		`"codegraph"`,
		`"instructions"`,
		`"AGENTS.md"`,
		"$schema",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "agent-runtime") {
		t.Fatalf("agent-runtime still present:\n%s", out)
	}
	if _, err := hujson.Parse([]byte(out)); err != nil {
		t.Fatalf("result not valid JSONC: %v\n%s", err, out)
	}
	// Idempotent.
	changed, err = RemoveOpenCodeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second remove should be a no-op")
	}
}

func TestRemoveOpenCodeConfigNoMCP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte("{\n  \"instructions\": [\"AGENTS.md\"]\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := RemoveOpenCodeConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("no mcp key, remove should be a no-op")
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
