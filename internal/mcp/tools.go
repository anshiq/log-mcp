package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/pkg/api"
)

// Input types carry jsonschema descriptions for agent-facing tool schemas.

type startIn struct {
	App     string   `json:"app,omitempty" jsonschema:"Name of an app declared in agent-runtime.yaml. Mutually exclusive with command."`
	Command string   `json:"command,omitempty" jsonschema:"Executable to launch (npm, go, python, java, ...). Mutually exclusive with app."`
	Args    []string `json:"args,omitempty" jsonschema:"Arguments passed to the command."`
	WorkDir string   `json:"workdir,omitempty" jsonschema:"Working directory, absolute or relative to the project root."`
	Env     []string `json:"env,omitempty" jsonschema:"Extra KEY=VALUE environment pairs for the process."`
}

type procIDIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
}

type getLogsIn struct {
	ProcessID string `json:"process_id" jsonschema:"The process_id returned by start_process."`
	Stream    string `json:"stream,omitempty" jsonschema:"Which stream to read: all (default), stdout or stderr."`
	Lines     int    `json:"lines,omitempty" jsonschema:"Maximum lines to return (default 100, capped at the configured maximum)."`
	Contains  string `json:"contains,omitempty" jsonschema:"Only return lines containing this substring."`
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

func registerTools(server *mcp.Server, h *handlers) {
	mcp.AddTool(server,
		&mcp.Tool{Name: "start_process", Description: "MANDATORY for starting any development process in this project (dev servers, watchers, backends). A process started any other way is invisible to agent-runtime and cannot be monitored. Start by raw {command, args, workdir} or prefer a named app (app=, from agent-runtime.yaml). Returns immediately with a process_id; the process keeps running in the background."},
		func(ctx context.Context, req *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, *api.StartResult, error) {
			h.log(ctx, "start_process", in)
			res, err := h.rt.Start(ctx, api.StartRequest{
				App: in.App, Command: in.Command, Args: in.Args, WorkDir: in.WorkDir, Env: in.Env,
			})
			if err != nil {
				return nil, nil, err
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
		&mcp.Tool{Name: "list_processes", Description: "List every process THIS runtime manages, including exited and stopped ones. Use it to recover a lost process_id or to find what is running before starting anything new."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *api.ListResult, error) {
			h.log(ctx, "list_processes", nil)
			res, err := h.rt.List()
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
				ProcessID: in.ProcessID, Stream: in.Stream, Lines: in.Lines, Contains: in.Contains,
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
		&mcp.Tool{Name: "list_apps", Description: "List the apps declared in agent-runtime.yaml with their detected profile and start command. Prefer start_process(app=...) over raw commands for reproducibility."},
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
}
