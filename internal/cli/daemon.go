package cli

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/daemon"
)

// newDaemonCmd adds the `daemon start|stop|status|run` tree. The daemon owns
// the runtime's processes so they survive the MCP session; `serve` and `repl`
// become thin clients when runtime.daemon is set.
func newDaemonCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the long-lived daemon that owns managed processes",
		Long:  "Manage the daemon that keeps managed processes alive across MCP sessions (runtime.daemon: true).",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the daemon in the background",
			RunE: func(c *cobra.Command, args []string) error {
				started, err := daemon.Start(loaded, logger)
				if err != nil {
					return err
				}
				if !started {
					fmt.Println("daemon is already running")
					return nil
				}
				fmt.Println("starting daemon...")
				if err := daemon.WaitReady(loaded.ProjectDir, 10*time.Second); err != nil {
					return err
				}
				fmt.Printf("daemon ready (socket %s)\n", daemon.SocketPath(loaded.ProjectDir))
				return nil
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the daemon, gracefully shutting down its processes",
			RunE: func(c *cobra.Command, args []string) error {
				if !daemon.IsRunning(loaded.ProjectDir) {
					fmt.Println("daemon is not running")
					return nil
				}
				if err := daemon.Stop(loaded.ProjectDir, 20*time.Second); err != nil {
					return err
				}
				fmt.Println("daemon stopped")
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report whether the daemon is running",
			RunE: func(c *cobra.Command, args []string) error {
				if daemon.IsRunning(loaded.ProjectDir) {
					fmt.Printf("running (pid %d)\n", daemon.ReadPid(loaded.ProjectDir))
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
	)
	return cmd
}
