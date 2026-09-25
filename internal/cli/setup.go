// Small filesystem helpers shared by the setup wizard (tui.go).
package cli

import (
	"os"
	"path/filepath"
)

// configPathUp walks from dir upward looking for agent-runtime.yaml/.yml,
// mirroring config.LoadDefault's resolution for the scaffold check.
func configPathUp(dir string) string {
	for {
		for _, name := range []string{"agent-runtime.yaml", "agent-runtime.yml"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
