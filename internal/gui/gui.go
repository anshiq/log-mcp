package gui

import (
	"os"
	"strings"

	"agent-runtime/internal/platform/paths"
)

var Version = "v0.4.2"

func SocketPath() string {
	if s := os.Getenv("AGENTD_SOCKET"); s != "" {
		return s
	}
	return paths.User().SocketPath()
}

func BackgroundMode() bool {
	for _, a := range os.Args {
		if a == "--background" {
			return true
		}
	}
	return false
}

func ApplySocketArgs(args []string) {
	for i, a := range args {
		if a == "--socket" && i+1 < len(args) {
			_ = os.Setenv("AGENTD_SOCKET", args[i+1])
		}
		if strings.HasPrefix(a, "--socket=") {
			_ = os.Setenv("AGENTD_SOCKET", strings.TrimPrefix(a, "--socket="))
		}
	}
}
