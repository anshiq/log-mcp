# Recipes for the supervising agent

You operate agent-runtime against an app built with this skill. Logs are
**never pushed to you** — pull them with `get_logs`. `start_process` returns
immediately; the process outlives the MCP call; `process_status` shows the
exit code. One process per instance — never start a second copy to "see
changes."

## When to pull logs

Logs are your eyes on a running process — nothing else shows what it is doing.
Reach for `get_logs`/`wait_for_log` whenever you need to verify, diagnose,
track, or reconstruct. The buffers are line-oriented, so `stream`, `contains`,
and `pattern` give you precise grep-style answers.

### Blocking vs non-blocking calls

Only `wait_for_log` and `wait_for_exit` block the agent's turn, and only for
`timeout_ms` (default 30s, max 600s) — they return as soon as the line matches
or the timeout fires, whichever is first. Everything else is immediate:
`start_process`/`restart_process` return a `process_id` before the app has
printed anything, `process_status` and `get_logs` are point-in-time reads.
So never assume the app is up because `start_process` returned — the process
runs in the background and the *only* confirmation is `wait_for_log(ready=true)`.
Always pass a bounded `timeout_ms` so a stuck app can never hang you; if it
times out, use `process_status` + `get_logs` to see whether it crashed, is
buffering, or never matched its readiness line.

| Situation | Do this | Why |
|---|---|---|
| Just started or restarted an app | `start_process`/`restart_process` returns a `process_id` immediately; then `wait_for_log(ready=true, timeout_ms=30000)` | `start`/`restart` never block on startup; the readiness wait is the blocking confirmation |
| App started, no readiness line | `wait_for_log(ready=true, timeout_ms=30000)`, then `get_logs(stream="stdout", lines=50)` | buffered output, or a readiness line that doesn't match the profile |
| App crashed at startup | `process_status`, then `get_logs(stream="stderr", lines=50)` | exit code plus the error tail |
| A request/test/command failed | `get_logs(stream="stderr", contains="status=5" or "Error", lines=50)` | errors first, targeted scan |
| Need the full trace of one request | `get_logs(contains="request_id=<id>")` | correlate api↔worker lines end to end |
| Code change just deployed | `restart_process` → `wait_for_log(ready=true)` → `get_logs(lines=50)` | confirm new lines appear and error classes are gone |
| Endpoint feels slow | `wait_for_log(pattern="duration_ms=[0-9]{4,}")` scoped by `contains="path=..."` | catch slow paths, not just statuses |
| Background job / worker | `wait_for_log(contains="event=job.done")`, watch `contains="retry="` | track progress; don't fire on transient retries |
| Process running but silent/stuck | `get_logs(lines=100)` | read the tail to see where it halted |
| Verify graceful shutdown | `signal_process(...)` then `process_status` | exit code 0, no SIGKILL escalation |
| Reconstruct what happened | `get_logs(contains="request_id=<id>")` | scoped forensics from the retained buffer |

### When the symptom is vague, ask first

If the user says "I'm trying to do X but I can't — the app is running," the log
tail is not the first move. **Gather input details first**: what exact action
or request failed, what they expected to happen, what they actually saw, when
it started, and which app/endpoint was involved. One targeted `contains`
filter beats a blind 100-line dump — `status=5` scoped by `path=/orders` plus
a `request_id` pinpoints the failing call, while a vague "it doesn't work"
scans everything. Ask for the failing input, or reproduce it yourself via a
request to the app's port (or `open_shell`/`send_stdin`), then filter on the
identifiers that input produces.

### Worked scenario — triage a 500, then verify the fix

The user reports "POST /orders returns a 500" while the app is running.

1. `process_status(process_id)` — confirm it's up, note the pid.
2. `get_logs(process_id, stream="stderr", contains="status=5", lines=50)` — find the failing request lines.
3. Read one line, grab its `request_id`, then `get_logs(process_id, contains="request_id=<id>")` — follow the request into the worker/call chain.
4. If the cause isn't in the logs, reproduce it with a request to the app's port (or `open_shell`/`send_stdin`), or ask the user for the exact input that failed.
5. Fix the cause, `restart_process(process_id)` → `wait_for_log(process_id, ready=true)`.
6. Re-run the failing input, then `get_logs(process_id, contains="request_id=<new id>")` / `contains="status=2"` — confirm the error class is gone before reporting done.

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

## Supervision (v2) — crash & health are watched for you

For apps that declare a `restart` policy and/or a `health_check` in
`agent-runtime.yaml`, agent-runtime supervises **continuously**: it notices a
3am crash or a failing health probe on its own and restarts with exponential
backoff. You do not need to poll `process_status`.

- `process_status(process_id)` now reports `health` (unknown/healthy/unhealthy),
  `consecutive_failures`, `backoff_state` (restarts within the window), and
  `restart_policy` (never/on-failure/always).
- A process that exhausts its restart budget shows `status="crashed"`. Manual
  `restart_process(process_id)` clears it and resets the budget.
- **A manual `stop_process` never triggers a restart**, whatever the policy —
  so a supervised app you deliberately stop stays stopped.
- Ad-hoc processes started without an app entry opt into supervision with
  `set_restart_policy(process_id, policy)` (never|on-failure|always).

## Events — subscribe instead of polling (v2)

Lifecycle events are **never pushed**. Instead of polling `process_status` in a
loop, open a subscription and drain it:

```
subscribe_events(process_id="<id>", types=["process.crashed","process.exited"], since="last")
   -> {subscription_id}
get_events(subscription_id=<id>, limit=100)
   -> {events: [...], dropped: N}   # dropped = events lost to a full 256-ring
unsubscribe_events(subscription_id=<id>)
```

Hard caps: 8 subscriptions per client, 256 buffered events each. `since` is an
event id to backfill from (pass `"last"` or empty to start fresh). `get_events`
returns immediately — events accumulate in the ring until you drain.

## Restart loop discipline

After any code change: `restart_process(process_id)` →
`wait_for_log(process_id, ready=true)` → then resume working. Never start a
second instance of the same app to "see changes" — you get a second process
group, duplicated ports, and two competing log buffers. The one handle is the
original `process_id`.
