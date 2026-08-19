package profile

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzProfileReadiness feeds arbitrary log lines through every built-in
// profile's readiness regexes (and the Generic fallback, which has a nil ready
// list and must return false rather than panic) to ensure matching never
// panics.
func FuzzProfileReadiness(f *testing.F) {
	reg := Default()
	seeds := []string{
		"Ready in 1.3s",
		"Started DemoApplication in 2.4 seconds",
		"Tomcat started on port 8080",
		"Starting development server at http://127.0.0.1:8000/",
		"listening on :8080",
		"arbitrary garbage line",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		for _, p := range reg.List() {
			p.Ready(line)
		}
		if Generic.Ready(line) {
			t.Fatal("Generic profile must never report ready")
		}
	})
}

// FuzzDetect feeds arbitrary file contents into a workdir and runs Detect
// against it, ensuring the detection/scoring path never panics on arbitrary
// bytes.
func FuzzDetect(f *testing.F) {
	seeds := []string{
		`{"dependencies":{"next":"15"}}`,
		"<project><dependencies/></project>",
		"module agent-runtime",
		"#!/usr/bin/env python",
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	// Every file name a profile rule looks for; the same fuzz bytes are written
	// to each so every scoring branch is exercised.
	ruleFiles := []string{
		"package.json",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"manage.py",
		"requirements.txt",
		"pyproject.toml",
		"go.mod",
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		for _, name := range ruleFiles {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		Default().Detect(dir)
	})
}
