# agent-runtime v3 API reference

Versioned contract (`agentruntime.v1`) defined in `api/proto` and served
with Connect-compatible RPC over the Unix socket
(`$XDG_RUNTIME_DIR/agent-runtime/agentd.sock`), and optionally over
authenticated loopback TCP for the web UI.

One schema produces the SDKs: `buf generate` emits the Go bindings
(`gen/agentruntime/...`, incl. connect-go clients) and the TypeScript
bindings (`ui/src/gen`). Generated code is committed; `make proto`
(checks generate+lint+breaking) is CI-gated. The daemon serves
Connect-compatible paths with JSON bodies today; the Connect-protocol
transport (headers/h2) is a swap that keeps these paths.

Every endpoint is `POST /api/agentruntime.v1.<Service>/<Method>` with a
JSON body. Debug with curl:

```sh
curl --unix-socket "$XDG_RUNTIME_DIR/agent-runtime/agentd.sock" \
  -H 'Content-Type: application/json' -d '{}' \
  http://agentd/agentruntime.v1.SystemService/GetVersion
```

Streams (TailLogs, WatchProcesses, WatchEvents, WatchConfig) are
newline-delimited JSON: a snapshot first, then deltas, each with an opaque
`cursor`. Reconnect with `resume_cursor`; a 15s `heartbeat` keeps proxies
alive; slow consumers get `Gap{dropped}` instead of backpressure (§5.4).

## SystemService

| RPC | Notes |
|---|---|
| GetVersion | `{daemonVersion, apiVersion, minClientVersion, shimProtocol}`; clients below minimum get an upgrade error |
| Health | liveness + uptime |
| GetStats | projects, processes (loaded runtimes), sessions |
| GetSettings / UpdateSettings | `log_level`, `idle_exit`, … |
| Shutdown | `{keepProcesses}` (default true): drain; false stops everything first |

## ProjectService

Resolve (path → project/workspace via the locator chain), ListProjects,
GetProject, UpdateProject (rename), ListWorkspaces, LinkWorkspace,
ForgetProject (soft delete, 30-day trash), GC (missing paths).

## ConfigService

GetConfig (raw layers + resolved apps + provenance + config path),
GetSchema (JSON Schema for editors), Validate (line/col errors),
Plan (terraform-style diff: added/removed/restart-required/applied-live),
Apply (`base_revision` optimistic concurrency → `stale_revision` on
conflict), ListRevisions, GetRevision, Rollback, WatchConfig (stream).

## ProcessService

Start (returns immediately), Stop, Restart, Signal, SendStdin, Remove,
Get, List (`workspaceId` or `allWorkspaces`), WatchProcesses (stream),
WaitForExit, SetRestartPolicy, GetEnv (redacted unless policy allows),
OpenShell (defaults `lifetime: session`), Attach (server-stream output +
SendStdin input; full bidi with codegen), GetResourceUsage,
WatchResourceUsage.

## LogService

GetLogs (MCP-compatible tail; `source` may be `segments`), SearchLogs
(FTS over the workspace index + bounded ring scan; `truncatedScan` flag),
TailLogs (stream: backlog + live), WaitForLog, ClearLogs, ExportLogs
(stream chunks), GetLogStats (index lag).

## EventService

WatchEvents (stream, `since` cursor), ListEvents (paged DB history),
Subscribe/Drain/Unsubscribe (MCP-compat pull model).

## SessionService

Register, Heartbeat (stream held for the session lifetime; drop = close),
Ping (unary heartbeat for bridges), ListSessions, GetSession, Close.

## IntegrationService

ListHarnesses (detection + install state), PreviewInstall (diff first),
InstallMCP/RemoveMCP (atomic + `.bak`), ListSkills, InstallSkills,
RemoveSkills, CheckUpdates.

## AuditService

ListAudit (filterable, newest-first, secrets redacted).

## Errors

HTTP status + `{"error", "code"}`; structured details
(`policy_denied` + rule, `stale_revision`, `not_found`)
travel as `ErrorInfo` with codegen. Handshake header on every call:
`X-Agent-Runtime-Client: <kind>/<version>`.

## Casing

Requests accept camelCase; snake_case aliases are accepted for
MCP compatibility (`process_id` ↔ `processId`, `timeout_ms` ↔ `timeoutMs`,
`backlog_lines` ↔ `backlog`). Some responses remain snake_case for
MCP compatibility (`process_id` in List/Get, `cpuNanos`); the web UI
normalises to camelCase (`id`, `cpuPercent`). New fields are camelCase.
