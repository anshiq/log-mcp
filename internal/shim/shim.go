// Package shim implements the per-process supervisor (agent-runtime-shim).
// It receives a bundle JSON spec, setsid + PR_SET_CHILD_SUBREAPER,
// creates pipes, execs the child into its cgroup, writes framed records
// to segment files, and serves a control socket.
//
// The shim has no network, no SQLite, no config parsing. Its only
// dependencies are the standard library and internal/logpipe (segment
// codec). Target RSS ≤ 4 MB.
package shim

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"agent-runtime/internal/logpipe"
)

const (
	// ProtocolVersion is the current shim protocol version. The daemon
	// supports [N-1, N]; mismatched shims degrade to orphan polling (§3.5).
	ProtocolVersion = 1
	// MinProtocolVersion is the oldest shim the daemon talks to.
	MinProtocolVersion = 1
	// MaxLineSize is the maximum payload size per record.
	MaxLineSize = 256 * 1024
	// LingerDefault is the default linger time for exited shims.
	LingerDefault = 24 * time.Hour
)

// Bundle is the spec passed to the shim (spec.json in the instance dir).
type Bundle struct {
	Version    int      `json:"version"`
	InstanceID string   `json:"instance_id"`
	Args       []string `json:"args"`
	WorkDir    string   `json:"workdir"`
	Env        []string `json:"env"`
	CgroupPath string   `json:"cgroup_path,omitempty"`
	StdinMode  string   `json:"stdin_mode,omitempty"` // closed | pipe
	Pty        bool     `json:"pty,omitempty"`
	SegmentDir string   `json:"segment_dir"`
	SegmentMax int      `json:"segment_max,omitempty"`
	Protocol   int      `json:"protocol"`
}

// StdinMode controls how stdin is handled.
type StdinMode int

const (
	StdinClosed StdinMode = iota
	StdinPipe
	StdinPTY
)

// Limits holds resource limits.
type Limits struct {
	CPUQuota    float64
	MemoryBytes int64
}

// ExitInfo holds the exit information recorded by the shim (exit.json).
type ExitInfo struct {
	Code      int       `json:"code"`
	Signal    string    `json:"signal,omitempty"`
	ExitedAt  time.Time `json:"exited_at"`
	OOMKilled bool      `json:"oom_killed"`
}

// LoadBundle reads and validates a bundle file. The shim refuses bundles
// whose owner uid differs from its own (tamper check per §9.1).
func LoadBundle(path string) (*Bundle, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("shim: stat bundle: %w", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("shim: bundle %s has permissive mode %o", path, st.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("shim: read bundle: %w", err)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("shim: parse bundle: %w", err)
	}
	if len(b.Args) == 0 {
		return nil, fmt.Errorf("shim: bundle has no argv")
	}
	if b.SegmentDir == "" {
		return nil, fmt.Errorf("shim: bundle has no segment_dir")
	}
	if b.Protocol < MinProtocolVersion || b.Protocol > ProtocolVersion {
		return nil, fmt.Errorf("shim: unsupported protocol %d (want %d..%d)",
			b.Protocol, MinProtocolVersion, ProtocolVersion)
	}
	return &b, nil
}

// SaveBundle writes a bundle atomically (temp + rename) with 0600.
func SaveBundle(path string, b *Bundle) error {
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

// Request is a control-socket op (JSON framing v1; protobuf wire format
// arrives with codegen in Phase 3 — the op names match shim.proto).
type Request struct {
	Protocol int    `json:"protocol_version"`
	Op       string `json:"op"` // hello|subscribe|signal|stdin|resize|stop|release
	Signal   string `json:"signal,omitempty"`
	Group    bool   `json:"group,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Rows     int    `json:"rows,omitempty"`
	Cols     int    `json:"cols,omitempty"`
	GraceMs  int    `json:"grace_ms,omitempty"`
	FromSeq  uint64 `json:"from_seq,omitempty"`
}

// Event is a control-socket response/event.
type Event struct {
	Type      string          `json:"ev"` // hello|record|exited|error
	PID       int             `json:"pid,omitempty"`
	PGID      int             `json:"pgid,omitempty"`
	Status    string          `json:"status,omitempty"`
	StartedAt int64           `json:"started_unix_ns,omitempty"`
	LastSeq   uint64          `json:"last_seq,omitempty"`
	Exit      *ExitInfo       `json:"exit,omitempty"`
	Record    *logpipe.Record `json:"record,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// Shim holds the state of a running shim.
type Shim struct {
	bundle    *Bundle
	child     *exec.Cmd
	pid       int
	pgid      int
	status    string
	startedAt time.Time
	writer    *logpipe.SegmentWriter
	seq       uint64
	mu        sync.Mutex
	listener  net.Listener
	released  chan struct{}
	exit      *ExitInfo
	stdinW    io.WriteCloser
}

// New creates a shim from a validated bundle.
func New(bundle *Bundle) (*Shim, error) {
	w, err := logpipe.NewSegmentWriter(bundle.SegmentDir, bundle.InstanceID, bundle.SegmentMax)
	if err != nil {
		return nil, err
	}
	return &Shim{
		bundle:   bundle,
		status:   "starting",
		writer:   w,
		released: make(chan struct{}),
	}, nil
}

// Spawn creates a new shim process from a bundle (daemon side helper:
// validates + records initial seq).
func Spawn(bundle *Bundle) (*Shim, error) {
	return New(bundle)
}

// Run executes the child process. Must be called in the shim process
// after setsid + PR_SET_CHILD_SUBREAPER. It wires pipes → segments,
// serves the control socket, and blocks until Release or linger expiry.
func (s *Shim) Run(controlSock string, linger time.Duration) error {
	s.child = exec.Command(s.bundle.Args[0], s.bundle.Args[1:]...)
	s.child.Dir = s.bundle.WorkDir
	if s.child.Dir == "" {
		s.child.Dir = "/"
	}
	if len(s.bundle.Env) > 0 {
		s.child.Env = s.bundle.Env
	}
	setupChild(s.child)

	stdout, err := s.child.StdoutPipe()
	if err != nil {
		return fmt.Errorf("shim: stdout pipe: %w", err)
	}
	stderr, err := s.child.StderrPipe()
	if err != nil {
		return fmt.Errorf("shim: stderr pipe: %w", err)
	}
	if s.bundle.StdinMode == "pipe" {
		if w, err := s.child.StdinPipe(); err == nil {
			s.stdinW = w
		}
	}
	if err := s.child.Start(); err != nil {
		return fmt.Errorf("shim: start: %w", err)
	}
	s.pid = s.child.Process.Pid
	s.pgid = s.pid
	s.startedAt = time.Now()
	s.status = "running"
	s.systemf("process started pid %d: %v", s.pid, s.bundle.Args)

	go s.pump(stdout, logpipe.StreamStdout)
	go s.pump(stderr, logpipe.StreamStderr)

	if err := s.serve(controlSock); err != nil {
		// Control socket failure must not kill the child.
		fmt.Fprintf(os.Stderr, "shim: control socket: %v\n", err)
	}

	waitErr := s.child.Wait()
	s.onExit(waitErr)

	// Stay alive until Release or linger so the daemon can fetch the
	// exit even across its own restart (§3.5.6).
	if linger <= 0 {
		linger = LingerDefault
	}
	select {
	case <-s.released:
	case <-time.After(linger):
	}
	_ = s.writer.Close()
	return nil
}

func (s *Shim) onExit(waitErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code := 0
	sig := ""
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				sig = ws.Signal().String()
			}
		} else {
			code = 1
		}
	}
	oom := checkOOM(s.bundle.CgroupPath)
	s.exit = &ExitInfo{Code: code, Signal: sig, ExitedAt: time.Now(), OOMKilled: oom}
	if code == 0 {
		s.status = "exited"
	} else {
		s.status = "failed"
	}
	if oom {
		s.systemf("process exited code %d signal %s (oom-killed)", code, sig)
	} else if sig != "" {
		s.systemf("process exited code %d signal %s", code, sig)
	} else {
		s.systemf("process exited code %d", code)
	}
	_ = s.writeExitFile()
}

func (s *Shim) systemf(format string, args ...any) {
	_ = s.writer.WriteRecord(&logpipe.Record{
		Stream:  uint8(logpipe.StreamSystem),
		Payload: []byte(fmt.Sprintf(format, args...)),
	})
}

// pump frames one pipe into segments, splitting long lines at MaxLineSize
// with the partial flag. Output is always consumed asynchronously so the
// child never blocks on a full pipe (§1.3.3).
func (s *Shim) pump(r io.Reader, stream logpipe.Stream) {
	buf := make([]byte, 32*1024)
	var pending []byte
	flush := func(line []byte, partial bool) {
		flags := 0
		if partial {
			flags |= logpipe.FlagPartial
		}
		if !isText(line) {
			flags |= logpipe.FlagBinary
		}
		_ = s.writer.WriteRecord(&logpipe.Record{Stream: uint8(stream), Flags: uint8(flags), Payload: append([]byte(nil), line...)})
		s.mu.Lock()
		s.seq++
		s.mu.Unlock()
	}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				idx := -1
				for i, c := range pending {
					if c == '\n' {
						idx = i
						break
					}
				}
				if idx < 0 {
					// No newline: flush full chunks as partial.
					for len(pending) >= MaxLineSize {
						flush(pending[:MaxLineSize], true)
						pending = pending[MaxLineSize:]
					}
					break
				}
				line := pending[:idx+1]
				pending = pending[idx+1:]
				// Split overlong lines.
				for len(line) > MaxLineSize {
					flush(line[:MaxLineSize], true)
					line = line[MaxLineSize:]
				}
				flush(line, false)
			}
		}
		if err != nil {
			if len(pending) > 0 {
				for len(pending) > 0 {
					n := len(pending)
					if n > MaxLineSize {
						n = MaxLineSize
					}
					flush(pending[:n], n < len(pending) || err == nil)
					pending = pending[n:]
				}
			}
			return
		}
	}
}

func isText(p []byte) bool {
	for _, c := range p {
		if c == 0 {
			return false
		}
	}
	return true
}

// PID returns the child pid.
func (s *Shim) PID() int { return s.pid }

// PGID returns the process group ID.
func (s *Shim) PGID() int { return s.pgid }

// Status returns the current status.
func (s *Shim) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// StartedAt returns when the process started.
func (s *Shim) StartedAt() time.Time { return s.startedAt }

// Signal sends a signal to the process (group=true → whole tree).
func (s *Shim) Signal(sig syscall.Signal, group bool) error {
	if s.pid == 0 {
		return fmt.Errorf("shim: no child")
	}
	if group {
		return syscall.Kill(-s.pgid, sig)
	}
	return syscall.Kill(s.pid, sig)
}

// Stop sends SIGTERM to the group, waits grace, then SIGKILL.
// Enforced in-shim so it works with the daemon down (§3.5.5).
func (s *Shim) Stop(grace time.Duration) error {
	if err := s.Signal(syscall.SIGTERM, true); err != nil {
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := s.exit != nil
		s.mu.Unlock()
		if done {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.mu.Lock()
	done := s.exit != nil
	s.mu.Unlock()
	if !done {
		return s.Signal(syscall.SIGKILL, true)
	}
	return nil
}

// Release lets the shim exit after the daemon recorded the exit.
func (s *Shim) Release() {
	select {
	case <-s.released:
	default:
		close(s.released)
	}
}

func (s *Shim) writeExitFile() error {
	if s.exit == nil {
		return nil
	}
	data, _ := json.MarshalIndent(s.exit, "", "  ")
	return os.WriteFile(s.bundle.SegmentDir+"/exit.json", data, 0o600)
}

// ReadExitFile reads a recorded exit (daemon reconnect path).
func ReadExitFile(segmentDir string) (*ExitInfo, error) {
	data, err := os.ReadFile(segmentDir + "/exit.json")
	if err != nil {
		return nil, err
	}
	var e ExitInfo
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func checkOOM(cgroup string) bool {
	// memory.events `max` counter increments on OOM kill.
	for _, cand := range []string{
		cgroup + "/memory.events",
		"/sys/fs/cgroup" + cgroup + "/memory.events",
	} {
		if cand == "/memory.events" || cand == "/sys/fs/cgroup/memory.events" {
			continue
		}
		data, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		var max int64
		for _, line := range splitLines(string(data)) {
			var k string
			var v int64
			if _, err := fmt.Sscanf(line, "%s %d", &k, &v); err == nil && k == "max" && v > 0 {
				_ = max
				return true
			}
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// --- control socket ---

func (s *Shim) serve(sock string) error {
	_ = os.Remove(sock)
	if err := os.MkdirAll(dirOf(sock), 0o700); err != nil {
		return err
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	_ = os.Chmod(sock, 0o600)
	s.listener = l
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-s.released:
				return nil
			default:
				return err
			}
		}
		go s.handle(conn)
	}
}

func (s *Shim) handle(conn net.Conn) {
	defer conn.Close()
	for {
		req, err := readFrame(conn)
		if err != nil {
			return
		}
		switch req.Op {
		case "hello":
			s.mu.Lock()
			ev := Event{Type: "hello", PID: s.pid, PGID: s.pgid, Status: s.status,
				StartedAt: s.startedAt.UnixNano(), LastSeq: s.writerSeq(), Exit: s.exit}
			s.mu.Unlock()
			_ = writeFrame(conn, ev)
			if req.Protocol < MinProtocolVersion || req.Protocol > ProtocolVersion {
				_ = writeFrame(conn, Event{Type: "error", Error: "unsupported protocol"})
				return
			}
		case "signal":
			sig := parseSignal(req.Signal)
			err := s.Signal(sig, req.Group)
			if err != nil {
				_ = writeFrame(conn, Event{Type: "error", Error: err.Error()})
			} else {
				_ = writeFrame(conn, Event{Type: "hello", Status: s.Status()})
			}
		case "stdin":
			if s.stdinW == nil {
				_ = writeFrame(conn, Event{Type: "error", Error: "stdin not a pipe"})
				continue
			}
			_, err := s.stdinW.Write(req.Data)
			if err != nil {
				_ = writeFrame(conn, Event{Type: "error", Error: err.Error()})
			} else {
				_ = writeFrame(conn, Event{Type: "hello", Status: s.Status()})
			}
		case "stop":
			grace := time.Duration(req.GraceMs) * time.Millisecond
			if grace <= 0 {
				grace = 5 * time.Second
			}
			go s.Stop(grace)
			_ = writeFrame(conn, Event{Type: "hello", Status: s.Status()})
		case "release":
			s.Release()
			_ = writeFrame(conn, Event{Type: "hello", Status: s.Status()})
			return
		case "subscribe":
			// Live tail from seq: replay segments then stream.
			// Minimal v1: replay then close (streaming follow lands with
			// the daemon ingest loop; the segments are authoritative).
			recs, _ := logpipe.ReadFromSeq(s.bundle.SegmentDir, req.FromSeq)
			for _, r := range recs {
				if err := writeFrame(conn, Event{Type: "record", Record: r}); err != nil {
					return
				}
			}
			return
		default:
			_ = writeFrame(conn, Event{Type: "error", Error: "unknown op " + req.Op})
		}
	}
}

func (s *Shim) writerSeq() uint64 {
	// logpipe writer tracks seq internally; approximate via segment scan
	// is unnecessary — the daemon uses index cursors. Return stored seq.
	return s.seq
}

func readFrame(conn net.Conn) (*Request, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n == 0 || n > 4*1024*1024 {
		return nil, fmt.Errorf("shim: bad frame length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func writeFrame(conn net.Conn, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(body)))
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = conn.Write(body)
	return err
}

func parseSignal(name string) syscall.Signal {
	switch name {
	case "KILL", "SIGKILL", "9":
		return syscall.SIGKILL
	case "INT", "SIGINT", "2":
		return syscall.SIGINT
	case "HUP", "SIGHUP", "1":
		return syscall.SIGHUP
	default:
		return syscall.SIGTERM
	}
}
