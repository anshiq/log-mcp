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
			addr := "127.0.0.1:7350"
			if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
				if data, err := os.ReadFile(v + "/agent-runtime/tcp.addr"); err == nil {
					if s := string(data); len(s) > 0 {
						for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
							s = s[:len(s)-1]
						}
						if s != "" {
							addr = s
						}
					}
				}
			}
			url := "http://" + addr + "/#token=" + token
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
