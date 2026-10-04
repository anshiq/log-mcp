package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"agent-runtime/internal/config"
	"agent-runtime/internal/daemon"
	"agent-runtime/internal/platform/paths"
)

const defaultWebTCPAddr = "127.0.0.1:7350"

func newWebCmd(loaded *config.Loaded, logger *slog.Logger) *cobra.Command {
	var addrFlag string
	var tcpFlag string
	var noOpen bool
	var printURL bool
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Open the web UI (token URL + browser)",
		Long: `Ensure the daemon web UI is running, print its token URL and open the browser.

The web UI is the primary interface. Without flags this starts the daemon
with its authenticated loopback TCP listener when needed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebOpen(addrFlag, tcpFlag, !noOpen, printURL)
		},
	}
	cmd.PersistentFlags().StringVar(&addrFlag, "addr", "", "override the address shown in the URL")
	cmd.PersistentFlags().StringVar(&tcpFlag, "tcp", "", "ensure the daemon TCP listener on this address")
	cmd.PersistentFlags().BoolVar(&noOpen, "no-open", false, "print the URL without opening the browser")
	cmd.PersistentFlags().BoolVar(&printURL, "print", true, "print the token URL")
	cmd.AddCommand(&cobra.Command{
		Use:   "open",
		Short: "Print the web UI token URL and open the browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebOpen(addrFlag, tcpFlag, !noOpen, printURL)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Report daemon, TCP and token reachability for the web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWebStatus(addrFlag)
		},
	})
	return cmd
}

func runWebOpen(displayAddr, tcpAddr string, open, printURL bool) error {
	p := paths.User()
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	want := tcpAddr
	if want == "" {
		want = displayAddr
	}
	addr, err := ensureWebTCP(p, want)
	if err != nil {
		return err
	}
	if displayAddr != "" {
		addr = displayAddr
	}
	token, err := readOrCreateToken(p.TokenPath())
	if err != nil {
		return err
	}
	url := "http://" + addr + "/#token=" + token
	if printURL {
		fmt.Println(url)
		fmt.Println("Remote: ssh -L 7350:127.0.0.1:7350 <host>, then open the URL above.")
	}
	if open {
		openBrowser(url)
	}
	return nil
}

func ensureWebTCP(p *paths.Paths, want string) (string, error) {
	if addr := tcpAddrFromFile(); addr != "" && tcpReachable(addr, 2*time.Second) {
		return addr, nil
	}
	if want == "" {
		if addr := tcpAddrFromFile(); addr != "" {
			want = addr
		} else {
			want = defaultWebTCPAddr
		}
	}
	d := daemon.New(p.SocketPath(), p.Data).WithTCP(want)
	if err := d.WriteTCPWant(want); err != nil {
		return "", err
	}
	if d.IsRunning() {
		if err := d.Restart(); err != nil {
			return "", err
		}
		fmt.Printf("restarted daemon with TCP listener on %s\n", want)
	} else {
		if err := d.Start(); err != nil {
			return "", err
		}
		fmt.Printf("started daemon with TCP listener on %s\n", want)
	}
	if err := d.WaitReady(10 * time.Second); err != nil {
		return "", err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if addr := tcpAddrFromFile(); addr != "" && tcpReachable(addr, 500*time.Millisecond) {
			return addr, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if addr := tcpAddrFromFile(); addr != "" {
		return addr, nil
	}
	return want, nil
}

func runWebStatus(displayAddr string) error {
	p := paths.User()
	addr := displayAddr
	if addr == "" {
		addr = tcpAddrFromFile()
	}
	sockAlive := daemon.New(p.SocketPath(), p.Data).IsRunning()
	tokenOK := false
	if data, err := os.ReadFile(p.TokenPath()); err == nil && strings.TrimSpace(string(data)) != "" {
		tokenOK = true
	}
	shown := addr
	if shown == "" {
		shown = "(none)"
	}
	tcpOK := addr != "" && tcpReachable(addr, 2*time.Second)
	fmt.Printf("daemon socket: %s alive=%v\n", p.SocketPath(), sockAlive)
	fmt.Printf("tcp listener: %s reachable=%v\n", shown, tcpOK)
	fmt.Printf("token: %s present=%v\n", p.TokenPath(), tokenOK)
	if !tcpOK {
		return fmt.Errorf("web UI not reachable (run `agent-runtime web` to start it)")
	}
	return nil
}

func tcpAddrFromFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(dir + "/agent-runtime/tcp.addr")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func tcpReachable(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func readOrCreateToken(tokenPath string) (string, error) {
	if data, err := os.ReadFile(tokenPath); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	if dir := dirOf(tokenPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(tokenPath, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
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
