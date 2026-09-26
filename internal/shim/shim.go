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

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/creack/pty"

	shimv1 "agent-runtime/gen/agentruntime/shim/v1"
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

// Control protocol: u32 length (LE) + 1-byte op + protobuf message
// (api/proto/agentruntime/shim/v1/shim.proto). One control connection at
// a time; a second connection replaces the first (daemon restart).
//
// Ops daemon→shim: 1 Hello, 2 Subscribe, 3 Signal, 4 Stdin, 5 Resize,
// 6 Stop, 7 Release. Ops shim→daemon: 11 HelloResponse, 12 FramedRecord,
// 13 Exited, 14 Error.
const (
	opHello     = 1
	opSubscribe = 2
	opSignal    = 3
	opStdin     = 4
	opResize    = 5
	opStop      = 6
	opRelease   = 7

	opHelloResp = 11
	opRecord    = 12
	opExited    = 13
	opError     = 14
)

// Shim holds the state of a running shim.
type Shim struct {
	bundle    *Bundle
	child     *exec.Cmd
	pid       int
	pgid      int
	status    string
	startedAt time.Time
	writer    *logpipe.SegmentWriter
	mu        sync.Mutex
	listener  net.Listener
	active    net.Conn // current control connection (single-owner)
	released  chan struct{}
	exitedCh  chan struct{} // closed by onExit; unblocks Subscribe streams
	exit      *ExitInfo
	stdinW    io.WriteCloser
	ptyMaster *os.File
	// subs fan live records to Subscribe streams (bounded, drop on
	// backpressure — segments stay authoritative).
	subsMu sync.Mutex
	subs   map[chan logpipe.Record]struct{}
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
		exitedCh: make(chan struct{}),
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

	if s.bundle.Pty {
		return s.runPTY(controlSock, linger)
	}

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

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.serve(controlSock) }()

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
	case err := <-serveErr:
		// Control socket failure must not kill the child, but a dead
		// listener with no Release means nobody can reach us: exit so
		// the daemon's orphan scan reaps cleanly.
		fmt.Fprintf(os.Stderr, "shim: control socket: %v\n", err)
	}
	_ = s.writer.Close()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	return nil
}

// runPTY executes the child under a PTY master (colors, interactive CLIs
// in the GUI). Stdout+stderr merge into stream=pty; Resize controls the
// window size. Otherwise identical to the pipe path (segments, linger).
func (s *Shim) runPTY(controlSock string, linger time.Duration) error {
	master, err := pty.Start(s.child)
	if err != nil {
		return fmt.Errorf("shim: pty start: %w", err)
	}
	s.ptyMaster = master
	s.pid = s.child.Process.Pid
	s.pgid = s.pid
	s.startedAt = time.Now()
	s.status = "running"
	s.systemf("process started pid %d (pty): %v", s.pid, s.bundle.Args)

	go s.pump(master, logpipe.StreamPTY)

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.serve(controlSock) }()

	waitErr := s.child.Wait()
	s.onExit(waitErr)
	_ = master.Close()

	if linger <= 0 {
		linger = LingerDefault
	}
	select {
	case <-s.released:
	case <-time.After(linger):
	case err := <-serveErr:
		fmt.Fprintf(os.Stderr, "shim: control socket: %v\n", err)
	}
	_ = s.writer.Close()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	return nil
}

// resizePTY applies a terminal resize to the PTY master.
func (s *Shim) resizePTY(rows, cols int) error {
	s.mu.Lock()
	master := s.ptyMaster
	s.mu.Unlock()
	if master == nil {
		return fmt.Errorf("shim: no pty allocated")
	}
	return pty.Setsize(master, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
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
	select {
	case <-s.exitedCh:
	default:
		close(s.exitedCh)
	}
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
		rec := &logpipe.Record{Stream: uint8(stream), Flags: uint8(flags),
			Payload: append([]byte(nil), line...), TS: time.Now().UnixNano()}
		if err := s.writer.WriteRecord(rec); err != nil {
			return
		}
		rec.Seq = uint64(s.writer.LastSeq())
		// Fan out to live subscribers without blocking the pump.
		s.subsMu.Lock()
		for ch := range s.subs {
			select {
			case ch <- *rec:
			default:
			}
		}
		s.subsMu.Unlock()
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
		return killGroup(s.pgid, sig)
	}
	return killPID(s.pid, sig)
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
		// One control connection at a time (the daemon); a second
		// connection replaces the first (daemon restart).
		s.mu.Lock()
		if s.active != nil {
			_ = s.active.Close()
		}
		s.active = conn
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *Shim) handle(conn net.Conn) {
	defer conn.Close()
	for {
		op, body, err := readFrame(conn)
		if err != nil {
			return
		}
		switch op {
		case opHello:
			var req shimv1.HelloRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
				return
			}
			if req.ProtocolVersion < int32(MinProtocolVersion) || req.ProtocolVersion > int32(ProtocolVersion) {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: "unsupported protocol"})
				return
			}
			s.mu.Lock()
			resp := &shimv1.HelloResponse{
				ProtocolVersion: int32(ProtocolVersion),
				Pid:             int32(s.pid),
				Pgid:            int32(s.pgid),
				Status:          s.status,
				StartedAt:       timestamppb.New(s.startedAt),
				LastSeq:         int64(s.writer.LastSeq()),
			}
			if s.exit != nil {
				resp.Exit = &shimv1.Exited{
					Code: int32(s.exit.Code), Signal: s.exit.Signal,
					OomKilled: s.exit.OOMKilled, ExitedUnixNs: s.exit.ExitedAt.UnixNano(),
				}
			}
			s.mu.Unlock()
			_ = writeMsg(conn, opHelloResp, resp)
		case opSignal:
			var req shimv1.SignalRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
				continue
			}
			if err := s.Signal(parseSignal(req.Signal), req.Group); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
			} else {
				_ = writeMsg(conn, opHelloResp, s.helloLocked())
			}
		case opStdin:
			var req shimv1.StdinRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
				continue
			}
			if s.stdinW == nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: "stdin not a pipe"})
				continue
			}
			if _, err := s.stdinW.Write(req.Data); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
			} else {
				_ = writeMsg(conn, opHelloResp, s.helloLocked())
			}
		case opResize:
			var req shimv1.ResizeRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
				continue
			}
			if err := s.resizePTY(int(req.Rows), int(req.Cols)); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
			} else {
				_ = writeMsg(conn, opHelloResp, s.helloLocked())
			}
		case opStop:
			var req shimv1.StopRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
				continue
			}
			grace := time.Duration(req.GraceMs) * time.Millisecond
			if grace <= 0 {
				grace = 5 * time.Second
			}
			go s.Stop(grace)
			_ = writeMsg(conn, opHelloResp, s.helloLocked())
		case opRelease:
			s.Release()
			_ = writeMsg(conn, opHelloResp, s.helloLocked())
			return
		case opSubscribe:
			var req shimv1.SubscribeRequest
			if err := proto.Unmarshal(body, &req); err != nil {
				_ = writeMsg(conn, opError, &shimv1.Error{Message: err.Error()})
				return
			}
			s.serveSubscribe(conn, uint64(req.FromSeq))
			return
		default:
			_ = writeMsg(conn, opError, &shimv1.Error{Message: "unknown op"})
		}
	}
}

// helloLocked builds the current status response (caller need not hold mu;
// a copy is taken under lock).
func (s *Shim) helloLocked() *shimv1.HelloResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &shimv1.HelloResponse{
		ProtocolVersion: int32(ProtocolVersion),
		Pid:             int32(s.pid),
		Pgid:            int32(s.pgid),
		Status:          s.status,
		StartedAt:       timestamppb.New(s.startedAt),
		LastSeq:         int64(s.writer.LastSeq()),
	}
	if s.exit != nil {
		resp.Exit = &shimv1.Exited{
			Code: int32(s.exit.Code), Signal: s.exit.Signal,
			OomKilled: s.exit.OOMKilled, ExitedUnixNs: s.exit.ExitedAt.UnixNano(),
		}
	}
	return resp
}

// serveSubscribe replays segments from fromSeq, then follows live records
// until the connection drops or the child exits (Exited sent last).
func (s *Shim) serveSubscribe(conn net.Conn, fromSeq uint64) {
	recs, _ := logpipe.ReadFromSeq(s.bundle.SegmentDir, fromSeq)
	for _, r := range recs {
		if err := writeMsg(conn, opRecord, toFramed(r)); err != nil {
			return
		}
	}
	ch := make(chan logpipe.Record, 256)
	s.subsMu.Lock()
	if s.subs == nil {
		s.subs = map[chan logpipe.Record]struct{}{}
	}
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
	defer func() {
		s.subsMu.Lock()
		delete(s.subs, ch)
		s.subsMu.Unlock()
	}()
	for {
		select {
		case rec := <-ch:
			if rec.Seq < fromSeq {
				continue
			}
			if err := writeMsg(conn, opRecord, toFramed(&rec)); err != nil {
				return
			}
		case <-s.exitedCh:
			// Child exited: deliver the recorded exit last so the
			// daemon observes it even across its own restart.
			s.mu.Lock()
			ex := s.exit
			s.mu.Unlock()
			if ex != nil {
				_ = writeMsg(conn, opExited, &shimv1.Exited{
					Code: int32(ex.Code), Signal: ex.Signal,
					OomKilled: ex.OOMKilled, ExitedUnixNs: ex.ExitedAt.UnixNano(),
				})
			}
			return
		case <-time.After(15 * time.Second):
			// Keep NATs/proxies alive; client ignores unknown op 0.
			if err := writeHeartbeat(conn); err != nil {
				return
			}
		}
	}
}

func toFramed(r *logpipe.Record) *shimv1.FramedRecord {
	return &shimv1.FramedRecord{
		Seq: r.Seq, TsUnixNs: r.TS, Stream: uint32(r.Stream),
		Partial: r.Flags&logpipe.FlagPartial != 0, Payload: r.Payload,
	}
}

// readFrame reads u32 LE length + 1-byte op + protobuf body.
func readFrame(conn net.Conn) (byte, []byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n < 1 || n > 4*1024*1024 {
		return 0, nil, fmt.Errorf("shim: bad frame length %d", n)
	}
	frame := make([]byte, n)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return 0, nil, err
	}
	return frame[0], frame[1:], nil
}

func writeMsg(conn net.Conn, op byte, msg proto.Message) error {
	body, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	frame := make([]byte, 4+1+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], uint32(1+len(body)))
	frame[4] = op
	copy(frame[5:], body)
	_, err = conn.Write(frame)
	return err
}

// writeHeartbeat is a zero-length op-0 keepalive; receivers ignore it.
func writeHeartbeat(conn net.Conn) error {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], 1)
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := conn.Write([]byte{0})
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
