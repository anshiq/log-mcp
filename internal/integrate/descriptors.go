// Data-driven harness descriptors: the per-agent switch statements are
// replaced by embedded YAML descriptors, overridable in
// $XDG_CONFIG_HOME/agent-runtime/harnesses.d/. IntegrationService runs in
// the daemon so CLI, TUI and GUI share one implementation and audit trail.
package integrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Descriptor describes one coding-agent harness.
type Descriptor struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"display_name"`
	Detect      struct {
		Binaries []string `yaml:"binaries"`
		Paths    []string `yaml:"paths"`
	} `yaml:"detect"`
	MCP struct {
		Scopes map[string]MCPScope `yaml:"scopes"`
	} `yaml:"mcp"`
	Skills struct {
		Scopes map[string]SkillScope `yaml:"scopes"`
	} `yaml:"skills"`
}

// MCPScope is one install location for an MCP entry.
type MCPScope struct {
	Path   string `yaml:"path"`
	Format string `yaml:"format"`
	Key    string `yaml:"key"`
}

// SkillScope is one install location for skills.
type SkillScope struct {
	Dir string `yaml:"dir"`
}

// Detection reports what is known about a harness on this machine.
type Detection struct {
	Detected        bool
	Version         string
	MCPConfigured   bool
	SkillsInstalled []string
}

var embedded = []string{
	harnessClaudeCode, harnessCodex, harnessGemini, harnessOpenCode,
	harnessCursor, harnessWindsurf, harnessVSCode, harnessZed, harnessCline,
}

const harnessClaudeCode = `
id: claude-code
display_name: Claude Code
detect:
  binaries: [claude]
  paths: ["~/.claude", "~/.claude.json"]
mcp:
  scopes:
    project: { path: "{project}/.mcp.json", format: json, key: "mcpServers.agent-runtime" }
    global:  { path: "~/.claude.json", format: json, key: "mcpServers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.claude/skills" }
    global:  { dir: "~/.claude/skills" }
`

const harnessCodex = `
id: codex
display_name: Codex
detect:
  binaries: [codex]
  paths: ["~/.codex/config.toml"]
mcp:
  scopes:
    project: { path: "{project}/.codex/config.toml", format: toml, key: "mcp_servers.agent-runtime" }
    global:  { path: "~/.codex/config.toml", format: toml, key: "mcp_servers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.codex/skills" }
    global:  { dir: "~/.codex/skills" }
`

const harnessGemini = `
id: gemini
display_name: Gemini CLI
detect:
  binaries: [gemini]
  paths: ["~/.gemini/settings.json"]
mcp:
  scopes:
    project: { path: "{project}/.gemini/settings.json", format: json, key: "mcpServers.agent-runtime" }
    global:  { path: "~/.gemini/settings.json", format: json, key: "mcpServers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.gemini/skills" }
    global:  { dir: "~/.gemini/skills" }
`

const harnessOpenCode = `
id: opencode
display_name: opencode
detect:
  binaries: [opencode]
  paths: ["~/.config/opencode/opencode.json"]
mcp:
  scopes:
    project: { path: "{project}/opencode.json", format: jsonc, key: "mcp.agent-runtime" }
    global:  { path: "~/.config/opencode/opencode.json", format: jsonc, key: "mcp.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.opencode/skills" }
    global:  { dir: "~/.config/opencode/skills" }
`

const harnessCursor = `
id: cursor
display_name: Cursor
detect:
  binaries: [cursor]
  paths: ["~/.cursor/mcp.json"]
mcp:
  scopes:
    project: { path: "{project}/.cursor/mcp.json", format: json, key: "mcpServers.agent-runtime" }
    global:  { path: "~/.cursor/mcp.json", format: json, key: "mcpServers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.cursor/skills" }
    global:  { dir: "~/.cursor/skills" }
`

const harnessWindsurf = `
id: windsurf
display_name: Windsurf
detect:
  binaries: [windsurf]
  paths: ["~/.codeium/windsurf/mcp_config.json"]
mcp:
  scopes:
    project: { path: "{project}/.windsurf/mcp_config.json", format: json, key: "mcpServers.agent-runtime" }
    global:  { path: "~/.codeium/windsurf/mcp_config.json", format: json, key: "mcpServers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.windsurf/skills" }
    global:  { dir: "~/.codeium/windsurf/skills" }
`

const harnessVSCode = `
id: vscode-copilot
display_name: VS Code (Copilot)
detect:
  binaries: [code]
  paths: ["~/.config/Code/User/mcp.json", "~/.vscode/mcp.json"]
mcp:
  scopes:
    project: { path: "{project}/.vscode/mcp.json", format: json, key: "servers.agent-runtime" }
    global:  { path: "~/.vscode/mcp.json", format: json, key: "servers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.vscode/skills" }
    global:  { dir: "~/.vscode/skills" }
`

const harnessZed = `
id: zed
display_name: Zed
detect:
  binaries: [zed]
  paths: ["~/.config/zed/settings.json"]
mcp:
  scopes:
    project: { path: "{project}/.zed/settings.json", format: json, key: "context_servers.agent-runtime" }
    global:  { path: "~/.config/zed/settings.json", format: json, key: "context_servers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.zed/skills" }
    global:  { dir: "~/.config/zed/skills" }
`

const harnessCline = `
id: cline
display_name: Cline
detect:
  binaries: []
  paths: ["~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/cline_mcp_settings.json"]
mcp:
  scopes:
    project: { path: "{project}/.roo/mcp.json", format: json, key: "mcpServers.agent-runtime" }
    global:  { path: "~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/cline_mcp_settings.json", format: json, key: "mcpServers.agent-runtime" }
skills:
  scopes:
    project: { dir: "{project}/.roo/skills" }
    global:  { dir: "~/.config/roo/skills" }
`

// Descriptors returns embedded harness descriptors plus user overrides
// from $XDG_CONFIG_HOME/agent-runtime/harnesses.d/*.yaml.
func Descriptors() []Descriptor {
	var out []Descriptor
	for _, raw := range embedded {
		var d Descriptor
		if err := yaml.Unmarshal([]byte(raw), &d); err != nil {
			continue
		}
		out = append(out, d)
	}
	if home, err := os.UserHomeDir(); err == nil {
		cfg := os.Getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		dir := filepath.Join(cfg, "agent-runtime", "harnesses.d")
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
				data, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				var d Descriptor
				if err := yaml.Unmarshal(data, &d); err != nil {
					continue
				}
				replaced := false
				for i, cur := range out {
					if cur.ID == d.ID {
						out[i] = d
						replaced = true
					}
				}
				if !replaced {
					out = append(out, d)
				}
			}
		}
	}
	return out
}

// FindDescriptor returns the descriptor for an id or an error listing known ids.
func FindDescriptor(id string) (Descriptor, error) {
	for _, d := range Descriptors() {
		if d.ID == id {
			return d, nil
		}
	}
	var ids []string
	for _, d := range Descriptors() {
		ids = append(ids, d.ID)
	}
	return Descriptor{}, fmt.Errorf("unknown harness %q (known: %s)", id, strings.Join(ids, ", "))
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return os.ExpandEnv(p)
}

// DetectGlobal reports detection + install state for the global scope.
func DetectGlobal(d Descriptor) Detection {
	var det Detection
	for _, bin := range d.Detect.Binaries {
		if path, err := exec.LookPath(bin); err == nil {
			det.Detected = true
			det.Version = toolVersion(bin, path)
			break
		}
	}
	for _, p := range d.Detect.Paths {
		if _, err := os.Stat(expandHome(p)); err == nil {
			det.Detected = true
			break
		}
	}
	if sc, ok := d.MCP.Scopes["global"]; ok {
		if _, err := os.Stat(expandHome(sc.Path)); err == nil {
			det.MCPConfigured = true
		}
	}
	if sc, ok := d.Skills.Scopes["global"]; ok {
		dir := expandHome(sc.Dir)
		for _, sk := range EmbeddedSkills() {
			if _, err := os.Stat(filepath.Join(dir, sk.Name, "SKILL.md")); err == nil {
				det.SkillsInstalled = append(det.SkillsInstalled, sk.Name)
			}
		}
	}
	return det
}

func toolVersion(bin, path string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		_ = path
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
