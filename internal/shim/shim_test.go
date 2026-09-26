package shim

import (
	"encoding/binary"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	shimv1 "agent-runtime/gen/agentruntime/shim/v1"
)

// Roundtrip over a real control socket: Hello negotiates, Subscribe
// replays segments, Release lets the shim exit.
func TestShimControlRoundtrip(t *testing.T) {
	dir := t.TempDir()
	segDir := filepath.Join(dir, "segments")
	bundle := &Bundle{
		Version: 1, InstanceID: "inst_test", Args: []string{"sh", "-c", "echo hello-shim"},
		WorkDir: dir, SegmentDir: segDir, Protocol: ProtocolVersion,
	}
	s, err := New(bundle)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "shim.sock")
	done := make(chan error, 1)
	go func() { done <- s.Run(sock, 5*time.Second) }()

	deadline := time.Now().Add(5 * time.Second)
	var cl *Client
	for time.Now().Before(deadline) {
		var err error
		cl, err = Dial(sock, time.Second)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cl == nil {
		t.Fatal("dial shim.sock")
	}
	defer cl.Close()

	hello, err := cl.Hello()
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if hello.Pid <= 0 || hello.Status == "" {
		t.Fatalf("hello = %+v", hello)
	}

	// Subscribe replays the echo line (segments authoritative).
	stop := make(chan struct{})
	defer close(stop)
	recs, exited, errc := cl.Subscribe(0, stop)
	found := false
	doneExited := false
	timeout := time.After(5 * time.Second)
	for !found && !doneExited {
		select {
		case r, ok := <-recs:
			if !ok {
				recs = nil
				continue
			}
			if string(r.Payload) == "hello-shim\n" {
				found = true
			}
		case <-exited:
			doneExited = true
		case err, ok := <-errc:
			if !ok {
				errc = nil
				continue
			}
			if err != nil {
				t.Fatalf("subscribe: %v", err)
			}
		case <-timeout:
			t.Fatal("echo line never arrived")
		}
	}

	if err := cl.Release(); err != nil {
		// The Subscribe stream owns this connection; Release goes over
		// a fresh one (a second connection replaces the first).
		cl2, err := Dial(sock, 2*time.Second)
		if err != nil {
			t.Fatalf("redial: %v", err)
		}
		defer cl2.Close()
		if err := cl2.Release(); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shim did not exit after Release")
	}
}

// Unsupported protocol versions degrade loudly (daemon falls back to
// orphan polling per §3.5).
func TestShimProtocolMismatch(t *testing.T) {
	dir := t.TempDir()
	bundle := &Bundle{
		Version: 1, InstanceID: "inst_old", Args: []string{"sh", "-c", "sleep 30"},
		WorkDir: dir, SegmentDir: filepath.Join(dir, "segments"), Protocol: ProtocolVersion,
	}
	s, err := New(bundle)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "shim.sock")
	go func() { _ = s.Run(sock, 20*time.Second) }()
	defer s.Release()

	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("dial shim.sock")
	}
	defer conn.Close()
	// Speak protocol 999: the shim must refuse.
	body, _ := proto.Marshal(&shimv1.HelloRequest{ProtocolVersion: 999})
	frame := make([]byte, 4+1+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], uint32(1+len(body)))
	frame[4] = opHello
	copy(frame[5:], body)
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	op, payload, err := readFrame(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if op != opError {
		t.Fatalf("op = %d, want error(%d)", op, opError)
	}
	var e shimv1.Error
	if err := proto.Unmarshal(payload, &e); err != nil {
		t.Fatal(err)
	}
	if e.Message == "" {
		t.Fatal("empty error message")
	}
}
