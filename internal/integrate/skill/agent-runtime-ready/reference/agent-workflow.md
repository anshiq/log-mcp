# Recipes for the supervising agent

You operate agent-runtime against an app built with this skill. Logs are
**never pushed to you** — pull them with `get_logs`. `start_process` returns
immediately; the process outlives the MCP call; `process_status` shows the
exit code. One process per instance — never start a second copy to "see
changes."

## Start & confirm

1. `start_process(app="api")` — returns a `process_id` immediately.
2. `wait_for_log(process_id, ready=true, timeout_ms=30000)` — waits for the
   app's profile readiness pattern (the canonical readiness line).

Interpret failure modes:

- **Timeout, process still running** — the app never printed a matching
  readiness line. `get_logs(process_id, stream="stdout", lines=50)`: look for
  buffered output (Python) or a readiness line that doesn't match the profile.
- **Process exited** — crashed at startup. `process_status(process_id)` for
  the exit code, then `get_logs(process_id, stream="stderr", lines=50)`.
- **Never started** — config problem (missing workdir fails before exec).
  `list_apps` to see what the runtime detected for the app entry.

## Error triage

- `get_logs(process_id, stream="stderr", lines=50)` — errors, fastest first look.
- `get_logs(process_id, contains="Error", lines=50)` — targeted scan; the
  buffers are line-oriented, so substring filtering works on real events only
  (no banners, no ANSI, one event per line).
- `process_status(process_id)` — lifecycle state, pid, exit code, line counts.
- Fix the cause, then `restart_process(process_id)` → `wait_for_log(process_id, ready=true)`.
  `restart_process` keeps the same logical `process_id` and log history.

## Env debugging

- `get_process_env(process_id)` — the complete merged env the runtime built
  (spec mode), with per-key `source` provenance showing which of the 5 layers
  won. Secret-like keys (token/password/api_key/...) show as `***`.
- `get_process_env(process_id, live=true)` — ground truth read from
  `/proc/<pid>/environ`: what uv/poetry/nvm shims actually injected into the
  running process.
- `get_process_env(process_id, reveal=true)` — only when the raw secret value
  is genuinely needed; prefer comparing `source` layers instead.

## Graceful ops

- `signal_process(process_id, "SIGINT")` — Ctrl+C semantics; a well-built app
  closes listeners and exits 0 (graceful shutdown path, no escalation).
- `signal_process(process_id, "SIGHUP")` — reload config / re-read files.
- `signal_process(process_id, "SIGUSR1"|"SIGUSR2")` — app-specific hooks.
- `stop_process(process_id)` — SIGTERM to the whole process group, SIGKILL
  after `stop_grace` (default 5s). The reliable way to bring something down.

All signals go to the **whole process group**, so a graceful SIGINT reaches a
`mycli -> uv -> python` tree, not just the direct child.

## Interactive debugging

`open_shell(process_id|app, shell)` — ssh-into-the-app pattern: a shell
inside the process's resolved workdir with its complete env. Drive it with
`send_stdin`, read it with `get_logs`/`wait_for_log`, stop it like any
managed process.

```
open_shell(app="api", shell="bash")
send_stdin(process_id, "uv run python -c 'import sys; print(sys.version)'\n")
get_logs(process_id, stream="stdout", lines=10)
send_stdin(process_id, "exit\n")
```

## Monorepo

Start both apps, confirm isolation:

```
start_process(app="api")
start_process(app="worker")
wait_for_log(process_id=<api>, ready=true)
wait_for_log(process_id=<worker>, ready=true)
get_process_env(process_id=<api>)     # PORT from services/api/.env
get_process_env(process_id=<worker>)  # PORT from services/worker/.env, different value
```

Each process owns its env, buffers, lifecycle, and waiters — one broken app
never affects the other.

## Restart loop discipline

After any code change: `restart_process(process_id)` →
`wait_for_log(process_id, ready=true)` → then resume working. Never start a
second instance of the same app to "see changes" — you get a second process
group, duplicated ports, and two competing log buffers. The one handle is the
original `process_id`.
