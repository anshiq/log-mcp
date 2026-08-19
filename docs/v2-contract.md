# agent-runtime v2 contract

This document pins down the v2 behavioral contract **before** code is written,
so the phases that build on it (supervision, events, daemon mode, security,
HTTP transport) share a single, unambiguous model. Where v2 amends v1 behavior,
the change is called out explicitly and is opt-in unless stated otherwise.

## 1. Lifetime model: session-scoped vs daemon

- **v1 (unchanged, default):** the runtime is *session-scoped*. All managed
  processes bind to the runtime's root context; when the MCP server (or CLI)
  exits, that context is cancelled and every managed process is terminated.
  This is the correct model for a single coding-agent session.
- **v2 (opt-in, Phase 3):** a `daemon` is a long-lived process that owns the
  project's managed processes and its durable log archive (single-writer). MCP
  servers and `agent-runtime repl` become *thin clients* of the daemon over a
  local Unix socket. Managed processes bind to the **daemon's** root context, so
  they survive any individual session. A session that attaches later re-attaches
  to the same registry and log history.

Rule: without daemon mode configured, behavior is byte-for-byte today's
session-scoped model. Daemon mode is opt-in and never the default in v2.

## 2. Supervision policy semantics

Supervision is **declarative and per-app**, configured in `agent-runtime.yaml`,
and is *continuous*: it is not request-driven. The runtime notices a 3am crash
on its own.

- **Readiness** — an app may override the profile-detected readiness regexes
  with its own `readiness:` list. A process is "ready" once a captured log line
  matches any armed readiness pattern (or immediately, if none are armed).
- **Health checks** — once a process is ready, a per-process probe loop runs
  (HTTP or TCP) on `interval`, with `timeout` per probe. After
  `failure_threshold` consecutive failures the process is reported
  `unhealthy` and, if its restart policy permits, a policy-driven restart is
  triggered. A successful probe resets the failure counter.
- **Auto-restart** — after an unexpected exit (or health failure), the
  supervisor may restart the process on an exponential backoff schedule. Policy
  is one of `never | on-failure | always`. `on-failure` restarts only on
  non-zero exit codes. Manual `stop_process` / shutdown never triggers a
  restart, regardless of policy.
- **Backoff & budget** — restarts within a rolling 10-minute window are limited
  to `max_restarts` and spaced by the `backoff` steps (capped exponential). An
  exhausted budget marks the process `crashed` and stops. Manual
  `restart_process` resets the backoff state.
- A process that has stayed up for a full 10-minute window has its backoff
  state reset, so a long-stable process "earns back" its restart budget.

## 3. Security model

- **Default: trusted local agent.** The agent is the developer on their own
  machine: arbitrary exec, arbitrary workdir, `open_shell(pid=)` attach, and
  heuristic secret redaction are all permitted.
- **Constrained mode (`runtime.security.mode: restricted`).** An enforceable
  policy for CI, shared machines, and semi-trusted agents:
  - `allowed_commands` — basename allowlist for `start_process`.
  - `allowed_workdirs` — confinement of `workdir` (project-relative).
  - `allow_pid_attach` — disables `open_shell(pid=)`.
  - `allow_stdin` — disables `send_stdin`.
  - `reveal_secrets` — hard-disables `get_process_env(reveal=true)`.
  - Policy violations return structured `policy_denied` errors (naming the rule
    that fired), never panics, and are written to the audit log.
- Enforcement lives in the **runtime facade**, never in MCP handlers, so the
  CLI, REPL, and MCP are constrained identically.

## 4. Transport matrix

| Transport | When | Auth | Session model |
|---|---|---|---|
| stdio (v1, default) | local single agent | trust-of-local-process | session-scoped, or a thin client of a daemon |
| streamable HTTP (v2, Phase 5) | shared/remote runtimes, CI | bearer token (env-expanded); **no unauthenticated HTTP** | per-client session isolation at the transport, shared registry via the daemon |

The go-sdk's streamable-HTTP transport is layered on unchanged handlers; the
facade interface keeps transport out of process-management logic. HTTP depends
on the security posture (Phase 4) being complete first.

## 5. Non-negotiable invariants carried into v2

1. `start_process` returns immediately — never blocks on startup or exit.
2. Processes outlive the MCP call (they bind to the daemon/runtime root
   context, never the request context).
3. Output is always consumed asynchronously (per-process reader goroutines).
4. **Logs are never pushed.** Lifecycle events are pushed only to explicit
   subscribers (Phase 2), never broadcast to the agent.
5. Nothing blocks anything else: per-process buffers, stdin, waiters,
   health loops, restart timers and cancellation keep one broken process from
   affecting another.
6. Per-process isolation: each process owns its buffers, lifecycle, health
   state and backoff; the registry keeps them independent.
7. Supervision policy is declarative and per-app; enforcement is in the facade,
   and a failure in any new subsystem degrades gracefully (warn + disable),
   never takes the daemon down.
