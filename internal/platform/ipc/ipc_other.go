//go:build !linux
// +build !linux

// Package ipc provides IPC abstractions for non-Linux platforms.
// The Linux implementation is in ipc_linux.go. macOS uses
// LOCAL_PEERCRED and Windows uses named pipes/AF_UNIX; both are
// future work behind this same interface.
package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	_ = os.Remove(addr)
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(addr, SocketMode)
	return l, nil
}

// Dial connects to a Unix domain socket at addr.
func Dial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{}
	return d.DialContext(ctx, "unix", addr)
}

// PeerUID returns the OS user ID of the peer.
// Non-Linux stub: credential inspection is platform-specific and
// not yet implemented; callers must additionally rely on the 0700
// directory and 0600 socket permissions.
func PeerUID(conn net.Conn) (uint32, error) {
	return 0, fmt.Errorf("ipc: peeruid not implemented on this platform")
}
