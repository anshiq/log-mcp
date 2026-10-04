package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/daemon"
	"agent-runtime/internal/platform/paths"
	"agent-runtime/internal/platform/servicemgr"
)

// newDaemonCmd adds the `daemon start|stop|status|run` tree. The daemon owns
// the runtime's processes so they survive the MCP session; `serve` and `repl`
// become thin clients when runtime.daemon is set.
func newDaemonCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var startTCP string
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the long-lived daemon that owns managed processes",
		Long:  "Manage the daemon that keeps managed processes alive across MCP sessions (runtime.daemon: true).",
	}
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		RunE: func(c *cobra.Command, args []string) error {
			p := paths.User()
			d := daemon.New(p.SocketPath(), p.Data)
			if startTCP != "" {
				if err := d.WriteTCPWant(startTCP); err != nil {
					return err
				}
				d = d.WithTCP(startTCP)
			}
			if d.IsRunning() {
				if startTCP != "" {
					if err := d.Restart(); err != nil {
						return err
					}
					fmt.Printf("daemon restarted with TCP listener on %s\n", startTCP)
					return nil
				}
				fmt.Println("daemon is already running")
				return nil
			}
			fmt.Println("starting daemon...")
			if err := d.Start(); err != nil {
				return err
			}
			if err := d.WaitReady(10 * time.Second); err != nil {
				return err
			}
			fmt.Printf("daemon ready (socket %s)\n", p.SocketPath())
			return nil
		},
	}
	startCmd.Flags().StringVar(&startTCP, "tcp", "", "also serve authenticated loopback TCP for the web UI (e.g. 127.0.0.1:7350)")
	cmd.AddCommand(
		startCmd,
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the daemon, gracefully shutting down its processes",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				d := daemon.New(p.SocketPath(), p.Data)
				if !d.IsRunning() {
					fmt.Println("daemon is not running")
					return nil
				}
				if err := d.Stop(false); err != nil {
					return err
				}
				deadline := time.Now().Add(20 * time.Second)
				for time.Now().Before(deadline) {
					if !d.IsRunning() {
						fmt.Println("daemon stopped")
						return nil
					}
					time.Sleep(20 * time.Millisecond)
				}
				return fmt.Errorf("daemon did not stop within 20s")
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report whether the daemon is running",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				d := daemon.New(p.SocketPath(), p.Data)
				if d.IsRunning() {
					fmt.Printf("running (pid %d)\n", d.ReadPid())
				} else {
					fmt.Println("not running")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:    "run",
			Short:  "Run the daemon in the foreground (internal)",
			Hidden: true,
			RunE: func(c *cobra.Command, args []string) error {
				return daemon.Run(loaded, logger)
			},
		},
		&cobra.Command{
			Use:   "restart",
			Short: "Restart the per-user daemon (processes keep running)",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				d := daemon.New(p.SocketPath(), p.Data)
				if err := d.Restart(); err != nil {
					return err
				}
				fmt.Println("daemon restarted (shims re-attached)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "logs",
			Short: "Tail the per-user daemon log",
			RunE: func(c *cobra.Command, args []string) error {
				p := paths.User()
				data, err := os.ReadFile(p.LogPath())
				if err != nil {
					return err
				}
				fmt.Print(tailLines(string(data), 100))
				return nil
			},
		},
		&cobra.Command{
			Use:   "install",
			Short: "Install autostart (systemd user socket / LaunchAgent / logon task)",
			RunE: func(c *cobra.Command, args []string) error {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				agentd := siblingBinary(exe, "agentd")
				p := paths.User()
				mgr, err := servicemgr.New(agentd, p.SocketPath())
				if err != nil {
					return err
				}
				return mgr.Install()
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Remove autostart",
			RunE: func(c *cobra.Command, args []string) error {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				agentd := siblingBinary(exe, "agentd")
				p := paths.User()
				mgr, err := servicemgr.New(agentd, p.SocketPath())
				if err != nil {
					return err
				}
				return mgr.Uninstall()
			},
		},
	)
	return cmd
}

func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func siblingBinary(exe, name string) string {
	cand := filepath.Join(filepath.Dir(exe), name)
	if st, err := os.Stat(cand); err == nil && !st.IsDir() {
		return cand
	}
	return exe
}
