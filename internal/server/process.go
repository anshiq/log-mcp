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
	"strings"
	"sync"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
	"agent-runtime/internal/events"
	"agent-runtime/internal/process"
	iruntime "agent-runtime/internal/runtime"
	"agent-runtime/internal/store"
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
	data = normalizeTopLevelCasing(data)
	// Strict first (catches typos), lenient fallback for forward-compat.
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(v); err == nil {
		return nil
	}
	return json.Unmarshal(data, v)
}

func normalizeTopLevelCasing(data []byte) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}
	for k, v := range m {
		if snake := toSnakeCase(k); snake != k {
			if _, ok := m[snake]; !ok {
				m[snake] = v
			}
		}
		if camel := toCamelCase(k); camel != k {
			if _, ok := m[camel]; !ok {
				m[camel] = v
			}
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

func toSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			b.WriteString(p)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(errorHTTPCode(err))
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": err.Error(), "code": errorCode(err),
	})
}

var (
	ErrNotFound      = fmt.Errorf("not_found")
	ErrStaleRevision = fmt.Errorf("stale_revision")
	ErrPolicyDenied  = fmt.Errorf("policy_denied")
	ErrInvalidArg    = fmt.Errorf("invalid_argument")
)

func errorCode(err error) string {
	if err == nil {
		return "internal"
	}
	if isErr(err, ErrStaleRevision) {
		return "stale_revision"
	}
	if isErr(err, ErrNotFound) {
		return "not_found"
	}
	if isErr(err, ErrPolicyDenied) {
		return "policy_denied"
	}
	if isErr(err, ErrInvalidArg) {
		return "invalid_argument"
	}
	msg := err.Error()
	switch {
	case contains(msg, "unknown process"), contains(msg, "not found"):
		return "not_found"
	case contains(msg, "stale_revision"):
		return "stale_revision"
	case contains(msg, "policy_denied"):
		return "policy_denied"
	case contains(msg, "denied"):
		return "denied"
	case contains(msg, "requires either"), contains(msg, "required"), contains(msg, "invalid"), contains(msg, "unknown layer"):
		return "invalid_argument"
	}
	return "internal"
}

func isErr(err, target error) bool {
	if err == nil {
		return false
	}
	if contains(err.Error(), target.Error()) {
		return true
	}
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func errorHTTPCode(err error) int {
	switch errorCode(err) {
	case "not_found":
		return http.StatusNotFound
	case "stale_revision":
		return http.StatusConflict
	case "policy_denied", "denied":
		return http.StatusForbidden
	case "invalid_argument":
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
	prow, _ := s.engine.Store().CreateProcess(res.ProcessID, req.WorkspaceID, req.App, string(specJSON), lifetime, "", req.SessionID, 0)
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
	d := 30 * time.Second
	if req.TimeoutMs > 0 {
		d = time.Duration(req.TimeoutMs)*time.Millisecond + 5*time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), d)
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
		seen := map[string]bool{}
		for _, m := range out {
			if mm, ok := m.(map[string]any); ok {
				if id, ok := mm["process_id"].(string); ok {
					seen[id] = true
				}
			}
		}
		for _, pr := range s.engine.LoadedRuntimes() {
			_ = pr
		}
		extra := s.unloadedStoreRows()
		for _, e := range extra {
			if id, ok := e["processId"].(string); ok {
				if !seen[id] {
					out = append(out, e)
				}
			}
		}
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

func (s *Server) unloadedStoreRows() []map[string]any {
	loaded := map[string]bool{}
	s.engine.RangeRuntimes(func(pr *corePR) bool {
		loaded[pr.WorkspaceID()] = true
		return true
	})
	var out []map[string]any
	projs, _ := s.engine.Store().ListProjects()
	for _, p := range projs {
		wss, _ := s.engine.Store().ListWorkspaces(p.ID)
		for _, ws := range wss {
			if loaded[ws.ID] {
				continue
			}
			rows, _ := s.engine.Store().ListProcesses(ws.ID)
			for _, row := range rows {
				out = append(out, s.storeRowInfo(ws.ID, p.ID, row))
			}
		}
	}
	return out
}

func (s *Server) processInfo(pr *corePR, st *api.StatusResult) map[string]any {
	ports := []int{}
	if st.PID > 0 {
		if p := process.ListeningPortsForPID(st.PID); p != nil {
			ports = p
		}
	}
	if ports == nil {
		ports = []int{}
	}
	app := ""
	lifetime := ""
	sessionID := ""
	if row, err := s.engine.Store().GetProcess(st.ProcessID); err == nil {
		app = row.App
		lifetime = row.Lifetime
		sessionID = row.StartedBy
		if lifetime == "" {
			lifetime = "persistent"
		}
	}
	return map[string]any{
		"id": st.ProcessID, "processId": st.ProcessID, "instanceId": st.InstanceID, "app": app,
		"workspaceId": pr.WorkspaceID(), "projectId": pr.ProjectID(),
		"command": st.Command, "args": st.Args, "workdir": st.WorkDir, "profile": st.Profile,
		"status": st.Status, "pid": st.PID, "restarts": st.Restarts,
		"health": st.Health, "startedAt": st.StartedAt, "exitedAt": st.ExitedAt,
		"exitCode": st.ExitCode, "exitSignal": "", "restartPolicy": st.RestartPolicy,
		"stdoutLines": st.StdoutLines, "stderrLines": st.StderrLines,
		"ports": ports, "stale": pr.IsStale(""), "loaded": true,
		"lifetime": lifetime, "sessionId": sessionID,
	}
}

func (s *Server) storeRowInfo(wsID, projectID string, row *store.ProcessRow) map[string]any {
	return map[string]any{
		"id": row.ID, "processId": row.ID, "instanceId": "", "app": row.App,
		"workspaceId": wsID, "projectId": projectID,
		"command": "", "args": []string{}, "workdir": "", "profile": "",
		"status": "exited", "pid": 0, "restarts": 0,
		"health": "unknown", "startedAt": nil, "exitedAt": nil,
		"exitCode": nil, "exitSignal": "", "restartPolicy": row.Restart,
		"stdoutLines": 0, "stderrLines": 0,
		"ports": []int{}, "stale": false, "loaded": false,
		"lifetime": row.Lifetime, "sessionId": row.StartedBy,
	}
}

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
				if ev.Type == events.Stdout || ev.Type == events.Stderr {
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
	seen := map[string]bool{}
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
			seen[p.ProcessID] = true
			out = append(out, s.processInfo(pr, st))
		}
		return true
	}
	if all || workspaceID == "" {
		s.engine.RangeRuntimes(collect)
		for _, e := range s.unloadedStoreRows() {
			if id, ok := e["processId"].(string); ok && !seen[id] {
				out = append(out, e)
			}
		}
	} else if pr, err := s.engine.GetOrCreateRuntime(workspaceID); err == nil {
		collect(pr)
	}
	if out == nil {
		out = []any{}
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
	d := 30 * time.Second
	if req.TimeoutMS > 0 {
		d = time.Duration(req.TimeoutMS)*time.Millisecond + 5*time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), d)
	defer cancel()
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
	// Follow live via log subscription.
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
	_ = sw.send(map[string]any{"kind": "batch", "lines": attachBatchLines(logs.Entries)})
	done := r.Context().Done()
	for {
		select {
		case <-done:
			return
		case <-wake:
			for _, e := range proc.Logs.From(lastID) {
				lastID = e.ID + 1
				if err := sw.send(map[string]any{"kind": "output", "data": e.Line,
					"stream": string(e.Stream), "id": e.ID,
					"timestamp": e.Timestamp.Format(time.RFC3339Nano),
					"cursor":    req.ProcessID + ":" + fmt.Sprint(e.ID)}); err != nil {
					return
				}
			}
		}
	}
}

func attachBatchLines(entries []api.LogEntry) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"data": e.Line, "stream": e.Stream, "id": e.ID,
			"timestamp": e.Timestamp, "cursor": fmt.Sprint(e.ID),
		})
	}
	return out
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
	if st != nil && st.PID > 0 {
		ports = process.ListeningPortsForPID(st.PID)
	}
	if ports == nil {
		ports = []int{}
	}
	pid := 0
	if st != nil {
		pid = st.PID
	}
	return usagePayload(req.ProcessID, pid, mem, cpu, ports), nil
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
			st, _ := rt.Status(req.ProcessID)
			pid := 0
			ports := []int{}
			if st != nil {
				pid = st.PID
				if pid > 0 {
					ports = process.ListeningPortsForPID(pid)
				}
			}
			if ports == nil {
				ports = []int{}
			}
			if err := sw.send(usagePayload(req.ProcessID, pid, mem, cpu, ports)); err != nil {
				return
			}
		}
	}
}
