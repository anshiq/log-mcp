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
var Version = "0.4.0"

// Instructions is surfaced to every connected agent (Claude Code, Codex,
// Gemini CLI, ...). It is deliberately imperative: it is the agent's authority
// for how this project runs and monitors development processes.
const Instructions = `agent-runtime is the SINGLE source of truth for running and monitoring development processes in this project. Follow these rules strictly.

RULES (mandatory):
1. ALWAYS start development processes through start_process. NEVER launch a dev server, watcher, or backend with your own shell or another tool (e.g. npm run dev, next dev, mvnw spring-boot:run, python manage.py runserver, go run, nodemon). A process started any other way is invisible to agent-runtime: no logs, no lifecycle control, no restart.
2. start_process returns a process_id. Treat it as the only handle to that process. All monitoring for that process goes through THIS server using that id.
3. Whenever you need to check health, status, or errors, ask THIS server — do not re-run the app to observe it:
   - process_status(process_id) for lifecycle state, pid, exit code, and supervision health (healthy/unhealthy) + restart policy.
   - get_logs(process_id, stream="stderr", lines=100) for error output; use stream and contains to stay small.
   - wait_for_log(process_id, ready=true) to confirm the app actually came up.
4. Prefer named apps when available: start_process(app="...") for apps in agent-runtime.yaml. list_apps shows them; list_processes recovers every process_id this runtime holds, including exited ones.
5. On failure, get the tail from get_logs, fix the cause, then restart_process(process_id) and wait_for_log(process_id, ready=true) again. Never rebuild the process by hand.
6. If you ever lose a process_id, recover it with list_processes instead of restarting the app yourself.

PERSISTENCE (v3 daemon):
- Processes OUTLIVE your session. When you connect, list_processes shows what earlier sessions started — re-attach (get_logs, process_status) instead of re-starting. Starting a duplicate dev server on the same port helps nobody.
- list_processes defaults to your workspace; scope=all spans the machine.
- list_sessions shows which other agents/UIs are attached here.
- get_project_info tells you where the project YAML lives now (central store, not the repo) and who else is watching. Config edits hot-reload; validate_config/plan_config/apply_config edit it safely, or edit the file at the returned configPath directly.

SUPERVISION (continuous, not request-driven):
- Apps declared in agent-runtime.yaml can configure readiness overrides, a health_check (HTTP/TCP probe), and a restart policy (never|on-failure|always) with exponential backoff. agent-runtime notices crashes and health failures on its own — you do not need to poll.
- A process that fails its health check repeatedly, or crashes per its restart policy, is auto-restarted with visible backoff. Exhausting the restart budget marks it crashed (process_status shows status="crashed"). Manual stop_process never triggers a restart.
- For an ad-hoc process started without an app entry, use set_restart_policy(process_id, policy) to opt into auto-restart.

EVENTS (pull-based, never pushed):
- Lifecycle events are NOT pushed to you. Use subscribe_events(process_id?, types?, since?) to open a capped subscription (8 per client, 256 buffered each), then get_events(subscription_id) to drain them. Subscribe to types like process.crashed, process.exited, process.healthy, process.unhealthy instead of polling process_status. Unsubscribe when done.
- runtime_stats reports whether the log pipeline is keeping up (archive/forwarder/subscription drops) and per-status process counts.

SECURITY & AUDIT:
- In restricted mode (runtime.security.mode: restricted), start_process commands and workdirs are confined, open_shell(pid=) and send_stdin may be disabled, and get_process_env(reveal=true) is a policy error. Denials return structured policy_denied errors.
- Tool calls are recorded to .agent-runtime/audit.log (secrets redacted). get_audit_log reads its tail.

Logs are never pushed to you. get_logs is bounded: lines default to 100 and are capped, and responses are truncated at a byte cap. Lifecycle events are pulled via get_events, never broadcast. send_stdin writes to a process's stdin; clear_logs empties a buffer; stop_process sends SIGTERM to the whole process group (then SIGKILL after the grace period).`

// NewServer builds the MCP server wired to a runtime facade. The facade is
// either a local *runtime.Runtime (session-scoped, the default) or a *Bridge
// (thin client of the per-user daemon); the handlers are identical for both
// because all process-management logic lives behind the Facade interface.
// v3 tools (get_project_info, validate/plan/apply_config, search_logs,
// list_sessions) are registered alongside; they require the daemon and
// return a descriptive error in embedded mode.
func NewServer(rt runtime.Facade, logger *slog.Logger) *mcp.Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := mcp.NewServer(
		&mcp.Implementation{Name: "agent-runtime", Version: Version},
		&mcp.ServerOptions{Instructions: Instructions},
	)
	h := &handlers{rt: rt, logger: logger}
	registerTools(server, h)
	registerV3Tools(server, h)
	return server
}

// handlers bundles the runtime access shared by all tool handlers.
type handlers struct {
	rt     runtime.Facade
	logger *slog.Logger
}

// hlog logs a tool call with its runtime context.
func (h *handlers) log(ctx context.Context, tool string, args any) {
	h.logger.Debug("tool call", "tool", tool, "args", args)
}
