package integrate

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skillFS embeds the agent-runtime-ready skill tree so the binary can install
// it after `go install` with no repo checkout on disk. Paths are relative to
// this file's directory, hence the leading "skill".
//
//go:embed all:skill
var skillFS embed.FS

// skillRoot is the directory inside skillFS that contains the skill bundle.
const skillRoot = "skill/agent-runtime-ready"

// SkillFiles returns the sorted relative paths of every file in the embedded
// agent-runtime-ready skill, e.g. "SKILL.md", "reference/logging.md",
// "templates/agent-runtime.yaml".
func SkillFiles() []string {
	var out []string
	fs.WalkDir(skillFS, skillRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, strings.TrimPrefix(path, skillRoot+"/"))
		return nil
	})
	sort.Strings(out)
	return out
}

// SkillTargetDirs returns the directories the agent-runtime-ready skill
// installs into for the given scope. Each returned path is the skill directory
// itself (…/skills/agent-runtime-ready), not the skills root.
func SkillTargetDirs(scope Scope, baseDir string) []string {
	switch scope {
	case ScopeProject:
		return []string{
			filepath.Join(baseDir, ".claude", "skills", "agent-runtime-ready"),
			filepath.Join(baseDir, ".opencode", "skills", "agent-runtime-ready"),
		}
	default: // ScopeGlobal
		home, _ := os.UserHomeDir()
		return []string{
			filepath.Join(home, ".claude", "skills", "agent-runtime-ready"),
			filepath.Join(home, ".config", "opencode", "skills", "agent-runtime-ready"),
		}
	}
}

// InstallSkillTree writes every file of the embedded agent-runtime-ready skill
// into destDir, creating parent directories as needed. It reports how many
// files were created, updated (existed with different bytes), or left
// unchanged (existed with identical bytes). Any failure is wrapped with the
// target file path.
func InstallSkillTree(destDir string) (created, updated, unchanged int, err error) {
	werr := fs.WalkDir(skillFS, skillRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := skillFS.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, strings.TrimPrefix(path, skillRoot+"/"))
		existing, err := os.ReadFile(target)
		switch {
		case err == nil && bytes.Equal(existing, data):
			unchanged++
			return nil
		case err == nil:
			updated++
		case os.IsNotExist(err):
			created++
		default:
			return fmt.Errorf("%s: %w", target, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("%s: %w", target, err)
		}
		return nil
	})
	if werr != nil {
		return 0, 0, 0, werr
	}
	return created, updated, unchanged, nil
}
