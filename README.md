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
| `wait_for_log`    | Wait for `contains`/`pattern`/`ready` (profile readiness); returns on match, exit, or timeout |
| `wait_for_exit`   | Wait for exit; returns the exit code; multiple waiters supported |
| `remove_process`  | Delete a process from the registry and free its log buffers; refuses running processes unless `force` (stops it first) |
| `list_apps`       | Apps declared in `agent-runtime.yaml` with detected profiles   |

Log queries are capped: `lines` defaults to 100 and never exceeds
`max_log_lines` (2000); responses are truncated at `max_log_bytes` (512 KiB)
with `truncated`/`available_lines` flags.

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
- Environment variables are never dumped into logs, status responses, or MCP
  output.
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
