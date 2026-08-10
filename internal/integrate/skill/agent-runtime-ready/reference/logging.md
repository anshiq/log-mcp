# Logging, startup & shutdown snippets per language

Golden copy-paste snippets. Each app must: read config from env with a
default, print a canonical readiness line (matches a profile regex), handle
SIGTERM/SIGINT with a graceful close then exit 0, and keep stdout/stderr
clean. Info goes to stdout, errors to stderr, secrets never.

The canonical Node snippet is a strict upgrade of
`examples/node-app/server.js`.

---

## Node (http / Express)

```js
const http = require("http");

// Config from env only, with a default.
const port = Number(process.env.PORT) || 3000;
const host = process.env.HOST || "127.0.0.1";

const server = http.createServer((req, res) => {
  res.writeHead(200, { "Content-Type": "text/plain" });
  res.end("ok\n");
});

// Canonical readiness line — matches node profile "(?i)listening on".
server.listen(port, host, () => {
  console.log(`Server listening on http://${host}:${port}`);
});

let shuttingDown = false;
function shutdown(signal) {
  if (shuttingDown) return;
  shuttingDown = true;
  console.log(`received ${signal}, shutting down`);
  // Graceful close, then exit 0 — within stop_grace (default 5s).
  server.close(() => process.exit(0));
  // Fallback: force-exit if connections hang past the grace period.
  setTimeout(() => process.exit(0), 5000).unref();
}
process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));

// Errors to stderr — get_logs(stream="stderr") surfaces them.
server.on("error", (err) => {
  console.error(err);
  process.exit(1);
});
```

Info via `console.log` (stdout), errors via `console.error` (stderr). No
spinners, no ANSI colors (the runtime captures raw bytes — colors garble
reads).

---

## Next.js

**Nothing to change.** The default `next dev` output prints `ready` and
`Local: http://localhost:3000`, which match the nextjs profile
(`(?i)ready`, `(?i)local:\s*https?://`).

- Set `PORT` via env: `env: ["PORT=3001"]` in `agent-runtime.yaml`, or
  `start_process(..., env=["PORT=3001"])`. `next dev` honors `PORT`.
- Keep `NODE_ENV=development` for dev (set it in `runtime.env` if you want
  it for every app).
- The default start command is auto-detected from the lockfile
  (`npm run dev` / `pnpm run dev` / `yarn dev` / `bun run dev`) — no
  `command:` needed in the app entry.

---

## Python / FastAPI + uvicorn

```python
# app.py
import os
import uvicorn

port = int(os.environ.get("PORT", "8000"))

def make_app():
    from fastapi import FastAPI
    app = FastAPI()

    @app.get("/health")
    def health():
        return {"status": "ok"}

    return app

if __name__ == "__main__":
    # uvicorn prints the canonical readiness line itself:
    #   "Uvicorn running on http://127.0.0.1:8000"
    # which matches the python profile "(?i)running on http".
    uvicorn.run(make_app(), host="127.0.0.1", port=port)
```

- **Unbuffered output is mandatory**: Python block-buffers piped stdout, so
  the readiness line can be delayed or lost. Set `PYTHONUNBUFFERED=1` in the
  app's `env:` block in `agent-runtime.yaml`, or run `python3 -u app.py`, or
  use `flush=True` on prints.
- uvicorn handles SIGTERM/SIGINT with its own graceful shutdown — no handler
  needed.
- **Flask variant**: `app.run(host="127.0.0.1", port=port)` prints
  `Running on http://127.0.0.1:8000`, which also matches the python profile
  (`(?i)running on http`). Pass `use_reloader=False` when supervised — the
  reloader's watchdog process complicates process-group management.

---

## Python / Django

**Keep `python manage.py runserver`.** It prints
`Starting development server at http://127.0.0.1:8000/` and
`Quit the server with CONTROL-C`, both matching the django profile.

```yaml
apps:
  web:
    type: django
    workdir: ./server
    command: ["python3", "-u", "manage.py", "runserver"]
    env:
      - "PYTHONUNBUFFERED=1"   # piped stdout is block-buffered otherwise
```

- The auto-reloader spawns a child process — that's fine: process-group
  signals reach it.
- For production-like runs, use gunicorn/uvicorn under the same rules
  (readiness line, env config, graceful shutdown).

---

## Go

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{Addr: "127.0.0.1:" + port}

	go func() {
		// log.Fatal writes to stderr — get_logs(stream="stderr") sees it.
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// Canonical readiness line — matches go profile "(?i)listening on".
	log.Printf("server listening on http://127.0.0.1:%s", port)

	// Graceful shutdown on SIGTERM (stop_process) and SIGINT (Ctrl-C /
	// signal_process). Context timeout keeps shutdown within stop_grace (5s).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
```

---

## Java / Spring Boot

**Nothing to change in code.** Add graceful shutdown to the config:

```properties
# src/main/resources/application.properties
server.shutdown=graceful
spring.lifecycle.timeout-per-shutdown-phase=5s
```

(or the YAML equivalent:

```yaml
server:
  shutdown: graceful
spring:
  lifecycle:
    timeout-per-shutdown-phase: 5s
```

)

- The default startup log — `Started MyApplication in 1.234 seconds` —
  already matches the spring-boot readiness regex
  `Started \S+ in \d+(\.\d+)?s?`. Don't rename it.
- Logback defaults already write to stdout, which the runtime captures.
- Start with `./mvnw spring-boot:run` (or `./gradlew bootRun`) as the app
  `command:`, or rely on the spring-boot profile default.

---

## Common pitfalls

- **ANSI colors** — the runtime captures raw bytes, not a TTY; escape
  sequences garble every `get_logs` read. Disable color when stdout is a pipe
  (`NO_COLOR=1`, `--no-color`, `FORCE_COLOR=0`, ...).
- **Buffering** — Python (and other runtimes) block-buffer piped output:
  use `-u` / `PYTHONUNBUFFERED=1` / `flush=True` or the readiness line may
  never be seen.
- **Printing `.env` values** — never log `process.env` or individual secrets;
  `get_process_env` redacts secret-like keys, but `get_logs` does not.
- **Readiness before listeners are bound** — print the readiness line *after*
  `listen`/`ListenAndServe` succeeds, never before, or `wait_for_log(ready=true)`
  fires while the port isn't accepting connections yet.
