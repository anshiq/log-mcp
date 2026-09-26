package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUser(t *testing.T) {
	p := User()
	if p.Runtime == "" {
		t.Error("Runtime should not be empty")
	}
	if p.Data == "" {
		t.Error("Data should not be empty")
	}
	if p.Config == "" {
		t.Error("Config should not be empty")
	}
	if p.State == "" {
		t.Error("State should not be empty")
	}
	if p.Cache == "" {
		t.Error("Cache should not be empty")
	}
}

func TestUser_DirsExist(t *testing.T) {
	p := User()
	// Just verify the paths look reasonable, not that they exist.
	if !filepath.IsAbs(p.Runtime) {
		t.Errorf("Runtime should be absolute, got %q", p.Runtime)
	}
	if !filepath.IsAbs(p.Data) {
		t.Errorf("Data should be absolute, got %q", p.Data)
	}
}

func TestPaths_Methods(t *testing.T) {
	p := &Paths{
		Runtime: "/run/agent-runtime",
		Data:    "/var/lib/agent-runtime",
		Config:  "/etc/agent-runtime",
		State:   "/var/lib/agent-runtime-state",
		Cache:   "/var/cache/agent-runtime",
	}

	tests := []struct {
		name string
		want string
		got  string
	}{
		{"SocketPath", "/run/agent-runtime/agentd.sock", p.SocketPath()},
		{"PIDPath", "/run/agent-runtime/agentd.pid", p.PIDPath()},
		{"LockPath", "/run/agent-runtime/agentd.lock", p.LockPath()},
		{"LogPath", "/var/lib/agent-runtime-state/agentd.log", p.LogPath()},
		{"ProjectDir", "/var/lib/agent-runtime/projects/proj_123", p.ProjectDir("proj_123")},
		{"ProjectConfigPath", "/var/lib/agent-runtime/projects/proj_123/agent-runtime.yaml", p.ProjectConfigPath("proj_123")},
		{"WorkspaceDir", "/var/lib/agent-runtime/logs/ws_456", p.WorkspaceDir("ws_456")},
		{"ShimDir", "/run/agent-runtime/shims/inst_789", p.ShimDir("inst_789")},
		{"TrashDir", "/var/lib/agent-runtime/trash", p.TrashDir()},
		{"BackupsDir", "/var/lib/agent-runtime/backups", p.BackupsDir()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestPaths_EnsureDirs(t *testing.T) {
	p := &Paths{
		Runtime: t.TempDir() + "/runtime",
		Data:    t.TempDir() + "/data",
	}
	if err := p.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, d := range []string{p.Runtime, p.Data} {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			t.Errorf("expected dir %q to exist", d)
		}
	}
}
