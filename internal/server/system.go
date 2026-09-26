// SystemService: GetVersion, Health, GetStats, GetSettings,
// UpdateSettings, Shutdown{keep_processes}.
package server

import (
	"net/http"
	"time"
)

func (s *Server) routeSystem(mux *http.ServeMux) {
	p := "/agentruntime.v1.SystemService/"
	mux.HandleFunc(p+"GetVersion", s.wrap(func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{
			"daemonVersion": s.version, "apiVersion": "v1",
			"minClientVersion": "v1", "shimProtocol": 1,
		}, nil
	}))
	mux.HandleFunc(p+"Health", s.wrap(func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{
			"healthy": true, "status": "ready",
			"uptimeMs": time.Since(s.started).Milliseconds(),
		}, nil
	}))
	mux.HandleFunc(p+"GetStats", s.wrap(s.sysStats))
	mux.HandleFunc(p+"GetSettings", s.wrap(s.sysGetSettings))
	mux.HandleFunc(p+"UpdateSettings", s.wrap(s.sysUpdateSettings))
	mux.HandleFunc(p+"Shutdown", s.wrap(s.sysShutdown))
}

func (s *Server) sysStats(w http.ResponseWriter, r *http.Request) (any, error) {
	projects := 0
	if ps, err := s.engine.Store().ListProjects(); err == nil {
		projects = len(ps)
	}
	processes := 0
	for _, pr := range s.engine.LoadedRuntimes() {
		if rt, err := pr.Runtime(); err == nil {
			if l, err := rt.List(); err == nil {
				processes += len(l.Processes)
			}
		}
	}
	return map[string]any{
		"projects": projects, "processes": processes,
		"sessions": s.engine.Sessions().Count(),
	}, nil
}

func (s *Server) sysGetSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	out := map[string]any{}
	for _, k := range []string{"log_level", "idle_exit", "shim_linger", "retention"} {
		if v, ok, _ := s.engine.Store().GetSetting(k); ok {
			out[k] = v
		}
	}
	return map[string]any{"settings": out}, nil
}

func (s *Server) sysUpdateSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		LogLevel *string `json:"logLevel"`
		IdleExit *string `json:"idleExit"`
	}
	_ = decode(r, &req)
	if req.LogLevel != nil {
		if err := s.engine.Store().SetSetting("log_level", `"`+*req.LogLevel+`"`); err != nil {
			return nil, err
		}
	}
	if req.IdleExit != nil {
		if err := s.engine.Store().SetSetting("idle_exit", `"`+*req.IdleExit+`"`); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true}, nil
}

func (s *Server) sysShutdown(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		KeepProcesses bool `json:"keepProcesses"`
	}
	_ = decode(r, &req)
	keep := req.KeepProcesses
	// Default is keep-processes (upgrade/restart path).
	if r.Body == nil {
		keep = true
	}
	if s.onShutdown != nil {
		go s.onShutdown(keep)
	}
	return map[string]any{"draining": true, "keepProcesses": keep}, nil
}
