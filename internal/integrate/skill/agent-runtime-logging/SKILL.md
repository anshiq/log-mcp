---
version: 5
name: agent-runtime-logging
description: Structured logging that makes an app's stdout/stderr filterable and correlatable by the agent-runtime MCP (get_logs, wait_for_log, search_logs).
---

# agent-runtime-logging

Complement to `agent-runtime-ready`: that skill makes a process startable and stoppable, this one shapes log lines so `get_logs`, `wait_for_log`, and `search_logs` match precisely.

## Rule

One event per line: human message plus stable `key=value` tail.

```
[2026-08-11 14:03:22] INFO  app.api.routes: request handled method=GET path=/health status=200 duration_ms=3 request_id=ab12f9 service=api
```

Timestamp comes from the formatter. One key per filter: `contains="status=500"`, `contains="request_id=ab12f9"`. Regex only with `pattern=`, such as `pattern="duration_ms=[0-9]{4,}"`. Key order is not significant.

## Practices

1. One event per line, token values, prose only before the tail.
2. Split streams without duplication: below `WARNING` to stdout, `WARNING` and above to stderr.
3. Keep the framework readiness line byte-for-byte, never as JSON.
4. Pass `request_id` explicitly, one UUID per request from middleware, plus `user_id` and `service` when known. Around 8 stable keys: `method path status duration_ms request_id user_id service err event retry`.
5. Run Python with `PYTHONUNBUFFERED=1`. Never log secrets.

## Wiring

```python
import logging
import os
import sys


class BelowWarning(logging.Filter):
    def filter(self, record):
        return record.levelno < logging.WARNING


def setup_logging():
    root = logging.getLogger()
    if root.handlers:
        return root
    fmt = logging.Formatter(style="{", fmt="{asctime} {levelname:<7} {name}: {message}", datefmt="%Y-%m-%d %H:%M:%S")
    info = logging.StreamHandler(sys.stdout)
    info.setLevel(logging.DEBUG)
    info.addFilter(BelowWarning())
    info.setFormatter(fmt)
    err = logging.StreamHandler(sys.stderr)
    err.setLevel(logging.WARNING)
    err.setFormatter(fmt)
    root.setLevel(getattr(logging, os.environ.get("LOG_LEVEL", "INFO").upper(), logging.INFO))
    root.addHandler(info)
    root.addHandler(err)
    return root
```

```python
import logging

logger = logging.getLogger("app")
ORDER = ("method", "path", "status", "duration_ms", "request_id", "user_id", "service", "err", "event", "retry")


def log_event(message, level=logging.INFO, **ctx):
    keys = [k for k in ORDER if k in ctx] + [k for k in ctx if k not in ORDER]
    kv = " ".join(f"{k}={ctx[k]}" for k in keys)
    logger.log(level, "%s", f"{message} {kv}".rstrip())


def error(message, **ctx):
    log_event(message, level=logging.ERROR, **ctx)
```

```python
import os
import time
import uuid
from fastapi import FastAPI, Request

app = FastAPI()
service = os.environ.get("SERVICE", "api")


@app.middleware("http")
async def stamp_request_id(request: Request, call_next):
    request_id = uuid.uuid4().hex[:12]
    start = time.perf_counter()
    response = await call_next(request)
    log_event("request handled", method=request.method, path=request.url.path, status=response.status_code, duration_ms=int((time.perf_counter() - start) * 1000), request_id=request_id, service=service)
    return response
```

Existing code, in order: unbuffer output, split streams and strip banners and ANSI, add `key=value` to the request line first, add `.env.example`.

## Verify

1. `start_process(app="<name>")`.
2. `wait_for_log(process_id, ready=true)`.
3. Curl `/health`.
4. `wait_for_log(process_id, contains="request_id=")`.
5. `get_logs(process_id, contains="status=200")`.
6. `signal_process(process_id, "SIGINT")`, then `process_status` shows exit 0.

Avoid: JSON-lines, env dumps, noise keys, sequential ids, swallowing SIGTERM. Library code with no logs is out of scope.
