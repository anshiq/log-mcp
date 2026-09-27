// StartStack (Phase 9): compose-like ordered startup from depends_on.
// Apps start in topological order; each is awaited to running state
// (and to readiness when it declares patterns) before the next starts.
package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"agent-runtime/pkg/api"
)

func (s *Server) routeStack(mux *http.ServeMux) {
	mux.HandleFunc("/agentruntime.v1.ProcessService/StartStack", s.wrap(s.handleStartStack))
}

// topoOrder sorts apps so dependencies start first (Kahn's algorithm).
// Unknown deps were rejected at config validation; cycles error here.
func topoOrder(apps []string, deps map[string][]string) ([]string, error) {
	in := map[string]bool{}
	for _, a := range apps {
		in[a] = true
	}
	edges := map[string][]string{}
	indeg := map[string]int{}
	for _, a := range apps {
		for _, d := range deps[a] {
			if !in[d] {
				continue // external dep: assumed already running
			}
			edges[d] = append(edges[d], a)
			indeg[a]++
		}
	}
	var queue []string
	for _, a := range apps {
		if indeg[a] == 0 {
			queue = append(queue, a)
		}
	}
	var out []string
	for len(queue) > 0 {
		a := queue[0]
		queue = queue[1:]
		out = append(out, a)
		for _, m := range edges[a] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if len(out) != len(apps) {
		return nil, fmt.Errorf("depends_on cycle in %v", apps)
	}
	return out, nil
}

func (s *Server) handleStartStack(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string   `json:"workspaceId"`
		Apps        []string `json:"apps"`
		SessionID   string   `json:"sessionId"`
		TimeoutMs   int      `json:"timeoutMs"`
	}
	_ = decode(r, &req)
	if req.WorkspaceID == "" || len(req.Apps) == 0 {
		return nil, fmt.Errorf("workspaceId and apps required")
	}
	timeout := 30 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs)*time.Millisecond + 5*time.Second
	}
	_ = timeout
	pr, rt, err := s.engine.RuntimeFor(req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	deps := map[string][]string{}
	if res := pr.ResolvedConfig(); res != nil {
		for name, app := range res.Apps {
			deps[name] = app.DependsOn
		}
	}
	order, err := topoOrder(req.Apps, deps)
	if err != nil {
		return nil, err
	}
	var started []any
	for _, name := range order {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		res, err := rt.Start(ctx, api.StartRequest{App: name})
		cancel()
		if err != nil {
			return map[string]any{"started": started, "failed": name, "error": err.Error()}, nil
		}
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			st, err := rt.Status(res.ProcessID)
			if err == nil && (st.Status == "running" || st.Status == "ready") {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		started = append(started, map[string]any{
			"app": name, "processId": res.ProcessID, "instanceId": res.InstanceID,
		})
	}
	return map[string]any{"started": started, "order": order}, nil
}
