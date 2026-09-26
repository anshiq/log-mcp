// EventService: WatchEvents (stream with since cursor) + ListEvents
// (paged history from DB) + MCP-compat Subscribe/Drain/Unsubscribe.
// SessionService: Register, Heartbeat, ListSessions, GetSession, Close.
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// --- EventService ---

func (s *Server) routeEvent(mux *http.ServeMux) {
	p := "/agentruntime.v1.EventService/"
	mux.HandleFunc(p+"WatchEvents", s.eventWatch)
	mux.HandleFunc(p+"ListEvents", s.wrap(s.eventList))
	mux.HandleFunc(p+"Subscribe", s.wrap(s.eventSubscribe))
	mux.HandleFunc(p+"Drain", s.wrap(s.eventDrain))
	mux.HandleFunc(p+"Unsubscribe", s.wrap(s.eventUnsubscribe))
}

func (s *Server) eventList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ProcessID   string `json:"processId"`
		Since       string `json:"since"`
		Limit       int    `json:"limit"`
	}
	_ = decode(r, &req)
	var since int64
	if req.Since != "" {
		since, _ = strconv.ParseInt(req.Since, 10, 64)
	}
	rows, err := s.engine.Store().ListEvents(req.WorkspaceID, req.ProcessID, since, req.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, e := range rows {
		out = append(out, map[string]any{
			"id": e.ID, "ts": e.TS, "type": e.Type,
			"projectId": e.ProjectID, "workspaceId": e.WorkspaceID,
			"processId": e.ProcessID, "instanceId": e.InstanceID,
			"sessionId": e.SessionID,
		})
	}
	return map[string]any{"events": out}, nil
}

func (s *Server) eventWatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		ProcessIDs  []string `json:"processIds"`
		Types       []string `json:"types"`
		Since       string   `json:"since"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	// History first (DB), then live from the manager buses.
	var since int64
	if req.Since != "" {
		since, _ = strconv.ParseInt(req.Since, 10, 64)
	}
	hist, _ := s.engine.Store().ListEvents(req.WorkspaceID, "", since, 200)
	for _, e := range hist {
		if !eventAllowed(e.Type, e.ProcessID, req.Types, req.ProcessIDs) {
			continue
		}
		_ = sw.send(map[string]any{"kind": "event", "id": e.ID, "type": e.Type,
			"processId": e.ProcessID, "cursor": fmt.Sprint(e.ID)})
		if e.ID > since {
			since = e.ID
		}
	}
	// Live: subscribe to relevant runtimes.
	done := r.Context().Done()
	type sub struct {
		ch  <-chan busEvent
		off func()
	}
	var subs []sub
	add := func(pr *corePR) bool {
		if req.WorkspaceID != "" && pr.WorkspaceID() != req.WorkspaceID {
			return true
		}
		if bus := pr.ManagerBus(); bus != nil {
			ch, off := bus.Subscribe()
			subs = append(subs, sub{ch: ch, off: off})
		} else if rt, err := pr.Runtime(); err == nil {
			ch, off := rt.Manager().Events().Subscribe()
			subs = append(subs, sub{ch: ch, off: off})
		}
		return true
	}
	if req.WorkspaceID != "" {
		if pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID); err == nil {
			add(pr)
		}
	} else {
		s.engine.RangeRuntimes(add)
	}
	defer func() {
		for _, sb := range subs {
			sb.off()
		}
	}()
	queue := make(chan map[string]any, 1024)
	dropped := 0
	for _, sb := range subs {
		go func(sb sub) {
			for ev := range sb.ch {
				if !eventAllowed(string(ev.Type), ev.ProcessID, req.Types, req.ProcessIDs) {
					continue
				}
				select {
				case queue <- map[string]any{"kind": "event", "type": string(ev.Type),
					"processId": ev.ProcessID, "instanceId": ev.InstanceID,
					"cursor": time.Now().UnixNano()}:
				default:
					dropped++
				}
			}
		}(sb)
	}
	for {
		select {
		case <-done:
			return
		case msg := <-queue:
			if dropped > 0 {
				_ = sw.send(gapMessage(dropped, "slow consumer"))
				dropped = 0
			}
			if err := sw.send(msg); err != nil {
				return
			}
		}
	}
}

func eventAllowed(typ, proc string, types, procs []string) bool {
	if len(types) > 0 {
		ok := false
		for _, t := range types {
			if t == typ {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(procs) > 0 {
		for _, p := range procs {
			if p == proc {
				return true
			}
		}
		return false
	}
	return true
}

// MCP-compat pull model over the daemon bus: subscriptions are keyed by
// the MCP session id so agents keep their pull semantics (§5.6).
func (s *Server) eventSubscribe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string   `json:"sessionId"`
		Types     []string `json:"types"`
		ProcessID string   `json:"processId"`
	}
	_ = decode(r, &req)
	id := s.engine.MCPSubscriptions().Subscribe(req.SessionID, req.Types, req.ProcessID)
	return map[string]any{"subscriptionId": id}, nil
}

func (s *Server) eventDrain(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SubscriptionID string `json:"subscriptionId"`
		Limit          int    `json:"limit"`
	}
	_ = decode(r, &req)
	evs, dropped := s.engine.MCPSubscriptions().Drain(req.SubscriptionID, req.Limit)
	return map[string]any{"events": evs, "dropped": dropped}, nil
}

func (s *Server) eventUnsubscribe(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	_ = decode(r, &req)
	s.engine.MCPSubscriptions().Unsubscribe(req.SubscriptionID)
	return map[string]any{"unsubscribed": true}, nil
}

// --- SessionService ---

func (s *Server) routeSession(mux *http.ServeMux) {
	p := "/agentruntime.v1.SessionService/"
	mux.HandleFunc(p+"Register", s.wrap(s.sessRegister))
	mux.HandleFunc(p+"Heartbeat", s.sessHeartbeat)
	mux.HandleFunc(p+"Ping", s.wrap(s.sessPing))
	mux.HandleFunc(p+"ListSessions", s.wrap(s.sessList))
	mux.HandleFunc(p+"GetSession", s.wrap(s.sessGet))
	mux.HandleFunc(p+"Close", s.wrap(s.sessClose))
}

// sessPing is the unary heartbeat used by the MCP bridge (cheap,
// no stream). Missing/closed sessions get a clear error so the bridge
// re-registers.
func (s *Server) sessPing(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	if err := s.engine.Sessions().Heartbeat(req.SessionID); err != nil {
		return nil, err
	}
	_ = s.engine.Store().HeartbeatSession(req.SessionID)
	return map[string]any{"ok": true}, nil
}

func (s *Server) sessRegister(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Kind           string `json:"kind"`
		Harness        string `json:"harness"`
		HarnessVersion string `json:"harnessVersion"`
		ClientPID      int    `json:"clientPid"`
		WorkspaceID    string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	if req.Kind == "" {
		req.Kind = "mcp"
	}
	sess, err := s.engine.Sessions().Register(
		sessionKind(req.Kind), sessionHarness(req.Harness), req.ClientPID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	_ = s.engine.Store().CreateSession(sess.ID, string(sess.Kind), string(sess.Harness),
		int64(sess.ClientPID), sess.WorkspaceID)
	return map[string]any{"sessionId": sess.ID}, nil
}

// sessHeartbeat holds the stream open for the session's lifetime: the
// client sends periodic lines, the server bumps last_seen. When the
// stream drops the session is marked closed (lease grace covers agent
// restarts before session-leased processes stop).
func (s *Server) sessHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	_ = s.engine.Sessions().Heartbeat(req.SessionID)
	_ = s.engine.Store().HeartbeatSession(req.SessionID)
	for {
		select {
		case <-r.Context().Done():
			// Grace timer for lifetime:session processes is enforced by
			// the reap loop; mark closed now.
			_ = s.engine.Sessions().Close(req.SessionID)
			_ = s.engine.Store().CloseSession(req.SessionID)
			return
		case <-t.C:
			_ = s.engine.Sessions().Heartbeat(req.SessionID)
			_ = s.engine.Store().HeartbeatSession(req.SessionID)
			_ = sw.send(map[string]any{"ok": true})
		}
	}
}

func (s *Server) sessList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
	}
	_ = decode(r, &req)
	var out []any
	for _, se := range s.engine.Sessions().List() {
		if req.WorkspaceID != "" && se.WorkspaceID != req.WorkspaceID {
			continue
		}
		out = append(out, sessionJSON(se))
	}
	if out == nil {
		out = []any{}
	}
	return map[string]any{"sessions": out}, nil
}

func (s *Server) sessGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	se, ok := s.engine.Sessions().Get(req.SessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %q", req.SessionID)
	}
	return map[string]any{"session": sessionJSON(se)}, nil
}

func (s *Server) sessClose(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		SessionID string `json:"sessionId"`
	}
	_ = decode(r, &req)
	if err := s.engine.Sessions().Close(req.SessionID); err != nil {
		return nil, err
	}
	_ = s.engine.Store().CloseSession(req.SessionID)
	return map[string]any{"closed": true}, nil
}
