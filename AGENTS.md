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
cmd/agent-runtime/        main.go — entry point
internal/process/         generic process manager, lifecycle, process-group kill
internal/logs/            bounded ring buffers + querying (Sink interface in sink.go)
internal/logstore/        optional durable SQLite log archive (async writer, retention)
internal/events/          non-blocking pub/sub for lifecycle events
internal/profile/         framework profiles (detection, readiness, defaults)
internal/config/          agent-runtime.yaml parsing + dotenv env files
internal/runtime/         the facade MCP and CLI talk to; waiters live here
internal/mcp/             MCP server: thin tool handlers over the facade
internal/cli/             cobra command tree (root.go), interactive REPL (repl.go), run/shell/integrate helpers
internal/integrate/       agent config generation (claude/codex/gemini/opencode)
internal/integrate/skill/ embedded skills (go:embed): agent-runtime-ready/, agent-runtime-logging/ — installed via integrate skill
internal/integration/     end-to-end tests against real runtimes
pkg/api/                  shared request/response types
```

## CLI

- `agent-runtime serve` — the MCP stdio server (what coding agents launch).
- `agent-runtime` with no arguments — the interactive setup wizard: init
  `agent-runtime.yaml`, install the embedded skills, connect an AI coding agent
  (claude/codex/gemini/opencode), remove installed skills or an agent's MCP
  config, open the process manager, or show project status.
- `agent-runtime repl` — the interactive process manager (REPL). Its commands
  map 1:1 to the MCP tools: `start --app <name>`, `ps`, `apps`, `status`,
  `logs`, `wait`, `waitfor`, `signal`, `stop`, `restart`, `remove`, `env`,
  `send`, `clear`, `shell`, `exit`. Ctrl-C aborts a blocking command or exits
  the session when idle; Ctrl-D exits.
- `agent-runtime run|shell|integrate|version` — one-shot helpers (unchanged).
- `agent-runtime integrate skill --write` — installs the embedded skills
  (agent-runtime-ready, agent-runtime-logging) into Claude Code's
  `.claude/skills` and opencode's `.config/opencode/skills` (global) or
  `.claude/skills` + `.opencode/skills` (project, `--scope project`).

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
- If you change readiness regexes or tool names in `internal/profile` or
  `internal/mcp`, update the embedded skill in
  `internal/integrate/skill/agent-runtime-ready/` to match (SKILL.md readiness
  table, agent-runtime-yaml.md profiles table, agent-workflow.md tool recipes).
  Keep `internal/integrate/skill/agent-runtime-logging/` in sync when log
  structure or readiness-line guidance changes.

## Environment resolution

Every process is started with a **complete merged environment** — the `env`
reported on `start_process` is the final result, never a delta. Layers, lowest
to highest precedence:

1. **Base env** — the captured login-shell environment (`runtime.shell_env:
   login`, the default) or `os.Environ()` (`shell_env: none`, or when the
   capture fails).
2. **`runtime.env`** — a runtime-wide layer applied to every app/process.
3. **`env_file`** — the app's (or the request's) dotenv file.
4. **app `env`** — the app's `env:` block in `agent-runtime.yaml`.
5. **request `env`** — the `env` array passed to `start_process`.

Later layers override earlier ones for the same key. Provenance is tracked per
key and exposed by `get_process_env` (spec mode) via the `source` map.

- `runtime.shell_env` defaults to `login`: the base layer is captured once per
  runtime via `$SHELL -lic 'env -0'` with a ~3s timeout, silently falling back
  to `os.Environ()` on any failure. This is how nvm/pyenv/uv shims and their
  PATH entries reach managed processes. Because the capture is
  environment-dependent, **pin `shell_env: none` in test configs where
  determinism matters** (mirroring `internal/runtime/runtime_test.go`).
- `workdir` (apps and raw starts) is resolved relative to the directory
  containing `agent-runtime.yaml`, then passed through `filepath.EvalSymlinks`
  so uv/poetry walk-ups behave predictably. A missing workdir fails the start
  during resolution, before anything is exec'd or registered.

## Process control & inspection tools

- `signal_process(process_id, signal)` — deliver SIGINT/SIGTERM/SIGHUP/SIGQUIT/
  SIGUSR1/SIGUSR2/SIGKILL to the **whole process group** (so a graceful SIGINT
  reaches a `mycli -> uv -> python` tree, not just the direct child).
- `get_process_env(process_id, live=false, reveal=false)` — the complete env
  the runtime constructed for the process (spec mode, with per-key `source`
  provenance), or the ground-truth env read live from `/proc/<pid>/environ`
  (`live=true`). Secret-like keys (token/password/api_key/...) are redacted to
  `***` by default; pass `reveal=true` only when the raw value is actually
  needed.
- `open_shell(process_id|app|pid, shell)` — start an interactive shell inside the
  resolved workdir + complete env of a running process, configured app, or — with
  `pid=<os-pid>` (Linux only) — ANY process, including ones started in another
  agent-runtime session (its workdir+env are read from `/proc/<pid>/cwd` and
  `/proc/<pid>/environ`). Drive it with `send_stdin` and read it with
  `get_logs`/`wait_for_log`; stop it like any managed process.
