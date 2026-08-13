# Log structure reference for agent-runtime

The agent-runtime MCP reads logs through **line-oriented matching**. Every
`wait_for_log(contains=...)`, `wait_for_log(pattern=...)`,
`wait_for_log(ready=true)` and `get_logs(contains=...)` is evaluated one
captured line at a time. Everything below follows from that single fact: the
line is the unit of search, so structure the line.

## Why `key=value` beats JSON-lines for this MCP

| | `key=value` plain line | JSON-lines |
|---|---|---|
| Readiness regex match | matches `(?i)running on http` directly | must be unwrapped/unquoted first |
| `contains` filtering | `status=500` matches cleanly | quoting/escaping makes it brittle |
| Human readable in `get_logs` | yes | noisy |
| Structured dimensions | yes | yes |

`key=value` gives you filterable structure and a readable line at once.

## Canonical line shape

```
timestamp level logger: message key=value key=value ...
```

Example:

```
[2026-08-11 14:03:22] INFO  app.api.routes: request handled method=GET path=/health status=200 duration_ms=3 request_id=ab12f9 user_id=42 service=api
[2026-08-11 14:03:23] ERROR app.api.routes: upstream failed service=db status=503 err=connection_refused request_id=ab12f9
```

Keep the key set small (~8–12 keys) and **stable** — every key is permanent.
Adopt one fixed ordering so lines are predictable and greppable.

## The dimensions (sorted by payoff for this MCP)

### Correlation & identity
| key | when | why it matters |
|---|---|---|
| `request_id` | every request | fresh UUID per request; ties API↔worker, reconstructs a trace across `contains` filters |
| `user_id` | when a user is known | scopes filters to one user: `contains="user_id=42 status=500"` |
| `session_id` | browser/client sessions | spans many requests for one human |
| `trace_id`/`span_id` | if you add OpenTelemetry | keeps logs and traces map 1:1 |
| `process_id` | the agent-runtime process_id | correlates a line back to your MCP handle (`get_logs(process_id)`) |

### Routing & flow
| key | when | why it matters |
|---|---|---|
| `method` + `path` | request logs | scopes to one endpoint: `contains="path=/orders status=5.."` |
| `status` | response code | the single most useful triage filter |
| `duration_ms` | request completion | grep slow paths: `contains="path=/search" pattern="duration_ms=[0-9]{4,}"` |

### Service & resource targeting
| key | when | why it matters |
|---|---|---|
| `service` | multiple supervised apps, one archive | says which app emitted the line |
| `module`/`func` | always | already the logger name (`%(name)s`); scopes to a subsystem |
| `process_id` | always | the agent-runtime handle (see above) |

### Outcome & context that drives decisions
| key | when | why it matters |
|---|---|---|
| `err` | error lines | short error code on the same line as the message — the key `wait_for_log` trigger for failure paths (`err=db_timeout`) |
| `retry` | retrying work | `retry=1/3` so a wait on a flaky job doesn't fire on transient attempts |
| `event` | business events | `event=order.created` — watch for an outcome, not a status code |

## What to keep OUT

- **Hand-rolled timestamps** — the runtime/line already carries one; a second
  drifts. If you keep it, ISO-8601 UTC, fixed format.
- **Free-text prose in the `key=value` region** — spaces/quotes make `contains`
  brittle. Keep values token-like; put prose in the fixed message position.
- **Jackpot keys that never vary** (`env=prod` on every line) — bytes, not
  filters.
- **Secrets, ever.** `get_process_env` redacts secret-like keys; `get_logs`
  never does.

## Python / FastAPI + uvicorn wiring

```python
# app/logging_config.py
import logging
import os
import sys

def setup_logging():
    fmt = "{asctime} {levelname:<7} {name}: {message}"
    formatter = logging.Formatter(style="{", fmt=fmt, datefmt="%Y-%m-%d %H:%M:%S")

    info = logging.StreamHandler(sys.stdout)  # info+ -> stdout
    info.setLevel(logging.INFO)
    info.setFormatter(formatter)
    err = logging.StreamHandler(sys.stderr)   # warning+ -> stderr
    err.setLevel(logging.WARNING)
    err.setFormatter(formatter)

    root = logging.getLogger()
    root.setLevel(os.environ.get("LOG_LEVEL", "INFO"))
    root.addHandler(info)
    root.addHandler(err)
```

```python
# app/log_event.py — one structured line per event, key=value tail
import logging
import uuid

logger = logging.getLogger("app")

def log_event(message, level=logging.INFO, user_id=None, **ctx):
    """Emit one key=value record. request_id is auto-stamped when absent."""
    ctx.setdefault("request_id", uuid.uuid4().hex[:12])
    if user_id is not None:
        ctx["user_id"] = user_id
    kv = " ".join(f"{k}={v}" for k, v in ctx.items())
    line = f"{message} {kv}".rstrip()
    logger.log(level, "%s", line)
```

```python
# app/main.py — FastAPI: uvicorn prints the canonical readiness line itself
import os
import time
import uuid
from fastapi import FastAPI, Request

from app.logging_config import setup_logging

setup_logging()

app = FastAPI()

@app.middleware("http")
async def stamp_request_id(request: Request, call_next):
    request.state.request_id = uuid.uuid4().hex[:12]
    start = time.perf_counter()
    response = await call_next(request)
    duration_ms = int((time.perf_counter() - start) * 1000)
    log_event("request handled",
        method=request.method, path=request.url.path,
        status=response.status_code, duration_ms=duration_ms,
        request_id=request.state.request_id)
    return response

@app.get("/health")
def health():
    return {"status": "ok"}

if __name__ == "__main__":
    import uvicorn
    # Readiness line is uvicorn's own: "Uvicorn running on http://127.0.0.1:PORT"
    uvicorn.run(app, host="127.0.0.1", port=int(os.environ.get("PORT", "8000")))
```

- **Unbuffered output is mandatory**: Python block-buffers piped stdout.
  Set `PYTHONUNBUFFERED=1` in the app's `env:` block in `agent-runtime.yaml`,
  or run with `python3 -u`, or `flush=True` on prints. Without it the
  readiness line and log lines are delayed or lost.
- uvicorn handles SIGTERM/SIGINT with its own graceful shutdown — no handler
  needed. It prints `Uvicorn running on http://127.0.0.1:PORT`, which matches
  the python profile `(?i)running on http`.
- Do **not** enable `--reload` under supervision: the reloader watchdog child
  complicates process-group management.

## Node (http / Express)

```js
const PORT = Number(process.env.PORT) || 3000;
// key=value, one line per event; info via console.log (stdout), errors via console.error (stderr)
const log = (msg, ctx = {}) => console.log(`${msg} ${Object.entries(ctx).map(([k, v]) => `${k}=${v}`).join(" ")}`.trim());
const request_id = require("crypto").randomUUID();
// ... in the request handler:
log("request handled", { method: req.method, path: req.url, status: res.statusCode, duration_ms, request_id });
```

## Go

```go
log.Printf("request handled method=%s path=%s status=%d duration_ms=%d request_id=%s",
    r.Method, r.URL.Path, status, durationMs, requestID)
// startup: log.Printf("server listening on http://127.0.0.1:%s", port)  // matches go profile "(?i)listening on"
```

## Readiness lines you must keep byte-for-byte

| Framework | Readiness line | Profile regex |
|---|---|---|
| Node/http | `Server listening on http://127.0.0.1:${PORT}` | node `(?i)listening on` |
| FastAPI+uvicorn | `Uvicorn running on http://127.0.0.1:${PORT}` | python `(?i)running on http` |
| Django | `Starting development server at ...` | django `Starting development server at` |
| Go | `server listening on http://127.0.0.1:${PORT}` | go `(?i)listening on` |

Do not rename or paraphrase these — profile regexes are literal,
case-insensitive substrings; a clever rephrase breaks `wait_for_log(ready=true)`
silently. Print the readiness line *after* the listener is bound, never
before.
