// LogService: GetLogs (MCP-compatible tail), SearchLogs (FTS/regex),
// TailLogs (stream: backlog + live), WaitForLog, ClearLogs, ExportLogs
// (stream chunks), GetLogStats.
package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/logpipe"
	"agent-runtime/internal/logs"
	"agent-runtime/pkg/api"
)

func (s *Server) routeLog(mux *http.ServeMux) {
	p := "/agentruntime.v1.LogService/"
	mux.HandleFunc(p+"GetLogs", s.wrap(s.logGet))
	mux.HandleFunc(p+"SearchLogs", s.wrap(s.logSearch))
	mux.HandleFunc(p+"TailLogs", s.logTail)
	mux.HandleFunc(p+"WaitForLog", s.wrap(s.logWait))
	mux.HandleFunc(p+"ClearLogs", s.wrap(s.logClear))
	mux.HandleFunc(p+"ExportLogs", s.logExport)
	mux.HandleFunc(p+"GetLogStats", s.wrap(s.logStats))
}

// redactorFor builds the workspace log redactor (nil-safe).
func (s *Server) redactorForWorkspace(workspaceID string) *config.Redactor {
	if workspaceID == "" {
		return nil
	}
	pr, err := s.engine.GetOrCreateRuntime(workspaceID)
	if err != nil {
		return nil
	}
	if res := pr.ResolvedConfig(); res != nil && len(res.Redact) > 0 {
		return config.CompileRedactor(res.Redact)
	}
	return nil
}

func (s *Server) logGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.GetLogsRequest
	_ = decode(r, &req)
	if req.ProcessID == "" {
		return nil, fmt.Errorf("processId required")
	}
	_, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	res, err := rt.GetLogs(req)
	if err != nil {
		return nil, err
	}
	if red := s.redactorForWorkspace(wsID); red != nil && !red.Empty() {
		for i := range res.Entries {
			res.Entries[i].Line = red.Redact(res.Entries[i].Line)
		}
	}
	return res, nil
}

func (s *Server) logSearch(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID   string   `json:"workspaceId"`
		ProcessIDs    []string `json:"processIds"`
		Query         string   `json:"query"`
		Regex         bool     `json:"regex"`
		CaseSensitive bool     `json:"caseSensitive"`
		Streams       []string `json:"streams"`
		MinLevel      string   `json:"minLevel"`
		Order         string   `json:"order"`
		MaxRows       int      `json:"maxRows"`
		ContextLines  int      `json:"contextLines"`
	}
	_ = decode(r, &req)
	if req.Query == "" {
		return nil, fmt.Errorf("query required")
	}
	ids := req.ProcessIDs
	if len(ids) == 0 && req.WorkspaceID != "" {
		// All processes in the workspace (loaded runtimes).
		if pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID); err == nil {
			if rt, err := pr.Runtime(); err == nil {
				if l, err := rt.List(); err == nil {
					for _, p := range l.Processes {
						ids = append(ids, p.ProcessID)
					}
				}
			}
		}
	}
	maxRows := req.MaxRows
	if maxRows <= 0 || maxRows > 1000 {
		maxRows = 200
	}
	var matches []any
	truncated := false
	// Workspace FTS index first (history, including migrated v2 logs),
	// then the live ring buffers. Segments stay authoritative.
	red := s.redactorForWorkspace(req.WorkspaceID)
	if req.WorkspaceID != "" && !req.Regex {
		if hits, trunc, err := s.searchIndex(req.WorkspaceID, ids, req.Query, maxRows-len(matches)); err == nil {
			for _, h := range hits {
				line := h.Line
				if red != nil {
					line = red.Redact(line)
				}
				matches = append(matches, map[string]any{
					"processId": h.ProcessID, "line": line,
					"stream": streamName(h.Stream), "timestamp": h.TS,
				})
			}
			truncated = truncated || trunc
		}
	}
	for _, id := range ids {
		_, rt, _, err := s.findProcess(id)
		if err != nil {
			continue
		}
		// Bounded scan over the ring buffer (FTS index lands with the
		// per-workspace index.db; segments remain authoritative).
		res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: 2000})
		if err != nil {
			continue
		}
		for _, e := range res.Entries {
			if matchLogLine(e.Line, req.Query, req.Regex, req.CaseSensitive) {
				if req.MinLevel != "" && !levelAtLeast(e.Line, req.MinLevel) {
					continue
				}
				line := e.Line
				if red != nil {
					line = red.Redact(line)
				}
				matches = append(matches, map[string]any{
					"processId": id, "line": line, "stream": e.Stream,
					"timestamp": e.Timestamp,
				})
				if len(matches) >= maxRows {
					truncated = true
					break
				}
			}
		}
		if truncated {
			break
		}
	}
	if matches == nil {
		matches = []any{}
	}
	if req.Order == "desc" {
		for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
			matches[i], matches[j] = matches[j], matches[i]
		}
	}
	return map[string]any{
		"matches": matches, "truncatedScan": truncated,
		"totalEstimate": len(matches),
	}, nil
}

// searchIndex queries the per-workspace FTS index (history + migrated logs).
func (s *Server) searchIndex(workspaceID string, processes []string, query string, maxRows int) ([]logpipe.SearchHit, bool, error) {
	if maxRows <= 0 {
		return nil, false, nil
	}
	idxPath := filepath.Join(s.engine.DataDir(), "logs", workspaceID, "index.db")
	idx, err := logpipe.OpenWorkspaceIndex(idxPath)
	if err != nil {
		return nil, false, err
	}
	defer idx.Close()
	return idx.Search(query, processes, nil, 0, 0, maxRows)
}

func streamName(st uint8) string {
	switch st {
	case 1:
		return "stdout"
	case 2:
		return "stderr"
	case 3:
		return "pty"
	case 4:
		return "system"
	}
	return "stdout"
}

func matchLogLine(line, query string, regex, caseSensitive bool) bool {
	if regex {
		return matchRegex(line, query, caseSensitive)
	}
	if !caseSensitive {
		return strings.Contains(strings.ToLower(line), strings.ToLower(query))
	}
	return strings.Contains(line, query)
}

func matchRegex(line, query string, caseSensitive bool) bool {
	q := query
	if !caseSensitive && !strings.HasPrefix(q, "(?i)") {
		q = "(?i)" + q
	}
	re, err := regexp.Compile(q)
	if err != nil {
		return strings.Contains(line, query)
	}
	return re.MatchString(line)
}

func (s *Server) logTail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		ProcessIDs  []string `json:"processIds"`
		Backlog     int      `json:"backlog"`
		Streams     []string `json:"streams"`
		Contains    string   `json:"contains"`
		Resume      string   `json:"resume"`
	}
	_ = decode(r, &req)
	if req.Backlog <= 0 {
		req.Backlog = 500
	}
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	red := s.redactorForWorkspace(req.WorkspaceID)
	// Live follow across the requested processes. Subscribe *before* reading
	// the backlog for each process, and start the live-follow cursor from
	// the last backlog entry actually sent (not from the oldest entry still
	// in the ring): the old code started live-follow at proc.EntryFrom(),
	// which is far earlier than the tail just sent, so the entire ring was
	// re-sent as "live" lines on every connect (B5).
	done := r.Context().Done()
	type sub struct {
		ch  <-chan struct{}
		off func()
		id  string
	}
	var subs []sub
	lastIDs := map[string]uint64{}
	for _, id := range req.ProcessIDs {
		_, rt, _, err := s.findProcess(id)
		if err != nil {
			continue
		}
		proc, ok := rt.Manager().Get(id)
		if !ok {
			continue
		}
		subID, wake := proc.SubscribeLogs()
		proc2 := proc
		subs = append(subs, sub{ch: wake, id: id, off: func() { proc2.UnsubscribeLogs(subID) }})
		// Default cursor if the backlog turns out empty: nothing has been
		// missed between subscribing and reading the backlog below.
		lastIDs[id] = proc.Logs.NextID()

		res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: req.Backlog, Contains: req.Contains})
		if err != nil {
			continue
		}
		var lines []any
		for _, e := range res.Entries {
			line := e.Line
			if red != nil {
				line = red.Redact(line)
			}
			lines = append(lines, map[string]any{
				"processId": id, "stream": e.Stream, "line": line,
				"timestamp": e.Timestamp, "id": e.ID,
			})
			if e.ID+1 > lastIDs[id] {
				lastIDs[id] = e.ID + 1
			}
		}
		_ = sw.send(map[string]any{"kind": "batch", "processId": id,
			"lines": lines, "cursor": time.Now().UnixNano()})
	}
	defer func() {
		for _, sb := range subs {
			sb.off()
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		progress := false
		for _, sb := range subs {
			select {
			case <-sb.ch:
				_, rt, _, err := s.findProcess(sb.id)
				if err != nil {
					continue
				}
				proc, ok := rt.Manager().Get(sb.id)
				if !ok {
					continue
				}
				for _, e := range proc.Logs.From(lastIDs[sb.id]) {
					lastIDs[sb.id] = e.ID + 1
					if req.Contains != "" && !strings.Contains(e.Line, req.Contains) {
						continue
					}
					line := e.Line
					if red != nil {
						line = red.Redact(line)
					}
					if err := sw.send(map[string]any{"kind": "line",
						"processId": sb.id, "stream": string(e.Stream),
						"line": line, "cursor": time.Now().UnixNano()}); err != nil {
						return
					}
				}
				progress = true
			default:
			}
		}
		if !progress {
			select {
			case <-done:
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
}

func (s *Server) logWait(w http.ResponseWriter, r *http.Request) (any, error) {
	var req api.WaitForLogRequest
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.WaitForLog(r.Context(), req)
}

func (s *Server) logClear(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProcessID string `json:"processId"`
		Stream    string `json:"stream"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		return nil, err
	}
	return rt.ClearLogs(req.ProcessID, req.Stream)
}

func (s *Server) logExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProcessID string `json:"processId"`
		Format    string `json:"format"`
	}
	_ = decode(r, &req)
	_, rt, _, err := s.findProcess(req.ProcessID)
	if err != nil {
		writeError(w, err)
		return
	}
	res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: req.ProcessID, Lines: 10000})
	if err != nil {
		writeError(w, err)
		return
	}
	sw, ok := newStream(w, r, nil)
	if !ok {
		return
	}
	defer sw.Close()
	const chunk = 100
	for i := 0; i < len(res.Entries); i += chunk {
		end := i + chunk
		if end > len(res.Entries) {
			end = len(res.Entries)
		}
		var lines []string
		for _, e := range res.Entries[i:end] {
			lines = append(lines, e.Line)
		}
		if err := sw.send(map[string]any{"chunk": lines}); err != nil {
			return
		}
	}
}

func (s *Server) logStats(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ProcessID   string `json:"processId"`
	}
	_ = decode(r, &req)
	stats := map[string]any{"indexLagSeconds": 0, "source": "ring"}
	if req.ProcessID != "" {
		_, rt, _, err := s.findProcess(req.ProcessID)
		if err != nil {
			return nil, err
		}
		if proc, ok := rt.Manager().Get(req.ProcessID); ok {
			stats["stdoutLines"] = proc.Logs.Count(logs.StreamStdout)
			stats["stderrLines"] = proc.Logs.Count(logs.StreamStderr)
		}
	}
	return stats, nil
}

// levelAtLeast applies the min_level filter using the shared detector.
func levelAtLeast(line, min string) bool {
	got := logs.ParseLevel(line)
	if got == "" {
		got = "info"
	}
	order := map[string]int{"trace": 0, "debug": 1, "info": 2, "warn": 3, "error": 4, "fatal": 5}
	g, ok1 := order[got]
	m, ok2 := order[min]
	if !ok1 || !ok2 {
		return true
	}
	return g >= m
}
