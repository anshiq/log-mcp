// Package runtime is the facade that wires the process manager, the log store,
// the event bus and the profile registry together. Both the MCP server and the
// direct CLI talk to the runtime; process-management logic never lives inside
// MCP tool handlers.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/logs"
	"agent-runtime/internal/process"
	"agent-runtime/internal/profile"
	"agent-runtime/pkg/api"
)

const (
	maxStdinBytes = 64 * 1024
	defaultWaitMS = 30000
	maxWaitMS     = 600000
)

// Runtime is the single entry point for all agent operations.
type Runtime struct {
	cfg      *config.Loaded
	manager  *process.Manager
	profiles *profile.Registry
	logger   *slog.Logger

	rootCtx context.Context
	cancel  context.CancelFunc
}

// New builds a Runtime from a loaded config.
func New(loaded *config.Loaded, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	manager := process.New(rootCtx, process.Options{
		LogCapacity:  loaded.Config.Runtime.LogBufferLines,
		DefaultGrace: loaded.Config.Runtime.StopGrace.Time(),
		Logger:       logger,
	})
	return &Runtime{
		cfg:      loaded,
		manager:  manager,
		profiles: profile.Default(),
		logger:   logger,
		rootCtx:  rootCtx,
		cancel:   cancel,
	}
}

// Manager exposes the underlying process manager (used by the CLI).
func (r *Runtime) Manager() *process.Manager { return r.manager }

// Config returns the loaded project configuration.
func (r *Runtime) Config() *config.Loaded { return r.cfg }

// Start launches a process and returns immediately. The process continues
// running independently of the calling MCP request.
func (r *Runtime) Start(ctx context.Context, req api.StartRequest) (*api.StartResult, error) {
	spec, prof, err := r.resolveStart(req)
	if err != nil {
		return nil, err
	}
	grace := r.cfg.Config.Runtime.StopGrace.Time()
	if prof != nil && prof.Grace() > grace {
		grace = prof.Grace()
	}
	profileName := "generic"
	if prof != nil {
		profileName = prof.Name
	}
	proc, err := r.manager.Start(ctx, spec, profileName, grace)
	if err != nil {
		return nil, err
	}
	info := proc.Info()
	return &api.StartResult{
		ProcessID:  info.ID,
		InstanceID: info.InstanceID,
		Status:     string(info.Status),
		Profile:    info.Profile,
		Command:    info.Command,
		Args:       info.Args,
		WorkDir:    info.WorkDir,
	}, nil
}

// Stop gracefully stops a process, escalating to SIGKILL on timeout.
func (r *Runtime) Stop(ctx context.Context, id string) error {
	return r.manager.Stop(ctx, id)
}

// Restart stops and re-launches a process with the same specification.
func (r *Runtime) Restart(ctx context.Context, id string) error {
	return r.manager.Restart(ctx, id)
}

// Status returns a snapshot of a process.
func (r *Runtime) Status(id string) (*api.StatusResult, error) {
	proc, ok := r.manager.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", id)
	}
	info := proc.Info()
	res := &api.StatusResult{
		ProcessID:   info.ID,
		InstanceID:  info.InstanceID,
		Status:      string(info.Status),
		PID:         info.PID,
		Command:     info.Command,
		Args:        info.Args,
		WorkDir:     info.WorkDir,
		Profile:     info.Profile,
		Restarts:    info.Restarts,
		StdoutLines: proc.Logs.Count(logs.StreamStdout),
		StderrLines: proc.Logs.Count(logs.StreamStderr),
	}
	if !info.StartedAt.IsZero() {
		res.StartedAt = info.StartedAt.Format(time.RFC3339Nano)
	}
	if info.ExitedAt != nil {
		s := info.ExitedAt.Format(time.RFC3339Nano)
		res.ExitedAt = &s
	}
	res.ExitCode = info.ExitCode
	return res, nil
}

// List returns all managed processes.
func (r *Runtime) List() (*api.ListResult, error) {
	procs := r.manager.List()
	out := &api.ListResult{Processes: make([]api.ProcessSummary, 0, len(procs))}
	for _, p := range procs {
		info := p.Info()
		out.Processes = append(out.Processes, api.ProcessSummary{
			ProcessID:  info.ID,
			InstanceID: info.InstanceID,
			Status:     string(info.Status),
			Command:    info.Command,
			Profile:    info.Profile,
		})
	}
	return out, nil
}

// GetLogs returns the tail of a process's log history, with stream filtering,
// substring filtering, line limits and a hard byte cap.
func (r *Runtime) GetLogs(req api.GetLogsRequest) (*api.GetLogsResult, error) {
	proc, ok := r.manager.Get(req.ProcessID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	filter := parseStream(req.Stream)
	lines := req.Lines
	if lines <= 0 {
		lines = 100
	}
	if lines > r.cfg.Config.Runtime.MaxLogLines {
		lines = r.cfg.Config.Runtime.MaxLogLines
	}
	q := logs.Query{Stream: filter, Lines: lines, Contains: req.Contains}
	res := proc.Logs.Query(q)

	entries := make([]api.LogEntry, 0, len(res.Entries))
	var totalBytes int
	for _, e := range res.Entries {
		entry := toAPIEntry(e)
		totalBytes += len(entry.Line) + 64
		if totalBytes > r.cfg.Config.Runtime.MaxLogBytes && len(entries) > 0 {
			// Drop oldest until under the byte cap.
			entries = entries[1:]
		}
		entries = append(entries, entry)
	}
	truncated := res.Truncated || len(entries) < len(res.Entries)
	info := proc.Info()
	return &api.GetLogsResult{
		ProcessID:      info.ID,
		InstanceID:     info.InstanceID,
		Entries:        entries,
		Truncated:      truncated,
		ReturnedLines:  len(entries),
		AvailableLines: res.Available,
	}, nil
}

// ClearLogs empties the selected log buffer(s).
func (r *Runtime) ClearLogs(id, stream string) (*api.ClearLogsResult, error) {
	proc, ok := r.manager.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", id)
	}
	removed := proc.Logs.Clear(parseStream(stream))
	return &api.ClearLogsResult{ProcessID: id, Cleared: true, Removed: removed}, nil
}

// SendStdin writes data to a running process's stdin.
func (r *Runtime) SendStdin(id, data string) (*api.SendStdinResult, error) {
	proc, ok := r.manager.Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", id)
	}
	if len(data) > maxStdinBytes {
		return nil, fmt.Errorf("data too large: %d bytes (max %d)", len(data), maxStdinBytes)
	}
	if err := proc.SendStdin(data); err != nil {
		return nil, err
	}
	return &api.SendStdinResult{Written: len(data)}, nil
}

// WaitForLog waits until a matching log entry appears (existing or future),
// the process exits, or the timeout elapses.
func (r *Runtime) WaitForLog(ctx context.Context, req api.WaitForLogRequest) (*api.WaitForLogResult, error) {
	proc, ok := r.manager.Get(req.ProcessID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	match, err := r.buildMatcher(req, proc.Profile)
	if err != nil {
		return nil, err
	}
	timeout, err := waitDuration(req.TimeoutMS)
	if err != nil {
		return nil, err
	}

	subID, wake := proc.SubscribeLogs()
	defer proc.UnsubscribeLogs(subID)

	lastID := proc.EntryFrom()
	scan := func() *logs.Entry {
		for _, e := range proc.Logs.From(lastID) {
			lastID = e.ID + 1
			if match(e.Line) {
				cp := e
				return &cp
			}
		}
		return nil
	}

	if e := scan(); e != nil {
		return &api.WaitForLogResult{Matched: true, Entry: toAPIEntryPtr(e)}, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-wake:
			if e := scan(); e != nil {
				return &api.WaitForLogResult{Matched: true, Entry: toAPIEntryPtr(e)}, nil
			}
		case <-proc.Done():
			// Final scan for lines the readers flushed just before exit.
			if e := scan(); e != nil {
				return &api.WaitForLogResult{Matched: true, Entry: toAPIEntryPtr(e)}, nil
			}
			return &api.WaitForLogResult{Matched: false, Exited: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return &api.WaitForLogResult{Matched: false, Timeout: true}, nil
		}
	}
}

// WaitForExit waits until the process exits or the timeout elapses.
func (r *Runtime) WaitForExit(ctx context.Context, req api.WaitForExitParams) (*api.WaitForExitResult, error) {
	proc, ok := r.manager.Get(req.ProcessID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	timeout, err := waitDuration(req.TimeoutMS)
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-proc.Done():
		info := proc.Info()
		return &api.WaitForExitResult{Exited: true, ExitCode: info.ExitCode}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return &api.WaitForExitResult{Exited: false, Timeout: true}, nil
	}
}

// Apps reports configured apps and their detected profiles.
func (r *Runtime) Apps() (*api.ListAppsResult, error) {
	out := &api.ListAppsResult{Apps: make([]api.AppInfo, 0, len(r.cfg.Config.Apps))}
	for name, app := range r.cfg.Config.Apps {
		workDir := r.abs(app.WorkDir)
		prof := r.profiles.Lookup(app.Type)
		if prof == nil {
			prof = r.profiles.Detect(workDir)
		}
		info := api.AppInfo{Name: name, WorkDir: workDir, Configured: true}
		if prof != nil {
			info.Type = prof.Name
		}
		if len(app.Command) > 0 {
			info.Command = app.Command[0]
			info.Args = app.Command[1:]
		} else if prof != nil {
			cmd := prof.Command(workDir)
			if len(cmd) > 0 {
				info.Command = cmd[0]
				info.Args = cmd[1:]
			}
		}
		out.Apps = append(out.Apps, info)
	}
	return out, nil
}

// Shutdown cancels the runtime and gracefully stops all managed processes.
func (r *Runtime) Shutdown() error {
	err := r.manager.Shutdown(r.cfg.Config.Runtime.ShutdownTimeout.Time())
	r.cancel()
	return err
}

// resolveStart converts a StartRequest into a concrete StartSpec and profile.
func (r *Runtime) resolveStart(req api.StartRequest) (process.StartSpec, *profile.Profile, error) {
	var spec process.StartSpec
	var prof *profile.Profile

	switch {
	case req.App != "":
		app, ok := r.cfg.Config.Apps[req.App]
		if !ok {
			return spec, nil, fmt.Errorf("unknown app %q (configured apps: %s)", req.App, strings.Join(r.cfg.Names(), ", "))
		}
		workDir := r.abs(app.WorkDir)
		prof = r.profiles.Lookup(app.Type)
		if prof == nil {
			prof = r.profiles.Detect(workDir)
		}
		cmd := app.Command
		if len(cmd) == 0 && prof != nil {
			cmd = prof.Command(workDir)
		}
		if len(cmd) == 0 {
			return spec, nil, fmt.Errorf("app %q has no start command; set one in agent-runtime.yaml", req.App)
		}
		env := append([]string{}, app.Env...)
		if app.EnvFile != "" {
			fileEnv, err := config.ParseEnvFile(r.abs(app.EnvFile))
			if err != nil {
				return spec, nil, fmt.Errorf("app %q env_file: %w", req.App, err)
			}
			env = append(env, fileEnv...)
		}
		env = append(env, req.Env...)
		spec = process.StartSpec{Command: cmd[0], Args: cmd[1:], WorkDir: workDir, Env: env}

	case req.Command != "":
		workDir := req.WorkDir
		if workDir == "" {
			workDir = r.cfg.ProjectDir
		} else if !filepath.IsAbs(workDir) {
			workDir = filepath.Join(r.cfg.ProjectDir, workDir)
		}
		prof = r.profiles.Detect(workDir)
		spec = process.StartSpec{Command: req.Command, Args: req.Args, WorkDir: workDir, Env: req.Env}

	default:
		return spec, nil, errors.New("start_process requires either app or command")
	}
	return spec, prof, nil
}

// buildMatcher constructs the log-line matcher for wait_for_log.
func (r *Runtime) buildMatcher(req api.WaitForLogRequest, profileName string) (func(string) bool, error) {
	switch {
	case req.Ready:
		prof := r.profiles.Lookup(profileName)
		if prof == nil || len(prof.Readiness) == 0 {
			return nil, fmt.Errorf("profile %q has no readiness patterns; use contains or pattern instead", profileName)
		}
		return prof.Ready, nil
	case req.Pattern != "":
		re, err := regexp.Compile(req.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
		return re.MatchString, nil
	case req.Contains != "":
		sub := req.Contains
		return func(line string) bool { return strings.Contains(line, sub) }, nil
	}
	return nil, errors.New("wait_for_log requires one of contains, pattern or ready")
}

func waitDuration(ms int) (time.Duration, error) {
	if ms <= 0 {
		ms = defaultWaitMS
	}
	if ms > maxWaitMS {
		return 0, fmt.Errorf("timeout_ms too large (max %d)", maxWaitMS)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func parseStream(s string) logs.StreamFilter {
	switch logs.StreamFilter(s) {
	case logs.FilterStdout:
		return logs.FilterStdout
	case logs.FilterStderr:
		return logs.FilterStderr
	default:
		return logs.FilterAll
	}
}

func (r *Runtime) abs(dir string) string {
	if dir == "" {
		return r.cfg.ProjectDir
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(r.cfg.ProjectDir, dir)
}

func toAPIEntry(e logs.Entry) api.LogEntry {
	return api.LogEntry{
		ID:        e.ID,
		Timestamp: e.Timestamp.Format(time.RFC3339Nano),
		Stream:    string(e.Stream),
		Line:      e.Line,
	}
}

func toAPIEntryPtr(e *logs.Entry) *api.LogEntry {
	if e == nil {
		return nil
	}
	cp := toAPIEntry(*e)
	return &cp
}
