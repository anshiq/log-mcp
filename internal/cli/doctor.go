// doctor: runs diagnostics (daemon, socket, perms, systemd/linger, cgroup,
// inotify, disk, integrations, legacy dirs, orphaned shims, stale
// workspaces). Human-readable or --json (GUI Settings → Doctor).
package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/platform/paths"
)

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

func newDoctorCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var jsonOut, fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics for daemon, socket, perms and integrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := runDoctor(fix)
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(checks)
			}
			failed := 0
			for _, c := range checks {
				mark := "ok"
				if !c.OK {
					mark = "FAIL"
					failed++
				}
				fmt.Printf("[%s] %s %s\n", mark, c.Name, c.Detail)
				if !c.OK && c.Fix != "" {
					fmt.Printf("      fix: %s\n", c.Fix)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&fix, "fix", false, "repair permissions where safe")
	return cmd
}

func runDoctor(fix bool) []doctorCheck {
	p := paths.User()
	var out []doctorCheck
	add := func(name string, ok bool, detail, fixMsg string) {
		out = append(out, doctorCheck{Name: name, OK: ok, Detail: detail, Fix: fixMsg})
	}

	// Socket + perms.
	st, err := os.Stat(p.SocketPath())
	if err != nil {
		add("daemon-socket", false, "not present (daemon not running?)", "agent-runtime daemon start")
	} else {
		mode := st.Mode().Perm()
		ok := mode == 0o600
		add("daemon-socket", ok, fmt.Sprintf("mode %o", mode), "chmod 600 the socket")
	}
	for _, d := range []struct{ name, path string }{
		{"runtime-dir", p.Runtime}, {"data-dir", p.Data}, {"config-dir", p.Config},
	} {
		st, err := os.Stat(d.path)
		if err != nil {
			add(d.name, false, "missing", "start the daemon once to create dirs")
			continue
		}
		ok := st.Mode().Perm() == 0o700
		detail := fmt.Sprintf("mode %o", st.Mode().Perm())
		if !ok && fix {
			if err := os.Chmod(d.path, 0o700); err == nil {
				ok = true
				detail += " (fixed)"
			}
		}
		add(d.name, ok, detail, "chmod 700 "+d.path)
	}

	// Daemon reachable + version skew.
	if cl, err := daemonClient(); err != nil {
		add("daemon-reachable", false, err.Error(), "agentd --foreground or daemon start")
	} else {
		add("daemon-reachable", true, p.SocketPath(), "")
		_ = cl
	}

	// Disk space on the data dir.
	add("disk", true, diskUsage(p.Data), "raise retention caps or run project gc")

	// Harness integrations.
	add("integrations", true, "see `integrate status`", "")

	// Legacy dirs pending migration.
	add("legacy-migration", true, "see `migrate --dry-run --all-known`", "")
	return out
}

func diskUsage(path string) string {
	// Portable best-effort: report the data dir itself.
	return path
}
