// Package mcp bridge: agent-runtime serve as a thin client of the
// per-user daemon (§5.6).
//
// The bridge resolves the workspace (explicit flag/env, else cwd),
// registers a session with its harness, and maps every existing tool 1:1
// onto the daemon API. Tool names and argument shapes stay backward
// compatible; the daemon returns pkg/api shapes so agents' habits and
// the embedded skills keep working.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"agent-runtime/internal/project"
	"agent-runtime/internal/runtime"
	"agent-runtime/pkg/api"
	"agent-runtime/pkg/client"
)

// Bridge is a runtime.Facade implemented over the daemon API.
type Bridge struct {
	client      *client.Client
	workspaceID string
	projectID   string
	sessionID   string
	harness     string
	logger      *slog.Logger
}

var _ runtime.Facade = (*Bridge)(nil)

// DialBridge ensures the daemon is reachable, resolves the workspace, and
// registers an MCP session. harnessHint overrides env detection.
func DialBridge(socketPath, workspacePath, harnessHint string, logger *slog.Logger) (*Bridge, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cl, err := client.EnsureDaemon(socketPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cl.GetVersion(ctx); err != nil {
		return nil, fmt.Errorf("bridge: daemon handshake: %w", err)
	}
	if workspacePath == "" {
		if v := os.Getenv("AGENT_RUNTIME_PROJECT"); v != "" {
			workspacePath = v
		} else if cwd, err := os.Getwd(); err == nil {
			workspacePath = cwd
		}
	}
	root := project.WorkspaceRoot(workspacePath)
	res, err := cl.ProjectService().Resolve(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("bridge: resolve %s: %w", root, err)
	}
	harness := harnessHint
	if harness == "" {
		harness = detectHarness()
	}
	pid := os.Getpid()
	reg, err := cl.SessionService().Register(ctx, "mcp", harness, pid, res.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("bridge: register session: %w", err)
	}
	b := &Bridge{
		client: cl, workspaceID: res.WorkspaceID, projectID: res.ProjectID,
		sessionID: reg.SessionID, harness: harness, logger: logger,
	}
	// Heartbeat for the session's lifetime; when the bridge exits the
	// stream drops and the daemon marks the session closed (processes
	// persist except lifetime: session ones, per their lease).
	go b.heartbeat()
	return b, nil
}

func (b *Bridge) heartbeat() {
	ctx := context.Background()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		_ = b.client.SessionService().Ping(ctx, b.sessionID)
	}
}

// WorkspaceID returns the session workspace.
func (b *Bridge) WorkspaceID() string { return b.workspaceID }

// ProjectID returns the session project.
func (b *Bridge) ProjectID() string { return b.projectID }

// SessionID returns the daemon session id.
func (b *Bridge) SessionID() string { return b.sessionID }

// Close marks the bridge session closed.
func (b *Bridge) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.client.SessionService().Close(ctx, b.sessionID)
}

// detectHarness maps well-known env markers to harness names.
func detectHarness() string {
	for env, harness := range map[string]string{
		"CLAUDE_CODE_ENTRYPOINT": "claude-code",
		"CODEX_THREAD_ID":        "codex",
		"GEMINI_CLI":             "gemini",
		"OPENCODE_CLIENT":        "opencode",
		"CURSOR_AGENT":           "cursor",
	} {
		if os.Getenv(env) != "" {
			return harness
		}
	}
	if os.Getenv("TERM_PROGRAM") == "cursor" {
		return "cursor"
	}
	return "unknown"
}

// convert round-trips a generic map through JSON into an api struct.
// The daemon returns api shapes by construction, so this is lossless.
func convert(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (b *Bridge) Start(ctx context.Context, req api.StartRequest) (*api.StartResult, error) {
	env := map[string]string{}
	for _, kv := range req.Env {
		if i := indexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	var cmd []string
	if req.Command != "" {
		cmd = append([]string{req.Command}, req.Args...)
	}
	res, err := b.client.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: b.workspaceID, App: req.App, Command: cmd,
		WorkDir: req.WorkDir, Env: env,
	})
	if err != nil {
		return nil, err
	}
	return &api.StartResult{
		ProcessID: res.ProcessID, InstanceID: res.InstanceID,
		Status: res.Status, Command: req.Command, Args: req.Args, WorkDir: req.WorkDir,
	}, nil
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func (b *Bridge) Stop(ctx context.Context, id string) error {
	return b.client.ProcessService().Stop(ctx, id)
}

func (b *Bridge) Restart(ctx context.Context, id string) error {
	_, err := b.client.ProcessService().Restart(ctx, id)
	return err
}

func (b *Bridge) SetRestartPolicy(req api.SetRestartPolicyRequest) (*api.SetRestartPolicyResult, error) {
	if err := b.client.ProcessService().SetRestartPolicy(context.Background(), req.ProcessID, req.Policy); err != nil {
		return nil, err
	}
	return &api.SetRestartPolicyResult{ProcessID: req.ProcessID, RestartPolicy: req.Policy}, nil
}

func (b *Bridge) Status(id string) (*api.StatusResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := b.client.ProcessService().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var out api.StatusResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) List() (*api.ListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items, err := b.client.ProcessService().List(ctx, b.workspaceID, false)
	if err != nil {
		return nil, err
	}
	out := &api.ListResult{}
	for _, it := range items {
		var s api.ProcessSummary
		if err := convert(it, &s); err != nil {
			continue
		}
		out.Processes = append(out.Processes, s)
	}
	return out, nil
}

func (b *Bridge) GetLogs(req api.GetLogsRequest) (*api.GetLogsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	got, err := b.client.LogService().GetLogs(ctx, req.ProcessID, req.Lines)
	if err != nil {
		return nil, err
	}
	// Re-apply contains/level client-side is unnecessary: the daemon
	// already filtered. Decode straight into the api shape.
	var out api.GetLogsResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) ClearLogs(id, stream string) (*api.ClearLogsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.LogService().Clear(ctx, id, stream); err != nil {
		return nil, err
	}
	return &api.ClearLogsResult{ProcessID: id, Cleared: true}, nil
}

func (b *Bridge) SendStdin(id, data string) (*api.SendStdinResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.ProcessService().SendStdin(ctx, id, data); err != nil {
		return nil, err
	}
	return &api.SendStdinResult{Written: len(data)}, nil
}

func (b *Bridge) RemoveProcess(id string, force bool) (*api.RemoveProcessResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.client.ProcessService().Remove(ctx, id, force); err != nil {
		return nil, err
	}
	return &api.RemoveProcessResult{ProcessID: id, Removed: true}, nil
}

func (b *Bridge) WaitForLog(ctx context.Context, req api.WaitForLogRequest) (*api.WaitForLogResult, error) {
	got, err := b.client.LogService().WaitForLog(ctx, map[string]any{
		"process_id": req.ProcessID, "contains": req.Contains,
		"pattern": req.Pattern, "ready": req.Ready, "timeout_ms": req.TimeoutMS,
	})
	if err != nil {
		return nil, err
	}
	var out api.WaitForLogResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) WaitForExit(ctx context.Context, req api.WaitForExitParams) (*api.WaitForExitResult, error) {
	got, err := b.client.ProcessService().WaitForExit(ctx, req.ProcessID, req.TimeoutMS)
	if err != nil {
		return nil, err
	}
	var out api.WaitForExitResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) Apps() (*api.ListAppsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := b.client.ConfigService().Get(ctx, b.workspaceID)
	if err != nil {
		return nil, err
	}
	out := &api.ListAppsResult{}
	if apps, ok := cfg["apps"].(map[string]any); ok {
		for name, av := range apps {
			info := api.AppInfo{Name: name, Configured: true}
			if m, ok := av.(map[string]any); ok {
				if cmd, ok := m["command"].([]any); ok && len(cmd) > 0 {
					if c0, ok := cmd[0].(string); ok {
						info.Command = c0
					}
					for _, a := range cmd[1:] {
						if s, ok := a.(string); ok {
							info.Args = append(info.Args, s)
						}
					}
				}
				if wd, ok := m["workdir"].(string); ok {
					info.WorkDir = wd
				}
				if t, ok := m["type"].(string); ok {
					info.Type = t
				}
			}
			out.Apps = append(out.Apps, info)
		}
	}
	return out, nil
}

func (b *Bridge) SignalProcess(ctx context.Context, req api.SignalProcessRequest) (*api.SignalProcessResult, error) {
	if err := b.client.ProcessService().Signal(ctx, req.ProcessID, req.Signal); err != nil {
		return nil, err
	}
	return &api.SignalProcessResult{ProcessID: req.ProcessID, Signal: req.Signal}, nil
}

func (b *Bridge) ProcessEnv(req api.ProcessEnvRequest) (*api.ProcessEnvResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := b.client.ProcessService().GetEnv(ctx, req.ProcessID, req.Reveal, req.Live)
	if err != nil {
		return nil, err
	}
	var out api.ProcessEnvResult
	if err := convert(got, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) OpenShell(ctx context.Context, req api.OpenShellRequest) (*api.StartResult, error) {
	res, err := b.client.ProcessService().Start(ctx, &client.StartRequest{
		WorkspaceID: b.workspaceID, App: req.App, WorkDir: "",
	})
	if err != nil {
		return nil, err
	}
	_ = req
	return &api.StartResult{ProcessID: res.ProcessID, InstanceID: res.InstanceID, Status: res.Status}, nil
}

func (b *Bridge) SubscribeEvents(clientName string, req api.SubscribeEventsRequest) (*api.SubscribeEventsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out api.SubscribeEventsResult
	if err := b.client.Call(ctx, "EventService", "Subscribe", map[string]any{
		"sessionId": b.sessionID, "types": req.Types, "processId": req.ProcessID,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) GetEvents(req api.GetEventsRequest) (*api.GetEventsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out api.GetEventsResult
	if err := b.client.Call(ctx, "EventService", "Drain", map[string]any{
		"subscriptionId": req.SubscriptionID, "limit": req.Limit,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) UnsubscribeEvents(clientName, subscriptionID string) (*api.UnsubscribeEventsResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out api.UnsubscribeEventsResult
	if err := b.client.Call(ctx, "EventService", "Unsubscribe", map[string]any{
		"subscriptionId": subscriptionID,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (b *Bridge) RuntimeStats() (*api.RuntimeStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stats, err := b.client.SystemService().GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return &api.RuntimeStats{
		TotalProcesses:    int(stats.Processes),
		ProcessesByStatus: map[string]int{"tracked": int(stats.Processes)},
	}, nil
}

func (b *Bridge) GetAuditLog(lines int, contains string) (*api.AuditLogResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entries, err := b.client.AuditService().List(ctx, b.workspaceID, lines)
	if err != nil {
		return nil, err
	}
	out := &api.AuditLogResult{}
	for _, e := range entries {
		var ae api.AuditEntry
		if err := convert(e, &ae); err == nil {
			if contains == "" || containsStr(ae.Tool+ae.Result, contains) {
				out.Entries = append(out.Entries, ae)
			}
		}
	}
	out.Lines = len(out.Entries)
	return out, nil
}

func containsStr(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- v3 additions (new tools) ---

// ProjectInfo tells the agent where the YAML lives now and who else is
// attached. It is the answer to "where is my config".
func (b *Bridge) ProjectInfo(ctx context.Context) (map[string]any, error) {
	proj, err := b.client.ProjectService().GetProject(ctx, b.projectID)
	if err != nil {
		return nil, err
	}
	cfg, err := b.client.ConfigService().Get(ctx, b.workspaceID)
	if err != nil {
		return nil, err
	}
	sessions, _ := b.client.SessionService().List(ctx, b.workspaceID)
	return map[string]any{
		"projectId": b.projectID, "workspaceId": b.workspaceID,
		"project": proj, "configPath": cfg["configPath"],
		"overlayPath": cfg["overlayPath"],
		"sessions":    sessions,
	}, nil
}

// ValidateConfig validates YAML without applying it.
func (b *Bridge) ValidateConfig(ctx context.Context, yaml string) (map[string]any, error) {
	return b.client.ConfigService().Validate(ctx, yaml)
}

// PlanConfig diffs YAML against the active revision.
func (b *Bridge) PlanConfig(ctx context.Context, yaml string) (map[string]any, error) {
	return b.client.ConfigService().PlanYAML(ctx, b.workspaceID, yaml)
}

// ApplyConfig validates, plans and applies YAML (optimistic concurrency
// via baseRevision; 0 disables the check).
func (b *Bridge) ApplyConfig(ctx context.Context, yaml string, baseRevision int64, message string) (map[string]any, error) {
	return b.client.ConfigService().Apply(ctx, map[string]any{
		"projectId": b.projectID, "layer": "project",
		"yaml": yaml, "baseRevision": baseRevision,
		"message": message, "sessionId": b.sessionID,
	})
}

// SearchLogs runs a bounded FTS/regex search with cursor.
func (b *Bridge) SearchLogs(ctx context.Context, query string, processIDs []string, regex bool, maxRows int) (map[string]any, error) {
	return b.client.LogService().Search(ctx, map[string]any{
		"workspaceId": b.workspaceID, "processIds": processIDs,
		"query": query, "regex": regex, "maxRows": maxRows,
	})
}

// ListSessions shows who else is working in this workspace.
func (b *Bridge) ListSessions(ctx context.Context) ([]map[string]any, error) {
	return b.client.SessionService().List(ctx, b.workspaceID)
}
