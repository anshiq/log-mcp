//go:build linux

package process

import (
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestListeningPortsSelf(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	want := ln.Addr().(*net.TCPAddr).Port
	ports := ListeningPortsForPID(os.Getpid())
	found := false
	for _, p := range ports {
		if p == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("port %d not in %v", want, ports)
	}
}

func TestTreePIDsChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	time.Sleep(200 * time.Millisecond)
	tree := TreePIDs(cmd.Process.Pid)
	if len(tree) < 1 {
		t.Fatal("empty tree")
	}
	seen := false
	for _, p := range tree {
		if p == cmd.Process.Pid {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("root %d not in tree %v", cmd.Process.Pid, tree)
	}
}
