// LogService: GetLogs (MCP-compatible tail), SearchLogs (FTS/regex),
// TailLogs (stream: backlog + live), WaitForLog, ClearLogs, ExportLogs
// (stream chunks), GetLogStats.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
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
		TimeFrom      string   `json:"timeFrom"`
		TimeTo        string   `json:"timeTo"`
		Offset        int      `json:"offset"`
	}
	_ = decode(r, &req)
	if req.Query == "" {
		return nil, fmt.Errorf("query required")
	}
	ids := req.ProcessIDs
	if len(ids) == 0 {
		if req.WorkspaceID != "" {
			if pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID); err == nil {
				if rt, err := pr.Runtime(); err == nil {
					if l, err := rt.List(); err == nil {
						for _, p := range l.Processes {
							ids = append(ids, p.ProcessID)
						}
					}
				}
			}
		} else {
			s.engine.RangeRuntimes(func(pr *core.ProjectRuntime) bool {
				if rt, err := pr.Runtime(); err == nil {
					if l, err := rt.List(); err == nil {
						for _, p := range l.Processes {
							ids = append(ids, p.ProcessID)
						}
					}
				}
				return true
			})
		}
	}
	maxRows := req.MaxRows
	if maxRows <= 0 || maxRows > 1000 {
		maxRows = 200
	}
	var fromMs, toMs int64
	if req.TimeFrom != "" {
		fromMs = parseTimeBound(req.TimeFrom)
	}
	if req.TimeTo != "" {
		toMs = parseTimeBound(req.TimeTo)
	}
	var matches []any
	truncated := false
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
					"id": 0, "level": logs.ParseLevel(line), "instanceId": "",
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
		res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: id, Lines: 2000})
		if err != nil {
			continue
		}
		instID := ""
		if proc, ok := rt.Manager().Get(id); ok {
			instID = proc.InstanceID()
		}
		var ring []api.LogEntry
		for _, e := range res.Entries {
			if fromMs > 0 || toMs > 0 {
				ts := parseEntryTime(e.Timestamp)
				if fromMs > 0 && ts < fromMs {
					continue
				}
				if toMs > 0 && ts > toMs {
					continue
				}
			}
			ring = append(ring, e)
		}
		for idx, e := range ring {
			if matchLogLine(e.Line, req.Query, req.Regex, req.CaseSensitive) {
				if req.MinLevel != "" && !levelAtLeast(e.Line, req.MinLevel) {
					continue
				}
				if len(req.Streams) > 0 && !streamAllowed(e.Stream, req.Streams) {
					continue
				}
				line := e.Line
				if red != nil {
					line = red.Redact(line)
				}
				m := map[string]any{
					"processId": id, "line": line, "stream": e.Stream,
					"timestamp": e.Timestamp, "id": e.ID,
					"level": logs.ParseLevel(line), "instanceId": instID,
				}
				if req.ContextLines > 0 {
					before := []any{}
					after := []any{}
					for b := idx - req.ContextLines; b < idx; b++ {
						if b >= 0 {
							before = append(before, map[string]any{"line": ring[b].Line, "id": ring[b].ID})
						}
					}
					for a := idx + 1; a <= idx+req.ContextLines && a < len(ring); a++ {
						after = append(after, map[string]any{"line": ring[a].Line, "id": ring[a].ID})
					}
					m["context"] = map[string]any{"before": before, "after": after}
				}
				matches = append(matches, m)
				if len(matches) >= maxRows+req.Offset {
					truncated = true
					break
				}
			}
		}
		if truncated {
			break
		}
	}
	if req.Offset > 0 {
		if req.Offset < len(matches) {
			matches = matches[req.Offset:]
		} else {
			matches = []any{}
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

func streamAllowed(got string, want []string) bool {
	for _, w := range want {
		if w == got || w == "all" {
			return true
		}
	}
	return false
}

func parseTimeBound(v string) int64 {
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n < 1e12 {
			return n * 1000
		}
		return n
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli()
	}
	return 0
}

func parseEntryTime(v string) int64 {
	if v == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UnixMilli()
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli()
	}
	return 0
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
	var raw map[string]json.RawMessage
	_ = decode(r, &raw)
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		ProcessIDs  []string `json:"processIds"`
		Backlog     int      `json:"backlog"`
		Streams     []string `json:"streams"`
		Contains    string   `json:"contains"`
	}
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(normalizeTopLevelCasing(b), &req)
	resume := map[string]uint64{}
	if v, ok := raw["resume"]; ok && len(v) > 0 {
		var m map[string]uint64
		if json.Unmarshal(v, &m) == nil {
			resume = m
		} else {
			var single uint64
			if json.Unmarshal(v, &single) == nil {
				_ = single
			}
		}
	}
	if req.Backlog <= 0 {
		req.Backlog = 500
	}
	ids := req.ProcessIDs
	if len(ids) == 0 && req.WorkspaceID != "" {
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
	if len(ids) == 0 && req.WorkspaceID == "" {
		s.engine.RangeRuntimes(func(pr *core.ProjectRuntime) bool {
			if rt, err := pr.Runtime(); err == nil {
				if l, err := rt.List(); err == nil {
					for _, p := range l.Processes {
						ids = append(ids, p.ProcessID)
					}
				}
			}
			return true
		})
	}
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	red := s.redactorForWorkspace(req.WorkspaceID)
	done := r.Context().Done()
	type sub struct {
		ch     <-chan struct{}
		off    func()
		id     string
		instID string
	}
	var subs []sub
	lastIDs := map[string]uint64{}
	fanIn := make(chan string, 1024)
	for _, id := range ids {
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
		pid := id
		go func() {
			for range wake {
				select {
				case fanIn <- pid:
				default:
				}
			}
		}()
		subs = append(subs, sub{ch: wake, id: id, instID: proc.InstanceID(), off: func() { proc2.UnsubscribeLogs(subID) }})
		lastIDs[id] = proc.Logs.NextID()
		if rv, ok := resume[id]; ok {
			oldest := proc.Logs.OldestID()
			if rv < oldest && oldest > 0 {
				_ = sw.send(map[string]any{"kind": "gap", "processId": id, "expected": rv, "oldest": oldest})
			}
			lastIDs[id] = rv
		} else {
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
				lv := logs.ParseLevel(line)
				lines = append(lines, map[string]any{
					"processId": id, "stream": e.Stream, "line": line,
					"timestamp": e.Timestamp, "id": e.ID, "level": lv,
					"instanceId": proc.InstanceID(), "cursor": id + ":" + strconv.FormatUint(e.ID, 10),
				})
				if e.ID+1 > lastIDs[id] {
					lastIDs[id] = e.ID + 1
				}
			}
			_ = sw.send(map[string]any{"kind": "batch", "processId": id,
				"lines": lines, "cursor": id + ":" + strconv.FormatUint(lastIDs[id], 10)})
		}
	}
	defer func() {
		for _, sb := range subs {
			sb.off()
		}
	}()
	byID := map[string]*sub{}
	for i := range subs {
		byID[subs[i].id] = &subs[i]
	}
	for {
		select {
		case <-done:
			return
		case pid := <-fanIn:
			sb, ok := byID[pid]
			if !ok {
				continue
			}
			_, rt, _, err := s.findProcess(sb.id)
			if err != nil {
				continue
			}
			proc, ok := rt.Manager().Get(sb.id)
			if !ok {
				continue
			}
			cur := proc.InstanceID()
			if cur != sb.instID {
				sb.instID = cur
				_ = sw.send(map[string]any{"kind": "instance", "processId": sb.id, "instanceId": cur})
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
				lv := logs.ParseLevel(line)
				if err := sw.send(map[string]any{"kind": "line",
					"processId": sb.id, "stream": string(e.Stream),
					"line": line, "id": e.ID, "timestamp": e.Timestamp.Format(time.RFC3339Nano),
					"level": lv, "instanceId": cur,
					"cursor": sb.id + ":" + strconv.FormatUint(e.ID, 10)}); err != nil {
					return
				}
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
	d := 30 * time.Second
	if req.TimeoutMS > 0 {
		d = time.Duration(req.TimeoutMS)*time.Millisecond + 5*time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), d)
	defer cancel()
	return rt.WaitForLog(ctx, req)
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
	_, rt, wsID, err := s.findProcess(req.ProcessID)
	if err != nil {
		writeError(w, err)
		return
	}
	format := req.Format
	if format == "" {
		format = "ndjson"
	}
	res, err := rt.GetLogs(api.GetLogsRequest{ProcessID: req.ProcessID, Lines: 100000})
	if err != nil {
		writeError(w, err)
		return
	}
	red := s.redactorForWorkspace(wsID)
	sw, ok := newStream(w, r, nil)
	if !ok {
		return
	}
	defer sw.Close()
	const chunk = 100
	instID := ""
	if proc, ok := rt.Manager().Get(req.ProcessID); ok {
		instID = proc.InstanceID()
	}
	n := 0
	for i := 0; i < len(res.Entries); i += chunk {
		end := i + chunk
		if end > len(res.Entries) {
			end = len(res.Entries)
		}
		var data string
		for _, e := range res.Entries[i:end] {
			line := e.Line
			if red != nil {
				line = red.Redact(line)
			}
			switch format {
			case "txt":
				data += line + "\n"
			case "json":
				b, _ := json.Marshal(map[string]any{"id": e.ID, "timestamp": e.Timestamp, "stream": e.Stream, "line": line, "instanceId": instID})
				data += string(b) + "\n"
			default:
				b, _ := json.Marshal(map[string]any{"line": line, "stream": e.Stream, "timestamp": e.Timestamp, "id": e.ID})
				data += string(b) + "\n"
			}
			n++
		}
		if err := sw.send(map[string]any{"kind": "chunk", "data": data, "format": format}); err != nil {
			return
		}
	}
	_ = sw.send(map[string]any{"kind": "done", "lines": n, "format": format})
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
			stdout := proc.Logs.Count(logs.StreamStdout)
			stderr := proc.Logs.Count(logs.StreamStderr)
			stats["stdoutLines"] = stdout
			stats["stderrLines"] = stderr
			stats["totalLines"] = stdout + stderr
			all := proc.Logs.Query(logs.Query{})
			var first, last string
			var disk int64
			for _, e := range all.Entries {
				if first == "" {
					first = e.Timestamp.Format(time.RFC3339Nano)
				}
				last = e.Timestamp.Format(time.RFC3339Nano)
				disk += int64(len(e.Line))
			}
			stats["firstTimestamp"] = first
			stats["lastTimestamp"] = last
			stats["diskBytes"] = disk
			stats["segmentCount"] = 0
			if req.WorkspaceID != "" {
				seg := filepath.Join(s.engine.DataDir(), "logs", req.WorkspaceID, "segments")
				if entries, err := os.ReadDir(seg); err == nil {
					stats["segmentCount"] = len(entries)
				}
			}
		}
	} else {
		stats["totalLines"] = 0
		stats["diskBytes"] = logDiskBytes(s.engine.DataDir())
		stats["segmentCount"] = 0
		stats["firstTimestamp"] = ""
		stats["lastTimestamp"] = ""
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
