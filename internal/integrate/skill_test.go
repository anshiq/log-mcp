package integrate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSkillFiles(t *testing.T) {
	want := []string{
		"SKILL.md",
		"reference/agent-runtime-yaml.md",
		"reference/agent-workflow.md",
		"reference/logging.md",
		"templates/agent-runtime.yaml",
	}
	got := SkillFiles()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SkillFiles() = %v, want %v", got, want)
	}
}

func TestInstallSkillTree(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "agent-runtime-ready")

	// First install: everything is created.
	created, updated, unchanged, err := InstallSkillTree(dest)
	if err != nil {
		t.Fatal(err)
	}
	if created != 5 || updated != 0 || unchanged != 0 {
		t.Fatalf("first install: created=%d updated=%d unchanged=%d, want 5/0/0", created, updated, unchanged)
	}
	for _, f := range SkillFiles() {
		data, err := os.ReadFile(filepath.Join(dest, f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s installed empty", f)
		}
	}

	// Second install: everything is byte-identical, nothing rewritten.
	created, updated, unchanged, err = InstallSkillTree(dest)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || updated != 0 || unchanged != 5 {
		t.Fatalf("second install: created=%d updated=%d unchanged=%d, want 0/0/5", created, updated, unchanged)
	}

	// Modify one file; the third install must report exactly one update.
	mod := filepath.Join(dest, "SKILL.md")
	orig, err := os.ReadFile(mod)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod, append(orig, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	created, updated, unchanged, err = InstallSkillTree(dest)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || updated != 1 || unchanged != 4 {
		t.Fatalf("third install: created=%d updated=%d unchanged=%d, want 0/1/4", created, updated, unchanged)
	}
}

func TestSkillTargetDirs(t *testing.T) {
	base := t.TempDir()

	project := SkillTargetDirs(ScopeProject, base)
	wantProject := []string{
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
		filepath.Join(home, ".claude", "skills", "agent-runtime-ready"),
		filepath.Join(home, ".config", "opencode", "skills", "agent-runtime-ready"),
	}
	if !reflect.DeepEqual(global, wantGlobal) {
		t.Fatalf("global dirs = %v, want %v", global, wantGlobal)
	}
}
