package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"agent-runtime/internal/config"
	"agent-runtime/internal/core"
	"agent-runtime/internal/detect"
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
	mux.HandleFunc(p+"ResolveProposal", s.wrap(s.cfgResolveProposal))
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
	core.EnsureConfig(pr)
	resolved := pr.ResolvedConfig()
	if resolved == nil {
		if _, _, err := s.engine.RuntimeFor(wsID); err != nil {
			return nil, err
		}
		resolved = pr.ResolvedConfig()
	}
	apps := map[string]any{}
	prov := map[string]any{}
	autoApps := []string{}
	if resolved != nil {
		for name, app := range resolved.Apps {
			apps[name] = app
		}
		for k, v := range resolved.Provenance {
			prov[k] = string(v)
		}
		autoApps = s.autoOwned(pr.ProjectID(), resolved.Apps)
	}
	raw := map[string]any{}
	projData, projRev, _ := s.engine.Store().GetConfig(pr.ProjectID(), "project", "")
	if len(projData) > 0 {
		raw["project"] = string(projData)
	}
	wsData, _, _ := s.engine.Store().GetConfig(pr.ProjectID(), "workspace", wsID)
	if len(wsData) > 0 {
		raw["workspace"] = string(wsData)
	}
	var latestRevision int64
	if revs, err := s.engine.Store().ListRevisions(pr.ProjectID(), "project", 1); err == nil && len(revs) > 0 {
		latestRevision = revs[0].ID
	}
	if projRev != 0 {
		latestRevision = projRev
	}
	layers := []any{
		map[string]any{"name": "project", "path": "", "exists": len(projData) > 0, "writable": true},
		map[string]any{"name": "workspace", "path": "", "exists": len(wsData) > 0, "writable": true},
	}
	pending := s.pendingProposal(pr.ProjectID())
	return map[string]any{
		"projectId": pr.ProjectID(), "workspaceId": wsID,
		"apps": apps, "provenance": prov, "autoApps": autoApps, "raw": raw,
		"configSource": "db", "configRevision": latestRevision, "revision": latestRevision, "layers": layers,
		"pendingProposal": pending,
		"warnings":        resolvedWarnings(resolved),
	}, nil
}

func (s *Server) autoOwned(projectID string, apps map[string]config.V3App) []string {
	syncRow, err := s.engine.Store().GetConfigSync(projectID)
	if err != nil || syncRow.AutoAppsJSON == "" {
		return []string{}
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(syncRow.AutoAppsJSON), &m); err != nil {
		return []string{}
	}
	var out []string
	for name, app := range apps {
		h, ok := m[name]
		if !ok {
			continue
		}
		norm := config.V3App{}
		norm.Type = app.Type
		norm.WorkDir = app.WorkDir
		if norm.WorkDir == "." {
			norm.WorkDir = ""
		}
		norm.Command = append([]string(nil), app.Command...)
		norm.Readiness = append([]string(nil), app.Readiness...)
		b, err := yaml.Marshal(norm)
		if err != nil {
			continue
		}
		if config.SHA256(b) == h {
			out = append(out, name)
		}
	}
	return out
}

func (s *Server) pendingProposal(projectID string) any {
	syncRow, err := s.engine.Store().GetConfigSync(projectID)
	if err != nil || syncRow == nil || syncRow.ProposalJSON == "" {
		return nil
	}
	var prop map[string]any
	if err := json.Unmarshal([]byte(syncRow.ProposalJSON), &prop); err != nil {
		return nil
	}
	return prop
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
		WorkspaceID  string `json:"workspaceId"`
		YAML         string `json:"yaml"`
		BaseRevision int64  `json:"baseRevision"`
	}
	_ = decode(r, &req)
	pr, wsID, err := s.resolveConfigScope(req.WorkspaceID, "")
	if err != nil {
		return nil, err
	}
	core.EnsureConfig(pr)
	oldApps := map[string]config.V3App{}
	if res := pr.ResolvedConfig(); res != nil {
		oldApps = res.Apps
	}
	newCfg, errs := config.ParseV3([]byte(req.YAML))
	if errs != nil {
		return map[string]any{"errors": validationJSON(errs)}, nil
	}
	running := map[string][]string{}
	if _, rt, err := s.engine.RuntimeFor(wsID); err == nil {
		if rows, err := s.engine.Store().ListProcesses(wsID); err == nil {
			for _, row := range rows {
				if row.App != "" {
					running[row.App] = append(running[row.App], row.ID)
				}
			}
		}
		_ = rt
	}
	changes := config.Plan(oldApps, newCfg.Apps, running)
	var latestRevision int64
	if revs, err := s.engine.Store().ListRevisions(pr.ProjectID(), "project", 1); err == nil && len(revs) > 0 {
		latestRevision = revs[0].ID
	}
	stale := req.BaseRevision != 0 && latestRevision != 0 && req.BaseRevision != latestRevision
	oldYAML := ""
	if data, _, _ := s.engine.Store().GetConfig(pr.ProjectID(), "project", ""); len(data) > 0 {
		oldYAML = string(data)
	}
	return map[string]any{"changes": changes, "latestRevision": latestRevision, "stale": stale, "diff": unifiedDiff(oldYAML, req.YAML)}, nil
}

func unifiedDiff(old, cur string) string {
	ol := splitLines(old)
	nl := splitLines(cur)
	var b strings.Builder
	max := len(ol)
	if len(nl) > max {
		max = len(nl)
	}
	for i := 0; i < max; i++ {
		var o, n string
		if i < len(ol) {
			o = ol[i]
		}
		if i < len(nl) {
			n = nl[i]
		}
		if o != n {
			if o != "" {
				b.WriteString("- " + o + "\n")
			}
			if n != "" {
				b.WriteString("+ " + n + "\n")
			}
		}
	}
	return b.String()
}

func splitLines(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(v, "\n"), "\n")
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
		WorkspaceID     string `json:"workspaceId"`
		Layer           string `json:"layer"`
		YAML            string `json:"yaml"`
		BaseRevision    int64  `json:"baseRevision"`
		Message         string `json:"message"`
		RestartAffected bool   `json:"restartAffected"`
		SessionID       string `json:"sessionId"`
	}
	_ = decode(r, &req)
	if req.WorkspaceID != "" && req.ProjectID == "" {
		if pr, err := s.engine.GetOrCreateRuntime(req.WorkspaceID); err == nil {
			req.ProjectID = pr.ProjectID()
		}
	}
	if req.ProjectID == "" || req.YAML == "" {
		return nil, fmt.Errorf("projectId and yaml required")
	}
	if errs := config.Validate([]byte(req.YAML)); len(errs) > 0 {
		return map[string]any{"applied": false, "errors": validationJSON(errs)}, nil
	}
	layer := req.Layer
	if layer == "" {
		layer = "project"
	}
	if layer != "project" && layer != "workspace" {
		return nil, fmt.Errorf("unknown layer %q (want project|workspace)", req.Layer)
	}
	if layer == "workspace" && req.WorkspaceID == "" {
		return nil, fmt.Errorf("workspaceId required for layer=workspace")
	}
	revs, _ := s.engine.Store().ListRevisions(req.ProjectID, layer, 1)
	if len(revs) > 0 && req.BaseRevision != 0 && revs[0].ID != req.BaseRevision {
		return nil, fmt.Errorf("%w: base %d, latest %d", ErrStaleRevision, req.BaseRevision, revs[0].ID)
	}
	sess, _ := s.sessionOf(r)
	if req.SessionID != "" {
		sess = req.SessionID
	}
	rev, restarted, err := s.engine.ApplyProjectYAML(req.ProjectID, layer, req.WorkspaceID, req.YAML, "api", sess, req.Message, req.RestartAffected)
	if err != nil {
		return nil, err
	}
	return map[string]any{"applied": true, "revision": rev, "restarted": restarted}, nil
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
		Revision  int64  `json:"revision"`
		ProjectID string `json:"projectId"`
	}
	_ = decode(r, &req)
	rev, err := s.engine.Store().GetRevision(req.Revision)
	if err != nil {
		return nil, fmt.Errorf("revision not found: %d", req.Revision)
	}
	prev := ""
	if rev.ID > 1 {
		if p, err := s.engine.Store().GetRevision(rev.ID - 1); err == nil && p.ProjectID == rev.ProjectID {
			prev = p.Content
		}
	}
	return map[string]any{"revision": revisionJSON(rev), "content": rev.Content, "previousContent": prev}, nil
}

func (s *Server) cfgRollback(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID    string `json:"projectId"`
		WorkspaceID  string `json:"workspaceId"`
		Revision     int64  `json:"revision"`
		BaseRevision int64  `json:"baseRevision"`
		Message      string `json:"message"`
	}
	_ = decode(r, &req)
	rev, err := s.engine.Store().GetRevision(req.Revision)
	if err != nil {
		return nil, fmt.Errorf("revision not found: %d", req.Revision)
	}
	if rev.ProjectID != req.ProjectID {
		return nil, fmt.Errorf("revision %d belongs to another project", req.Revision)
	}
	layer := rev.Layer
	if layer == "" {
		layer = "project"
	}
	wsID := req.WorkspaceID
	if layer == "workspace" && wsID == "" {
		wss, _ := s.engine.Store().ListWorkspaces(req.ProjectID)
		if len(wss) > 0 {
			wsID = wss[0].ID
		} else {
			return nil, fmt.Errorf("workspaceId required for layer=workspace")
		}
	}
	revs, _ := s.engine.Store().ListRevisions(req.ProjectID, layer, 1)
	if len(revs) > 0 && req.BaseRevision != 0 && revs[0].ID != req.BaseRevision {
		return nil, fmt.Errorf("%w: base %d, latest %d", ErrStaleRevision, req.BaseRevision, revs[0].ID)
	}
	sess, _ := s.sessionOf(r)
	newRev, restarted, err := s.engine.ApplyProjectYAML(req.ProjectID, layer, wsID, rev.Content, "rollback", sess, req.Message, false)
	if err != nil {
		return nil, err
	}
	_ = restarted
	return map[string]any{"applied": true, "revision": newRev}, nil
}

func (s *Server) cfgResolveProposal(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID  string `json:"projectId"`
		ProposalID string `json:"proposalId"`
		Action     string `json:"action"`
		Message    string `json:"message"`
	}
	_ = decode(r, &req)
	if req.ProjectID == "" || req.Action == "" {
		return nil, fmt.Errorf("projectId and action required")
	}
	if req.Action != "approve" && req.Action != "dismiss" {
		return nil, fmt.Errorf("unknown action %q (want approve|dismiss)", req.Action)
	}
	syncRow, err := s.engine.Store().GetConfigSync(req.ProjectID)
	if err != nil {
		return nil, err
	}
	if syncRow.ProposalJSON == "" {
		return nil, fmt.Errorf("no pending proposal for project %q", req.ProjectID)
	}
	var prop struct {
		ID   string `json:"id"`
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal([]byte(syncRow.ProposalJSON), &prop); err != nil {
		return nil, err
	}
	if req.ProposalID != "" && prop.ID != req.ProposalID {
		return nil, fmt.Errorf("proposal %q no longer current", req.ProposalID)
	}
	sess, _ := s.sessionOf(r)
	if req.Action == "dismiss" {
		_ = s.engine.Store().ClearProposal(req.ProjectID)
		wss, _ := s.engine.Store().ListWorkspaces(req.ProjectID)
		sig := ""
		if len(wss) > 0 {
			if pr, err := s.engine.GetOrCreateRuntime(wss[0].ID); err == nil {
				sig = detect.Signature(pr.WorkspacePath())
			}
		}
		autoJSON := syncRow.AutoAppsJSON
		if autoJSON == "" {
			autoJSON = "{}"
		}
		_ = s.engine.Store().PutConfigSync(req.ProjectID, sig, autoJSON)
		return map[string]any{"dismissed": true, "proposalId": prop.ID}, nil
	}
	rev, restarted, err := s.engine.ApplyProjectYAML(req.ProjectID, "project", "", prop.YAML, "proposal", sess, req.Message, false)
	if err != nil {
		return nil, err
	}
	_ = s.engine.Store().ClearProposal(req.ProjectID)
	return map[string]any{"applied": true, "revision": rev, "restarted": restarted, "proposalId": prop.ID}, nil
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
	lastValid := map[string]bool{}
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
					if !revs[i].Valid {
						_ = sw.send(map[string]any{"kind": "invalid", "layer": revs[i].Layer, "errors": revs[i].Errors, "cursor": last})
					} else {
						key := revs[i].Layer
						if !lastValid[key] {
							lastValid[key] = true
						} else {
							_ = sw.send(map[string]any{"kind": "changed", "layer": revs[i].Layer, "revision": revs[i].ID, "cursor": last})
						}
					}
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
