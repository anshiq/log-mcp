//go:build windows
// +build windows

// Package ipc on Windows: AF_UNIX sockets (Win10 1803+) via Go's net
// package. Peer identity falls back to the 0700/0600 ACL-equivalent
// modes; SO_PEERCRED-style checks arrive with the named-pipe transport.
package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// SocketMode is the file mode for the daemon socket.
const SocketMode = 0o600

// DirMode is the directory mode for the runtime dir.
const DirMode = 0o700

// Listen creates an AF_UNIX listener at addr.
func Listen(addr string) (net.Listener, error) {
	dir := filepath.Dir(addr)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return nil, fmt.Errorf("ipc: create runtime dir: %w", err)
	}
	_ = os.Remove(addr)
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(addr, SocketMode)
	return l, nil
}

// Dial connects to an AF_UNIX socket at addr.
func Dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{}
	return d.DialContext(ctx, "unix", addr)
}

// PeerUID is not implemented on Windows (named-pipe
// GetNamedPipeClientProcessId transport is the follow-up); callers must
// rely on directory/socket ACLs.
func PeerUID(conn net.Conn) (uint32, error) {
	return 0, fmt.Errorf("ipc: peeruid not implemented on Windows")
}
