//go:build unix

package process

import (
	"fmt"
	"strings"
	"syscall"
)

// signalNames maps the whitelisted signal names (without the "SIG" prefix) to
// their OS signals. Lookups are case-insensitive.
var signalNames = map[string]syscall.Signal{
	"int":  syscall.SIGINT,
	"term": syscall.SIGTERM,
	"hup":  syscall.SIGHUP,
	"quit": syscall.SIGQUIT,
	"usr1": syscall.SIGUSR1,
	"usr2": syscall.SIGUSR2,
	"kill": syscall.SIGKILL,
}

// SignalByName maps a signal name to the OS signal. Accepts names with or
// without the "SIG" prefix; the whitelist is intentionally small.
func SignalByName(name string) (syscall.Signal, error) {
	norm := strings.ToLower(name)
	norm = strings.TrimPrefix(norm, "sig")
	sig, ok := signalNames[norm]
	if !ok {
		return 0, fmt.Errorf("unsupported signal %q (allowed: SIGINT, SIGTERM, SIGHUP, SIGQUIT, SIGUSR1, SIGUSR2, SIGKILL)", name)
	}
	return sig, nil
}
