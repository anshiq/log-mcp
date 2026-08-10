# agent-runtime architecture

`agent-runtime` is a local, asynchronous process supervisor exposed to AI coding
agents over MCP/stdio. This document describes the layout, the key invariants,
and how the pieces fit together.

## Goals

- Agents (Claude Code, Codex, Gemini CLI, Cursor, ...) can start, monitor,
  query, restart and stop development processes without flooding their context.
- Multiple application types are supported through **profiles** (spring-boot,
  nextjs, django, node, python, go) — the process manager itself stays generic.
- The runtime is the single source of truth for managed processes. MCP is only
  an interface to it.

## Repository layout

```
cmd/agent-runtime/        main.go — subcommand dispatch (stdlib flag)
internal/process/         generic process manager, lifecycle, process-group kill
internal/logs/            bounded ring buffers + querying
internal/events/          non-blocking pub/sub for lifecycle events
internal/profile/         framework profiles (detection, readiness, defaults)
internal/config/          agent-runtime.yaml parsing + dotenv env files
internal/runtime/         the facade MCP and CLI talk to; waiters live here
internal/mcp/             MCP server: thin tool handlers over the facade
internal/cli/             serve / run / integrate / version
internal/integrate/       agent config generation (claude/codex/gemini)
internal/integration/     end-to-end tests against real runtimes
pkg/api/                  shared request/response types
examples/                 node / nextjs / django / java example apps
```

## Invariants (non-negotiable)

1. **Start returns immediately.** `start_process` launches the child, spawns the
   stdout/stderr readers and a wait goroutine, and returns a `process_id`.
2. **Processes outlive the MCP call.** The child's command is bound to the
   runtime's *root* context, never the request context.
3. **Output is always consumed asynchronously.** Two independent reader
   goroutines feed two per-process ring buffers (stdout and stderr stay
   separate). No MCP handler ever reads a process synchronously.
4. **Logs are never pushed to the agent.** The agent explicitly requests them
   via `get_logs`.
5. **Nothing blocks anything else.** Every process has its own buffers, stdin
   writer, lifecycle goroutine, waiters, cancellation, and status. A broken
   process cannot affect another; waiters and MCP requests are cancellable.

## Runtime flow

```
MCP tool handler                    (internal/mcp/tools.go — thin)
      │
      ▼
Runtime facade                      (internal/runtime/runtime.go)
      │   resolves spec/app/profile, enforces caps, runs waiters
      ▼
Process Manager                     (internal/process/manager.go)
      │   registry, Start/Stop/Restart, concurrency-safe
      ▼
ManagedProcess                      (per logical process_id)
      ├── stdout reader goroutine ──► logs.ProcessLogs.stdout (ring buffer)
      ├── stderr reader goroutine ──► logs.ProcessLogs.stderr (ring buffer)
      └── wait goroutine ──► cmd.Wait() ──► status/exit code ──► events.Bus
```

### Process identity

- `process_id` (`proc_<hex>`) is **logical and stable** across restarts. Logs
  accumulate across instances.
- `instance_id` (`run_<hex>`) changes on every start/restart.
- An *instance log floor* (`entryFrom`) marks the first log entry ID belonging
  to the current instance, so `wait_for_log` after a restart never matches the
  previous instance's output.

### Process-group termination (Unix)

Each child is started with `SysProcAttr{Setpgid: true}`, making it the leader of
its own process group. `stop_process` sends `SIGTERM` to the whole group
(`kill(-pgid, TERM)`), waits up to the grace period, then escalates to
`SIGKILL`. This kills `npm → next-server`, `mvnw → java`, `nodemon → node`
trees together. On Windows (build tag `procattr_windows.go`) only the direct
child is signalled — documented limitation.

### Log buffers

- Default 10,000 lines per stream per process (configurable).
- `logs.Entry{ID, Timestamp, Stream, Line}` with a single monotonic ID counter
  per process shared across stdout/stderr, enabling chronological merge and
  `Since`/`After` scans for waiters.
- Reader uses `bufio.Reader.ReadString('\n')`, accumulates partial lines, and
  caps an unterminated line at 256 KiB to avoid unbounded growth.

### Waiters

`wait_for_log` is a condition-variable loop over the ring buffer:

1. Subscribe to the process's log wakeup (buffered-1 channel per waiter).
2. Scan `From(entryFrom)` for a match.
3. Else `select` on wakeup / process-done / ctx / timeout, then rescan.

Because the buffer is the source of truth, dropped wakeups are harmless — a
missed ping just means the next scan catches up. Multiple independent waiters
and cancellation are supported. `wait_for_exit` is a `select` on the instance's
`done` channel.

### MCP

Built on the official `github.com/modelcontextprotocol/go-sdk` (`mcp.NewServer`,
typed `mcp.AddTool`, `StdioTransport`). Tool handlers contain no process logic;
they translate params, call the facade, and return structured results. The
server ships an `Instructions` string that every agent surfaces to the model,
teaching the intended workflow (start → wait_for_log(ready) → get_logs on
failure → restart).

## Profiles

`internal/profile` defines detection rules (file + optional substring + weight),
readiness regexes, default start commands, and shutdown grace per framework.
`Registry.Detect(workdir)` scores rules and picks the best match, falling back to
`generic`. Profiles *inform* the manager (which command to run, what "ready"
means) but never contain lifecycle logic. Detection only — no reliance on the
framework being installed.

## Multi-agent integration

MCP-over-stdio is the common denominator. `internal/integrate` renders the
exact config block per agent:

- **Claude Code** — `.mcp.json` (project-scoped), JSON-merged on `--write`.
- **Codex** — `~/.codex/config.toml` `[mcp_servers.agent-runtime]`, appended if
  absent.
- **Gemini CLI** — `~/.gemini/settings.json`, JSON-merged.
- **generic** — prints the stdio command line for any MCP client.

`--write` is opt-in; the default is print-only (nothing outside the project is
touched without an explicit flag).

## Shutdown

On `SIGINT`/`SIGTERM` or agent disconnect: the MCP transport closes, the runtime
cancels its root context, and the manager stops every process (SIGTERM group →
bounded grace → SIGKILL group). Never hangs waiting on a child.

## Security boundary

Arbitrary command execution is the core feature, so: stdio-only transport, no
HTTP listener, `exec.Command` with argument arrays (no shell), workdir
validation, environment never logged/exposed, per-process log buffers never
auto-injected into agent context.

## Future (explicitly out of scope for v1)

- Recursive process-tree walking and detached-process tracking.
- Windows job-object termination.
- Persistent event/log store (SQLite).
- Streamable HTTP / remote transport.
- Background tasks, schedulers, watchers as first-class runtime entities.
- Regex/custom filtering beyond `contains`.
