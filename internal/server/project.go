// ProjectService: Resolve, ListProjects, GetProject, UpdateProject,
// ListWorkspaces, LinkWorkspace, ForgetProject, GC.
package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"agent-runtime/internal/project"
)

func (s *Server) routeProject(mux *http.ServeMux) {
	p := "/agentruntime.v1.ProjectService/"
	mux.HandleFunc(p+"Resolve", s.wrap(s.projResolve))
	mux.HandleFunc(p+"ListProjects", s.wrap(s.projList))
	mux.HandleFunc(p+"GetProject", s.wrap(s.projGet))
	mux.HandleFunc(p+"UpdateProject", s.wrap(s.projUpdate))
	mux.HandleFunc(p+"ListWorkspaces", s.wrap(s.projWorkspaces))
	mux.HandleFunc(p+"LinkWorkspace", s.wrap(s.projLink))
	mux.HandleFunc(p+"ForgetProject", s.wrap(s.projForget))
	mux.HandleFunc(p+"GC", s.wrap(s.projGC))
}

func (s *Server) projResolve(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Path string `json:"path"`
	}
	_ = decode(r, &req)
	if req.Path == "" {
		return nil, fmt.Errorf("path required")
	}
	root := project.WorkspaceRoot(req.Path)
	pid, wid, err := s.engine.ResolveWorkspace(root)
	if err != nil {
		return nil, err
	}
	p, _ := s.engine.Store().GetProject(string(pid))
	name := ""
	if p != nil {
		name = p.Name
	}
	return map[string]any{
		"projectId": string(pid), "workspaceId": string(wid),
		"projectName": name, "newlyCreated": false,
	}, nil
}

func (s *Server) projList(w http.ResponseWriter, r *http.Request) (any, error) {
	ps, err := s.engine.Store().ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(ps))
	for _, p := range ps {
		wss, _ := s.engine.Store().ListWorkspaces(p.ID)
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "configMode": p.ConfigMode,
			"lastUsedAt": p.LastUsedAt, "workspaceCount": len(wss),
		})
	}
	return map[string]any{"projects": out}, nil
}

func (s *Server) projGet(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	_ = decode(r, &req)
	p, err := s.engine.Store().GetProject(req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %q", req.ProjectID)
	}
	wss, _ := s.engine.Store().ListWorkspaces(p.ID)
	var wso []any
	for _, x := range wss {
		wso = append(wso, workspaceJSON(x))
	}
	return map[string]any{"project": map[string]any{
		"id": p.ID, "name": p.Name, "configMode": p.ConfigMode,
		"lastUsedAt": p.LastUsedAt, "workspaces": wso,
	}}, nil
}

func (s *Server) projUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
	}
	_ = decode(r, &req)
	p, err := s.engine.Store().GetProject(req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %q", req.ProjectID)
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	if err := s.engine.Store().UpdateProject(p); err != nil {
		return nil, err
	}
	return map[string]any{"project": map[string]any{"id": p.ID, "name": p.Name}}, nil
}

func (s *Server) projWorkspaces(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	_ = decode(r, &req)
	wss, err := s.engine.Store().ListWorkspaces(req.ProjectID)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(wss))
	for _, x := range wss {
		out = append(out, workspaceJSON(x))
	}
	return map[string]any{"workspaces": out}, nil
}

func (s *Server) projLink(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
		Path      string `json:"path"`
	}
	_ = decode(r, &req)
	if req.ProjectID == "" || req.Path == "" {
		return nil, fmt.Errorf("projectId and path required")
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		return nil, err
	}
	ws, err := s.engine.Store().LinkWorkspace(req.ProjectID, abs)
	if err != nil {
		return nil, err
	}
	return map[string]any{"workspace": workspaceJSON(ws)}, nil
}

func (s *Server) projForget(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	_ = decode(r, &req)
	if err := s.engine.Store().ForgetProject(req.ProjectID); err != nil {
		return nil, err
	}
	return map[string]any{"forgotten": req.ProjectID}, nil
}

func (s *Server) projGC(w http.ResponseWriter, r *http.Request) (any, error) {
	// Scan all workspaces; flag vanished paths as missing.
	var missing []string
	for _, p := range mustListProjects(s) {
		for _, ws := range mustListWorkspaces(s, p.ID) {
			if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
				_ = s.engine.Store().MarkWorkspaceMissing(ws.ID)
				missing = append(missing, ws.ID)
			}
		}
	}
	if missing == nil {
		missing = []string{}
	}
	return map[string]any{"removedWorkspaces": missing}, nil
}

func mustListProjects(s *Server) []storeProject {
	ps, _ := s.engine.Store().ListProjects()
	out := make([]storeProject, 0, len(ps))
	for _, p := range ps {
		out = append(out, storeProject{ID: p.ID})
	}
	return out
}

func mustListWorkspaces(s *Server, pid string) []storeWorkspace {
	wss, _ := s.engine.Store().ListWorkspaces(pid)
	out := make([]storeWorkspace, 0, len(wss))
	for _, w := range wss {
		out = append(out, storeWorkspace{ID: w.ID, Path: w.Path})
	}
	return out
}
