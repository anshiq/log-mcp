# agent-runtime

A **local, asynchronous process supervisor** for AI coding agents. It runs your
development processes (Next.js, Spring Boot, Django, Node, Go, Python, ...),
captures their output into bounded in-memory logs, and exposes everything to the
agent over the **Model Context Protocol (MCP)** over stdio.

The agent decides what to look at: logs are never pushed into its context
automatically. It asks for the tail it needs via `get_logs`.

Works with any MCP-over-stdio agent — **Claude Code**, **Codex**, **Gemini CLI**,
**opencode**, Cursor, and more.

```
Agent (Claude / Codex / ...)
   │  MCP over stdio
   ▼
agent-runtime ──► process manager ──► npm / mvnw / python / java ...
   │                │
   │                └─► stdout ─┐
   │                └─► stderr ─┴─► bounded ring buffers (10k lines/stream)
   └── agent asks get_logs(process_id, stream, lines, contains)
```

## Why

Node dev tools (`npm run dev` → `next-server`), Spring Boot (`mvnw
spring-boot:run` → `java`), and Django (`manage.py runserver`) print thousands
of lines. Streaming them all into the agent's context destroys it. Instead:

1. `start_process` returns a `process_id` **immediately** — never blocks on
   startup or exit.
2. `wait_for_log(process_id, ready=true)` waits for the app's readiness line.
3. On failure, `get_logs(process_id, stream="stderr", lines=100)` returns just
   what's needed.
4. `restart_process` then `wait_for_log(ready=true)` again.

## Build

Requires Go 1.23+ (tested with Go 1.26).

```bash
make build                 # -> bin/agent-runtime
# or
go install ./cmd/agent-runtime
```

## Quick start

```bash
# Build and print the Claude Code config
./bin/agent-runtime integrate claude

# Install it for Claude Code (writes ./.mcp.json)
./bin/agent-runtime integrate claude --write

# Codex and Gemini CLI
./bin/agent-runtime integrate codex   --write
./bin/agent-runtime integrate gemini  --write

# opencode: interactive install (discovers the real config, merges surgically)
./bin/agent-runtime integrate opencode --write
```

Now ask your agent to start the app:

```
start_process(command="npm", args=["run","dev"], workdir="examples/nextjs-app")
wait_for_log(process_id="proc_...", ready=true)
get_logs(process_id="proc_...", stream="stderr", lines=100)   # on failure
restart_process(process_id="proc_...")
```

## Named apps (`agent-runtime.yaml`)

Declare apps with stable names, types, workdirs, commands and env files. The
runtime detects the app framework ("profile") for readiness patterns:

```yaml
runtime:
  log_buffer_lines: 10000
  shutdown_timeout: 5s
  stop_grace: 5s
  max_log_lines: 2000
  max_log_bytes: 524288
  max_exited_processes: 50          # auto-evict oldest exited/stopped/failed beyond this
  # Optional durable SQLite log archive (see "Persistence" below):
  # log_store: sqlite               # memory (default) | sqlite
  # db_path: .agent-runtime/logs.db # default <projectdir>/.agent-runtime/logs.db
  # db_max_age_days: 7              # retention in days; 0 = keep forever
  # db_max_mb: 512                  # size cap in MB; 0 = unlimited

apps:
  backend:
    type: spring-boot          # profile: detects readiness + default command
    workdir: ./backend
    command: ["./mvnw", "spring-boot:run"]
    env_file: .env             # dotenv, never logged
  frontend:
    type: nextjs               # command defaults to the detected package manager
    workdir: ./web
  api:
    type: django
    workdir: ./server
    command: ["python3", "-u", "manage.py", "runserver"]   # -u for line-buffered pipes
```

Then `start_process(app="backend")`. Relative `workdir`/`env_file` paths resolve
against the directory containing the config file (found by walking up from the
current directory).

## Environment & process resolution

Every process runs with a **complete merged environment** — the `env` the
runtime reports for a process is the final result, never a delta. Layers, from
lowest to highest precedence:

1. **Base env** — the captured login-shell environment (`runtime.shell_env:
   login`, the default; `$SHELL -lic 'env -0'`, ~3s timeout, silent fallback to
   `os.Environ()`) or the parent process env (`shell_env: none`). Login-shell
   capture is what lets nvm/pyenv/uv shims and their PATH entries reach managed
   processes.
2. **`runtime.env`** — a runtime-wide layer applied to every app/process.
3. **`env_file`** — the app's (or request's) dotenv file.
4. **app `env`** — the app's `env:` block in `agent-runtime.yaml`.
5. **request `env`** — the `env` array passed to `start_process`.

Later layers override earlier ones for the same key. `workdir` and `env_file`
paths are resolved relative to the config file; workdirs are symlink-resolved
(`filepath.EvalSymlinks`, for predictable uv/poetry walk-ups) and must exist
before the process starts.

```yaml
runtime:
  shell_env: login            # "login" (default) | "none" (use parent env)
  env:
    - "NODE_ENV=development"  # every app/process inherits this

apps:
  api:
    workdir: ./services/api
    env_file: ./services/api/.env   # e.g. DATABASE_URL=...
    env:
      - "LOG_LEVEL=info"            # per-app overrides
    command: ["node", "server.js"]
```

Environment and process inspection:

- `signal_process(process_id, signal)` — deliver SIGINT/SIGTERM/SIGHUP/SIGQUIT/
  SIGUSR1/SIGUSR2/SIGKILL to the **whole process group**, so a graceful SIGINT
  reaches a `mycli -> uv -> python` tree rather than just the direct child.
- `get_process_env(process_id, live=false, reveal=false)` — the complete env
  the runtime built for the process (spec mode, with a per-key `source` layer
  map) or the ground truth read live from `/proc/<pid>/environ`. Secret-like
  values are redacted to `***` by default; pass `reveal=true` only when the raw
  value is actually needed.
- `open_shell(process_id|app|pid, shell)` — start an interactive shell inside the
  resolved workdir + complete env of a running process, a configured app, or —
  with `pid=<os-pid>` (Linux only) — ANY process on the machine, including ones
  started in another agent-runtime session (its workdir+env are read from
  `/proc/<pid>/cwd` and `/proc/<pid>/environ`). Drive it with `send_stdin`, read
  it with `get_logs` / `wait_for_log`.

### Profiles

Built-in profiles: `nextjs`, `spring-boot`, `django`, `node`, `python`, `go`,
`generic`. Each supplies file-based detection rules, readiness patterns, and a
default start command. Profiles only *inform* the generic process manager; no
runtime is hard-coded into it.

### Persistence

By default all logs live in bounded in-memory ring buffers and are lost when
the runtime exits. Set `runtime.log_store: sqlite` to additionally archive every
entry (plus per-instance start/exit records) to a local SQLite database — writes
are asynchronous and batched, so they never block process output. `get_logs`
transparently back-fills from the archive when the in-memory tail has been
rotated out, and responses carry a `source` field (`memory` or `memory+db`).
The archive is bounded and auto-retained: `db_max_age_days` (default 7) and
`db_max_mb` (default 512) prune old data on boot and hourly. If the database
can't be opened the runtime warns and falls back to memory — persistence never
prevents startup. The archive stores logs and instance history, not the process
registry.

## Skill: agent-runtime-ready

Ships an embedded skill that teaches an AI coding agent how to build — or
retrofit — runnable applications so they integrate perfectly with
agent-runtime: a detectable readiness line, clean stdout/stderr, graceful
SIGTERM/SIGINT shutdown within the grace period, env-only configuration, and a
declared `apps:` entry in `agent-runtime.yaml`. Install it with
`agent-runtime integrate skill --write` (writes to `~/.claude/skills` and
`~/.config/opencode/skills`, or `.claude`/`.opencode` under the current
directory with `--scope project`); it ships inside the binary, so no
downloading needed.

## MCP tools

| Tool              | Purpose                                                        |
|-------------------|----------------------------------------------------------------|
| `start_process`   | Launch by raw `{command, args, workdir}` or by `{app}`; returns immediately |
| `stop_process`    | SIGTERM to the process group, escalate to SIGKILL after grace  |
| `restart_process` | Stop + start with the same spec; same logical `process_id`, new `instance_id`, logs preserved |
| `process_status`  | Status, pid, exit code, stdout/stderr line counts              |
| `list_processes`  | All managed processes, including exited ones                   |
| `get_logs`        | Tail with `stream` (all/stdout/stderr), `lines`, `contains`; bounded |
| `clear_logs`      | Empty a process's buffer(s)                                    |
| `send_stdin`      | Write to a process's stdin (e.g. `"q\n"`)                      |
| `signal_process`  | Deliver SIGINT/SIGTERM/SIGHUP/SIGQUIT/SIGUSR1/SIGUSR2/SIGKILL to the whole process group |
| `get_process_env` | Complete env the runtime built (spec, with per-key `source` provenance) or live `/proc` env; secrets redacted unless `reveal` |
| `open_shell`      | Interactive shell inside a process's/app's workdir + env (or by OS pid via `/proc`); drive with `send_stdin`, read with `get_logs`/`wait_for_log` |
| `wait_for_log`    | Wait for `contains`/`pattern`/`ready` (profile readiness); returns on match, exit, or timeout |
| `wait_for_exit`   | Wait for exit; returns the exit code; multiple waiters supported |
| `remove_process`  | Delete a process from the registry and free its log buffers; refuses running processes unless `force` (stops it first) |
| `list_apps`       | Apps declared in `agent-runtime.yaml` with detected profiles   |
| `set_restart_policy` | Set the auto-restart policy (`never`/`on-failure`/`always`) of an existing process, opting an ad-hoc process into supervision |
| `subscribe_events`  | Open a capped lifecycle-event subscription (filter by process_id/types; backfill via `since`) |
| `get_events`        | Drain a subscription's buffered events (pull-based; never pushed); `dropped` shows ring overruns |
| `unsubscribe_events`| Close an event subscription and release its ring buffer |
| `runtime_stats`     | Runtime health: process counts by status, log pipeline drops (archive/forwarder/subscription), uptime |
| `get_audit_log`     | Tail of the JSONL audit log (secrets redacted), bounded like `get_logs` |

`process_status` additionally reports live `memory_bytes` / `cpu_usage_nanos`
from a process's cgroup when resource `limits` are configured.

### HTTP transport (`serve --http`)

Beyond stdio, the same MCP server is served over **streamable HTTP**:
`agent-runtime serve --http :7341`. A bearer token (`runtime.http.token`,
env-expanded) is **required** — there is no unauthenticated HTTP mode, even on
localhost. Tool calls are rate-limited (token bucket, 20 rps / burst 50).
With `runtime.daemon`, the HTTP server is also a thin client of the shared
daemon.

```yaml
runtime:
  http:
    token: ${AGENT_RUNTIME_HTTP_TOKEN}
```
```
curl -H "Authorization: Bearer $AGENT_RUNTIME_HTTP_TOKEN" ... http://localhost:7341/
```

Log queries are capped: `lines` defaults to 100 and never exceeds
`max_log_lines` (2000); responses are truncated at `max_log_bytes` (512 KiB)
with `truncated`/`available_lines` flags.

## Supervision (v2)

Beyond request-driven control, apps declared in `agent-runtime.yaml` can opt
into **continuous, declarative supervision** (all fields optional; v1 files
load unchanged):

- **`readiness`** — override the profile-detected readiness regexes for an app.
- **`health_check`** — after the app is ready, agent-runtime probes an HTTP or
  TCP endpoint on an `interval`; after `failure_threshold` consecutive failures
  it reports the app unhealthy and, per the restart policy, restarts it.
- **`restart`** — `policy: never | on-failure | always` with a capped
  exponential `backoff` and a `max_restarts` budget per rolling 10-minute
  window. A process that exhausts its budget is marked `crashed`; a manual
  `stop_process` or shutdown never triggers a restart; a process stable for a
  full window resets its budget.

`process_status` now also reports `health` (unknown/healthy/unhealthy),
`consecutive_failures`, `backoff_state`, and `restart_policy`.

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]
    health_check:
      http: "http://localhost:8080/healthz"   # or tcp: "localhost:8080"
      interval: 10s
      timeout: 3s
      failure_threshold: 3
    restart:
      policy: on-failure
      backoff: [1s, 2s, 5s, 15s, 60s]
      max_restarts: 10
```

## Events, security & observability (v2)

**Events.** Lifecycle events (`process.started/exited/crashed/healthy/...`) are
never pushed. Subscribe with `subscribe_events` (8 subscriptions per client,
256 buffered events each, with a drop counter) and drain with `get_events` —
an alternative to polling `process_status`.

**Security.** Default `runtime.security.mode: trusted` (arbitrary exec, as
today). Set `restricted` for CI/shared machines: `allowed_commands` (basename
allowlist), `allowed_workdirs` (`${project}` confinement), `allow_pid_attach`,
`allow_stdin`, `reveal_secrets`. Denials return structured `policy_denied`
errors and are recorded to the audit log.

**Audit.** Every tool call is appended to `.agent-runtime/audit.log` (JSONL,
secrets redacted, 10MB × 5 rotation); `get_audit_log` reads its tail.

**Observability.** `runtime_stats` reports the log pipeline's health (archive/
forwarder/subscription drop counters). Optional `runtime.metrics: ":9341"`
serves Prometheus counters; `runtime.log_forward: { stdout: true, otlp: "..." }`
forwards lines (best-effort, drop-on-backpressure). `get_logs` accepts a
`level` filter (debug|info|warn|error) for structured logs.
## Resource limits (v2)

Per-app `limits` bound a process's machine impact (Linux-first via cgroup v2;
degrades gracefully to unlimited when unsupported):

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]
    limits:
      cpu: "1.0"        # cores; also "500m" (milli-cores)
      memory: "512M"    # size: 512M, 1G, 256MiB, ...
```

Each managed process is placed in its own cgroup v2 slice under
`agent-runtime.scope`; `memory.max`/`cpu.max` are set. `process_status` reports
live `memory_bytes` / `cpu_usage_nanos`. An OOM-kill is surfaced to event
subscribers as `process.crashed` with `reason: "oom"` and triggers the app's
restart policy. On systems without a delegated cgroup v2 controller, or
non-Linux platforms, the process runs unlimited with a warning.

## Daemon mode (v2)

By default the runtime is **session-scoped**: when the MCP server exits, all
managed processes are stopped. With `runtime.daemon: true`, `serve` (and the
`daemon` CLI) run a **long-lived daemon** that owns the processes, so they
survive the session and a later session re-attaches to the same registry and
log history over a local Unix socket (`.agent-runtime/run.sock`).

```yaml
runtime:
  daemon: true
```

```
agent-runtime daemon start     # spawn the background daemon (PID file + flock lock)
agent-runtime daemon status    # is it running?
agent-runtime daemon stop      # SIGTERM → graceful shutdown of all its processes
```

- One daemon per project (lock file). Multiple MCP sessions may attach and
  co-manage the same stack; `serve` auto-starts the daemon if it isn't running.
- Processes bind to the daemon's root context, so killing an MCP session leaves
  them running.
- Without `runtime.daemon`, behavior is exactly the default session-scoped
  model (no regression).
- Platform note: daemon mode is Unix-only (flock/setsid/`/proc`); Windows gets
  a clear "not supported" error. The socket RPC itself is portable.

## CLI

```
agent-runtime [serve]                     MCP stdio server (default)
agent-runtime run <cmd> [args]            foreground debug: start + tail until Ctrl-C
agent-runtime run --app <name>            start a named app
agent-runtime integrate <agent>           print/install config for claude|codex|gemini|opencode|generic
agent-runtime version
```

`integrate opencode --write` is an interactive installer: it discovers every
config opencode actually reads (project `opencode.json[.c]`,
`.opencode/opencode.json`, global `~/.config/opencode/opencode.json[.c]`), asks
where to install, and performs a **byte-preserving JSONC merge** — comments,
formatting and every unrelated key (other MCP servers, agents, skills) are
untouched. After installing it verifies with `opencode mcp list`.

Non-interactive flags for scripting:

```bash
agent-runtime integrate opencode --write --yes --scope global   # auto-target global
agent-runtime integrate opencode --write --yes --scope project  # this project
agent-runtime integrate opencode --write --yes --create         # create if missing
agent-runtime integrate opencode --write --yes --no-verify      # skip the connection check
```

Note: in `run` mode the managed process's lifetime is tied to the invocation.
In `serve` (MCP) mode processes live independently; when the agent disconnects
or a shutdown signal arrives, the runtime gracefully stops everything.

## Testing

```bash
make vet
make test
make race          # go test -race ./...
make build
```

Integration tests spin up real Node / Python / Java apps from `examples/`
(skipped with `-short` or when a toolchain is missing). The full suite also
covers the SQLite persistence layer (async batching, retention, `get_logs`
back-fill).

## Security

- Executes arbitrary commands — treat it as a local-only tool. No remote
  listener; MCP is stdio-only.
- Commands run via `exec.Command` with argument arrays, **never** a shell.
- Working directories are validated to exist before start.
- Environment variables are never dumped into logs or status responses;
  `get_process_env` exposes them only on request, redacted by default.
- No managed-process log is ever injected into the agent's context.

## Known limitations

- **Windows**: process-group termination is not implemented yet; only the
  direct child is signalled on Windows (Unix uses process-group TERM/KILL so
  `npm → next-server` trees die together).
- **Piped stdout buffering**: some runtimes (Python) buffer output when stdout
  is a pipe; use `python3 -u` or `flush=True` so readiness lines are seen.
- **Process trees**: supervision is process-group-level, not a full recursive
  walk. A grandchild that detaches from its group is not tracked.
- Persistence is optional: logs default to in-memory ring buffers, but
  `log_store: sqlite` adds a durable, auto-retained archive (see "Persistence").
  The process *registry* is still session-scoped — no cross-session process
  resurrection; the DB is a log archive (with `instances` rows for context),
  not a registry. No remote transport, no Docker/Kubernetes integration — by
  design.

See `docs/architecture.md` for the full design.
