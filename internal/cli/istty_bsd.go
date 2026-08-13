//go:build darwin || freebsd || netbsd || openbsd || dragonfly || ios

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

// isTTY reports whether f is an interactive terminal. A char device alone is
// not enough (/dev/null is one): the fd must answer the termios ioctl.
// Darwin and the BSDs use TIOCGETA where Linux uses TCGETS.
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	_, err = unix.IoctlGetTermios(int(f.Fd()), unix.TIOCGETA)
	return err == nil
}
