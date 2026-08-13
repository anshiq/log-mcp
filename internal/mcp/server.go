// Package mcp exposes the runtime to AI coding agents over the Model Context
// Protocol. Tool handlers are thin adapters: all process-management logic lives
// in the runtime facade, never here.
package mcp

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"agent-runtime/internal/runtime"
)

// Version is the MCP server version reported during initialization. It is a
// var so release builds can stamp the git tag via -ldflags
// (-X agent-runtime/internal/mcp.Version={{.Version}}); local builds keep the
// default.
var Version = "0.1.0"

// Instructions is surfaced to every connected agent (Claude Code, Codex,
// Gemini CLI, ...). It is deliberately imperative: it is the agent's authority
// for how this project runs and monitors development processes.
const Instructions = `agent-runtime is the SINGLE source of truth for running and monitoring development processes in this project. Follow these rules strictly.

RULES (mandatory):
1. ALWAYS start development processes through start_process. NEVER launch a dev server, watcher, or backend with your own shell or another tool (e.g. npm run dev, next dev, mvnw spring-boot:run, python manage.py runserver, go run, nodemon). A process started any other way is invisible to agent-runtime: no logs, no lifecycle control, no restart.
2. start_process returns a process_id. Treat it as the only handle to that process. All monitoring for that process goes through THIS server using that id.
3. Whenever you need to check health, status, or errors, ask THIS server — do not re-run the app to observe it:
   - process_status(process_id) for lifecycle state, pid, and exit code.
   - get_logs(process_id, stream="stderr", lines=100) for error output; use stream and contains to stay small.
   - wait_for_log(process_id, ready=true) to confirm the app actually came up.
4. Prefer named apps when available: start_process(app="...") for apps in agent-runtime.yaml. list_apps shows them; list_processes recovers every process_id this runtime holds, including exited ones.
5. On failure, get the tail from get_logs, fix the cause, then restart_process(process_id) and wait_for_log(process_id, ready=true) again. Never rebuild the process by hand.
6. If you ever lose a process_id, recover it with list_processes instead of restarting the app yourself.

Logs are never pushed to you. get_logs is bounded: lines default to 100 and are capped, and responses are truncated at a byte cap. send_stdin writes to a process's stdin; clear_logs empties a buffer; stop_process sends SIGTERM to the whole process group (then SIGKILL after the grace period).`

// NewServer builds the MCP server wired to a runtime.
func NewServer(rt *runtime.Runtime, logger *slog.Logger) *mcp.Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := mcp.NewServer(
		&mcp.Implementation{Name: "agent-runtime", Version: Version},
		&mcp.ServerOptions{Instructions: Instructions},
	)
	registerTools(server, &handlers{rt: rt, logger: logger})
	return server
}

// handlers bundles the runtime access shared by all tool handlers.
type handlers struct {
	rt     *runtime.Runtime
	logger *slog.Logger
}

// hlog logs a tool call with its runtime context.
func (h *handlers) log(ctx context.Context, tool string, args any) {
	h.logger.Debug("tool call", "tool", tool, "args", args)
}
