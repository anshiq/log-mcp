# Plan: Upgrading agent-runtime for Production-Grade Software Building

This plan upgrades agent-runtime from a session-scoped dev-process supervisor
into infrastructure trustworthy for building, testing, and operating
production-grade software. It is grounded in the current codebase
(~12k LOC Go, layered `internal/logs ← internal/logstore ← internal/process ←
internal/runtime ← internal/mcp`) and preserves the engineering properties that
make it sound: boundedness, per-process isolation, non-blocking handlers, and
race-tested concurrency.

## Guiding principles

1. **Preserve the six invariants** in AGENTS.md. Where a feature requires
   amending one (e.g. opt-in event push), amend it explicitly in AGENTS.md and
   the MCP server `Instructions` — never silently.
2. **Additive, not corrective.** Every phase bolts on; no redesign of the
   process manager, ring buffers, or logstore writer loop.
3. **Graceful degradation stays the policy.** Every new subsystem (health
   checks, metrics, daemon mode, security policy) must fail soft: warn and
   disable, never take the daemon down.
4. **Race suite (`make race`) must be green after every phase.** New
   concurrency gets new stress tests, not just unit tests.
5. **Skills stay in sync.** Any change to tool names, readiness regexes, or
   config schema updates `internal/integrate/skill/agent-runtime-ready/` and
   `agent-runtime-logging/` in the same commit (per AGENTS.md rule).

---

## Phase 0 — Contracts and foundations (0.5 week)

**Goal:** pin down the v2 contract before writing code.

- [ ] Write `docs/v2-contract.md` covering: session-scoped vs daemon lifetime,
      supervision policy semantics, security model (trusted local agent vs
      policy-constrained mode), and transport matrix (stdio / HTTP).
- [ ] Amend AGENTS.md invariants: add invariant 7 ("supervision policy is
      declarative and per-app") and amend invariant 4 to "logs are never
      pushed; lifecycle events are pushed only to explicit subscribers."
- [ ] Version the config: add `version: 2` support in `internal/config` with
      v1 configs parsed unchanged (no breaking change).
- [ ] Stamp the MCP server `Version` from goreleaser (the `-X` ldflags hook
      already exists in `internal/mcp/server.go`).

**Acceptance:** v1 `agent-runtime.yaml` files load identically; docs reviewed.

---

## Phase 1 — Supervision policy: readiness, health, auto-restart (2–3 weeks)

**Goal:** the single biggest production gap. Today supervision is
request-driven only; nothing notices a 3am crash. Make supervision declarative
and continuous.

### 1.1 Per-app config extensions (`internal/config`)

Extend `AppConfig` (all optional, zero breaking change):

```yaml
apps:
  api:
    command: ["go", "run", "./cmd/api"]
    readiness:                      # overrides profile regexes
      - 'listening on :8080'
    health_check:
      http: "http://localhost:8080/healthz"   # or tcp: "localhost:8080"
      interval: 10s
      timeout: 3s
      failure_threshold: 3
    restart:
      policy: on-failure            # never | on-failure | always
      backoff: [1s, 2s, 5s, 15s, 60s]   # capped exponential steps
      max_restarts: 10              # per 10-minute window; then mark crashed
```

- [ ] Parse + validate in `internal/config/config.go` (reuse the existing
      `defaults()` pattern; invalid policy strings fail load with the file/line).
- [ ] Provenance: health/readiness/restart values participate in the existing
      layered-config reporting where applicable.

### 1.2 Readiness overrides (`internal/profile`, `internal/runtime`)

- [ ] `resolveStart` already picks a profile; add: if the app declares
      `readiness`, compile those regexes (once, at config load — not per start)
      and use them instead of `prof.Ready`.
- [ ] Add a `readiness` field to `StartResult` so agents see which patterns
      are armed.
- [ ] Keep profile detection as the fallback; do not remove `internal/profile`.

### 1.3 Continuous health checking (`internal/process` — new file `health.go`)

- [ ] A per-process health goroutine starts when the instance becomes ready
      (not at exec): HTTP/TCP probe on `interval`, `timeout` per probe.
- [ ] After `failure_threshold` consecutive failures: publish
      `events.Unhealthy`, and if `restart.policy` permits, trigger a
      policy-driven restart.
- [ ] The goroutine terminates with the instance; restart creates a fresh one.
      One probe loop per process — never a shared ticker that one wedging app
      can stall (invariant 5).
- [ ] Health state (`healthy|unhealthy|unknown`, consecutive failures, last
      probe latency) goes into `process_status` output.

### 1.4 Auto-restart with backoff (`internal/process/manager.go`)

- [ ] New supervisor decision point in `waitLoop` after the exit event fires:
      if policy says restart (and backoff budget remains), schedule the restart
      on a timer — never synchronously inside `waitLoop`.
- [ ] Backoff state lives on `ManagedProcess` (survives instances, like
      `restarts`); reset after 10 minutes of stable uptime.
- [ ] Exhausted budget → status `crashed`, publish `events.Crashed`, stop.
- [ ] `restart_process` (manual) resets backoff.
- [ ] Race-focus: restart timer vs concurrent `stop_process` — the timer must
      check `stopReq` and lose. Add a dedicated stress test: crash-looping
      process + concurrent stop/signal/remove under `-race`.

### 1.5 MCP surface

- [ ] `process_status` gains: `health`, `consecutive_failures`,
      `backoff_state`, `restart_policy`.
- [ ] New tool `set_restart_policy(process_id, policy)` for ad-hoc processes
      started without an app entry.
- [ ] Update `Instructions` in `internal/mcp/server.go` and both embedded
      skills (readiness table, tool recipes).

**Acceptance:** an app that crashes on a timer is restarted with visible
backoff; an app that fails its health check 3× is restarted or marked
`crashed`; `make race` green with a new crash-loop stress test; docs + skills
updated.

---

## Phase 2 — Event subscription over MCP (1 week)

**Goal:** agents stop polling `process_status`. The `internal/events` bus
already exists and is non-blocking; it is simply not exposed.

- [ ] New tool `subscribe_events(process_id?, types?, since?)` → returns a
      subscription handle; events are delivered as MCP **notifications** (or
      polled via `get_events(subscription_id)` as a fallback for clients
      without notification support).
- [ ] Subscriber channels follow the existing broadcaster pattern (buffered
      capacity-1, re-scan model — never block the publisher).
- [ ] Hard caps: max 8 subscriptions per client, per-subscription ring of 256
      events with a drop counter (boundedness invariant).
- [ ] Event types: `started`, `ready`, `unhealthy`, `healthy`, `exited`,
      `stopped`, `crashed`, `restarted`.
- [ ] Amend invariant 4 in AGENTS.md + `Instructions`: "logs are pull-only;
      lifecycle events push only to explicit subscribers."
- [ ] Update `agent-runtime-ready` skill with the subscribe-first workflow.

**Acceptance:** an agent subscribing to `crashed,exited` receives a
notification within ~100ms of the event; a slow subscriber never delays a
publisher (stress test); drop counter observable via a new `event_stats` field
or tool.

---

## Phase 3 — Daemon mode: processes that outlive the session (2–3 weeks)

**Goal:** close the session-scoped lifetime gap. Today root-context
cancellation kills everything when the MCP server exits. Production work needs
a runtime that survives agent sessions — and lets two sessions co-manage one
stack (the demand already proven by `open_shell(pid=)`).

### 3.1 Daemon lifecycle

- [ ] `agent-runtime daemon start|stop|status` — long-lived process, PID file +
      lock in `.agent-runtime/`, owns the logstore DB (single-writer).
- [ ] Managed processes bind to the *daemon's* root context, not the MCP
      server's. The stdio server becomes a thin client of the daemon.
- [ ] On MCP-server exit: processes keep running; the next session
      re-attaches. `list_processes` after reattach shows prior processes with
      correct status (instance records already persist in the logstore).

### 3.2 Transport between server and daemon

- [ ] Local RPC over Unix socket (`$XDG_RUNTIME_DIR` or `.agent-runtime/run.sock`),
      reusing `pkg/api` types verbatim — the facade interface already isolates
      transport from logic (`internal/mcp` handlers are thin by design).
- [ ] Daemon reattachment is *adoptive*: on startup, scan for orphaned child
      processes it spawned (persist pid ↔ process_id in the logstore
      `instances` table) and resume log reading + lifecycle watching.
- [ ] Without daemon mode, behavior is exactly today's (session-scoped) — no
      regression for the default path.

### 3.3 Multi-client rules

- [ ] Single daemon per project (lock file). Multiple MCP clients may attach;
      process registry is shared; stdin ownership is exclusive (first
      `open_shell`/`send_stdin` wins, others get a clear error).
- [ ] `agent-runtime repl` and the MCP server both become daemon clients —
      REPL parity is preserved.

**Acceptance:** start app via session A, kill session A, attach session B →
`list_processes` shows the app running, `get_logs` returns its full history
(logstore), `signal_process` works. Daemon crash → next client transparently
restarts it and adoption recovers the registry. `make race` green.

---

## Phase 4 — Security posture (2 weeks)

**Goal:** today's model is "trusted local agent" — arbitrary exec, arbitrary
workdir, `open_shell(pid=)` attaches to anything, redaction is heuristic. Keep
that as the *default* (it is correct for local coding) but add an enforceable
constrained mode so CI, shared machines, and semi-trusted agents can adopt it.

### 4.1 Policy config (`internal/config`, new `internal/policy`)

```yaml
runtime:
  security:
    mode: restricted            # trusted (default) | restricted
    allowed_commands: ["go", "npm", "pnpm", "./mvnw", "python"]   # basename match
    allowed_workdirs: ["${project}", "${project}/../shared"]      # confinement
    allow_pid_attach: false     # disables open_shell(pid=)
    allow_stdin: true
    reveal_secrets: false       # hard-disable reveal=true
```

- [ ] Enforcement point is the **facade** (`internal/runtime/runtime.go`),
      never the MCP handlers — so CLI and REPL are constrained identically.
- [ ] Violations return structured errors (`policy_denied` with the rule that
      fired), not panics.

### 4.2 Audit log

- [ ] Every tool call appended to `.agent-runtime/audit.log` (JSONL): tool,
      args (secrets redacted), result, duration, caller. Bounded by size
      rotation (10MB × 5), reusing the logstore retention mindset.
- [ ] New tool `get_audit_log(lines?, contains?)` — pull-based, byte-capped,
      consistent with `get_logs`.

### 4.3 Redaction hardening (`internal/config/redact.go`)

- [ ] Current pattern list (token/password/key/secret) → configurable
      `redact_patterns` in yaml, plus entropy-based detection for high-entropy
      values as an opt-in.
- [ ] `reveal_secrets: false` in restricted mode makes `reveal=true` a
      policy error, not a silent ignore.

**Acceptance:** in restricted mode, `start_process` with a non-allowlisted
command or out-of-tree workdir fails with `policy_denied` and an audit entry;
`open_shell(pid=)` is refused; audit log is queryable and rotated.

---

## Phase 5 — Streamable HTTP transport (1.5 weeks)

**Goal:** shared/remote runtimes and CI. Depends on Phase 4 (never expose the
current no-auth server on a network).

- [ ] Add MCP streamable-HTTP transport alongside stdio in `cmd/agent-runtime`
      (`serve --http :7341`). The go-sdk already supports it; handlers unchanged.
- [ ] Auth: bearer token in yaml (`runtime.http.token`, env-expanded), plus
      optional mTLS later. No unauthenticated HTTP mode — even on localhost.
- [ ] Per-client session isolation at the transport layer; process registry
      stays shared (daemon semantics from Phase 3).
- [ ] Rate-limit tool calls per token (token bucket, e.g. 20 rps burst 50) —
      boundedness at the network edge.

**Acceptance:** two agents (one stdio, one HTTP) co-manage the same daemon;
wrong token → 401; REPL works against `--http` endpoint.

---

## Phase 6 — Observability (1.5 weeks)

**Goal:** make the runtime's own behavior measurable and the logs shippable.

- [ ] `runtime_stats` tool: process count by status, per-process line counts,
      logstore **drop counter** (already tracked in `sqlite.go` — just expose
      it), queue depth, subscription drops, uptime.
- [ ] Optional Prometheus endpoint (`runtime.metrics: ":9341"`) — counters for
      starts/stops/crashes/restarts, log lines per stream, health-probe
      latency histogram.
- [ ] `get_logs` gains `level` filter for structured logs (parse `level=`
      / `"level":` prefixes; cheap heuristic, documented as such).
- [ ] Log forwarding: implement the existing `logs.Sink` seam — a tee sink
      that fans out to SQLite **and** an OTLP/Loki/stdout writer. Config:
      `runtime.log_forward: { otlp: "localhost:4317" }`. Forwarder must be
      drop-on-backpressure like the SQLite enqueue path (invariant 5).
- [ ] Docs: "agent-runtime in CI" recipe (daemon + HTTP + metrics + audit).

**Acceptance:** `runtime_stats` reports non-zero drop counter under forced
queue-full (reuse the existing `testWriterGate` hook); Prometheus scrape shows
crash counter incrementing during the Phase 1 crash-loop test.

---

## Phase 7 — Resource governance (1 week, Linux-first)

**Goal:** bound machine impact, not just log storage.

- [ ] Per-app limits in yaml: `limits: { cpu: "1.0", memory: "512M" }`.
- [ ] Linux: one cgroup v2 slice per managed process (delegate under
      `agent-runtime.scope`); memory high/max + cpu.max. Failure to create the
      cgroup → warn and run unlimited (graceful degradation).
- [ ] Portable fallback: `RLIMIT_AS`/`RLIMIT_NPROC` via `SysProcAttr` on Unix.
- [ ] `process_status` exposes live usage from cgroup `memory.current` /
      `cpu.stat` when available.
- [ ] OOM-kill surfaces as `events.Crashed` with `reason: oom` (parse from
      cgroup `memory.events`).

**Acceptance:** a process limited to 128M that allocates 512M dies, appears as
`crashed/oom`, triggers policy restart, and the event reaches subscribers.

---

## Phase 8 — Platform parity & hardening (ongoing / 1 week initial)

- [ ] **Windows:** Job Objects for group termination (kill process tree like
      Unix `Setpgid` groups); `SignalByName` maps to CTRL_BREAK/terminate where
      meaningful; `open_shell(pid=)` via `OpenProcess`+`NtQueryInformationProcess`
      or documented as permanently Unix-only.
- [ ] **macOS:** verify `Setpgid` group-kill and `/proc`-free paths (live env
      already degrades correctly — keep the documented limitation).
- [ ] Fuzz `ParseEnvFile` and readiness-regex config paths (user-controlled
      input). `regexp.Compile` (not `MustCompile`) for yaml-supplied patterns,
      with size limits.
- [ ] Fuzz the MCP tool input structs against the go-sdk decoder.

---

## Cross-cutting workstream (every phase)

| Item | Rule |
|---|---|
| Tests | Every phase ships unit + `-race` stress tests; Phases 1/3 add integration tests against real runtimes in `internal/integration/`. |
| Docs | AGENTS.md invariants amended in the same commit as the behavior change. README feature matrix updated per phase. |
| Skills | `agent-runtime-ready` + `agent-runtime-logging` updated whenever tools, regexes, or config schema change (existing AGENTS.md rule). |
| Compatibility | v1 yaml always parses; stdio default unchanged; daemon mode opt-in until v3. |
| Layering | `internal/logstore` stays free of runtime/process/mcp imports; new `internal/policy` imports only `internal/config`. |

## Sequencing and effort

```
Phase 0  Contracts            0.5 wk  ──┐
Phase 1  Supervision policy   2–3 wk    │  biggest production win; do first
Phase 2  Event subscription   1 wk      │  cheap, unlocks agent UX
Phase 3  Daemon mode          2–3 wk    │  depends on 1 (supervision events)
Phase 4  Security posture     2 wk      │  gates Phase 5
Phase 5  HTTP transport       1.5 wk    │  depends on 4
Phase 6  Observability        1.5 wk    │  parallelizable after 2
Phase 7  Resource limits      1 wk      │  parallelizable after 3
Phase 8  Platform parity      ongoing
```

**Total: ~10–13 weeks** of focused work. Phases 1+2 alone deliver most of the
production-grade dev-loop value (crash supervision + push events); Phases 3–5
convert it from a single-agent tool into shared infrastructure; 6–8 make it
operable at scale.

## What stays out of scope (deliberately)

- Clustering / multi-node scheduling — agent-runtime supervises one machine.
- Log shipping as a managed pipeline (Phase 6 forwards; it does not buffer,
  batch, or guarantee delivery — that's a real observability agent's job).
- A web UI. The REPL and MCP tools are the interface; a dashboard is a
  separate project consuming the HTTP transport.
- Container orchestration. If the app is in Kubernetes, the platform already
  restarts and health-checks — agent-runtime targets the dev/CI/bare-metal gap.
