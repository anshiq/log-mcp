//go:build windows
// +build windows

package paths

import (
	"os"
	"path/filepath"
)

// userPaths resolves Windows roots under %LOCALAPPDATA% (§10) with XDG
// overrides honoured when set (e.g. Git Bash environments).
func userPaths(get func(string) string) *Paths {
	base := get("LOCALAPPDATA")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, "AppData", "Local")
		} else {
			base = os.TempDir()
		}
	}
	join := func(env, sub string) string {
		if v := get(env); v != "" {
			return filepath.Join(v, "agent-runtime")
		}
		return filepath.Join(base, "agent-runtime", sub)
	}
	return &Paths{
		Runtime: join("", "run"),
		Data:    join("XDG_DATA_HOME", "data"),
		Config:  join("XDG_CONFIG_HOME", "config"),
		State:   join("XDG_STATE_HOME", "state"),
		Cache:   join("XDG_CACHE_HOME", "cache"),
	}
}
