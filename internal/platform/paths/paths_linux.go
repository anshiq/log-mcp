//go:build linux
// +build linux

package paths

import (
	"os"
	"path/filepath"
	"strconv"
)

// userPaths resolves XDG roots with per-§3.2 fallbacks. Runtime falls back
// to /tmp/agent-runtime-<uid> when XDG_RUNTIME_DIR is unset.
func userPaths(get func(string) string) *Paths {
	home, _ := os.UserHomeDir()

	runtime := get("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = filepath.Join("/tmp", "agent-runtime-"+strconv.Itoa(os.Getuid()))
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
