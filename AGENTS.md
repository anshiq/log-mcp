# AGENTS.md

`agent-runtime` is a local, asynchronous process supervisor for AI coding
agents, exposed over the Model Context Protocol (MCP) over stdio.

## Build & test

```bash
make build                 # -> bin/agent-runtime
make vet                   # go vet ./...
make test                  # go test ./...
make race                  # go test -race ./... (must be green)
make fmt                   # gofmt -l -w .
go test ./...
go test -bench=. -benchmem ./internal/logs/   # log-path benchmarks
```

## Repo layout

```
cmd/agent-runtime/        main.go — subcommand dispatch (stdlib flag)
internal/process/         generic process manager, lifecycle, process-group kill
internal/logs/            bounded ring buffers + querying (Sink interface in sink.go)
internal/logstore/        optional durable SQLite log archive (async writer, retention)
internal/events/          non-blocking pub/sub for lifecycle events
internal/profile/         framework profiles (detection, readiness, defaults)
internal/config/          agent-runtime.yaml parsing + dotenv env files
internal/runtime/         the facade MCP and CLI talk to; waiters live here
internal/mcp/             MCP server: thin tool handlers over the facade
internal/cli/             serve / run / integrate / version
internal/integrate/       agent config generation (claude/codex/gemini/opencode)
internal/integration/     end-to-end tests against real runtimes
pkg/api/                  shared request/response types
examples/                 node / nextjs / django / java example apps
```

## Invariants (non-negotiable)

1. `start_process` returns immediately — never blocks on startup or exit.
2. Processes outlive the MCP call: the command binds to the runtime root
   context, never the request context.
3. Output is always consumed asynchronously (per-process stdout/stderr reader
   goroutines); no MCP handler reads a process synchronously.
4. Logs are never pushed to the agent; it asks for them via `get_logs`.
5. Nothing blocks anything else: per-process buffers, stdin, waiters and
   cancellation keep one broken process from affecting another.
6. Per-process isolation: each process owns its buffers, lifecycle, and
   waiters; the registry keeps them independent.

## Development rules

- Run `go vet` and `gofmt` before committing; the race suite (`make race`) must
  be green.
- Keep `internal/logstore` free of imports from `internal/runtime`,
  `internal/process`, `internal/mcp`, or `pkg` — layering is
  `internal/logs ← internal/logstore` only.
- Never send managed-process log lines to the agent automatically.
- Preserve the byte-for-byte merge behavior in `internal/integrate/opencode.go`
  (JSONC installs must not rewrite anything the user didn't ask for).
