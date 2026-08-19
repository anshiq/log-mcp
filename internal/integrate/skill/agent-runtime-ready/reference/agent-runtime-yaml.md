# agent-runtime.yaml reference

`agent-runtime.yaml` declares how runnable apps are started, supervised, and
env-configured. It lives at the project root; the runtime finds it by walking
up from the current directory. A ready-to-edit starter lives in
`templates/agent-runtime.yaml`.

## Full annotated template

```yaml
runtime:
  # Base env layer: "login" (default) captures the login-shell env once via
  # $SHELL -lic 'env -0' (~3s timeout, silent fallback to os.Environ()).
  # This is what makes nvm/pyenv/uv shims and their PATH entries reach managed
  # processes. Use "none" to inherit the parent env instead — pin "none" in
  # test configs where determinism matters.
  shell_env: login

  # Global env layer applied to EVERY app/process. Lower precedence than
  # env_file, app env, and request env.
  # env:
  #   - "NODE_ENV=development"

  # Log ring buffer size per stream (stdout/stderr). get_logs reads from here.
  log_buffer_lines: 10000

  # Grace period before SIGKILL after SIGTERM on stop_process (default 5s).
  stop_grace: 5s

  # How long to wait for graceful shutdown of everything on runtime exit.
  shutdown_timeout: 5s

  # Optional durable SQLite log archive (server-side persistence). The app
  # itself never writes log files.
  # log_store: sqlite

apps:
  # Each app is a name -> spec map. start_process(app="<name>") launches it.
  api:
    # Profile name — gives readiness detection + optional default command.
    type: node

    # Relative to the directory containing agent-runtime.yaml, then resolved
    # through filepath.EvalSymlinks (predictable uv/poetry walk-ups). Must
    # exist — a missing workdir fails the start BEFORE anything is exec'd.
    workdir: ./services/api

    # Explicit argv. Omit to use the profile's default command
    # (nextjs/node: detected package manager run dev; django:
    # python manage.py runserver; spring-boot: ./mvnw spring-boot:run /
    # ./gradlew bootRun; go: go run .; python: none).
    command: ["node", "server.js"]

    # Dotenv file, relative to the config file. Never logged. Higher
    # precedence than runtime.env, lower than this app's env block.
    # env_file: .env

    # Per-app env overrides, KEY=VALUE. Higher precedence than env_file.
    # env:
    #   - "LOG_LEVEL=info"
    #   - "PYTHONUNBUFFERED=1"
```

## Env precedence

Every process runs with a **complete merged environment** — the env the
runtime reports for a process is the final result, never a delta. Layers,
lowest to highest precedence:

1. **Base env** — captured login-shell environment (`shell_env: login`, the
   default) or the parent process env (`shell_env: none`).
2. **`runtime.env`** — a runtime-wide layer applied to every app/process.
3. **`env_file`** — the app's (or the request's) dotenv file.
4. **app `env`** — the app's `env:` block in `agent-runtime.yaml`.
5. **request `env`** — the `env` array passed to `start_process`.

Later layers override earlier ones for the same key. `get_process_env`
(spec mode) shows the merged result with per-key `source` provenance.

Example — `PORT` set in every layer:

```yaml
runtime:
  env: ["PORT=3000"]          # layer 2
apps:
  web:
    type: node
    env_file: .env            # .env contains PORT=3001 → layer 3 wins
    env: ["PORT=3002"]        # layer 4 wins over env_file
```

`start_process(app="web", env=["PORT=3003"])` → layer 5 wins → the process
sees `PORT=3003`.

## Monorepo

One config file, several independent services, each with its own workdir and
its own `.env` — same key, different values, no leakage:

```yaml
runtime:
  env: ["NODE_ENV=development"]   # shared default for every service
  log_buffer_lines: 10000

apps:
  api:
    type: node
    workdir: ./services/api
    env_file: ./services/api/.env     # PORT=3000, DATABASE_URL=...
    command: ["node", "server.js"]
  worker:
    type: python
    workdir: ./services/worker
    env_file: ./services/worker/.env  # PORT=4000, DATABASE_URL=... (same key, different value)
    command: ["python3", "-u", "worker.py"]
```

- Paths (`workdir`, `env_file`) resolve **relative to the config file**, then
  through `filepath.EvalSymlinks`.
- Each app's merged env is independent — `get_process_env(process_id)` per
  process shows exactly what that process got.
- A missing `workdir` fails the start during resolution, **before anything is
  exec'd or registered** — a typo fails loudly and fast, not silently.

## Profiles

| Profile | Detected by | Readiness patterns | Default command |
|---|---|---|---|
| `nextjs` | `package.json` contains `"next"` | `(?i)ready in`, `(?i)started server`, `(?i)local:\s*https?://`, `(?i)compiled successfully`, `(?i)ready` | detected package manager `run dev` (npm/pnpm/yarn/bun by lockfile) |
| `spring-boot` | `pom.xml` / `build.gradle` / `build.gradle.kts` | `Started \S+ in \d+(\.\d+)?s?`, `Tomcat started on port`, `Netty started on port`, `Jetty started on port`, `(UnderTow\|Undertow) started on port` | `./mvnw spring-boot:run` or `./gradlew bootRun` (by wrapper lockfile) |
| `django` | `manage.py` | `Starting development server at`, `Quit the server with`, `Uvicorn running on http` | `python manage.py runserver` |
| `node` | `package.json` | `(?i)listening on`, `(?i)server started`, `(?i)started server`, `(?i)ready` | detected package manager `run dev` |
| `python` | `requirements.txt` / `pyproject.toml` | `(?i)running on http`, `(?i)listening on`, `(?i)server started` | none |
| `go` | `go.mod` | `(?i)listening on`, `(?i)server started` | `go run .` |
| `generic` | fallback | none | none |

`wait_for_log(process_id, ready=true)` matches the app's profile readiness
patterns against captured log lines, one line at a time.

## Supervision (v2)

Supervision is **declarative and per-app**: agent-runtime notices crashes and
health failures on its own, without the agent polling. All fields are optional
(v1 files load unchanged). An app with none of these behaves exactly as in v1.

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]

    # Override the profile-detected readiness regexes for this app. A process
    # is "ready" once a captured log line matches. Omit to use the profile's.
    readiness:
      - 'listening on :8080'

    # Continuous health probe, run once the process is ready. After
    # failure_threshold consecutive failed probes the process is reported
    # unhealthy and, if the restart policy permits, restarted.
    health_check:
      http: "http://localhost:8080/healthz"   # or tcp: "localhost:8080" (set one)
      interval: 10s      # probe interval (default 10s)
      timeout: 3s        # per-probe timeout (default 3s, must be < interval)
      failure_threshold: 3  # consecutive failures that mark unhealthy (default 3)

    # Declarative auto-restart with exponential backoff.
    restart:
      policy: on-failure      # never (default) | on-failure | always
      backoff: [1s, 2s, 5s, 15s, 60s]   # capped-exponential steps (default)
      max_restarts: 10        # per rolling 10-minute window (default 10; 0 = no budget)
```

Rules:
- `on-failure` restarts only on non-zero exit; `always` restarts regardless of
  exit code.
- **A manual `stop_process` or shutdown never triggers a restart**, regardless
  of policy.
- Exhausting the restart budget marks the process `crashed` (process_status
  shows `status="crashed"`). Manual `restart_process` resets the budget.
- A process stable for a full 10-minute window has its budget reset.
- `set_restart_policy(process_id, policy)` opts an ad-hoc process (started
  without an app entry) into auto-restart.
- `process_status` reports `health` (unknown/healthy/unhealthy),
  `consecutive_failures`, `backoff_state`, and `restart_policy`.

## Security (v2)

Default `runtime.security.mode: trusted` — arbitrary exec, as today. Set
`restricted` for CI/shared machines:

```yaml
runtime:
  security:
    mode: restricted
    allowed_commands: ["go", "npm", "pnpm", "./mvnw", "python"]  # basename match
    allowed_workdirs: ["${project}", "${project}/../shared"]     # confinement
    allow_pid_attach: false   # disables open_shell(pid=)
    allow_stdin: true
    reveal_secrets: false     # hard-disables get_process_env(reveal=true)
```

In restricted mode, `start_process` with a non-allowlisted command or an
out-of-tree workdir fails with a structured `policy_denied` error and an audit
entry. Tool calls are recorded to `.agent-runtime/audit.log` (JSONL, secrets
redacted, 10MB × 5 rotation); `get_audit_log` reads its tail. In restricted
mode `reveal=true` is a policy error, not a silent ignore.

## Observability (v2)

- `runtime.metrics: ":9341"` — optional Prometheus scrape endpoint.
- `runtime.log_forward: { stdout: true, otlp: "http://localhost:4318/v1/logs" }`
  — best-effort, drop-on-backpressure log forwarding.
- `get_logs(..., level="error")` — filter structured logs by level
  (`level=...` / `"level":...` prefixes).
- `runtime_stats` — process counts by status and log-pipeline drop counters.

## Daemon mode (v2)

With `runtime.daemon: true`, `serve` runs a long-lived daemon that owns the
managed processes, so they survive the MCP session and a later session
re-attaches over the Unix socket. Manage it with `agent-runtime daemon
start|status|stop`. One daemon per project (flock). Without it, behavior is the
default session-scoped model. Unix-only.

## HTTP transport (v2)

`agent-runtime serve --http :7341` serves the same MCP server over streamable
HTTP. `runtime.http.token` (env-expanded) is **required** — no unauthenticated
HTTP. Tool calls are rate-limited (20 rps / burst 50).

```yaml
runtime:
  http:
    token: ${AGENT_RUNTIME_HTTP_TOKEN}
```

## Resource limits (v2)

Per-app `limits` bound machine impact (Linux-first via cgroup v2; degrades to
unlimited when unsupported):

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]
    limits:
      cpu: "1.0"       # cores, or "500m" (milli-cores)
      memory: "512M"   # 512M, 1G, 256MiB, ...
```

Each process gets its own cgroup v2 slice; `process_status` reports live
`memory_bytes`/`cpu_usage_nanos`; an OOM-kill surfaces to subscribers as
`process.crashed` with `reason: "oom"`.

## CLI equivalence

`agent-runtime run` mirrors the request-level env layer from the shell:

```bash
agent-runtime run --app api --env PORT=3003 --env-file ./services/api/.env
```

maps to `start_process(app="api", env=["PORT=3003"], env_file="./services/api/.env")`:
the `--env-file` becomes layer 3, `--env` becomes layer 5 (above the app's
`env:` block, above `runtime.env`). In `run` mode the process lifetime is tied
to the invocation; in MCP `serve` mode it outlives the call.
