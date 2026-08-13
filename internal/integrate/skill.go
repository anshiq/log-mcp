// Embedded skill bundle installation. The bundle lives in skill/ and is
// compiled into the binary via go:embed so `agent-runtime integrate skill`
// works after `go install` with no repo checkout on disk. Each skill installs
// into the agent skill trees (Claude Code's .claude/skills and opencode's
// .config/opencode/skills — or project .opencode/skills), at global or project
// scope.
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

// skillFS embeds every skill under skill/<skill-name>/.
//
//go:embed all:skill
var skillFS embed.FS

// skillRoot is the top-level directory inside skillFS holding the skills.
const skillRoot = "skill"

// SkillNames returns the embedded skill names, sorted.
func SkillNames() []string {
	entries, err := fs.ReadDir(skillFS, skillRoot)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// SkillFiles returns the sorted relative paths of every file in the bundle,
// each prefixed with its skill name, e.g. "agent-runtime-ready/SKILL.md".
func SkillFiles() []string {
	var out []string
	_ = fs.WalkDir(skillFS, skillRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
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

// SkillTargetDirs returns the directories every skill installs into for the
// given scope. Each returned path is a skill directory itself
// (…/skills/<skill-name>), not the skills root. For each skill there is one
// Claude Code target and one opencode target.
func SkillTargetDirs(scope Scope, baseDir string) []string {
	var dirs []string
	for _, name := range SkillNames() {
		dirs = append(dirs, skillDirs(scope, baseDir, name)...)
	}
	return dirs
}

// skillDirs returns the two agent skill directories for one skill. opencode
// reads project skills from .opencode/skills and global skills from
// .config/opencode/skills; Claude Code reads both scopes from .claude/skills.
func skillDirs(scope Scope, baseDir, name string) []string {
	root := baseDir
	if scope != ScopeProject {
		root, _ = os.UserHomeDir()
	}
	opencode := filepath.Join(root, ".config", "opencode", "skills", name)
	if scope == ScopeProject {
		opencode = filepath.Join(root, ".opencode", "skills", name)
	}
	return []string{
		filepath.Join(root, ".claude", "skills", name),
		opencode,
	}
}

// InstallSkillTree writes every file of the embedded skill named after
// destDir's basename into destDir, creating parent directories as needed. It
// reports how many files were created, updated (existed with different bytes),
// or left unchanged (existed with identical bytes).
func InstallSkillTree(destDir string) (created, updated, unchanged int, err error) {
	name := filepath.Base(destDir)
	root := filepath.ToSlash(filepath.Join(skillRoot, name))
	if _, err := fs.Stat(skillFS, root); err != nil {
		return 0, 0, 0, fmt.Errorf("unknown skill %q", name)
	}
	werr := fs.WalkDir(skillFS, root, func(path string, d fs.DirEntry, walkErr error) error {
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
		target := filepath.Join(destDir, strings.TrimPrefix(path, root+"/"))
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

// RemoveSkillTrees removes every installed copy of the embedded skills for the
// given scope. It returns the directories actually removed (a directory that
// was never installed is skipped, not an error).
func RemoveSkillTrees(scope Scope, baseDir string) ([]string, error) {
	var removed []string
	for _, d := range SkillTargetDirs(scope, baseDir) {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			continue
		}
		if err := os.RemoveAll(d); err != nil {
			return removed, err
		}
		removed = append(removed, d)
	}
	return removed, nil
}
