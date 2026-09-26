// AuditService (ListAudit) and IntegrationService (harness descriptors,
// PreviewInstall, InstallMCP/RemoveMCP, skills, CheckUpdates).
package server

import (
	"fmt"
	"net/http"
	"time"

	"agent-runtime/internal/integrate"
)

func (s *Server) routeAudit(mux *http.ServeMux) {
	p := "/agentruntime.v1.AuditService/"
	mux.HandleFunc(p+"ListAudit", s.wrap(s.auditList))
}

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		Limit       int    `json:"limit"`
	}
	_ = decode(r, &req)
	rows, err := s.engine.Store().ListAudit(req.WorkspaceID, req.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, map[string]any{
			"id": a.ID, "ts": a.TS, "sessionId": a.SessionID, "harness": a.Harness,
			"projectId": a.ProjectID, "workspaceId": a.WorkspaceID,
			"action": a.Action, "result": a.Result, "durationMs": a.DurationMs,
		})
	}
	return map[string]any{"entries": out}, nil
}

func (s *Server) routeIntegration(mux *http.ServeMux) {
	p := "/agentruntime.v1.IntegrationService/"
	mux.HandleFunc(p+"ListHarnesses", s.wrap(s.intHarnesses))
	mux.HandleFunc(p+"PreviewInstall", s.wrap(s.intPreview))
	mux.HandleFunc(p+"InstallMCP", s.wrap(s.intInstallMCP))
	mux.HandleFunc(p+"RemoveMCP", s.wrap(s.intRemoveMCP))
	mux.HandleFunc(p+"ListSkills", s.wrap(s.intSkills))
	mux.HandleFunc(p+"InstallSkills", s.wrap(s.intInstallSkills))
	mux.HandleFunc(p+"RemoveSkills", s.wrap(s.intRemoveSkills))
	mux.HandleFunc(p+"CheckUpdates", s.wrap(s.intCheckUpdates))
}

func (s *Server) intHarnesses(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	_ = decode(r, &req)
	descs := integrate.Descriptors()
	out := make([]any, 0, len(descs))
	for _, d := range descs {
		global := integrate.DetectGlobal(d)
		out = append(out, map[string]any{
			"id": d.ID, "displayName": d.DisplayName,
			"detected": global.Detected, "version": global.Version,
			"mcpGlobal": global.MCPConfigured, "skillsGlobal": global.SkillsInstalled,
		})
	}
	return map[string]any{"harnesses": out}, nil
}

func (s *Server) intPreview(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Harness string `json:"harness"`
		Scope   string `json:"scope"`
		Kind    string `json:"kind"`
	}
	_ = decode(r, &req)
	diff, err := integrate.Preview(req.Harness, req.Scope, req.Kind)
	if err != nil {
		return nil, err
	}
	return map[string]any{"diff": diff}, nil
}

func (s *Server) intInstallMCP(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Harness string `json:"harness"`
		Scope   string `json:"scope"`
	}
	_ = decode(r, &req)
	sess, _ := s.sessionOf(r)
	res, err := integrate.InstallMCP(req.Harness, req.Scope)
	if err != nil {
		return nil, err
	}
	_ = s.engine.Store().AppendAudit(sess, "", "", "", "InstallMCP", req.Harness+"/"+req.Scope, "ok", 0)
	return map[string]any{"path": res.Path, "backup": res.Backup, "changed": res.Changed}, nil
}

func (s *Server) intRemoveMCP(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Harness string `json:"harness"`
		Scope   string `json:"scope"`
	}
	_ = decode(r, &req)
	res, err := integrate.RemoveMCP(req.Harness, req.Scope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": res.Path, "changed": res.Changed}, nil
}

func (s *Server) intSkills(w http.ResponseWriter, r *http.Request) (any, error) {
	skills := integrate.EmbeddedSkills()
	out := make([]any, 0, len(skills))
	for _, sk := range skills {
		out = append(out, map[string]any{
			"name": sk.Name, "version": sk.Version, "description": sk.Description,
		})
	}
	return map[string]any{"skills": out}, nil
}

func (s *Server) intInstallSkills(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Harness string `json:"harness"`
		Scope   string `json:"scope"`
	}
	_ = decode(r, &req)
	res, err := integrate.InstallSkills(req.Harness, req.Scope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"installed": res.Installed, "path": res.Path}, nil
}

func (s *Server) intRemoveSkills(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Harness string `json:"harness"`
		Scope   string `json:"scope"`
	}
	_ = decode(r, &req)
	if err := integrate.RemoveSkills(req.Harness, req.Scope); err != nil {
		return nil, err
	}
	return map[string]any{"removed": true}, nil
}

func (s *Server) intCheckUpdates(w http.ResponseWriter, r *http.Request) (any, error) {
	updates := integrate.CheckSkillUpdates()
	return map[string]any{"updates": updates, "at": time.Now().Unix(),
		"embedded": fmt.Sprintf("%d skills", len(integrate.EmbeddedSkills()))}, nil
}
