package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectProfiles(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"nextjs", map[string]string{"package.json": `{"dependencies":{"next":"15"}}`}, "nextjs"},
		{"node only", map[string]string{"package.json": `{"scripts":{"dev":"node server.js"}}`}, "node"},
		{"spring boot", map[string]string{"pom.xml": "<project/>"}, "spring-boot"},
		{"spring gradle", map[string]string{"build.gradle": "dependencies {}"}, "spring-boot"},
		{"django", map[string]string{"manage.py": "#!/usr/bin/env python"}, "django"},
		{"python", map[string]string{"requirements.txt": "django\n"}, "python"},
		{"go", map[string]string{"go.mod": "module x\n"}, "go"},
		{"generic", map[string]string{}, "generic"},
		{"django beats python", map[string]string{"manage.py": "x", "requirements.txt": "y"}, "django"},
		{"nextjs beats node", map[string]string{"package.json": `{"devDependencies":{"next":"15"}}`}, "nextjs"},
	}

	reg := Default()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				writeFile(t, dir, name, content)
			}
			got := reg.Detect(dir)
			if got.Name != tc.want {
				t.Fatalf("detect = %q, want %q", got.Name, tc.want)
			}
		})
	}
}

func TestReadinessMatching(t *testing.T) {
	reg := Default()
	cases := []struct {
		profile string
		line    string
		want    bool
	}{
		{"spring-boot", "Started DemoApplication in 2.4 seconds", true},
		{"spring-boot", "Tomcat started on port 8080", true},
		{"spring-boot", "compiling source files", false},
		{"nextjs", "Ready in 1.3s", true},
		{"nextjs", "Compiled successfully", true},
		{"nextjs", "waiting for file changes", false},
		{"django", "Starting development server at http://127.0.0.1:8000/", true},
		{"node", "Server listening on 3000", true},
		{"go", "listening on :8080", true},
		{"python", "Running on http://0.0.0.0:8000", true},
	}
	for _, tc := range cases {
		t.Run(tc.profile+"/"+tc.line, func(t *testing.T) {
			p := reg.Lookup(tc.profile)
			if p == nil {
				t.Fatalf("profile %q not found", tc.profile)
			}
			if got := p.Ready(tc.line); got != tc.want {
				t.Fatalf("Ready(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestDefaultCommands(t *testing.T) {
	reg := Default()
	dir := t.TempDir()

	if got := reg.Lookup("spring-boot").Command(dir); got[0] != "mvn" {
		t.Fatalf("spring-boot default command = %v", got)
	}
	writeFile(t, dir, "mvnw", "#!/bin/sh")
	if got := reg.Lookup("spring-boot").Command(dir); got[0] != "./mvnw" {
		t.Fatalf("spring-boot with mvnw = %v", got)
	}

	nodeDir := t.TempDir()
	if got := reg.Lookup("node").Command(nodeDir); got[0] != "npm" {
		t.Fatalf("node default command = %v", got)
	}
	writeFile(t, nodeDir, "pnpm-lock.yaml", "lockfileVersion: 5.4")
	if got := reg.Lookup("nextjs").Command(nodeDir); got[0] != "pnpm" {
		t.Fatalf("nextjs with pnpm-lock = %v", got)
	}

	if got := reg.Lookup("python").Command(dir); got != nil {
		t.Fatalf("python should have no default command, got %v", got)
	}
}

func TestStopGraceDefault(t *testing.T) {
	p := Generic
	if p.Grace() != 5_000_000_000 {
		t.Fatalf("default stop grace = %v", p.Grace())
	}
}
