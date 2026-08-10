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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"agent-runtime/internal/config"
	"agent-runtime/internal/logs"
	"agent-runtime/internal/logstore"
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
	sink     logs.Sink

	// baseEnv is the lowest environment layer every process inherits: the
	// captured login-shell environment (baseEnvName == "shell") or os.Environ()
	// (baseEnvName == "parent") when capture fails or shell_env: none.
	baseEnv     []string
	baseEnvName string

	// envSrc records, per process ID, which layer won for each env key
	// (provenance for get_process_env spec mode).
	envSrcMu sync.RWMutex
	envSrc   map[string]map[string]string

	rootCtx context.Context
	cancel  context.CancelFunc
}

// New builds a Runtime from a loaded config.
func New(loaded *config.Loaded, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	rootCtx, cancel := context.WithCancel(context.Background())

	sink := buildSink(loaded, logger, rootCtx)

	manager := process.New(rootCtx, process.Options{
		LogCapacity:        loaded.Config.Runtime.LogBufferLines,
		DefaultGrace:       loaded.Config.Runtime.StopGrace.Time(),
		MaxExitedProcesses: loaded.Config.Runtime.MaxExitedProcesses,
		Logger:             logger,
		Sink:               sink,
	})
	baseEnv, baseEnvName := captureBaseEnv(loaded, logger)
	return &Runtime{
		cfg:         loaded,
		manager:     manager,
		profiles:    profile.Default(),
		logger:      logger,
		sink:        sink,
		baseEnv:     baseEnv,
		baseEnvName: baseEnvName,
		envSrc:      make(map[string]map[string]string),
		rootCtx:     rootCtx,
		cancel:      cancel,
	}
}

// captureBaseEnv resolves the lowest environment layer for every process. With
// shell_env: login (the default) the login-shell environment is captured once
// at startup; on any capture failure the runtime falls back to os.Environ() so
// process management never breaks. With shell_env: none os.Environ() is used
// directly. The returned name is the provenance label for the layer.
func captureBaseEnv(loaded *config.Loaded, logger *slog.Logger) (env []string, name string) {
	if loaded.Config.Runtime.ShellEnv == "login" {
		captured, err := config.CaptureShellEnv(os.Getenv("SHELL"), 3*time.Second)
		if err != nil {
			logger.Warn("shell env capture failed; falling back to os.Environ()", "error", err)
			return os.Environ(), "parent"
		}
		return captured, "shell"
	}
	return os.Environ(), "parent"
}

// buildSink constructs the durable log archive when log_store: sqlite is
// configured, otherwise returns nil (in-memory mode). A sqlite failure is
// never fatal: the runtime warns and falls back to memory so the daemon keeps
// running even when the archive cannot be opened.
func buildSink(loaded *config.Loaded, logger *slog.Logger, rootCtx context.Context) logs.Sink {
	if loaded.Config.Runtime.LogStore != "sqlite" {
		return nil
	}
	path := loaded.Config.Runtime.DBPath
	if path == "" {
		path = filepath.Join(loaded.ProjectDir, ".agent-runtime", "logs.db")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(loaded.ProjectDir, path)
	}
	sink, err := logstore.Open(path, logger)
	if err != nil {
		logger.Warn("log_store=sqlite failed to open, falling back to memory", "path", path, "error", err)
		return nil
	}
	maxAge := time.Duration(*loaded.Config.Runtime.DBMaxAgeDays) * 24 * time.Hour
	if err := sink.Retain(maxAge, *loaded.Config.Runtime.DBMaxMB); err != nil {
		logger.Warn("logstore: initial retain failed", "path", path, "error", err)
	}
	go sink.Janitor(rootCtx.Done(), time.Hour)
	logger.Debug("logstore: sqlite archive enabled", "path", path)
	return sink
}

// Manager exposes the underlying process manager (used by the CLI).
func (r *Runtime) Manager() *process.Manager { return r.manager }

// Config returns the loaded project configuration.
func (r *Runtime) Config() *config.Loaded { return r.cfg }

// Start launches a process and returns immediately. The process continues
// running independently of the calling MCP request.
func (r *Runtime) Start(ctx context.Context, req api.StartRequest) (*api.StartResult, error) {
	spec, prof, source, err := r.resolveStart(req)
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
	r.envSrcMu.Lock()
	r.envSrc[info.ID] = source
	r.envSrcMu.Unlock()
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

// SignalProcess delivers a named signal to the entire process group of a
// process (mycli -> uv -> python tree).
func (r *Runtime) SignalProcess(ctx context.Context, req api.SignalProcessRequest) (*api.SignalProcessResult, error) {
	sig, err := process.SignalByName(req.Signal)
	if err != nil {
		return nil, err
	}
	if err := r.manager.Signal(ctx, req.ProcessID, sig); err != nil {
		return nil, err
	}
	return &api.SignalProcessResult{ProcessID: req.ProcessID, Signal: req.Signal}, nil
}

// ProcessEnv returns a process's environment: either the complete env the
// runtime constructed for it (spec mode, with provenance) or the ground-truth
// environment read live from /proc/<pid>/environ. Secret-like values are
// masked unless Reveal is set.
func (r *Runtime) ProcessEnv(req api.ProcessEnvRequest) (*api.ProcessEnvResult, error) {
	proc, ok := r.manager.Get(req.ProcessID)
	if !ok {
		return nil, fmt.Errorf("unknown process %q", req.ProcessID)
	}
	info := proc.Info()
	res := &api.ProcessEnvResult{ProcessID: req.ProcessID, Live: req.Live, PID: info.PID}

	var vars []string
	if req.Live {
		if info.PID <= 0 {
			return nil, fmt.Errorf("process %q has no pid (status %s); cannot read live env", req.ProcessID, info.Status)
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", info.PID))
		if err != nil {
			return nil, fmt.Errorf("read /proc/%d/environ: %w (process may have exited)", info.PID, err)
		}
		vars, err = config.ParseNulEnv(data)
		if err != nil {
			return nil, err
		}
	} else {
		vars = proc.Spec.Env
		r.envSrcMu.RLock()
		if src, ok := r.envSrc[req.ProcessID]; ok {
			res.Source = src
		}
		r.envSrcMu.RUnlock()
	}

	env, n := config.RedactEnv(vars, req.Reveal)
	res.Env = env
	res.Redacted = n
	return res, nil
}

// OpenShell starts an interactive shell inside the environment (resolved
// workdir + complete env) of a running process, a configured app, or any
// process on the machine by OS pid (read from /proc/<pid>/cwd and
// /proc/<pid>/environ, Linux only). The shell is started as an ordinary
// managed process and returned to the caller.
func (r *Runtime) OpenShell(ctx context.Context, req api.OpenShellRequest) (*api.StartResult, error) {
	var workDir string
	var env []string
	var prof *profile.Profile

	switch {
	case req.ProcessID != "":
		proc, ok := r.manager.Get(req.ProcessID)
		if !ok {
			return nil, fmt.Errorf("unknown process %q", req.ProcessID)
		}
		workDir = proc.Spec.WorkDir
		env = proc.Spec.Env
		prof = r.profiles.Lookup(proc.Profile)
		if _, err := os.Stat(workDir); err != nil {
			return nil, fmt.Errorf("open_shell workdir %q: %w", workDir, err)
		}
	case req.App != "":
		spec, p, _, err := r.resolveStart(api.StartRequest{App: req.App})
		if err != nil {
			return nil, err
		}
		workDir = spec.WorkDir
		env = spec.Env
		prof = p
	case req.PID > 0:
		cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", req.PID))
		if err != nil {
			return nil, fmt.Errorf("open shell for pid %d: %w (process may have exited, or /proc is unavailable)", req.PID, err)
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", req.PID))
		if err != nil {
			return nil, fmt.Errorf("open shell for pid %d: %w (process may have exited, or /proc is unavailable)", req.PID, err)
		}
		env, _ = config.ParseNulEnv(data)
		if _, err := os.Stat(cwd); err != nil {
			return nil, fmt.Errorf("open shell for pid %d: workdir %q: %w", req.PID, cwd, err)
		}
		workDir = cwd
		// prof stays nil: generic profile, default stop grace.
	default:
		return nil, errors.New("open_shell requires process_id, app or pid")
	}

	return r.startShell(ctx, req.Shell, workDir, env, prof)
}

// startShell launches a shell as a managed process in the given workdir+env
// and returns the StartResult. prof may be nil, in which case the profile is
// "generic" with the runtime's default stop grace. Shared by all OpenShell
// branches.
func (r *Runtime) startShell(ctx context.Context, shell, workDir string, env []string, prof *profile.Profile) (*api.StartResult, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	grace := r.cfg.Config.Runtime.StopGrace.Time()
	if prof != nil && prof.Grace() > grace {
		grace = prof.Grace()
	}
	profileName := "generic"
	if prof != nil {
		profileName = prof.Name
	}

	spec := process.StartSpec{Command: shell, WorkDir: workDir, Env: env}
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
		PGID:        info.PID, // Setpgid on Unix means pgid == pid
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
//
// When a durable sink is active and the in-memory ring buffer cannot satisfy
// the requested tail (ring eviction has dropped older matching entries), the
// missing tail is back-filled from the archive and merged by ID. Live waiting
// (From/After/waiters) always uses the ring buffer; the archive is only ever
// a read-only back-fill source here.
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

	source := "memory"
	if r.sink != nil && res.Available < lines {
		source, res = r.backfillFromSink(req, filter, lines, res, proc)
	}

	entries := make([]api.LogEntry, 0, len(res.Entries))
	var retainedBytes int
	dropped := false
	for _, e := range res.Entries {
		entry := toAPIEntry(e)
		retainedBytes += len(entry.Line) + 64
		entries = append(entries, entry)
		// Drop the oldest retained entries until the response fits the byte
		// cap, always keeping at least one entry (a single oversized line is
		// still returned). retainedBytes tracks only what is kept, so long
		// tails do not degenerate toward a single entry.
		for retainedBytes > r.cfg.Config.Runtime.MaxLogBytes && len(entries) > 1 {
			retainedBytes -= len(entries[0].Line) + 64
			entries = entries[1:]
			dropped = true
		}
	}
	truncated := res.Truncated || dropped || retainedBytes > r.cfg.Config.Runtime.MaxLogBytes
	info := proc.Info()
	return &api.GetLogsResult{
		ProcessID:      info.ID,
		InstanceID:     info.InstanceID,
		Entries:        entries,
		Truncated:      truncated,
		ReturnedLines:  len(entries),
		AvailableLines: res.Available,
		Source:         source,
	}, nil
}

// backfillFromSink fetches the missing tail from the durable archive: entries
// with ID strictly below the oldest in-memory retained ID (i.e. evicted from
// the ring), re-applying the stream and contains filters. The archive results
// are merged with the in-memory results by ID and the response is trimmed to
// the requested line count. On any archive error it degrades gracefully to the
// in-memory result.
func (r *Runtime) backfillFromSink(req api.GetLogsRequest, filter logs.StreamFilter, lines int, res logs.Result, proc *process.ManagedProcess) (string, logs.Result) {
	oldest := proc.Logs.OldestID()
	missing := lines - res.Available
	dbQuery := logs.Query{Stream: filter, Lines: missing, Contains: req.Contains, BeforeID: oldest}
	dbEntries, err := r.sink.Query(req.ProcessID, dbQuery)
	if err != nil {
		r.logger.Warn("logstore: back-fill query failed", "process_id", req.ProcessID, "error", err)
		return "memory", res
	}
	if len(dbEntries) == 0 {
		return "memory", res
	}
	merged := mergeEntriesByID(dbEntries, res.Entries)
	if len(merged) > lines {
		merged = merged[len(merged)-lines:]
	}
	res.Entries = merged
	res.Available += len(dbEntries)
	return "memory+db", res
}

// mergeEntriesByID merges two ID-ordered entry slices (ascending) into one
// ascending slice without sorting. IDs are unique per process.
func mergeEntriesByID(a, b []logs.Entry) []logs.Entry {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]logs.Entry, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].ID < b[j].ID {
			out = append(out, a[i])
			i++
		} else if a[i].ID > b[j].ID {
			out = append(out, b[j])
			j++
		} else {
			out = append(out, a[i])
			i++
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
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

// RemoveProcess deletes a terminated process from the registry and frees its
// log buffers. force=true stops a running process first.
func (r *Runtime) RemoveProcess(id string, force bool) (*api.RemoveProcessResult, error) {
	if err := r.manager.Remove(id, force); err != nil {
		return nil, err
	}
	return &api.RemoveProcessResult{ProcessID: id, Removed: true}, nil
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
		// Resolve symlinks for display consistency with start (so uv/poetry
		// walk-ups are predictable). Apps is a listing: an unresolvable workdir
		// skips the app rather than failing the whole list.
		if target, err := filepath.EvalSymlinks(workDir); err == nil {
			workDir = target
		} else {
			r.logger.Warn("list_apps: skipping app with unresolvable workdir", "app", name, "workdir", workDir, "error", err)
			continue
		}
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
	// Flush and close the durable archive before cancelling the root context,
	// so the archive's writer goroutine drains cleanly and the rootCtx-bound
	// janitor observes cancellation only after the archive is closed.
	if r.sink != nil {
		r.sink.Close()
	}
	r.cancel()
	return err
}

// resolveStart converts a StartRequest into a concrete StartSpec, profile and
// env provenance map (key -> winning layer name).
func (r *Runtime) resolveStart(req api.StartRequest) (process.StartSpec, *profile.Profile, map[string]string, error) {
	var spec process.StartSpec
	var prof *profile.Profile
	var source map[string]string

	switch {
	case req.App != "":
		app, ok := r.cfg.Config.Apps[req.App]
		if !ok {
			return spec, nil, nil, fmt.Errorf("unknown app %q (configured apps: %s)", req.App, strings.Join(r.cfg.Names(), ", "))
		}
		workDir, err := r.resolveWorkDir(app.WorkDir)
		if err != nil {
			return spec, nil, nil, err
		}
		prof = r.profiles.Lookup(app.Type)
		if prof == nil {
			prof = r.profiles.Detect(workDir)
		}
		cmd := app.Command
		if len(cmd) == 0 && prof != nil {
			cmd = prof.Command(workDir)
		}
		if len(cmd) == 0 {
			return spec, nil, nil, fmt.Errorf("app %q has no start command; set one in agent-runtime.yaml", req.App)
		}
		layers := []config.EnvLayer{{Name: r.baseEnvName, Vars: r.baseEnv}}
		if len(r.cfg.Config.Runtime.Env) > 0 {
			layers = append(layers, config.EnvLayer{Name: "runtime.env", Vars: r.cfg.Config.Runtime.Env})
		}
		if app.EnvFile != "" {
			fileEnv, err := config.ParseEnvFile(r.abs(app.EnvFile))
			if err != nil {
				return spec, nil, nil, fmt.Errorf("app %q env_file: %w", req.App, err)
			}
			layers = append(layers, config.EnvLayer{Name: "env_file", Vars: fileEnv})
		}
		layers = append(layers,
			config.EnvLayer{Name: "app.env", Vars: app.Env},
			config.EnvLayer{Name: "request.env", Vars: req.Env},
		)
		merged, src := config.MergeEnv(layers...)
		source = src
		spec = process.StartSpec{Command: cmd[0], Args: cmd[1:], WorkDir: workDir, Env: merged}

	case req.Command != "":
		workDir, err := r.resolveWorkDir(req.WorkDir)
		if err != nil {
			return spec, nil, nil, err
		}
		prof = r.profiles.Detect(workDir)
		layers := []config.EnvLayer{{Name: r.baseEnvName, Vars: r.baseEnv}}
		if len(r.cfg.Config.Runtime.Env) > 0 {
			layers = append(layers, config.EnvLayer{Name: "runtime.env", Vars: r.cfg.Config.Runtime.Env})
		}
		if req.EnvFile != "" {
			fileEnv, err := config.ParseEnvFile(r.abs(req.EnvFile))
			if err != nil {
				return spec, nil, nil, fmt.Errorf("env_file: %w", err)
			}
			layers = append(layers, config.EnvLayer{Name: "env_file", Vars: fileEnv})
		}
		layers = append(layers, config.EnvLayer{Name: "request.env", Vars: req.Env})
		merged, src := config.MergeEnv(layers...)
		source = src
		spec = process.StartSpec{Command: req.Command, Args: req.Args, WorkDir: workDir, Env: merged}

	default:
		return spec, nil, nil, errors.New("start_process requires either app or command")
	}
	return spec, prof, source, nil
}

// resolveWorkDir resolves a declared workdir against the project root and
// follows symlinks, so uv/poetry walk-ups behave predictably. The error names
// both the resolved path and the declared path for debuggability.
func (r *Runtime) resolveWorkDir(declaredPath string) (string, error) {
	workDir := r.abs(declaredPath)
	target, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", fmt.Errorf("workdir %q: %w (resolved from %q)", workDir, err, declaredPath)
	}
	return target, nil
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
