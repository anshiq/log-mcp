# skill: agent-runtime-logging

Structured logging that makes an app's stdout/stderr **filterable and
correlatable** by the agent-runtime MCP (get_logs, wait_for_log, open_shell).
It is the logging complement to `agent-runtime-ready`: that skill makes a
process startable/observable/stoppable; this one shapes the log *lines* so the
supervisor's line-oriented matching (`contains`, `pattern`, `ready`) is
precise instead of guesswork.

Read `reference/logging.md` for the full pattern library and copy-paste
wiring per language.

## Why this matters — when the agent reads your logs

Logs are the only window a supervising agent has into a running process, and
it looks in specific situations: verifying a deploy came up, triaging a failed
request, tracking a background job, confirming a fix, or reconstructing what
happened. Each situation maps to a filter — `status=5` finds failures,
`request_id=<id>` traces one request, `err=` names the cause,
`duration_ms=[0-9]{4,}` catches slow paths, `event=`/`retry=` follow jobs.
The `key=value` dimensions below exist precisely so those filters are one
exact match instead of a fragile substring guess. When the user's report is
vague ("I can't do X, the app is running"), the agent may also ask for the
failing input to build a targeted filter — stable keys make that round-trip
productive instead of a second dump.

## The one rule

**One log event per line, and every line is a `key=value` record.** The MCP
matches logs line-by-line — `wait_for_log(contains=...)`, `wait_for_log(
pattern=...)`, and `get_logs(contains=...)` are grep over the captured
buffers. A multiline banner is invisible; a wrapped or prose-only line can
only be matched on a fragile substring. Structure is what makes the
supervisor's filters accurate.

## Canonical line shape

```
[2026-08-11 14:03:22] INFO  app.api.routes: request handled method=GET path=/health status=200 duration_ms=3 request_id=ab12f9 user_id=42 service=api
```

Fixed order: `timestamp level logger: message` then a stable `key=value` tail.
Every key is permanent — filters written today keep working tomorrow.

## The 7 invariants

1. **One event per line.** Never banners, wrapped output, or progress spinners.
   A line IS the event; the supervisor reads lines, not streams.
2. **`key=value` tail, token-like values.** `status=500`, `user_id=42`,
   `err=db_timeout` are filterable; free-text prose in the value region is
   not (`contains` gets brittle on spaces/quotes).
3. **info/debug → stdout, warning/error → stderr.** This is how
   `get_logs(stream="stderr")` triage works: errors first, fast.
4. **Keep the framework readiness line byte-for-byte.** uvicorn prints
   `Uvicorn running on http://...` — the python profile regex `(?i)running on
   http` matches it, so `wait_for_log(ready=true)` succeeds. Never rename or
   suppress it, and don't switch to JSON-lines (the plain readiness line is
   required to match).
5. **Correlate with stable ids.** A fresh, non-sequential `request_id` (UUID)
   per request, plus `user_id` when you have it, plus `service` when logs from
   several supervised apps land in one archive. These are the dimensions that
   turn `contains="status=500"` (matches every 500 from everyone) into
   `contains="user_id=42 status=500"` (exactly one).
6. **Keep the key set small and stable (~8–12 keys).** Every key is a
   permanent contract. Don't add noise keys that never vary (`env=prod` on
   every line adds bytes, not filters).
7. **Never log secrets.** A logged token/password is recoverable via
   `get_logs` forever — `get_process_env` redacts secret-like keys, but the
   log buffers never redact anything.

## Flow A — new / freshly-initialized project

Scaffold logging from the golden wiring so it is correct from line one:

1. Apply the language snippet from `reference/logging.md` (Python logging with
   a stdout + stderr handler, `key=value` tail via a `log_event` helper).
2. Add `.env.example` documenting every variable the app logs against
   (`PORT`, `LOG_LEVEL`, `SERVICE`, `DATABASE_URL`, ...).
3. Ship a middleware/filter that stamps `request_id` per request and appends
   `method path status duration_ms` to each request-scoped line.
4. Declare the app in `agent-runtime.yaml` (`type: python`, command via the
   venv interpreter, `PYTHONUNBUFFERED=1` — see the agent-runtime-ready skill
   for the full contract).
5. Verify with the loop below and only report "done" once it passes.

## Flow B — existing project

**Best effort, in this order — do what the project allows, skip the rest:**

1. **Fix buffering.** Python block-buffers piped stdout: add
   `PYTHONUNBUFFERED=1` (or `-u`, or `flush=True`) so the readiness line and
   every log line appear in `get_logs` at all. This alone changes nothing
   about the app's behavior but makes everything else possible.
2. **Route error paths to stderr.** Confirm errors go to stderr and info to
   stdout; if a framework logs everything to one stream, split handlers.
3. **Strip banners/ANSI.** Kill ASCII banners, spinners, and color codes (or
   disable color when stdout is a pipe: `NO_COLOR=1` / `--no-color`).
4. **Add `key=value` dimensions to the highest-value lines** — the request
   log line first: `method path status duration_ms`, then `request_id`,
   then `user_id`. Do not reformat every line; start with the ones you
   actually filter on.
5. **Add a SIGTERM/SIGINT handler** if the app has none (graceful close,
   exit 0) — without it `stop_process` escalates to SIGKILL.
6. **Add `.env.example`** and replace hardcoded config with env reads.
7. If the project can't change (third-party binary, generated code): keep the
   app as-is and teach the supervisor to use `get_logs(contains=...)` on the
   lines it does emit — a single stable substring per concern beats nothing.

## The verification loop (must pass before "done")

1. `start_process(app="<name>")` — returns a `process_id` immediately.
2. `wait_for_log(process_id, ready=true)` — the readiness line appears.
3. `wait_for_log(process_id, contains="request_id=")` — request logging is
   live (hits after you curl/health-check the port).
4. `get_logs(process_id, contains="status=5", lines=50)` — error-class
   filtering works (after intentionally hitting a failing path).
5. `signal_process(process_id, "SIGINT")` — graceful shutdown; then
   `process_status` shows exit code 0.

On failure: `get_logs(process_id, stream="stderr", lines=50)` → fix →
`restart_process(process_id)` → `wait_for_log(process_id, ready=true)` again.

## Anti-patterns

- **JSON-lines logging** — breaks the plain readiness line the profile regex
  needs and makes `contains` brittle on escaping. Use `key=value` instead.
- **Multiline ASCII banners** — flood the buffer and fragment line matching.
- **`print()` of a dict / `process.env` dump** — a one-line dump of every env
  var is a permanent secret leak in the log buffer.
- **`env=prod` noise keys on every line** — no discriminating power.
- **Hand-rolled timestamps that drift** — the line already carries one; if you
  keep a second, make it ISO-8601 UTC with a fixed format.
- **`request_id` from a sequential counter** — leaks request volume and is
  collision-prone; use a UUID or random hex.
- **Silently swallowing SIGTERM** — the app gets SIGKILLed and the exit code
  is non-zero.

## When NOT to use this skill

One-shot scripts that start and exit immediately don't need the full
dimension set — but they still benefit from the `key=value`, one-line-per-event
rule and `PYTHONUNBUFFERED=1`, because `get_logs`/`wait_for_log` work the same
way for them. Pure library code that emits no logs is out of scope.
