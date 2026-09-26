//go:build darwin
// +build darwin

// Package ipc on macOS: UDS with LOCAL_PEERCRED (effective uid check).
package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// SocketMode is the file mode for the daemon socket (0600).
const SocketMode = 0o600

// DirMode is the directory mode for the runtime dir (0700).
const DirMode = 0o700

// Listen creates a Unix domain socket listener at addr.
func Listen(addr string) (net.Listener, error) {
	dir := filepath.Dir(addr)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return nil, fmt.Errorf("ipc: create runtime dir: %w", err)
	}
	_ = os.Chmod(dir, DirMode)
	_ = os.Remove(addr)
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen %s: %w", addr, err)
	}
	if err := os.Chmod(addr, SocketMode); err != nil {
		l.Close()
		return nil, fmt.Errorf("ipc: chmod socket: %w", err)
	}
	return l, nil
}

// Dial connects to a Unix domain socket at addr, honouring ctx.
func Dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", addr)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial %s: %w", addr, err)
	}
	return conn, nil
}

// PeerUID returns the effective uid of the peer via LOCAL_PEERCRED,
// failing closed when credentials are unreadable.
func PeerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("ipc: peeruid: not a unix conn (%T)", conn)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("ipc: peeruid syscallconn: %w", err)
	}
	var uid uint32
	var ucredErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			ucredErr = err
			return
		}
		uid = cred.Uid
	})
	if ctrlErr != nil {
		return 0, fmt.Errorf("ipc: peeruid control: %w", ctrlErr)
	}
	if ucredErr != nil {
		return 0, fmt.Errorf("ipc: peeruid getsockopt: %w", ucredErr)
	}
	return uid, nil
}

// CheckPeer verifies the peer uid matches want.
func CheckPeer(conn net.Conn, want uint32) error {
	got, err := PeerUID(conn)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("ipc: peer uid %d != daemon uid %d", got, want)
	}
	return nil
}
