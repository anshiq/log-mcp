package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectNode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"scripts":{"dev":"next dev"},"dependencies":{"next":"14.0.0"}}`)
	apps := Detect(dir)
	if len(apps) != 1 {
		t.Fatalf("apps = %v", apps)
	}
	if apps[0].Type != "nextjs" {
		t.Fatalf("type = %q", apps[0].Type)
	}
}

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/app\n")
	apps := Detect(dir)
	if len(apps) != 1 || apps[0].Type != "go" {
		t.Fatalf("apps = %v", apps)
	}
}

func TestDetectDjango(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "manage.py", "x")
	apps := Detect(dir)
	if len(apps) != 1 || apps[0].Type != "django" {
		t.Fatalf("apps = %v", apps)
	}
}

func TestDetectJava(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pom.xml", "<project/>")
	apps := Detect(dir)
	if len(apps) != 1 || apps[0].Type != "spring-boot" {
		t.Fatalf("apps = %v", apps)
	}
}

func TestDetectProcfileMakefileCompose(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Procfile", "web: node server.js\n")
	writeFile(t, dir, "Makefile", "dev:\n\techo hi\n")
	writeFile(t, dir, "docker-compose.yml", "services:\n  web:\n    image: x\n")
	apps := Detect(dir)
	if len(apps) < 2 {
		t.Fatalf("apps = %v", apps)
	}
}

func TestSignatureStableUnderDependencyChurn(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"scripts":{"dev":"next dev"},"dependencies":{"next":"14.0.0"}}`)
	s1 := Signature(dir)
	writeFile(t, dir, "package.json", `{"scripts":{"dev":"next dev"},"dependencies":{"next":"14.0.1"}}`)
	s2 := Signature(dir)
	if s1 != s2 {
		t.Fatalf("signature changed under dep churn: %q vs %q", s1, s2)
	}
	writeFile(t, dir, "package.json", `{"scripts":{"dev":"next dev --port 3001"},"dependencies":{"next":"14.0.1"}}`)
	s3 := Signature(dir)
	if s3 == s1 {
		t.Fatalf("signature did not change on script edit")
	}
}
