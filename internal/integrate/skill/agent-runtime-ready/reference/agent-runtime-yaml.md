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

## CLI equivalence

`agent-runtime run` mirrors the request-level env layer from the shell:

```bash
agent-runtime run --app api --env PORT=3003 --env-file ./services/api/.env
```

maps to `start_process(app="api", env=["PORT=3003"], env_file="./services/api/.env")`:
the `--env-file` becomes layer 3, `--env` becomes layer 5 (above the app's
`env:` block, above `runtime.env`). In `run` mode the process lifetime is tied
to the invocation; in MCP `serve` mode it outlives the call.
