// Package integrate generates and installs MCP client configuration for the
// coding agents the runtime targets (Claude Code, Codex, Gemini CLI, and any
// generic MCP-over-stdio client). Writing files outside the project is opt-in
// via --write; the default is to print the exact config block.
package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Agent identifies a supported MCP client.
type Agent string

const (
	Claude   Agent = "claude"
	Codex    Agent = "codex"
	Gemini   Agent = "gemini"
	OpenCode Agent = "opencode"
	Generic  Agent = "generic"
)

// Supported returns the agents that have concrete config formats.
func Supported() []Agent { return []Agent{Claude, Codex, Gemini, OpenCode} }

// serverCommand builds the stdio command line for the runtime binary.
func serverCommand(serverPath string) []string {
	return []string{serverPath, "serve"}
}

// Block returns the configuration snippet for an agent. serverPath should be
// the absolute path to the agent-runtime binary.
func Block(agent Agent, serverPath string) (string, error) {
	cmd := serverCommand(serverPath)
	switch agent {
	case Claude:
		return claudeBlock(cmd)
	case Codex:
		return codexBlock(cmd)
	case Gemini:
		return geminiBlock(cmd)
	case OpenCode:
		return opencodeBlock(cmd)
	case Generic:
		return genericBlock(cmd)
	}
	return "", fmt.Errorf("unknown agent %q", agent)
}

func claudeBlock(cmd []string) (string, error) {
	cfg := map[string]any{"mcpServers": map[string]any{
		"agent-runtime": map[string]any{"command": cmd[0], "args": cmd[1:]},
	}}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func codexBlock(cmd []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("[mcp_servers.agent-runtime]\n")
	fmt.Fprintf(&sb, "command = %q\n", cmd[0])
	if len(cmd) > 1 {
		args := make([]string, len(cmd)-1)
		for i, a := range cmd[1:] {
			args[i] = fmt.Sprintf("%q", a)
		}
		fmt.Fprintf(&sb, "args = [%s]\n", strings.Join(args, ", "))
	}
	return sb.String(), nil
}

func geminiBlock(cmd []string) (string, error) {
	cfg := map[string]any{"mcpServers": map[string]any{
		"agent-runtime": map[string]any{"command": cmd[0], "args": cmd[1:]},
	}}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func genericBlock(cmd []string) (string, error) {
	return fmt.Sprintf("Command: %s\nArgs:    %s\nTransport: stdio (MCP)\n", cmd[0], strings.Join(cmd[1:], " ")), nil
}

func opencodeBlock(cmd []string) (string, error) {
	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{
			"agent-runtime": map[string]any{"type": "local", "command": cmd, "enabled": true},
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// targetPath returns where an agent's config lives. projectDir is used for
// Claude's project-scoped .mcp.json.
func targetPath(agent Agent, projectDir string) string {
	home, _ := os.UserHomeDir()
	switch agent {
	case Claude:
		return filepath.Join(projectDir, ".mcp.json")
	case Codex:
		return filepath.Join(home, ".codex", "config.toml")
	case Gemini:
		return filepath.Join(home, ".gemini", "settings.json")
	case OpenCode:
		return filepath.Join(projectDir, "opencode.json")
	}
	return ""
}

// Write installs the config block for an agent. Claude and Gemini configs are
// JSON and are merged into any existing file; Codex's TOML config is appended
// if the section is not already present. It returns the file path written.
func Write(agent Agent, serverPath, projectDir string) (string, error) {
	path := targetPath(agent, projectDir)
	if path == "" {
		return "", fmt.Errorf("unknown agent %q", agent)
	}
	block, err := Block(agent, serverPath)
	if err != nil {
		return "", err
	}

	switch agent {
	case Claude:
		if err := mergeJSON(path, block, "mcpServers", "agent-runtime"); err != nil {
			return "", err
		}
	case Gemini:
		if err := mergeJSON(path, block, "mcpServers", "agent-runtime"); err != nil {
			return "", err
		}
	case OpenCode:
		if err := mergeJSON(path, block, "mcp", "agent-runtime"); err != nil {
			return "", err
		}
		if err := ensureSchema(path); err != nil {
			return "", err
		}
	case Codex:
		if err := appendTOML(path, block); err != nil {
			return "", err
		}
	}
	return path, nil
}

// mergeJSON merges a JSON config block's "mcpServers.agent-runtime" entry into
// an existing JSON file (creating it if needed).
func mergeJSON(path, block, topKey, serverKey string) error {
	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	}
	// Parse the block's top-level map and transplant just the server entry.
	var blockCfg map[string]any
	if err := json.Unmarshal([]byte(block), &blockCfg); err != nil {
		return err
	}
	servers, _ := blockCfg[topKey].(map[string]any)
	entry, _ := servers[serverKey].(map[string]any)

	top, _ := existing[topKey].(map[string]any)
	if top == nil {
		top = map[string]any{}
		existing[topKey] = top
	}
	top[serverKey] = entry

	return writeJSON(path, existing)
}

// ensureSchema adds the opencode config schema pointer to an existing JSON
// file without disturbing its other keys.
func ensureSchema(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if _, ok := cfg["$schema"]; ok {
		return nil
	}
	cfg["$schema"] = "https://opencode.ai/config.json"
	return writeJSON(path, cfg)
}

func writeJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// appendTOML appends a TOML section unless the section key already exists.
func appendTOML(path, block string) error {
	key := "mcp_servers.agent-runtime"
	if data, err := os.ReadFile(path); err == nil {
		if strings.Contains(string(data), key) {
			return nil // already configured
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n" + block)
	return err
}
