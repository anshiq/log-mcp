//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// isTTY reports whether f is an interactive terminal. Windows console handles
// are not char devices, so the fd must answer the console mode ioctl.
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(f.Fd()), &mode)
	return err == nil
}
