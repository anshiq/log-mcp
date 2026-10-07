// Package api defines the request/response types shared by the MCP server and
// the direct CLI. MCP tools and CLI commands both translate to and from these
// structs; neither contains process-management logic.
package api

// StartRequest is the input to start_process / `agent-runtime run`.
//
// Either App or Command must be set. Command takes a raw start specification;
// App resolves through the project's central config (profile defaults,
// workdir, env_file) and may be combined with overrides.
type StartRequest struct {
	App     string   `json:"app,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	WorkDir string   `json:"workdir,omitempty"`
	Env     []string `json:"env,omitempty"`
	EnvFile string   `json:"env_file,omitempty"` // dotenv file applied to this start, resolved relative to the project root; layered below env
}

// StartResult is returned immediately after the process is launched.
type StartResult struct {
	ProcessID  string   `json:"process_id"`
	InstanceID string   `json:"instance_id"`
	Status     string   `json:"status"`
	Profile    string   `json:"profile,omitempty"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	WorkDir    string   `json:"workdir"`
	// Readiness lists the armed readiness regex patterns for this process
	// (empty when the process has no readiness gating).
	Readiness []string `json:"readiness,omitempty"`
}

// StatusResult is a snapshot of a process for process_status.
type StatusResult struct {
	ProcessID   string   `json:"process_id"`
	InstanceID  string   `json:"instance_id,omitempty"`
	Status      string   `json:"status"`
	PID         int      `json:"pid,omitempty"`
	Command     string   `json:"command"`
	Args        []string `json:"args,omitempty"`
	WorkDir     string   `json:"workdir"`
	Profile     string   `json:"profile,omitempty"`
	StartedAt   string   `json:"started_at,omitempty"`
	ExitedAt    *string  `json:"exited_at,omitempty"`
	ExitCode    *int     `json:"exit_code,omitempty"`
	Restarts    int      `json:"restarts"`
	StdoutLines int      `json:"stdout_lines"`
	StderrLines int      `json:"stderr_lines"`
	PGID        int      `json:"pgid,omitempty"` // process group id; on Unix processes are started with Setpgid so pgid == pid, the group is what stop/signal target
	// Health is the supervision health state: "unknown" | "healthy" | "unhealthy".
	Health              string `json:"health,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures,omitempty"`
	// BackoffState is a human-readable view of the restart budget, e.g.
	// "restarted=3 within window". Omitted when no restarts have occurred.
	BackoffState string `json:"backoff_state,omitempty"`
	// RestartPolicy is the effective policy: "never" | "on-failure" | "always".
	RestartPolicy string `json:"restart_policy,omitempty"`
	// MemoryBytes and CPUUsageNanos report live resource usage from the
	// process's cgroup v2 slice (Phase 7), when one is configured.
	MemoryBytes   int64 `json:"memory_bytes,omitempty"`
	CPUUsageNanos int64 `json:"cpu_usage_nanos,omitempty"`
}

// SetRestartPolicyRequest is the input to set_restart_policy.
type SetRestartPolicyRequest struct {
	ProcessID string `json:"process_id"`
	Policy    string `json:"policy"` // never | on-failure | always
}

// SetRestartPolicyResult is the response of set_restart_policy.
type SetRestartPolicyResult struct {
	ProcessID     string `json:"process_id"`
	RestartPolicy string `json:"restart_policy"`
}

// ProcessSummary is one row of list_processes.
type ProcessSummary struct {
	ProcessID  string `json:"process_id"`
	InstanceID string `json:"instance_id,omitempty"`
	Status     string `json:"status"`
	Command    string `json:"command"`
	Profile    string `json:"profile,omitempty"`
}

// ListResult is the response of list_processes.
type ListResult struct {
	Processes []ProcessSummary `json:"processes"`
}

// GetLogsRequest is the input to get_logs.
type GetLogsRequest struct {
	ProcessID string `json:"process_id"`
	Stream    string `json:"stream,omitempty"` // "all" | "stdout" | "stderr"
	Lines     int    `json:"lines,omitempty"`  // default 100, capped at the configured maximum
	Contains  string `json:"contains,omitempty"`
	// Level filters lines by a cheap level heuristic parsed from `level=...` /
	// `"level":"..."` prefixes: debug|info|warn|error. Empty = no filter.
	Level string `json:"level,omitempty"`
}

// LogEntry is one log line returned to callers.
type LogEntry struct {
	ID        uint64 `json:"id,omitempty"`
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream"`
	Line      string `json:"line"`
}

// GetLogsResult is the response of get_logs.
type GetLogsResult struct {
	ProcessID      string     `json:"process_id"`
	InstanceID     string     `json:"instance_id,omitempty"`
	Entries        []LogEntry `json:"entries"`
	Truncated      bool       `json:"truncated"`
	ReturnedLines  int        `json:"returned_lines"`
	AvailableLines int        `json:"available_lines"`
	Source         string     `json:"source,omitempty"` // "memory" (default) or "memory+db" when back-filled from the archive
}

// ClearLogsResult is the response of clear_logs.
type ClearLogsResult struct {
	ProcessID string `json:"process_id"`
	Cleared   bool   `json:"cleared"`
	Removed   int    `json:"removed,omitempty"`
}

// SendStdinResult is the response of send_stdin.
type SendStdinResult struct {
	Written int `json:"written_bytes"`
}

// RemoveProcessRequest is the input to remove_process.
type RemoveProcessRequest struct {
	ProcessID string `json:"process_id"`
	Force     bool   `json:"force,omitempty"` // stop a running process before removing it
}

// RemoveProcessResult is the response of remove_process.
type RemoveProcessResult struct {
	ProcessID string `json:"process_id"`
	Removed   bool   `json:"removed"`
}

// WaitForLogRequest is the input to wait_for_log. Exactly one matcher must be
// set: Contains (substring), Pattern (regex), or Ready (use the detected
// profile's readiness patterns).
type WaitForLogRequest struct {
	ProcessID string `json:"process_id"`
	Contains  string `json:"contains,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Ready     bool   `json:"ready,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// WaitForLogResult is the response of wait_for_log.
type WaitForLogResult struct {
	Matched bool      `json:"matched"`
	Timeout bool      `json:"timeout,omitempty"`
	Exited  bool      `json:"exited,omitempty"`
	Entry   *LogEntry `json:"entry,omitempty"`
}

// WaitForExitRequest is the input to wait_for_exit.
type WaitForExitParams struct {
	ProcessID string `json:"process_id"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// WaitForExitResult is the response of wait_for_exit.
type WaitForExitResult struct {
	Exited   bool `json:"exited"`
	Timeout  bool `json:"timeout,omitempty"`
	ExitCode *int `json:"exit_code,omitempty"`
}

// AppInfo describes a configured app for list_apps.
type AppInfo struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	WorkDir    string   `json:"workdir"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	Configured bool     `json:"configured"`
}

// ListAppsResult is the response of list_apps.
type ListAppsResult struct {
	Apps []AppInfo `json:"apps"`
}

// SignalProcessRequest is the input to signal_process.
type SignalProcessRequest struct {
	ProcessID string `json:"process_id"`
	Signal    string `json:"signal"` // SIGINT | SIGTERM | SIGHUP | SIGQUIT | SIGUSR1 | SIGUSR2 | SIGKILL
}

// SignalProcessResult is the response of signal_process.
type SignalProcessResult struct {
	ProcessID string `json:"process_id"`
	Signal    string `json:"signal"`
}

// ProcessEnvRequest is the input to get_process_env.
type ProcessEnvRequest struct {
	ProcessID string `json:"process_id"`
	Live      bool   `json:"live,omitempty"`   // true: read /proc/<pid>/environ (ground truth); false: env the runtime constructed
	Reveal    bool   `json:"reveal,omitempty"` // true: show secret values; false: redact keys matching secret/token/password/key patterns
}

// ProcessEnvResult is the response of get_process_env.
type ProcessEnvResult struct {
	ProcessID string            `json:"process_id"`
	Live      bool              `json:"live"`
	PID       int               `json:"pid,omitempty"`
	Env       []string          `json:"env"`
	Source    map[string]string `json:"source,omitempty"`   // key -> provenance layer name (spec mode only)
	Redacted  int               `json:"redacted,omitempty"` // number of values masked
}

// OpenShellRequest is the input to open_shell: start an interactive shell
// inside the environment (resolved workdir + env) of a running process, a
// configured app, or any process on the machine by OS pid. Exactly one of
// ProcessID, App or PID must be set.
type OpenShellRequest struct {
	ProcessID string `json:"process_id,omitempty"`
	App       string `json:"app,omitempty"`
	PID       int    `json:"pid,omitempty"`   // OS process id: start the shell inside the workdir+environment of ANY process (read from /proc/<pid>/cwd and /proc/<pid>/environ). Mutually exclusive with process_id and app. Linux only.
	Shell     string `json:"shell,omitempty"` // absolute path or name; default $SHELL, then /bin/sh
}
