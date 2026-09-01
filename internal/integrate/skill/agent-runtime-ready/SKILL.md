---
name: agent-runtime-ready
description: Use when scaffolding, creating, or modifying any runnable application (web server, worker, CLI, backend service) or its logging/startup/shutdown/config code so it integrates with the agent-runtime MCP process supervisor (start_process, wait_for_log, get_logs, signal_process, get_process_env, open_shell, agent-runtime.yaml). Ensures a detectable readiness line, clean stdout/stderr, graceful SIGTERM/SIGINT shutdown, env-only config, and a declared app entry.
---

# agent-runtime-ready

Make applications runnable, observable, and stoppable under
**agent-runtime**, the MCP process supervisor. Read `reference/logging.md`
for copy-paste snippets per language, `reference/agent-runtime-yaml.md` for the
config reference, and `reference/agent-workflow.md` for the recipes the
supervising agent uses against your app.

## What this skill guarantees

Apps built with this skill start in one shot via `start_process(app=...)` —
no hidden setup, no missing env, no buffered-log surprises. Their readiness is
detectable, so `wait_for_log(ready=true)` succeeds and the supervising agent
knows exactly when to proceed. Startup and runtime issues are visible through
`get_logs`, and every app shuts down gracefully on SIGTERM/SIGINT within the
runtime's grace period instead of being SIGKILLed.

## The 8 invariants

1. **Print a detectable readiness line.** `wait_for_log(ready=true)` matches
   your framework's profile regexes — a literal, predictable line is the
   only signal the supervisor trusts that your app is actually up.
2. **Keep stdout/stderr clean.** Logs are the *only* window into a running
   process (`get_logs` reads the captured buffers), so every line must carry
   signal: info on stdout, errors on stderr, nothing else.
3. **One event per line.** The runtime matches readiness and error patterns
   line-by-line — multiline banners and wrapped output fragment the stream
   and hide events from `contains`/`wait_for_log` filtering.
4. **Run unbuffered in the foreground.** Piped stdout is block-buffered by
   some runtimes (Python), which delays or swallows the readiness line — use
   `-u`, `PYTHONUNBUFFERED=1`, or `flush=True`.
5. **Stay a foreground process.** Supervision is process-group-level: a
   daemonized or detached grandchild escapes the group and survives
   `signal_process`/`stop_process`, orphaned and unstoppable.
6. **Shut down gracefully within the grace period.** `stop_process` sends
   SIGTERM and SIGKILLs after `stop_grace` (default 5s) — a SIGTERM handler
   that closes listeners and exits 0 keeps shutdown clean and fast.
7. **Take config from env only.** The runtime merges 5 env layers
   (login-shell base, runtime, env_file, app env, request env) — your app
   reads `process.env`/`os.Getenv` and ships a `.env.example` documenting
   every variable.
8. **Never log secrets.** Secret-like keys are redacted by
   `get_process_env` unless `reveal=true`; printing a token/password to the
   captured logs leaks it through `get_logs` permanently.

## Canonical readiness lines

| Language / framework | Recommended readiness line | Profile regex that matches it |
|---|---|---|
| Node (http/Express) | `Server listening on http://127.0.0.1:${PORT}` | node `(?i)listening on` |
| Next.js | keep the framework default (`ready`, `Local: http://...`) | nextjs `(?i)ready`, `(?i)local:\s*https?://` |
| Python / FastAPI + uvicorn | `Uvicorn running on http://127.0.0.1:${PORT}` (printed by uvicorn) | python `(?i)running on http` |
| Django | keep `Starting development server at ...` | django `Starting development server at` |
| Go | `server listening on http://127.0.0.1:${PORT}` | go `(?i)listening on` |
| Spring Boot | keep `Started X in Ys` (e.g. `Started MyApplication in 1.234 seconds`) | spring-boot `Started \S+ in \d+(\.\d+)?s?` |

Do **not** rename or paraphrase the readiness line — profile regexes are
literal, case-insensitive substrings; a clever rephrase breaks
`wait_for_log(ready=true)` silently.

## Flow A — build a new app

1. Pick the language snippet from `reference/logging.md` (Node, Next.js,
   Python/FastAPI/Flask, Django, Go, Spring Boot).
2. Write the server/worker with a canonical readiness line and a
   SIGTERM/SIGINT handler that closes and exits 0.
3. Add `.env.example` listing every required variable (`PORT`, `DATABASE_URL`,
   ...) with sensible defaults where safe.
4. Write `agent-runtime.yaml` from `templates/agent-runtime.yaml` — declare
   the app under `apps:` with `type`, `workdir`, `command` (or the profile
   default), `env_file`, and `env` as needed.
5. Run the Verification Loop below and only report "done" once it passes.

## Flow B — onboard an existing app

- [ ] Find the app's readiness line and change it to the canonical form.
- [ ] Ensure errors go to stderr, info to stdout; strip banners/ANSI.
- [ ] Add a SIGTERM/SIGINT handler if the app has none.
- [ ] Confirm the app does not daemonize, re-exec, or detach children.
- [ ] Add or repair `.env.example`; replace hardcoded config with env reads.
- [ ] Add an `apps:` entry in `agent-runtime.yaml`.
- [ ] Run the Verification Loop below and only report "done" once it passes.

## Verification Loop (must pass before "done")

1. `start_process(app="<name>")` — returns a `process_id` immediately.
2. `wait_for_log(process_id, ready=true)` — confirm the readiness line.
3. If it's an HTTP service, curl/health-check the port.
4. `signal_process(process_id, "SIGINT")` — the app should shut down.
5. `process_status(process_id)` — exit code 0, clean stop.
6. `get_process_env(process_id)` — sanity-check `PORT` and friends merged
   from the right layers.

## Supervision (v2) — optional continuous health & restart

Beyond the v1 contract, apps may opt into **continuous supervision** by
declaring `health_check` and/or `restart` under their `apps:` entry
(see `reference/agent-runtime-yaml.md`):

- **`health_check`** — agent-runtime probes an HTTP/TCP endpoint on an
  interval once the app is ready; after `failure_threshold` consecutive
  failures it reports the app unhealthy and (per the restart policy) restarts it.
- **`restart.policy`** — `never` (default) | `on-failure` | `always`, with an
  exponential `backoff` schedule and a `max_restarts` budget per 10-minute
  window. A manual `stop_process` never restarts.

Best practice: if you add a `health_check`, print its URL or bind a `/healthz`
endpoint that returns 200 only when the app is actually healthy — the probe
should fail when the app is wedged, not merely alive.

## Anti-patterns

On failure: `get_logs(process_id, stream="stderr", lines=50)` → fix the
cause → `restart_process(process_id)` → `wait_for_log(process_id, ready=true)`
again. Never start a second instance to "see changes."

## Anti-patterns

- **Daemonizing / `nohup` / `setsid`** — the child escapes the process group
  and survives every shutdown signal, orphaned forever.
- **Writing log files** — the runtime captures stdout/stderr; files are
  invisible to `get_logs` and bypass server-side persistence.
- **Multiline ASCII banners** — they flood the log buffer and break
  line-oriented `contains` matching.
- **ANSI color codes when not a TTY** — the runtime captures raw bytes;
  escape sequences garble every `get_logs` read.
- **Spinner / progress output** — hundreds of rewrite lines churn the buffer
  and push real events out of the readable tail.
- **Dumping `process.env` / `os.Environ()`** — leaks the whole merged env
  (tokens included) into logs that are visible on request.
- **Swallowing SIGTERM** — ignoring it makes `stop_process` escalate to
  SIGKILL, killing your state and reporting a non-zero exit.
- **Hardcoded ports** — `PORT` overrides at every env layer become
  impossible; the app collides with other services.
- **Printing secrets** — a logged token is recoverable via `get_logs`
  long after the process exits.

## When NOT to use this skill

This skill is for runnable, long-lived processes supervised by agent-runtime.
Pure library code, and one-shot scripts that start and exit immediately,
don't need the readiness/shutdown contract — though they still benefit from
the env-only configuration convention and a `.env.example`.
