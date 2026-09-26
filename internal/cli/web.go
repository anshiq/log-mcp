// web open: print the token URL for the web UI (served by the daemon
// on its TCP listener) and open the browser.
package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/platform/paths"
)

func newWebCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Web UI helpers (token URL, browser)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "open",
		Short: "Print the web UI token URL and open the browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := paths.User()
			raw, err := os.ReadFile(p.TokenPath())
			if err != nil {
				return fmt.Errorf("no token (is agentd --tcp running?): %w", err)
			}
			token := string(raw)
			if len(token) > 0 && token[len(token)-1] == '\n' {
				token = token[:len(token)-1]
			}
			// Fragment (#token=) never hits the wire; safe to print for
			// pasting into the login screen.
			url := "http://127.0.0.1:7350/#token=" + token
			fmt.Println(url)
			fmt.Println("Remote: ssh -L 7350:127.0.0.1:7350 <host>, then open the URL above.")
			openBrowser(url)
			return nil
		},
	})
	return cmd
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
