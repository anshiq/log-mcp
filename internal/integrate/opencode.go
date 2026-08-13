// OpenCode integration. Unlike the other agents, opencode config files are
// JSONC (comments and trailing commas allowed) and can live in several places
// (project or global, .json or .jsonc). Merging must never rewrite anything the
// user did not ask for, so installs are performed as a byte-preserving syntax
// tree edit: only the mcp.agent-runtime member is inserted, and the file is
// packed back byte-for-byte from the parsed tree (comments, formatting and
// every other key untouched).
package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

// Scope identifies where an opencode config lives.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

// OpenCodeSchema is the JSON schema pointer opencode configs carry.
const OpenCodeSchema = "https://opencode.ai/config.json"

// Candidate is one place an opencode config may live.
type Candidate struct {
	Scope  Scope
	Path   string
	Exists bool
}

// DiscoverOpenCode returns the config locations opencode reads, ordered by
// opencode's precedence (project first, then global). Exists reports whether a
// file is present at that path today.
func DiscoverOpenCode(projectDir string) []Candidate {
	home, _ := os.UserHomeDir()
	cands := []Candidate{
		{Scope: ScopeProject, Path: filepath.Join(projectDir, "opencode.json")},
		{Scope: ScopeProject, Path: filepath.Join(projectDir, "opencode.jsonc")},
		{Scope: ScopeProject, Path: filepath.Join(projectDir, ".opencode", "opencode.json")},
		{Scope: ScopeGlobal, Path: filepath.Join(home, ".config", "opencode", "opencode.json")},
		{Scope: ScopeGlobal, Path: filepath.Join(home, ".config", "opencode", "opencode.jsonc")},
	}
	for i := range cands {
		if _, err := os.Stat(cands[i].Path); err == nil {
			cands[i].Exists = true
		}
	}
	return cands
}

// OpenCodeEntry describes the mcp server entry installed into a config.
func OpenCodeEntry(cmd []string) string {
	entry := map[string]any{"type": "local", "command": cmd, "enabled": true}
	b, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		panic(err) // entry is a closed shape; cannot fail
	}
	return string(b)
}

// MergeOpenCodeConfig surgically inserts the agent-runtime server entry into an
// existing opencode config. It reports whether the config was changed (a
// second call for the same path is a no-op) and never touches keys other than
// mcp.agent-runtime.
func MergeOpenCodeConfig(path string, cmd []string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return false, fmt.Errorf("%s: not valid JSON/JSONC: %w", path, err)
	}
	root, ok := v.Value.(*hujson.Object)
	if !ok {
		return false, fmt.Errorf("%s: top level must be a JSON object", path)
	}

	var mcpVal *hujson.Value
	for i := range root.Members {
		if lit, ok := root.Members[i].Name.Value.(hujson.Literal); ok && string(lit) == `"mcp"` {
			mcpVal = &root.Members[i].Value
			break
		}
	}

	if mcpVal == nil {
		// No top-level "mcp": add it, wrapping our entry in a new object.
		rootIndent := indentOf(root)
		mcpIndent := rootIndent + "  "
		mcpValue := "{\n" + renderMember("agent-runtime", cmd, mcpIndent) + "\n" + rootIndent + "}"
		pair := parseMemberPair("mcp", mcpValue)
		pair.Name.BeforeExtra = hujson.Extra("\n" + rootIndent)
		root.Members = append(root.Members, pair)
	} else {
		obj, ok := mcpVal.Value.(*hujson.Object)
		if !ok {
			return false, fmt.Errorf("%s: \"mcp\" must be a JSON object", path)
		}
		for i := range obj.Members {
			if lit, ok := obj.Members[i].Name.Value.(hujson.Literal); ok && string(lit) == `"agent-runtime"` {
				return false, nil // already configured
			}
		}
		indent := indentOf(obj)
		pair := parseMemberPair("agent-runtime", renderValue(cmd, indent))
		pair.Name.BeforeExtra = hujson.Extra("\n" + indent)
		obj.Members = append(obj.Members, pair)
	}

	if err := os.WriteFile(path, v.Pack(), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveOpenCodeConfig surgically removes the agent-runtime entry from the mcp
// object of an opencode config. It reports whether the config was changed. Like
// MergeOpenCodeConfig it only touches the mcp.agent-runtime member — comments,
// formatting and every other key survive byte-for-byte.
func RemoveOpenCodeConfig(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return false, fmt.Errorf("%s: not valid JSON/JSONC: %w", path, err)
	}
	root, ok := v.Value.(*hujson.Object)
	if !ok {
		return false, fmt.Errorf("%s: top level must be a JSON object", path)
	}
	for i := range root.Members {
		lit, ok := root.Members[i].Name.Value.(hujson.Literal)
		if !ok || string(lit) != `"mcp"` {
			continue
		}
		obj, ok := root.Members[i].Value.Value.(*hujson.Object)
		if !ok {
			return false, fmt.Errorf("%s: \"mcp\" must be a JSON object", path)
		}
		for j := range obj.Members {
			mlit, ok := obj.Members[j].Name.Value.(hujson.Literal)
			if !ok || string(mlit) != `"agent-runtime"` {
				continue
			}
			obj.Members = append(obj.Members[:j], obj.Members[j+1:]...)
			if err := os.WriteFile(path, v.Pack(), 0o644); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil // "mcp" present but no agent-runtime entry
	}
	return false, nil // no top-level "mcp"
}

// renderMember renders a `"name": <entry>` pair indented at nameIndent (the
// indent of the line the member's name sits on).
func renderMember(name string, cmd []string, nameIndent string) string {
	lines := strings.Split(OpenCodeEntry(cmd), "\n")
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s%q: %s", nameIndent, name, lines[0])
	for _, l := range lines[1:] {
		sb.WriteString("\n")
		if l != "" {
			sb.WriteString(nameIndent + l)
		}
	}
	return sb.String()
}

// renderValue renders the pretty-printed server entry such that its first line
// sits on the member-name line and every continuation line is indented at
// nameIndent (the indent of the line the member's name sits on).
func renderValue(cmd []string, nameIndent string) string {
	lines := strings.Split(OpenCodeEntry(cmd), "\n")
	var sb strings.Builder
	sb.WriteString(lines[0])
	for _, l := range lines[1:] {
		sb.WriteString("\n")
		if l != "" {
			sb.WriteString(nameIndent + l)
		}
	}
	return sb.String()
}

// parseMemberPair parses a single `{"name": value}` object and returns its
// member. AfterExtra is left nil so no trailing comma is emitted.
func parseMemberPair(name, valueText string) hujson.ObjectMember {
	pair, err := hujson.Parse([]byte(`{"` + name + `": ` + valueText + `}`))
	if err != nil {
		panic(err) // valueText is always valid JSON we constructed
	}
	return pair.Value.(*hujson.Object).Members[0]
}

// indentOf guesses the indentation used by an object's members so inserts blend
// in with the surrounding file instead of being reflowed.
func indentOf(obj *hujson.Object) string {
	if len(obj.Members) > 0 {
		be := string(obj.Members[0].Name.BeforeExtra)
		if i := strings.LastIndex(be, "\n"); i >= 0 && len(be) > i+1 {
			return be[i+1:]
		}
	}
	return "  "
}
