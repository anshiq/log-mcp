//go:build windows

package process

import (
	"errors"
	"syscall"
)

// SignalByName maps a signal name to the OS signal. Signal forwarding is not
// supported on Windows, so this always returns an error. It exists so callers
// that resolve signals by name still compile for GOOS=windows.
func SignalByName(name string) (syscall.Signal, error) {
	return 0, errors.New("signal forwarding is unsupported on windows")
}
