//go:build linux
// +build linux

// Package ipc provides Unix domain socket and peer-credential
// functionality for the daemon. The daemon listens on a UDS with
// strict perms and authenticates every connection via SO_PEERCRED.
package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	// SocketMode is the file mode for the daemon socket (0600).
	SocketMode = 0o600
	// DirMode is the directory mode for the runtime dir (0700).
	DirMode = 0o700
)

// Listen creates a Unix domain socket listener at addr with 0700
// directory and 0600 socket. The directory is created if needed.
// A stale socket file is removed first; the listener repairs modes
// on existing directories.
func Listen(addr string) (net.Listener, error) {
	dir := filepath.Dir(addr)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return nil, fmt.Errorf("ipc: create runtime dir: %w", err)
	}
	_ = os.Chmod(dir, DirMode)
	// Remove any stale socket file.
	_ = os.Remove(addr)

	lc := net.ListenConfig{}
	l, err := lc.Listen(context.Background(), "unix", addr)
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

// PeerUID returns the OS user ID of the peer connected to conn
// using SO_PEERCRED. It errors when the credential cannot be read
// so callers fail closed.
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
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
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

// CheckPeer verifies the peer uid matches want (normally os.Getuid()).
// It is a small helper so handlers fail closed with a useful error.
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
