// ConfigService: GetConfig (raw + resolved + provenance), GetSchema,
// Validate, Plan, Apply (optimistic concurrency), ListRevisions,
// GetRevision, Rollback, WatchConfig (stream).
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"agent-runtime/internal/config"
)

func (s *Server) routeConfig(mux *http.ServeMux) {
	p := "/agentruntime.v1.ConfigService/"
	mux.HandleFunc(p+"GetConfig", s.wrap(s.cfgGet))
	mux.HandleFunc(p+"GetSchema", s.wrap(s.cfgSchema))
	mux.HandleFunc(p+"Validate", s.wrap(s.cfgValidate))
	mux.HandleFunc(p+"Plan", s.wrap(s.cfgPlan))
	mux.HandleFunc(p+"Apply", s.wrap(s.cfgApply))
	mux.HandleFunc(p+"ListRevisions", s.wrap(s.cfgRevisions))
	mux.HandleFunc(p+"GetRevision", s.wrap(s.cfgRevision))
	mux.HandleFunc(p+"Rollback", s.wrap(s.cfgRollback))
	mux.HandleFunc(p+"WatchConfig", s.cfgWatch)
}

func (s *Server) cfgGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ProjectID   string `json:"projectId"`
		Layer       string `json:"layer"`
	}
	_ = decode(r, &req)
	pr, wsID, err := s.resolveConfigScope(req.WorkspaceID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	resolved := pr.ResolvedConfig()
	if resolved == nil {
		if _, _, err := s.engine.RuntimeFor(wsID); err != nil {
			return nil, err
		}
		resolved = pr.ResolvedConfig()
	}
	apps := map[string]any{}
	prov := map[string]any{}
	if resolved != nil {
		for name, app := range resolved.Apps {
			apps[name] = app
		}
		for k, v := range resolved.Provenance {
			prov[k] = string(v)
		}
	}
	// Raw layers for the editor.
	raw := map[string]any{}
	ep := config.DiscoverEffective(s.engine.DataDir(), pr.ProjectID(), wsID, pr.WorkspacePath())
	if ep.Repo != "" {
		if data, err := os.ReadFile(ep.Repo); err == nil {
			raw["repo"] = string(data)
		}
	}
	if data, err := os.ReadFile(ep.Project); err == nil {
		raw["project"] = string(data)
	}
	if ep.Overlay != "" {
		if data, err := os.ReadFile(ep.Overlay); err == nil {
			raw["workspace"] = string(data)
		}
	}
	var latestRevision int64
	if revs, err := s.engine.Store().ListRevisions(pr.ProjectID(), "project", 1); err == nil && len(revs) > 0 {
		latestRevision = revs[0].ID
	}
	return map[string]any{
		"projectId": pr.ProjectID(), "workspaceId": wsID,
		"apps": apps, "provenance": prov, "raw": raw,
		"configPath": ep.Project, "overlayPath": ep.Overlay,
		"repoPath": ep.Repo, "warnings": resolvedWarnings(resolved),
		"revision": latestRevision,
	}, nil
}

func resolvedWarnings(resolved *config.Resolved) []string {
	if resolved == nil {
		return nil
	}
	return resolved.Warnings
}

func (s *Server) resolveConfigScope(workspaceID, projectID string) (*corePR, string, error) {
	if workspaceID != "" {
		pr, err := s.engine.GetOrCreateRuntime(workspaceID)
		if err != nil {
			return nil, "", err
		}
		return pr, workspaceID, nil
	}
	if projectID == "" {
		return nil, "", fmt.Errorf("workspaceId or projectId required")
	}
	wss, err := s.engine.Store().ListWorkspaces(projectID)
	if err != nil || len(wss) == 0 {
		return nil, "", fmt.Errorf("project has no workspaces: %q", projectID)
	}
	pr, err := s.engine.GetOrCreateRuntime(wss[0].ID)
	if err != nil {
		return nil, "", err
	}
	return pr, wss[0].ID, nil
}

func (s *Server) cfgSchema(w http.ResponseWriter, r *http.Request) (any, error) {
	var schema any
	if err := json.Unmarshal(config.V3JSONSchemaRaw(), &schema); err != nil {
		return nil, fmt.Errorf("embedded schema is invalid JSON: %w", err)
	}
	return map[string]any{"schema": schema}, nil
}

func (s *Server) cfgValidate(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		YAML string `json:"yaml"`
	}
	_ = decode(r, &req)
	errs := config.Validate([]byte(req.YAML))
	out := make([]any, 0, len(errs))
	for _, e := range errs {
		out = append(out, map[string]any{
			"line": e.Line, "column": e.Column, "path": e.Path, "message": e.Message,
		})
	}
	return map[string]any{"valid": len(errs) == 0, "errors": out,
		"warnings": config.DeprecationWarnings([]byte(req.YAML))}, nil
}

func (s *Server) cfgPlan(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		YAML        string `json:"yaml"`
	}
	_ = decode(r, &req)
	pr, wsID, err := s.resolveConfigScope(req.WorkspaceID, "")
	if err != nil {
		return nil, err
	}
	oldApps := map[string]config.V3App{}
	if res := pr.ResolvedConfig(); res != nil {
		oldApps = res.Apps
	}
	newCfg, errs := config.ParseV3([]byte(req.YAML))
	if errs != nil {
		return map[string]any{"errors": validationJSON(errs)}, nil
	}
	// Running processes per app for affected_process_ids.
	running := map[string][]string{}
	if _, rt, err := s.engine.RuntimeFor(wsID); err == nil {
		if l, err := rt.List(); err == nil {
			for _, p := range l.Processes {
				// Match by command prefix is best-effort; exact app
				// attribution lands with store process rows.
				_ = p
			}
		}
		if rows, err := s.engine.Store().ListProcesses(wsID); err == nil {
			for _, row := range rows {
				if row.App != "" {
					running[row.App] = append(running[row.App], row.ID)
				}
			}
		}
	}
	changes := config.Plan(oldApps, newCfg.Apps, running)
	return map[string]any{"changes": changes}, nil
}

func validationJSON(errs []*config.ValidationError) []any {
	out := make([]any, 0, len(errs))
	for _, e := range errs {
		out = append(out, map[string]any{
			"line": e.Line, "column": e.Column, "path": e.Path, "message": e.Message,
		})
	}
	return out
}

func (s *Server) cfgApply(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID       string `json:"projectId"`
		Layer           string `json:"layer"`
		YAML            string `json:"yaml"`
		BaseRevision    int64  `json:"baseRevision"`
		Message         string `json:"message"`
		RestartAffected bool   `json:"restartAffected"`
		SessionID       string `json:"sessionId"`
	}
	_ = decode(r, &req)
	if req.ProjectID == "" || req.YAML == "" {
		return nil, fmt.Errorf("projectId and yaml required")
	}
	if errs := config.Validate([]byte(req.YAML)); len(errs) > 0 {
		return map[string]any{"applied": false, "errors": validationJSON(errs)}, nil
	}
	layer := req.Layer
	if layer == "" || layer == "project" {
		layer = "project"
	}
	// Optimistic concurrency: base_revision must match the latest known
	// revision for this layer.
	revs, _ := s.engine.Store().ListRevisions(req.ProjectID, layer, 1)
	if len(revs) > 0 && req.BaseRevision != 0 && revs[0].ID != req.BaseRevision {
		return nil, fmt.Errorf("stale_revision: base %d, latest %d", req.BaseRevision, revs[0].ID)
	}
	// Resolve target file.
	var target string
	if layer == "project" {
		target = filepath.Join(s.engine.DataDir(), "projects", req.ProjectID, "agent-runtime.yaml")
	} else {
		return nil, fmt.Errorf("unknown layer %q (want project|workspace:<id>)", req.Layer)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(req.YAML), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, target); err != nil {
		return nil, err
	}
	sess, _ := s.sessionOf(r)
	if req.SessionID != "" {
		sess = req.SessionID
	}
	rev, err := s.engine.Store().AddRevision(req.ProjectID, layer, req.YAML,
		config.SHA256([]byte(req.YAML)), true, "", "api", sess, req.Message)
	if err != nil {
		return nil, err
	}
	_ = s.engine.Store().SetActiveRevision(req.ProjectID, rev)
	// Synchronously reload every loaded workspace of this project so the
	// next Start sees the new revision without waiting for the ~300ms
	// file-watcher debounce (the watcher still reconciles stale flags).
	for _, ws := range mustWorkspacesByProject(s, req.ProjectID) {
		if pr, err := s.engine.GetOrCreateRuntime(ws); err == nil {
			trusted := true
			if res, errs := config.Resolve(s.engine.DataDir(), req.ProjectID, ws, pr.WorkspacePath(), trusted, nil); errs == nil {
				if rt, err := pr.Runtime(); err == nil {
					rt.ReloadConfig(res.ToLoaded(pr.WorkspacePath()))
				}
			}
		}
	}
	// The file watcher reconciles (stale marking / reload: restart) within
	// ~300ms; reload: restart with restartAffected applies immediately via
	// the planner flags in the Plan response the GUI already showed.
	return map[string]any{"applied": true, "revision": rev}, nil
}

func (s *Server) cfgRevisions(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
		Layer     string `json:"layer"`
		Limit     int    `json:"limit"`
	}
	_ = decode(r, &req)
	revs, err := s.engine.Store().ListRevisions(req.ProjectID, req.Layer, req.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(revs))
	for _, rev := range revs {
		out = append(out, revisionJSON(rev))
	}
	return map[string]any{"revisions": out}, nil
}

func (s *Server) cfgRevision(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Revision int64 `json:"revision"`
	}
	_ = decode(r, &req)
	rev, err := s.engine.Store().GetRevision(req.Revision)
	if err != nil {
		return nil, fmt.Errorf("revision not found: %d", req.Revision)
	}
	return map[string]any{"revision": revisionJSON(rev), "content": rev.Content}, nil
}

func (s *Server) cfgRollback(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
		Revision  int64  `json:"revision"`
		Message   string `json:"message"`
	}
	_ = decode(r, &req)
	rev, err := s.engine.Store().GetRevision(req.Revision)
	if err != nil {
		return nil, fmt.Errorf("revision not found: %d", req.Revision)
	}
	if rev.ProjectID != req.ProjectID {
		return nil, fmt.Errorf("revision %d belongs to another project", req.Revision)
	}
	target := filepath.Join(s.engine.DataDir(), "projects", req.ProjectID, "agent-runtime.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, []byte(rev.Content), 0o600); err != nil {
		return nil, err
	}
	sess, _ := s.sessionOf(r)
	newRev, err := s.engine.Store().AddRevision(req.ProjectID, rev.Layer, rev.Content,
		rev.SHA256, true, "", "rollback", sess, req.Message)
	if err != nil {
		return nil, err
	}
	_ = s.engine.Store().SetActiveRevision(req.ProjectID, newRev)
	return map[string]any{"applied": true, "revision": newRev}, nil
}

func (s *Server) cfgWatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"projectId"`
		Since     int64  `json:"since"`
	}
	_ = decode(r, &req)
	sw, ok := newStream(w, r, heartbeatMessage)
	if !ok {
		return
	}
	defer sw.Close()
	last := req.Since
	t := time.NewTicker(time.Second)
	defer t.Stop()
	_ = sw.send(map[string]any{"kind": "snapshot", "cursor": last})
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			revs, _ := s.engine.Store().ListRevisions(req.ProjectID, "", 10)
			for i := len(revs) - 1; i >= 0; i-- {
				if revs[i].ID > last {
					last = revs[i].ID
					_ = sw.send(map[string]any{"kind": "revision",
						"revision": revisionJSON(revs[i]), "cursor": last})
				}
			}
		}
	}
}

func revisionJSON(rev *storeRevision) map[string]any {
	return map[string]any{
		"id": rev.ID, "layer": rev.Layer, "sha256": rev.SHA256,
		"valid": rev.Valid, "source": rev.Source, "sessionId": rev.SessionID,
		"message": rev.Message, "createdAt": rev.CreatedAt,
	}
}

func mustWorkspacesByProject(s *Server, projectID string) []string {
	wss, _ := s.engine.Store().ListWorkspaces(projectID)
	var out []string
	for _, w := range wss {
		out = append(out, w.ID)
	}
	return out
}
