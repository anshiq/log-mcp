// SystemService: GetVersion, Health, GetStats, GetSettings,
// UpdateSettings, Shutdown{keep_processes}.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
			"uptimeMs":      time.Since(s.started).Milliseconds(),
			"uptimeSeconds": int64(time.Since(s.started).Seconds()),
			"version":       s.version, "socketPath": s.Addr(),
			"dataDir": s.engine.DataDir(), "tcpAddr": s.tcpAddr,
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
	running := 0
	failed := 0
	for _, pr := range s.engine.LoadedRuntimes() {
		if rt, err := pr.Runtime(); err == nil {
			if l, err := rt.List(); err == nil {
				processes += len(l.Processes)
				for _, p := range l.Processes {
					if st, err := rt.Status(p.ProcessID); err == nil {
						switch st.Status {
						case "running", "ready":
							running++
						case "failed", "crashed":
							failed++
						}
					}
				}
			}
		}
	}
	return map[string]any{
		"projects": projects, "processes": processes,
		"sessions":         s.engine.Sessions().Count(),
		"processesRunning": running, "processesFailed": failed,
		"logDiskBytes":    logDiskBytes(s.engine.DataDir()),
		"indexLagSeconds": 0,
		"uptimeSeconds":   int64(time.Since(s.started).Seconds()),
	}, nil
}

func sysSettingDefs() map[string]any {
	return map[string]any{
		"log_level":   "info",
		"idle_exit":   "never",
		"shim_linger": "30s",
		"retention":   "7d",
	}
}

func sysSettingSchema() map[string]any {
	return map[string]any{
		"log_level":   map[string]any{"type": "string", "enum": []string{"debug", "info", "warn", "error"}, "description": "Daemon log verbosity"},
		"idle_exit":   map[string]any{"type": "string", "description": "Idle shutdown timeout, e.g. never, 30m, 2h"},
		"shim_linger": map[string]any{"type": "string", "description": "How long shims linger after detach, e.g. 30s, 5m"},
		"retention":   map[string]any{"type": "string", "description": "Log retention window, e.g. 7d, 30d"},
	}
}

func (s *Server) sysGetSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	defs := sysSettingDefs()
	out := map[string]any{}
	for k, dv := range defs {
		if v, ok, _ := s.engine.Store().GetSetting(k); ok {
			out[k] = settingJSONValue(v, dv)
		} else {
			out[k] = dv
		}
	}
	return map[string]any{"settings": out, "schema": sysSettingSchema()}, nil
}

func (s *Server) sysUpdateSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var raw map[string]any
	_ = decode(r, &raw)
	known := sysSettingDefs()
	for k := range raw {
		if _, ok := known[toSnakeCase(k)]; !ok {
			if _, ok2 := known[k]; !ok2 {
				return nil, fmt.Errorf("invalid_argument: unknown setting %q", k)
			}
		}
	}
	updates := map[string]string{}
	for k, v := range raw {
		sk := toSnakeCase(k)
		str, ok := v.(string)
		if !ok {
			b, _ := json.Marshal(v)
			str = string(b)
			if len(str) >= 2 && str[0] == '"' {
				var s string
				if json.Unmarshal(b, &s) == nil {
					str = s
				}
			}
		}
		if sk == "log_level" {
			switch str {
			case "debug", "info", "warn", "error":
			default:
				return nil, fmt.Errorf("invalid_argument: log_level must be debug|info|warn|error")
			}
		}
		updates[sk] = str
	}
	var req struct {
		LogLevel    *string `json:"logLevel"`
		Log_Level   *string `json:"log_level"`
		IdleExit    *string `json:"idleExit"`
		Idle_Exit   *string `json:"idle_exit"`
		ShimLinger  *string `json:"shimLinger"`
		Shim_Linger *string `json:"shim_linger"`
		Retention   *string `json:"retention"`
	}
	_ = decodeRaw(raw, &req)
	if req.LogLevel != nil {
		updates["log_level"] = *req.LogLevel
	}
	if req.Log_Level != nil {
		updates["log_level"] = *req.Log_Level
	}
	if req.IdleExit != nil {
		updates["idle_exit"] = *req.IdleExit
	}
	if req.Idle_Exit != nil {
		updates["idle_exit"] = *req.Idle_Exit
	}
	if req.ShimLinger != nil {
		updates["shim_linger"] = *req.ShimLinger
	}
	if req.Shim_Linger != nil {
		updates["shim_linger"] = *req.Shim_Linger
	}
	if req.Retention != nil {
		updates["retention"] = *req.Retention
	}
	for k, v := range updates {
		b, _ := json.Marshal(v)
		if err := s.engine.Store().SetSetting(k, string(b)); err != nil {
			return nil, err
		}
	}
	return s.sysGetSettings(w, r)
}

func settingJSONValue(stored string, def any) any {
	var v any
	if json.Unmarshal([]byte(stored), &v) == nil {
		return v
	}
	return def
}

func decodeRaw(raw map[string]any, v any) error {
	b, _ := json.Marshal(raw)
	data := normalizeTopLevelCasing(b)
	return json.Unmarshal(data, v)
}

func logDiskBytes(dataDir string) int64 {
	var total int64
	root := filepath.Join(dataDir, "logs")
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
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
