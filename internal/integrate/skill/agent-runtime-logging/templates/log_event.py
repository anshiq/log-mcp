"""agent-runtime-logging: one structured key=value line per event.

Copy to app/log_event.py. Every emitted line carries a stable key=value tail
so the agent-runtime MCP can filter it with get_logs(contains=...) and
wait_for_log(contains/pattern=...).

Dimension keys (keep the set small and stable):
  request_id  fresh UUID per request (auto-stamped when omitted)
  user_id     when a user is known
  method path status duration_ms   on request-completion lines
  service     which supervised app emitted the line
  err         short error code on failure lines (err=db_timeout)
  retry       retry=1/3 on retrying work
  event       stable business event name (event=order.created)
"""
import logging
import uuid

logger = logging.getLogger("app")


def log_event(message: str, level: int = logging.INFO, user_id: str | None = None, **ctx) -> None:
    """Emit one line: '<message> key=value key=value ...'."""
    ctx.setdefault("request_id", uuid.uuid4().hex[:12])
    if user_id is not None:
        ctx["user_id"] = user_id
    kv = " ".join(f"{k}={v}" for k, v in ctx.items())
    logger.log(level, "%s", f"{message} {kv}".rstrip())


def error(message: str, **ctx) -> None:
    """Emit an error line (stderr). Always pair with a short err= code."""
    log_event(message, level=logging.ERROR, **ctx)
