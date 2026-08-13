package integrate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSkillNames(t *testing.T) {
	got := SkillNames()
	want := []string{"agent-runtime-logging", "agent-runtime-ready"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SkillNames() = %v, want %v", got, want)
	}
}

func TestSkillFiles(t *testing.T) {
	got := SkillFiles()
	for _, want := range []string{
		"agent-runtime-ready/SKILL.md",
		"agent-runtime-ready/reference/agent-runtime-yaml.md",
		"agent-runtime-ready/reference/agent-workflow.md",
		"agent-runtime-ready/reference/logging.md",
		"agent-runtime-ready/templates/agent-runtime.yaml",
		"agent-runtime-logging/SKILL.md",
		"agent-runtime-logging/reference/logging.md",
		"agent-runtime-logging/templates/logging_config.py",
		"agent-runtime-logging/templates/log_event.py",
	} {
		if !containsStr(got, want) {
			t.Fatalf("SkillFiles() missing %q: %v", want, got)
		}
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestInstallSkillTree(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "agent-runtime-ready")
	logging := filepath.Join(t.TempDir(), "agent-runtime-logging")

	// First install of each skill: every file is created.
	created, updated, unchanged, err := InstallSkillTree(ready)
	if err != nil {
		t.Fatal(err)
	}
	if created != 5 || updated != 0 || unchanged != 0 {
		t.Fatalf("ready first install: created=%d updated=%d unchanged=%d, want 5/0/0", created, updated, unchanged)
	}
	created, updated, unchanged, err = InstallSkillTree(logging)
	if err != nil {
		t.Fatal(err)
	}
	if created != 4 || updated != 0 || unchanged != 0 {
		t.Fatalf("logging first install: created=%d updated=%d unchanged=%d, want 4/0/0", created, updated, unchanged)
	}

	// Files landed at the expected relative paths with content.
	for _, f := range []string{"SKILL.md", "reference/logging.md"} {
		data, err := os.ReadFile(filepath.Join(logging, f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s installed empty", f)
		}
	}

	// Second install: everything is byte-identical, nothing rewritten.
	created, updated, unchanged, err = InstallSkillTree(ready)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || updated != 0 || unchanged != 5 {
		t.Fatalf("ready second install: created=%d updated=%d unchanged=%d, want 0/0/5", created, updated, unchanged)
	}

	// Modify one file; the third install must report exactly one update.
	mod := filepath.Join(ready, "SKILL.md")
	orig, err := os.ReadFile(mod)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod, append(orig, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	created, updated, unchanged, err = InstallSkillTree(ready)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || updated != 1 || unchanged != 4 {
		t.Fatalf("ready third install: created=%d updated=%d unchanged=%d, want 0/1/4", created, updated, unchanged)
	}

	// Unknown skill name is an error.
	if _, _, _, err := InstallSkillTree(filepath.Join(t.TempDir(), "no-such-skill")); err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestRemoveSkillTrees(t *testing.T) {
	base := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Nothing installed yet: a no-op, not an error.
	removed, err := RemoveSkillTrees(ScopeProject, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed %v from an empty tree", removed)
	}

	// Install at project scope, then remove.
	dirs := SkillTargetDirs(ScopeProject, base)
	for _, d := range dirs {
		if _, _, _, err := InstallSkillTree(d); err != nil {
			t.Fatal(err)
		}
	}
	removed, err = RemoveSkillTrees(ScopeProject, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != len(dirs) {
		t.Fatalf("removed %d dirs, want %d: %v", len(removed), len(dirs), removed)
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after remove", d)
		}
	}

	// Global scope untouched by the project removal.
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "agent-runtime-ready")); !os.IsNotExist(err) {
		t.Fatalf("project removal touched global scope: %v", err)
	}

	// Second remove is a no-op.
	removed, err = RemoveSkillTrees(ScopeProject, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("second remove returned %v", removed)
	}
}

func TestSkillTargetDirs(t *testing.T) {
	base := t.TempDir()

	project := SkillTargetDirs(ScopeProject, base)
	wantProject := []string{
		filepath.Join(base, ".claude", "skills", "agent-runtime-logging"),
		filepath.Join(base, ".opencode", "skills", "agent-runtime-logging"),
		filepath.Join(base, ".claude", "skills", "agent-runtime-ready"),
		filepath.Join(base, ".opencode", "skills", "agent-runtime-ready"),
	}
	if !reflect.DeepEqual(project, wantProject) {
		t.Fatalf("project dirs = %v, want %v", project, wantProject)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	global := SkillTargetDirs(ScopeGlobal, base)
	wantGlobal := []string{
		filepath.Join(home, ".claude", "skills", "agent-runtime-logging"),
		filepath.Join(home, ".config", "opencode", "skills", "agent-runtime-logging"),
		filepath.Join(home, ".claude", "skills", "agent-runtime-ready"),
		filepath.Join(home, ".config", "opencode", "skills", "agent-runtime-ready"),
	}
	if !reflect.DeepEqual(global, wantGlobal) {
		t.Fatalf("global dirs = %v, want %v", global, wantGlobal)
	}
}
