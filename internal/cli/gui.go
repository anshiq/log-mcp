package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/gui"
)

func newGuiCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var socket string
	var background bool
	cmd := &cobra.Command{
		Use:   "gui",
		Short: "Open the native GUI application window",
		Long: `Open the native GUI application window (desktop build only).

Without the desktop build this prints a hint pointing at
` + "`agent-runtime web`" + ` and ` + "`agent-runtime tui`" + ` instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if socket != "" {
				_ = os.Setenv("AGENTD_SOCKET", socket)
			}
			if background {
				os.Args = append(os.Args, "--background")
			}
			return gui.Run(logger)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "override daemon socket path (or AGENTD_SOCKET)")
	cmd.Flags().BoolVar(&background, "background", false, "hide window on close instead of quitting")
	cmd.AddCommand(&cobra.Command{
		Use:   "open",
		Short: "Check daemon reachability for the GUI without opening a window",
		RunE: func(cmd *cobra.Command, args []string) error {
			if socket != "" {
				_ = os.Setenv("AGENTD_SOCKET", socket)
			}
			return gui.OpenDiagnostic(logger)
		},
	})
	return cmd
}

func newSetupCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Open the interactive setup wizard",
		Long:  "Open the interactive setup wizard (init agent-runtime.yaml, connect AI agents, open the process manager).",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupMain(loaded, logger)
		},
	}
}
