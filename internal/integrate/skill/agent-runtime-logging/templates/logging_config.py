"""agent-runtime-logging: Python logging wiring for the agent-runtime MCP.

Info goes to stdout, warnings/errors to stderr, one structured key=value line
per event. Readiness line comes from uvicorn itself ("Uvicorn running on
http://..."), which matches the python profile "(?i)running on http".

Run supervised with PYTHONUNBUFFERED=1 (or python3 -u) so piped stdout is not
block-buffered. Copy to app/logging_config.py and call setup_logging() in
your entrypoint before importing app modules.
"""
import logging
import os
import sys

FMT = "{asctime} {levelname:<7} {name}: {message}"


def setup_logging() -> None:
    formatter = logging.Formatter(style="{", fmt=FMT, datefmt="%Y-%m-%d %H:%M:%S")

    info = logging.StreamHandler(sys.stdout)  # info+ -> stdout
    info.setLevel(logging.INFO)
    info.setFormatter(formatter)

    err = logging.StreamHandler(sys.stderr)  # warning+ -> stderr
    err.setLevel(logging.WARNING)
    err.setFormatter(formatter)

    root = logging.getLogger()
    root.setLevel(os.environ.get("LOG_LEVEL", "INFO"))
    root.addHandler(info)
    root.addHandler(err)
