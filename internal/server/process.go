// ProcessService handlers: Start/Stop/Restart/Signal/SendStdin/Remove/
// Get/List/WatchProcesses/WaitForExit/SetRestartPolicy/GetEnv/OpenShell/
// Attach/GetResourceUsage/WatchResourceUsage.
//
// Process IDs are globally unique (proc_<ulid-ish>); the workspace is
// stored on the process row, so every call needs only the process id.
// Policy is evaluated against that process's own project.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
	"agent-runtime/internal/events"
	"agent-runtime/internal/process"
	iruntime "agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
)

// Aliases keep handler signatures short.
type (
	corePR   = core.ProjectRuntime
	coreRT   = iruntime.Runtime
	busEvent = events.Event
)

// routeProcess registers ProcessService routes.
func (s *Server) routeProcess(mux *http.ServeMux) {
	p := "/agentruntime.v1.ProcessService/"
	mux.HandleFunc(p+"Start", s.wrap(s.handleProcStart))
	mux.HandleFunc(p+"Stop", s.wrap(s.handleProcStop))
	mux.HandleFunc(p+"Restart", s.wrap(s.handleProcRestart))
	mux.HandleFunc(p+"Signal", s.wrap(s.handleProcSignal))
	mux.HandleFunc(p+"SendStdin", s.wrap(s.handleProcSendStdin))
	mux.HandleFunc(p+"Remove", s.wrap(s.handleProcRemove))
	mux.HandleFunc(p+"Get", s.wrap(s.handleProcGet))
	mux.HandleFunc(p+"List", s.wrap(s.handleProcList))
	mux.HandleFunc(p+"WatchProcesses", s.handleProcWatch)
	mux.HandleFunc(p+"WaitForExit", s.wrap(s.handleProcWaitExit))
	mux.HandleFunc(p+"SetRestartPolicy", s.wrap(s.handleProcSetRestart))
	mux.HandleFunc(p+"GetEnv", s.wrap(s.handleProcGetEnv))
	mux.HandleFunc(p+"OpenShell", s.wrap(s.handleProcOpenShell))
	mux.HandleFunc(p+"Attach", s.handleProcAttach)
	mux.HandleFunc(p+"GetResourceUsage", s.wrap(s.handleProcResUsage))
	mux.HandleFunc(p+"WatchResourceUsage", s.handleProcWatchUsage)
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) (any, error)

func (s *Server) wrap(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		res, err := fn(w, r)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			s.audit(r, "error:"+errorCode(err), ms)
			writeError(w, err)
			return
		}
		s.audit(r, "ok", ms)
		writeJSON(w, res)
	}
}

func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	// Strict first (catches typos), lenient fallback for forward-compat.
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(v); err == nil {
		return nil
	}
	return json.Unmarshal(data, v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(errorHTTPCode(err))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": err.Error(), "code": errorCode(err),
	})
}

func errorCode(err error) string { return "internal" }

func errorHTTPCode(err error) int {
	msg := err.Error()
	switch {
	case contains(msg, "unknown process"), contains(msg, "not found"):
		return http.StatusNotFound
	case contains(msg, "policy_denied"), contains(msg, "denied"):
		return http.StatusForbidden
	case contains(msg, "repo_untrusted"):
		return http.StatusForbidden
	case contains(msg, "requires either"), contains(msg, "required"), contains(msg, "invalid"):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

// parseEnvField accepts env as ["K=V", ...] or {"K": "V"}.
func parseEnvField(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		out := make([]string, 0, len(m))
		for k, v := range m {
			out = append(out, k+"="+v)
		}
		return out
	}
	return nil
}

// sessionOf extracts session attribution headers for audit rows.
func (s *Server) sessionOf(r *http.Request) (id, harness string) {
	return r.Header.Get("X-Agent-Runtime-Session"), r.Header.Get("X-Agent-Runtime-Harness")
}

func (s *Server) audit(r *http.Request, result string, ms int64) {
	sess, harness := s.sessionOf(r)
	_ = s.engine.Store().AppendAudit(sess, harness, "", "", r.URL.Path, "", result, ms)
}

// runtimeForRequest resolves workspace_id → hosted runtime.
func (s *Server) runtimeForRequest(workspaceID string) (*corePR, *coreRT, error) {
	return s.engine.RuntimeFor(workspaceID)
}

func (s *Server) handleProcStart(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string          `json:"workspaceId"`
		App         string          `json:"app"`
		Command     []string        `json:"command"`
		Workdir     string          `json:"workdir"`
		Env         json.RawMessage `json:"env"`
		Lifetime    string          `json:"lifetime"`
		Pty         bool            `json:"pty"`
		SessionID   string          `json:"sessionId"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if req.WorkspaceID == "" {
		return nil, fmt.Errorf("workspaceId required")
	}
	_, rt, err := s.engine.RuntimeFor(req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	env := parseEnvField(req.Env)
	// Request-time ${port:name} templates allocate without a default
	// (app-layer templates were already expanded at config load).
	if expanded, err := config.ExpandPortTemplates(env, func(name string) (int, error) {
		return s.engine.AllocatePort(req.WorkspaceID, name, 0)
	}); err == nil {
		env = expanded
	}
	startReq := api.StartRequest{App: req.App, WorkDir: req.Workdir, Env: env}
	if len(req.Command) > 0 {
		startReq.Command = req.Command[0]
		startReq.Args = req.Command[1:]
	}
	if req.SessionID == "" {
		req.SessionID, _ = s.sessionOf(r)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := rt.Start(ctx, startReq)
	if err != nil {
		return nil, err
	}
	lifetime := req.Lifetime
	if lifetime == "" {
		lifetime = "persistent"
	}
	specJSON, _ := json.Marshal(map[string]any{"app": req.App, "command": req.Command, "workdir": req.Workdir})
	prow, _ := s.engine.Store().CreateProcess(req.WorkspaceID, req.App, string(specJSON), lifetime, "", req.SessionID, 0)
	pid := 0
	if st, err := rt.Status(res.ProcessID); err == nil {
		pid = st.PID
		if prow != nil {
			_, _ = s.engine.Store().CreateInstance(prow.ID, int64(pid), 0, "", string(st.Status))
		}
	}
	return map[string]any{
		"processId": res.ProcessID, "instanceId": res.InstanceID,
		"status": res.Status, "pid": pid,
		"profile": res.Profile, "command": res.Command,
	}, nil
}

func (s *Server) findProcess(id string) (*corePR, *coreRT, string, error) {
	// Loaded runtimes first (hot path).
	var found *corePR
	var foundRT *coreRT
	s.engine.RangeRuntimes(func(pr *corePR) bool {
		rt, err := pr.Runtime()
		if err != nil {
			return true
		}
		if _, ok := rt.Manager().Get(id); ok {
			found, foundRT = pr, rt
			return false
		}
		return true
	})
	if found != nil {
		return found, foundRT, found.WorkspaceID(), nil
	}
	// Store row → load that workspace's runtime on demand.
	if row, err := s.engine.Store().GetProcess(id); err == nil {
		pr, rt, err := s.engine.RuntimeFor(row.WorkspaceID)
		if err != nil {
			return nil, nil, "", err
		}
		if _, ok := rt.Manager().Get(id); ok {
			return pr, rt, row.WorkspaceID, nil
		}
	}
	return nil, nil, "", fmt.Errorf("unknown process %q", id)
}

func (s *Server) handleProcStop(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
		TimeoutMs int    `json:"timeoutMs"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := rt.Stop(ctx, req.ProcessID); err != nil {
		return nil, err
	}
	return map[string]any{"processId": req.ProcessID, "stopped": true}, nil
}

func (s *Server) handleProcRestart(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := rt.Restart(ctx, req.ProcessID); err != nil {
		return nil, err
	}
	st, _ := rt.Status(req.ProcessID)
	return map[string]any{"processId": req.ProcessID, "newInstanceId": st.InstanceID}, nil
}
func (s *Server) handleProcSignal(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.SignalProcessRequest
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := rt.SignalProcess(ctx, req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) handleProcSendStdin(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
		Data      string `json:"data"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	res, err := rt.SendStdin(req.ProcessID, req.Data)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) handleProcRemove(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
		Force     bool   `json:"force"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	res, err := rt.RemoveProcess(req.ProcessID, req.Force)
	if err != nil {
		return nil, err
	}
	_ = s.engine.Store().RemoveProcess(req.ProcessID)
	return res, nil
}

func (s *Server) handleProcGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	pr, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	st, err := rt.Status(req.ProcessID)
	if err != nil {
		return nil, err
	}
	// api.StatusResult shape (snake_case) + daemon extras (ignored by Go
	// decoders, read by the TS GUI). The MCP bridge decodes this straight
	// into api.StatusResult so tool shapes never change.
	return enrichStatus(st, pr), nil
}

// enrichStatus merges an api.StatusResult with daemon extras.
func enrichStatus(st *api.StatusResult, pr *corePR) map[string]any {
	m := structToMap(st)
	m["workspaceId"] = pr.WorkspaceID()
	m["projectId"] = pr.ProjectID()
	if st.PID > 0 {
		m["ports"] = process.ListeningPortsForPID(st.PID)
	}
	return m
}

func structToMap(v any) map[string]any {
	data, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func (s *Server) handleProcList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID   string `json:"workspaceId"`
		AllWorkspaces bool   `json:"allWorkspaces"`
		Filter        string `json:"filter"`
	}
	_ = decode(r, &req)
	var out []any
	collect := func(pr *corePR) bool {
		rt, err := pr.Runtime()
		if err != nil {
			return true
		}
		l, err := rt.List()
		if err != nil {
			return true
		}
		for _, p := range l.Processes {
			st, err := rt.Status(p.ProcessID)
			if err != nil {
				continue
			}
			if req.Filter != "" && !contains(st.Command, req.Filter) && !contains(p.ProcessID, req.Filter) {
				continue
			}
			// api.ProcessSummary shape + daemon extras (decode-safe).
			m := structToMap(p)
			m["workspaceId"] = pr.WorkspaceID()
			m["projectId"] = pr.ProjectID()
			out = append(out, m)
		}
		return true
	}
	if req.AllWorkspaces || req.WorkspaceID == "" {
		s.engine.RangeRuntimes(collect)
		// Also surface store rows for workspaces not currently loaded.
	} else {
		pr, _, err := s.engine.RuntimeFor(req.WorkspaceID)
		if err != nil {
			return nil, err
		}
		collect(pr)
	}
	if out == nil {
		out = []any{}
	}
	return map[string]any{"processes": out}, nil
}

func (s *Server) processInfo(pr *corePR, st *api.StatusResult) map[string]any {
	ports := []int{}
	if st.PID > 0 {
		if p := process.ListeningPortsForPID(st.PID); p != nil {
			ports = p
		}
	}
	return map[string]any{
		"id": st.ProcessID, "instanceId": st.InstanceID,
		"workspaceId": pr.WorkspaceID(), "projectId": pr.ProjectID(),
		"command": st.Command, "args": st.Args, "workdir": st.WorkDir, "profile": st.Profile,
		"status": st.Status, "pid": st.PID, "restarts": st.Restarts,
		"health": st.Health, "startedAt": st.StartedAt, "exitedAt": st.ExitedAt,
		"exitCode": st.ExitCode, "restartPolicy": st.RestartPolicy,
		"stdoutLines": st.StdoutLines, "stderrLines": st.StderrLines,
		"ports": ports, "stale": pr.IsStale(""),
	}
}

// handleProcWatch streams process lifecycle: a snapshot (never null, even
// when nothing is loaded yet), then upserts carrying the full ProcessInfo
// (not just the event name — the UI store needs the process to update its
// rows) and removed markers on Remove. It attaches to runtimes that load
// *after* the stream opens (via Engine.SubscribeRuntimeLoad) so a fresh
// daemon with nothing loaded yet still sees processes started later in any
// workspace, instead of only ever seeing the runtimes that existed when the
// WatchProcesses call was made.
func (s *Server) handleProcWatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID   string `json:"workspaceId"`
		AllWorkspaces bool   `json:"allWorkspaces"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()

	all := req.AllWorkspaces || req.WorkspaceID == ""
	snap, _ := s.snapshotProcesses(req.WorkspaceID, all)
	if snap == nil {
		snap = []any{}
	}
	_ = sw.send(map[string]any{"kind": "snapshot", "snapshot": snap, "cursor": fmt.Sprint(time.Now().UnixNano())})

	done := r.Context().Done()
	queue := make(chan map[string]any, 1024)
	dropped := 0
	var subMu sync.Mutex
	offs := []func(){}
	stopSubs := func() {
		subMu.Lock()
		defer subMu.Unlock()
		for _, off := range offs {
			off()
		}
		offs = nil
	}
	defer stopSubs()

	enqueue := func(msg map[string]any) {
		select {
		case queue <- msg:
		default:
			dropped++
		}
	}

	watchRuntime := func(pr *corePR) {
		rt, err := pr.Runtime()
		if err != nil {
			return
		}
		ch, off := rt.Manager().Events().Subscribe()
		subMu.Lock()
		offs = append(offs, off)
		subMu.Unlock()
		go func() {
			for ev := range ch {
				if ev.Type == events.Removed {
					enqueue(map[string]any{"kind": "removed", "processId": ev.ProcessID,
						"cursor": fmt.Sprint(time.Now().UnixNano())})
					continue
				}
				st, err := rt.Status(ev.ProcessID)
				if err != nil {
					continue
				}
				enqueue(map[string]any{"kind": "upsert", "event": string(ev.Type),
					"processId": ev.ProcessID, "process": s.processInfo(pr, st),
					"cursor": fmt.Sprint(time.Now().UnixNano())})
			}
		}()
	}

	if all {
		s.engine.RangeRuntimes(func(pr *corePR) bool { watchRuntime(pr); return true })
	} else if pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID); err == nil {
		watchRuntime(pr)
	}

	// Attach to workspaces loaded after this stream opened.
	loadCh, unloadSub := s.engine.SubscribeRuntimeLoad()
	defer unloadSub()
	go func() {
		for wsID := range loadCh {
			if !all && wsID != req.WorkspaceID {
				continue
			}
			if pr, err := s.engine.GetOrCreateRuntime(wsID); err == nil {
				watchRuntime(pr)
			}
		}
	}()

	coalesce := map[string]map[string]any{}
	flush := time.NewTicker(100 * time.Millisecond)
	defer flush.Stop()
	for {
		select {
		case <-done:
			return
		case msg := <-queue:
			key, _ := msg["processId"].(string)
			if msg["kind"] == "upsert" {
				coalesce[key] = msg
				continue
			}
			if len(coalesce) > 0 {
				if err := s.flushCoalesced(sw, coalesce); err != nil {
					return
				}
				coalesce = map[string]map[string]any{}
			}
			if err := sw.send(msg); err != nil {
				return
			}
		case <-flush.C:
			if dropped > 0 {
				_ = sw.send(gapMessage(dropped, "slow consumer"))
				dropped = 0
			}
			if len(coalesce) == 0 {
				continue
			}
			if err := s.flushCoalesced(sw, coalesce); err != nil {
				return
			}
			coalesce = map[string]map[string]any{}
		}
	}
}

// flushCoalesced sends at most one upsert per process per 100ms tick, so a
// restart storm (started/health/exited in quick succession) doesn't flood
// the stream with redundant frames for the same process.
func (s *Server) flushCoalesced(sw *streamWriter, coalesce map[string]map[string]any) error {
	for _, msg := range coalesce {
		if err := sw.send(msg); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) snapshotProcesses(workspaceID string, all bool) ([]any, error) {
	var out []any
	collect := func(pr *corePR) bool {
		rt, err := pr.Runtime()
		if err != nil {
			return true
		}
		l, err := rt.List()
		if err != nil {
			return true
		}
		for _, p := range l.Processes {
			st, err := rt.Status(p.ProcessID)
			if err != nil {
				continue
			}
			out = append(out, s.processInfo(pr, st))
		}
		return true
	}
	if all || workspaceID == "" {
		s.engine.RangeRuntimes(collect)
	} else if pr, err := s.engine.GetOrCreateRuntime(workspaceID); err == nil {
		collect(pr)
	}
	return out, nil
}

func (s *Server) handleProcWaitExit(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.WaitForExitParams
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	res, err := rt.WaitForExit(ctx, req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Server) handleProcSetRestart(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.SetRestartPolicyRequest
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.SetRestartPolicy(req)
}

func (s *Server) handleProcGetEnv(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.ProcessEnvRequest
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.ProcessEnv(req)
}

func (s *Server) handleProcOpenShell(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		api.OpenShellRequest
		WorkspaceID string `json:"workspaceId"`
		Lifetime    string `json:"lifetime"`
		SessionID   string `json:"sessionId"`
	}
	_ = decode(r, &req)
	var rt *coreRT
	if req.ProcessID != "" {
		_, rtv, _, err := s.findProcess(req.ProcessID)
		if err != nil {
			return nil, err
		}
		rt = rtv
	} else {
		if req.WorkspaceID == "" {
			return nil, fmt.Errorf("workspaceId required when process_id is empty")
		}
		_, rtv, err := s.engine.RuntimeFor(req.WorkspaceID)
		if err != nil {
			return nil, err
		}
		rt = rtv
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := rt.OpenShell(ctx, req.OpenShellRequest)
	if err != nil {
		return nil, err
	}
	// open_shell defaults to lifetime: session (§5.6).
	return map[string]any{
		"processId": res.ProcessID, "instanceId": res.InstanceID,
		"status": res.Status, "lifetime": "session",
	}, nil
}

// handleProcAttach serves PTY output as a server stream (half-duplex:
// input goes through SendStdin). Full bidi arrives with Connect codegen.
func (s *Server) handleProcAttach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProcessID string `json:"processId"`
		Backlog   int    `json:"backlog"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		writeError(w, err)
		return
	}
	backlog := req.Backlog
	if backlog <= 0 {
		backlog = 500
	}
	// Follow live via log subscription. Subscribe *before* reading the
	// backlog, and start the live cursor at the last backlog entry sent
	// (falling back to the subscribe-time NextID when the backlog is
	// empty), so the live stream never re-sends what the backlog already
	// covered (B5: the old code started at proc.EntryFrom(), the oldest
	// entry still in the ring, and replayed the whole ring as "live").
	proc, ok := rt.Manager().Get(req.ProcessID)
	if !ok {
		writeError(w, fmt.Errorf("unknown process %q", req.ProcessID))
		return
	}
	subID, wake := proc.SubscribeLogs()
	lastID := proc.Logs.NextID()
	logs, err := rt.GetLogs(api.GetLogsRequest{ProcessID: req.ProcessID, Lines: backlog})
	if err != nil {
		proc.UnsubscribeLogs(subID)
		writeError(w, err)
		return
	}
	for _, e := range logs.Entries {
		if e.ID+1 > lastID {
			lastID = e.ID + 1
		}
	}
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		proc.UnsubscribeLogs(subID)
		return
	}
	defer sw.Close()
	defer proc.UnsubscribeLogs(subID)
	_ = sw.send(map[string]any{"kind": "batch", "lines": logs.Entries})
	done := r.Context().Done()
	for {
		select {
		case <-done:
			return
		case <-wake:
			for _, e := range proc.Logs.From(lastID) {
				lastID = e.ID + 1
				if err := sw.send(map[string]any{"kind": "line",
					"line": map[string]any{"stream": string(e.Stream), "line": e.Line}}); err != nil {
					return
				}
			}
		}
	}
}

func (s *Server) handleProcResUsage(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	proc, ok := rt.Manager().Get(req.ProcessID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	mem, cpu := proc.LiveUsage()
	st, _ := rt.Status(req.ProcessID)
	ports := []int{}
	if st.PID > 0 {
		ports = process.ListeningPortsForPID(st.PID)
	}
	return map[string]any{
		"processId": req.ProcessID, "pid": st.PID,
		"memoryBytes": mem, "cpuNanos": cpu, "ports": ports,
	}, nil
}

func (s *Server) handleProcWatchUsage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProcessID string `json:"processId"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		writeError(w, err)
		return
	}
	proc, ok := rt.Manager().Get(req.ProcessID)
	if !ok {
		writeError(w, fmt.Errorf("unknown process %q", req.ProcessID))
		return
	}
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			mem, cpu := proc.LiveUsage()
			if err := sw.send(map[string]any{"memoryBytes": mem, "cpuNanos": cpu,
				"at": time.Now().UnixNano()}); err != nil {
				return
			}
		}
	}
}
