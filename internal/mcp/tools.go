package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/pkg/api"
)

// Input types carry jsonschema descriptions for agent-facing tool schemas.

// mcpClient is the identity used for event-subscription ownership. The stdio
// MCP server serves a single agent, so one client id caps subscriptions at
// MaxSubsPerClient across the session.
const mcpClient = "mcp"

const maxStartWaitMS = 600000

func (h *handlers) waitAfterStart(ctx context.Context, res *api.StartResult, timeoutMS int) {
	wait := &api.StartWait{Mode: "ready"}
	lr, err := h.rt.WaitForLog(ctx, api.WaitForLogRequest{ProcessID: res.ProcessID, Ready: true, TimeoutMS: timeoutMS})
	switch {
	case err != nil:
		wait.Mode = "exit"
		er, eerr := h.rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: timeoutMS})
		if eerr == nil {
			wait.Exited, wait.Timeout, wait.ExitCode = er.Exited, er.Timeout, er.ExitCode
		}
	case lr.Matched:
		wait.Ready = true
		if lr.Entry != nil {
			wait.Line = lr.Entry.Line
		}
	default:
		wait.Exited, wait.Timeout = lr.Exited, lr.Timeout
		if lr.Exited {
			if er, eerr := h.rt.WaitForExit(ctx, api.WaitForExitParams{ProcessID: res.ProcessID, TimeoutMS: 1000}); eerr == nil {
				wait.ExitCode = er.ExitCode
			}
		}
	}
	res.Wait = wait
	if st, serr := h.rt.Status(res.ProcessID); serr == nil {
		res.Status = st.Status
	}
}

type startIn struct {
	App       string   `json:"app,omitempty" jsonschema:"Name of an app declared in the project config. Mutually exclusive with command."`
	Command   string   `json:"command,omitempty" jsonschema:"Executable to launch (npm, go, python, java, ...). Mutually exclusive with app."`
	Args      []string `json:"args,omitempty" jsonschema:"Arguments passed to the command."`
	WorkDir   string   `json:"workdir,omitempty" jsonschema:"Working directory, absolute or relative to the project root."`
	Env       []string `json:"env,omitempty" jsonschema:"Extra KEY=VALUE environment pairs for the process."`
	TimeoutMS *int     `json:"timeout_ms,omitempty" jsonschema:"Optional. Omit to return immediately (non-blocking). When set, block up to this many milliseconds (0 means 30000, max 600000): servers with a readiness line wait until ready, one-shot commands wait until exit. The result's wait field says which happened."`
}

type procIDIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
}

type getLogsIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Stream    string `json:"stream,omitempty" jsonschema:"Which stream to read: all (default), stdout or stderr."`
	Lines     int    `json:"lines,omitempty" jsonschema:"Maximum lines to return (default 100, capped at the configured maximum)."`
	Contains  string `json:"contains,omitempty" jsonschema:"Only return lines containing this substring."`
	Level     string `json:"level,omitempty" jsonschema:"Only return lines whose level (parsed from level=... / \"level\":... prefixes) is at this level: debug|info|warn|error. Empty disables the filter."`
}

type clearLogsIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Stream    string `json:"stream,omitempty" jsonschema:"Which buffer to clear: all (default), stdout or stderr."`
}

type sendStdinIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Data      string `json:"data" jsonschema:"Raw bytes to write to the process stdin."`
}

type waitForLogIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Contains  string `json:"contains,omitempty" jsonschema:"Wait for a line containing this substring. One of contains/pattern/ready is required."`
	Pattern   string `json:"pattern,omitempty" jsonschema:"Wait for a line matching this regular expression."`
	Ready     bool   `json:"ready,omitempty" jsonschema:"Wait for the app's readiness line, using the detected profile's readiness patterns."`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"Timeout in milliseconds (default 30000, max 600000)."`
}

type waitForExitIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"Timeout in milliseconds (default 30000, max 600000)."`
}

type removeProcessIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Force     bool   `json:"force,omitempty" jsonschema:"Stop the process first if it is still running, then remove it. Default false (running processes are refused)."`
}

type signalProcessIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Signal    string `json:"signal" jsonschema:"Signal name (SIG prefix optional): SIGINT, SIGTERM, SIGHUP, SIGQUIT, SIGUSR1, SIGUSR2 or SIGKILL. Delivered to the entire process group (mycli -> uv -> python), not just the direct child."`
}

type processEnvIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Live      bool   `json:"live,omitempty" jsonschema:"false (default): the env the runtime constructed (layers merged at start). true: read /proc/<pid>/environ — the ground-truth env, including anything uv/poetry/nvm injected after start. Unix only."`
	Reveal    bool   `json:"reveal,omitempty" jsonschema:"false (default): values of secret-like keys (secret/token/password/key patterns) are masked as ***. true: return raw values."`
}

type openShellIn struct {
	ProcessID string `json:"process_id,omitempty" jsonschema:"Clone the environment+workdir of this running process. Mutually exclusive with app and pid."`
	App       string `json:"app,omitempty" jsonschema:"Clone the resolved environment+workdir of this configured app from the project config. Mutually exclusive with process_id and pid."`
	PID       int    `json:"pid,omitempty" jsonschema:"OS process id: shell into any process's workdir+env via /proc/<pid> (Linux). Mutually exclusive with process_id and app."`
	Shell     string `json:"shell,omitempty" jsonschema:"Shell to launch (absolute path or name). Default $SHELL, then /bin/sh."`
}

type setRestartPolicyIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Policy    string `json:"policy" jsonschema:"Restart policy: never | on-failure | always. on-failure restarts only on non-zero exit; always restarts regardless. Manual stop_process never triggers a restart."`
}

type subscribeEventsIn struct {
	ProcessID string   `json:"process_id,omitempty" jsonschema:"Restrict delivery to a single process; empty subscribes to all processes."`
	Types     []string `json:"types,omitempty" jsonschema:"Restrict to these event types (process.started, process.exited, process.crashed, process.healthy, process.unhealthy, ...); empty subscribes to all."`
	Since     string   `json:"since,omitempty" jsonschema:"Backfill cursor: an event id to receive events after, \"last\" or empty to start fresh from now."`
}

type getEventsIn struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"The subscription_id returned by subscribe_events."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum events to drain; empty or <=0 drains all buffered."`
}

type unsubscribeEventsIn struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"The subscription_id returned by subscribe_events."`
}

type getAuditLogIn struct {
	Lines    int    `json:"lines,omitempty" jsonschema:"Maximum audit lines to return (default 100)."`
	Contains string `json:"contains,omitempty" jsonschema:"Only return lines containing this substring."`
}

func registerTools(server *mcp.Server, h *handlers) {
	mcp.AddTool(server,
		&mcp.Tool{Name: "start_process", Description: "MANDATORY for starting any development process in this project (dev servers, watchers, backends). A process started any other way is invisible to agent-runtime and cannot be monitored. Start by raw {command, args, workdir} or prefer a named app (app=, from the project config). Returns immediately with a process_id unless timeout_ms is set; the process keeps running in the background. With timeout_ms it blocks until the app is ready (servers) or exits (one-shot commands), or the timeout elapses."},
		func(ctx context.Context, req *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, *api.StartResult, error) {
			h.log(ctx, "start_process", in)
			if in.TimeoutMS != nil && (*in.TimeoutMS < 0 || *in.TimeoutMS > maxStartWaitMS) {
				return nil, nil, fmt.Errorf("timeout_ms must be between 0 and %d", maxStartWaitMS)
			}
			res, err := h.rt.Start(ctx, api.StartRequest{
				App: in.App, Command: in.Command, Args: in.Args, WorkDir: in.WorkDir, Env: in.Env,
			})
			if err != nil {
				return nil, nil, err
			}
			if in.TimeoutMS != nil {
				h.waitAfterStart(ctx, res, *in.TimeoutMS)
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "stop_process", Description: "Gracefully stop a process (SIGTERM to its process group, escalating to SIGKILL after the grace period). Idempotent: stopping an already-stopped process is a no-op."},
		func(ctx context.Context, req *mcp.CallToolRequest, in procIDIn) (*mcp.CallToolResult, map[string]string, error) {
			h.log(ctx, "stop_process", in)
			if err := h.rt.Stop(ctx, in.ProcessID); err != nil {
				return nil, nil, err
			}
			return nil, map[string]string{"process_id": in.ProcessID, "status": "stopped"}, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "restart_process", Description: "Stop and re-launch a process with the same start specification. The logical process_id and its log history are preserved."},
		func(ctx context.Context, req *mcp.CallToolRequest, in procIDIn) (*mcp.CallToolResult, *api.StartResult, error) {
			h.log(ctx, "restart_process", in)
			if err := h.rt.Restart(ctx, in.ProcessID); err != nil {
				return nil, nil, err
			}
			st, err := h.rt.Status(in.ProcessID)
			if err != nil {
				return nil, nil, err
			}
			return nil, &api.StartResult{
				ProcessID: st.ProcessID, InstanceID: st.InstanceID, Status: st.Status,
				Profile: st.Profile, Command: st.Command, Args: st.Args, WorkDir: st.WorkDir,
			}, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "process_status", Description: "Use to check a process's health: lifecycle state, pid, exit code, log line counts. The primary way to tell if the app is up or crashed."},
		func(ctx context.Context, req *mcp.CallToolRequest, in procIDIn) (*mcp.CallToolResult, *api.StatusResult, error) {
			h.log(ctx, "process_status", in)
			res, err := h.rt.Status(in.ProcessID)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "list_processes", Description: "List processes including exited and stopped ones. Use it to recover a lost process_id or to find what is running before starting anything new. Scoped to this session's workspace by default; pass scope=all to see every workspace on this machine."},
		func(ctx context.Context, req *mcp.CallToolRequest, in listProcessesIn) (*mcp.CallToolResult, *api.ListResult, error) {
			h.log(ctx, "list_processes", in)
			res, err := listScoped(h, in.Scope)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "get_logs", Description: "THE way to read a process's output. Returns captured stdout/stderr from the bounded buffers for a process started via start_process. Never invoked automatically; call it when you need details, especially after a failure. Use stream and contains to stay small."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getLogsIn) (*mcp.CallToolResult, *api.GetLogsResult, error) {
			h.log(ctx, "get_logs", in)
			res, err := h.rt.GetLogs(api.GetLogsRequest{
				ProcessID: in.ProcessID, Stream: in.Stream, Lines: in.Lines, Contains: in.Contains, Level: in.Level,
			})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "clear_logs", Description: "Clear the selected log buffer(s) of a process."},
		func(ctx context.Context, req *mcp.CallToolRequest, in clearLogsIn) (*mcp.CallToolResult, *api.ClearLogsResult, error) {
			h.log(ctx, "clear_logs", in)
			res, err := h.rt.ClearLogs(in.ProcessID, in.Stream)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "send_stdin", Description: "Write data to a running process's stdin, e.g. \"yes\\n\" or \"q\\n\"."},
		func(ctx context.Context, req *mcp.CallToolRequest, in sendStdinIn) (*mcp.CallToolResult, *api.SendStdinResult, error) {
			h.log(ctx, "send_stdin", procIDIn{ProcessID: in.ProcessID})
			res, err := h.rt.SendStdin(in.ProcessID, in.Data)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "wait_for_log", Description: "Use to confirm startup/readiness: wait until a log line matches (substring, regex, or the app's readiness pattern), the process exits, or the timeout elapses. Matches existing or future lines."},
		func(ctx context.Context, req *mcp.CallToolRequest, in waitForLogIn) (*mcp.CallToolResult, *api.WaitForLogResult, error) {
			h.log(ctx, "wait_for_log", in)
			res, err := h.rt.WaitForLog(ctx, api.WaitForLogRequest{
				ProcessID: in.ProcessID, Contains: in.Contains, Pattern: in.Pattern,
				Ready: in.Ready, TimeoutMS: in.TimeoutMS,
			})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "wait_for_exit", Description: "Wait until the process exits (or times out). Returns the exit code. Multiple callers may wait on the same process independently."},
		func(ctx context.Context, req *mcp.CallToolRequest, in waitForExitIn) (*mcp.CallToolResult, *api.WaitForExitResult, error) {
			h.log(ctx, "wait_for_exit", in)
			res, err := h.rt.WaitForExit(ctx, api.WaitForExitParams{
				ProcessID: in.ProcessID, TimeoutMS: in.TimeoutMS,
			})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "list_apps", Description: "List the apps declared in the project config with their detected profile and start command. Prefer start_process(app=...) over raw commands for reproducibility."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *api.ListAppsResult, error) {
			h.log(ctx, "list_apps", nil)
			res, err := h.rt.Apps()
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "remove_process", Description: "Remove a terminated process from the registry and free its log buffers. Refuses to remove a process that is still running unless force=true, which stops it first. Use to keep long-running daemons bounded by removing exited processes you no longer need."},
		func(ctx context.Context, req *mcp.CallToolRequest, in removeProcessIn) (*mcp.CallToolResult, *api.RemoveProcessResult, error) {
			h.log(ctx, "remove_process", in)
			res, err := h.rt.RemoveProcess(in.ProcessID, in.Force)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "signal_process", Description: "Forward a signal to a managed process's whole process group, e.g. SIGINT for Ctrl+C semantics or SIGHUP for config reloads. Unlike stop_process (SIGTERM then SIGKILL) this sends exactly the requested signal once. Unknown signal names are rejected."},
		func(ctx context.Context, req *mcp.CallToolRequest, in signalProcessIn) (*mcp.CallToolResult, *api.SignalProcessResult, error) {
			h.log(ctx, "signal_process", in)
			res, err := h.rt.SignalProcess(ctx, api.SignalProcessRequest{ProcessID: in.ProcessID, Signal: in.Signal})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "get_process_env", Description: "Inspect the environment a process runs with. Use to answer 'what PATH/nvm/venv is this process using?' or to debug why a toolchain resolved differently than expected. Redaction is on by default — pass reveal=true only when you need the actual secret values."},
		func(ctx context.Context, req *mcp.CallToolRequest, in processEnvIn) (*mcp.CallToolResult, *api.ProcessEnvResult, error) {
			h.log(ctx, "get_process_env", in)
			res, err := h.rt.ProcessEnv(api.ProcessEnvRequest{ProcessID: in.ProcessID, Live: in.Live, Reveal: in.Reveal})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "open_shell", Description: "Start an interactive shell INSIDE the environment of a process, app, or any process on the machine by OS pid: same resolved workdir and environment (PATH, venv, nvm shims, config env). Returns a process_id you then drive with send_stdin / get_logs / wait_for_log — an ssh-into-the-app experience over MCP, fully logged and isolated. e.g. open_shell(app='api'), then send_stdin('uv run python -c ...\\n'), then get_logs. Pass pid=<os-pid> (Linux only) to attach to a process started in another session, e.g. open_shell(pid=12345). Stop it with stop_process when done."},
		func(ctx context.Context, req *mcp.CallToolRequest, in openShellIn) (*mcp.CallToolResult, *api.StartResult, error) {
			h.log(ctx, "open_shell", in)
			res, err := h.rt.OpenShell(ctx, api.OpenShellRequest{ProcessID: in.ProcessID, App: in.App, PID: in.PID, Shell: in.Shell})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "set_restart_policy", Description: "Set the auto-restart policy of an existing process. Use for ad-hoc processes started without an app entry so supervision (auto-restart on crash/health failure) applies. Policies: never (default), on-failure (restart only on non-zero exit), always (restart regardless of exit code). Manual stop_process never triggers a restart."},
		func(ctx context.Context, req *mcp.CallToolRequest, in setRestartPolicyIn) (*mcp.CallToolResult, *api.SetRestartPolicyResult, error) {
			h.log(ctx, "set_restart_policy", in)
			res, err := h.rt.SetRestartPolicy(api.SetRestartPolicyRequest{ProcessID: in.ProcessID, Policy: in.Policy})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "subscribe_events", Description: "Open a capped event subscription. Lifecycle events (process.started, process.exited, process.crashed, process.healthy, process.unhealthy, ...) are never pushed to you; you pull them with get_events. Filter by process_id and/or types; pass since=<event id> to backfill. Hard caps: 8 subscriptions per client, 256 buffered events each (overruns are counted in get_events.dropped)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in subscribeEventsIn) (*mcp.CallToolResult, *api.SubscribeEventsResult, error) {
			h.log(ctx, "subscribe_events", in)
			res, err := h.rt.SubscribeEvents(mcpClient, api.SubscribeEventsRequest{ProcessID: in.ProcessID, Types: in.Types, Since: in.Since})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "get_events", Description: "Drain buffered events from a subscription. Returns the events since the previous get_events plus the dropped count (events lost because the 256-event ring overran while you weren't draining). Returns immediately."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getEventsIn) (*mcp.CallToolResult, *api.GetEventsResult, error) {
			h.log(ctx, "get_events", in)
			res, err := h.rt.GetEvents(api.GetEventsRequest{SubscriptionID: in.SubscriptionID, Limit: in.Limit})
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "unsubscribe_events", Description: "Close an event subscription and release its ring buffer."},
		func(ctx context.Context, req *mcp.CallToolRequest, in unsubscribeEventsIn) (*mcp.CallToolResult, *api.UnsubscribeEventsResult, error) {
			h.log(ctx, "unsubscribe_events", in)
			res, err := h.rt.UnsubscribeEvents(mcpClient, in.SubscriptionID)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "runtime_stats", Description: "Snapshot of the runtime's own health: process counts by status, per-process log line counts, and whether the log pipeline is keeping up (durable archive drops, forwarder drops, event-subscription drops) plus uptime."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *api.RuntimeStats, error) {
			h.log(ctx, "runtime_stats", nil)
			res, err := h.rt.RuntimeStats()
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{Name: "get_audit_log", Description: "Return the tail of the audit log (JSONL of tool calls with secrets redacted), bounded and pull-based like get_logs. Empty when no audit log is configured."},
		func(ctx context.Context, req *mcp.CallToolRequest, in getAuditLogIn) (*mcp.CallToolResult, *api.AuditLogResult, error) {
			h.log(ctx, "get_audit_log", in)
			res, err := h.rt.GetAuditLog(in.Lines, in.Contains)
			if err != nil {
				return nil, nil, err
			}
			return nil, res, nil
		})
}
