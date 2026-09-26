// Service operations over harness descriptors: diff preview, atomic
// install/remove with backups, and versioned skills management.
package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstallResult reports what an install changed.
type InstallResult struct {
	Path    string
	Backup  string
	Changed bool
}

// SkillInstallResult reports installed skills.
type SkillInstallResult struct {
	Path      string
	Installed []string
}

// SkillUpdate flags an outdated install location.
type SkillUpdate struct {
	Name     string `json:"name"`
	Have     string `json:"have"`
	Want     string `json:"want"`
	Scope    string `json:"scope"`
	Harness  string `json:"harness"`
	Outdated bool   `json:"outdated"`
}

// Preview returns a unified-style diff of what InstallMCP would change,
// without writing anything.
func Preview(harnessID, scope, kind string) (string, error) {
	d, err := FindDescriptor(harnessID)
	if err != nil {
		return "", err
	}
	if kind == "" || kind == "mcp" {
		sc, ok := d.MCP.Scopes[scope]
		if !ok {
			return "", fmt.Errorf("harness %q has no mcp scope %q", harnessID, scope)
		}
		path := expandScopePath(sc.Path, scope)
		entry := mcpEntryLine(harnessID)
		before := ""
		if data, err := os.ReadFile(path); err == nil {
			before = string(data)
		}
		after := before
		if !strings.Contains(before, entry) {
			after = before + "\n# managed by agent-runtime (preview)\n# " + entry + "\n"
		}
		if before == after {
			return "(no changes: entry already present in " + path + ")", nil
		}
		return "--- " + path + "\n+++ " + path + " (proposed)\n" + lineDiff(before, after), nil
	}
	sc, ok := d.Skills.Scopes[scope]
	if !ok {
		return "", fmt.Errorf("harness %q has no skills scope %q", harnessID, scope)
	}
	return "(skills install to " + expandScopePath(sc.Dir, scope) + ": " +
		strings.Join(SkillNames(), ", ") + ")", nil
}

func mcpEntryLine(harnessID string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "agent-runtime"
	}
	return fmt.Sprintf("%s: %s serve (stdio)", harnessID, exe)
}

func expandScopePath(p, scope string) string {
	_ = scope
	return expandHome(p)
}

func lineDiff(before, after string) string {
	var sb strings.Builder
	for _, l := range strings.Split(after, "\n") {
		if !strings.Contains(before, l) && strings.TrimSpace(l) != "" {
			sb.WriteString("+" + l + "\n")
		}
	}
	return sb.String()
}

// InstallMCP writes the stdio entry `agent-runtime serve` for a harness.
// Global scope is the recommended default (one install serves every
// project); project scope remains for checked-in team configs.
func InstallMCP(harnessID, scope string) (InstallResult, error) {
	agent, projectDir, err := mapHarness(harnessID, scope)
	if err != nil {
		return InstallResult{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return InstallResult{}, err
	}
	path, err := Write(agent, Scope(scope), exe, projectDir)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Path: path, Backup: path + ".agent-runtime.bak", Changed: true}, nil
}

// RemoveMCP removes the entry.
func RemoveMCP(harnessID, scope string) (InstallResult, error) {
	agent, projectDir, err := mapHarness(harnessID, scope)
	if err != nil {
		return InstallResult{}, err
	}
	path, changed, err := Remove(agent, Scope(scope), projectDir)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Path: path, Changed: changed}, nil
}

func mapHarness(harnessID, scope string) (Agent, string, error) {
	if scope != "global" && scope != "project" {
		return "", "", fmt.Errorf("scope must be global|project, got %q", scope)
	}
	cwd := ""
	if scope == "project" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", "", err
		}
	}
	switch harnessID {
	case "claude-code":
		return Claude, cwd, nil
	case "codex":
		return Codex, cwd, nil
	case "gemini":
		return Gemini, cwd, nil
	case "opencode":
		return OpenCode, cwd, nil
	default:
		// New harnesses (cursor, windsurf, vscode-copilot, zed, cline)
		// share the generic JSON writer until dedicated support lands.
		if _, err := FindDescriptor(harnessID); err != nil {
			return "", "", err
		}
		return Generic, cwd, nil
	}
}

// Skill describes one embedded skill with its version.
type Skill struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// EmbeddedSkills lists embedded skills with versions parsed from
// SKILL.md frontmatter (version: key; skills without one are "0").
func EmbeddedSkills() []Skill {
	var out []Skill
	for _, name := range SkillNames() {
		out = append(out, Skill{
			Name:        name,
			Version:     skillVersion(name),
			Description: skillDescription(name),
		})
	}
	return out
}

func skillFile(name string) string {
	// SkillFiles are embed paths; resolve against the source tree when
	// running from repo, else report unknown version.
	for _, f := range SkillFiles() {
		if strings.Contains(f, name+"/SKILL.md") {
			if data, err := os.ReadFile(f); err == nil {
				return string(data)
			}
		}
	}
	return ""
}

func skillVersion(name string) string {
	for _, line := range strings.Split(skillFile(name), "\n") {
		if strings.HasPrefix(line, "version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		}
	}
	return "0"
}

func skillDescription(name string) string {
	for _, line := range strings.Split(skillFile(name), "\n") {
		if strings.HasPrefix(line, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return ""
}

// InstallSkills installs embedded skills into a harness scope.
func InstallSkills(harnessID, scope string) (SkillInstallResult, error) {
	d, err := FindDescriptor(harnessID)
	if err != nil {
		return SkillInstallResult{}, err
	}
	sc, ok := d.Skills.Scopes[scope]
	if !ok {
		return SkillInstallResult{}, fmt.Errorf("harness %q has no skills scope %q", harnessID, scope)
	}
	dir := expandHome(sc.Dir)
	if scope == "project" {
		if cwd, err := os.Getwd(); err == nil {
			dir = strings.ReplaceAll(sc.Dir, "{project}", cwd)
			dir = expandHome(dir)
		}
	}
	var installed []string
	for _, sk := range EmbeddedSkills() {
		dest := filepath.Join(dir, sk.Name)
		created, updated, _, err := InstallSkillTree(dest)
		if err != nil {
			return SkillInstallResult{}, err
		}
		if created+updated > 0 {
			installed = append(installed, sk.Name)
		}
	}
	return SkillInstallResult{Path: dir, Installed: installed}, nil
}

// RemoveSkills removes embedded skills from a harness scope.
func RemoveSkills(harnessID, scope string) error {
	if scope != "global" && scope != "project" {
		return fmt.Errorf("scope must be global|project, got %q", scope)
	}
	base := ""
	if scope == "project" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	_, err := RemoveSkillTrees(Scope(scope), base)
	return err
}

// CheckSkillUpdates compares installed and embedded skill versions.
func CheckSkillUpdates() []SkillUpdate {
	var out []SkillUpdate
	for _, d := range Descriptors() {
		for scopeName, sc := range d.Skills.Scopes {
			dir := expandHome(sc.Dir)
			for _, sk := range EmbeddedSkills() {
				verFile := filepath.Join(dir, sk.Name, "VERSION")
				have := ""
				if data, err := os.ReadFile(verFile); err == nil {
					have = strings.TrimSpace(string(data))
				} else if _, err := os.Stat(filepath.Join(dir, sk.Name, "SKILL.md")); err != nil {
					continue // not installed here
				}
				if have != sk.Version {
					out = append(out, SkillUpdate{
						Name: sk.Name, Have: have, Want: sk.Version,
						Scope: scopeName, Harness: d.ID, Outdated: true,
					})
				}
			}
		}
	}
	return out
}
