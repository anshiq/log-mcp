// Package paths provides XDG-based path resolution for agent-runtime.
//
// All path constants are derived from the standard XDG Base Directory
// specification with platform-appropriate fallbacks. Nothing else in the
// codebase should call os.UserHomeDir() directly; resolve through here.
package paths

import (
	"os"
	"path/filepath"
	"strconv"
)

// Paths holds all the directory paths used by agent-runtime for a
// single OS user.
type Paths struct {
	// Runtime is the directory for runtime files (sockets, PID, locks).
	Runtime string
	// Data is the directory for durable data (state.db, projects, logs).
	Data string
	// Config is the directory for daemon-wide config.
	Config string
	// State is the directory for state/log files.
	State string
	// Cache is the directory for caches (profile detection, git probes).
	Cache string
}

// envGetter abstracts os.Getenv for testing.
type envGetter func(string) string

func getenv(key string) string { return os.Getenv(key) }

// User returns the Paths for the current user, using XDG environment
// variables or their standard defaults.
//
// Runtime falls back to /tmp/agent-runtime-<uid> when XDG_RUNTIME_DIR is
// unset, per §3.2 of the v3 plan.
func User() *Paths {
	return fromEnv(getenv)
}

func fromEnv(get envGetter) *Paths {
	home, _ := os.UserHomeDir()

	runtime := get("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = filepath.Join("/tmp", "agent-runtime-"+currentUID())
	}
	runtime = filepath.Join(runtime, "agent-runtime")

	data := get("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	data = filepath.Join(data, "agent-runtime")

	config := get("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	config = filepath.Join(config, "agent-runtime")

	state := get("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, ".local", "state")
	}
	state = filepath.Join(state, "agent-runtime")

	cache := get("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	cache = filepath.Join(cache, "agent-runtime")

	return &Paths{
		Runtime: runtime,
		Data:    data,
		Config:  config,
		State:   state,
		Cache:   cache,
	}
}

// SocketPath returns the daemon's Unix socket path.
func (p *Paths) SocketPath() string {
	return filepath.Join(p.Runtime, "agentd.sock")
}

// PIDPath returns the daemon's PID file path.
func (p *Paths) PIDPath() string {
	return filepath.Join(p.Runtime, "agentd.pid")
}

// LockPath returns the daemon's lock file path.
func (p *Paths) LockPath() string {
	return filepath.Join(p.Runtime, "agentd.lock")
}

// LogPath returns the daemon's own log file path.
func (p *Paths) LogPath() string {
	return filepath.Join(p.State, "agentd.log")
}

// StateDBPath returns the central state database path.
func (p *Paths) StateDBPath() string {
	return filepath.Join(p.Data, "state.db")
}

// AgentdConfigPath returns the daemon-wide settings file path.
func (p *Paths) AgentdConfigPath() string {
	return filepath.Join(p.Config, "agentd.yaml")
}

// TokenPath returns the bearer-token file for the TCP listener.
func (p *Paths) TokenPath() string {
	return filepath.Join(p.Config, "token")
}

// ProjectDir returns the directory for a specific project's config.
func (p *Paths) ProjectDir(projectID string) string {
	return filepath.Join(p.Data, "projects", projectID)
}

// ProjectConfigPath returns the editable config file for a project.
func (p *Paths) ProjectConfigPath(projectID string) string {
	return filepath.Join(p.ProjectDir(projectID), "agent-runtime.yaml")
}

// WorkspaceOverlayPath returns the optional per-workspace overlay path.
func (p *Paths) WorkspaceOverlayPath(projectID, workspaceID string) string {
	return filepath.Join(p.ProjectDir(projectID), "workspaces", workspaceID+".yaml")
}

// WorkspaceDir returns the directory for a specific workspace's logs.
func (p *Paths) WorkspaceDir(workspaceID string) string {
	return filepath.Join(p.Data, "logs", workspaceID)
}

// IndexDBPath returns the FTS5 log index for a workspace.
func (p *Paths) IndexDBPath(workspaceID string) string {
	return filepath.Join(p.WorkspaceDir(workspaceID), "index.db")
}

// SegmentDir returns the directory for an instance's log segments.
func (p *Paths) SegmentDir(workspaceID, instanceID string) string {
	return filepath.Join(p.WorkspaceDir(workspaceID), "segments", instanceID)
}

// ShimDir returns the runtime directory for a shim instance.
func (p *Paths) ShimDir(instanceID string) string {
	return filepath.Join(p.Runtime, "shims", instanceID)
}

// ShimSocket returns the control socket for a shim instance.
func (p *Paths) ShimSocket(instanceID string) string {
	return filepath.Join(p.ShimDir(instanceID), "shim.sock")
}

// TrashDir returns the directory for forgotten projects.
func (p *Paths) TrashDir() string {
	return filepath.Join(p.Data, "trash")
}

// BackupsDir returns the directory for DB backups.
func (p *Paths) BackupsDir() string {
	return filepath.Join(p.Data, "backups")
}

// EnsureDirs creates all the necessary directories with 0700 permissions.
// Existing directories are chmodded to 0700 to repair permissive modes
// (doctor also checks this).
func (p *Paths) EnsureDirs() error {
	dirs := []string{
		p.Runtime,
		p.Data,
		p.Config,
		p.State,
		p.Cache,
		p.TrashDir(),
		p.BackupsDir(),
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		// MkdirAll does not chmod existing dirs; enforce 0700.
		_ = os.Chmod(dir, 0o700)
	}
	return nil
}

func currentUID() string {
	return strconv.Itoa(os.Getuid())
}
