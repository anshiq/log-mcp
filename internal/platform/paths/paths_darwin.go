//go:build darwin
// +build darwin

package paths

import (
	"os"
	"path/filepath"
)

// userPaths resolves macOS roots (~/Library/..., §10) with XDG overrides
// honoured when set (Nix/Homebrew environments often export them).
func userPaths(get func(string) string) *Paths {
	home, _ := os.UserHomeDir()

	runtime := get("XDG_RUNTIME_DIR")
	if runtime == "" {
		tmp := os.TempDir()
		runtime = filepath.Join(tmp, "agent-runtime")
	} else {
		runtime = filepath.Join(runtime, "agent-runtime")
	}

	data := get("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, "Library", "Application Support")
	}
	data = filepath.Join(data, "agent-runtime")

	config := get("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, "Library", "Preferences")
	}
	config = filepath.Join(config, "agent-runtime")

	state := get("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(home, "Library", "Logs")
	}
	state = filepath.Join(state, "agent-runtime")

	cache := get("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, "Library", "Caches")
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
