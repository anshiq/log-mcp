# agent-runtime v3 — Migration Plan

> From a per-project, session-scoped MCP supervisor to a **per-user background
> platform**: an always-on daemon, a central project registry under
> `~/.local/share/agent-runtime`, a versioned API, and a desktop GUI, with a TUI
> and web UI to follow on the same API.

| | |
|---|---|
| Status | Draft for build |
| Primary target | Linux (x86_64, aarch64), systemd and non-systemd hosts |
| Future targets | macOS (launchd), Windows (Service / Task Scheduler) |
| Language | Go (daemon, CLI, shim, MCP bridge, GUI shell) + TypeScript (shared UI) |
| Baseline | `master` @ `6741346` (~21k LOC Go) |

---

## Table of contents

0. [Executive summary](#0-executive-summary)
1. [Where we are today (baseline audit)](#1-where-we-are-today-baseline-audit)
2. [Target architecture](#2-target-architecture)
3. [Pillar A — The background daemon (`agentd`)](#3-pillar-a--the-background-daemon-agentd)
4. [Pillar B — Project identity & central storage](#4-pillar-b--project-identity--central-storage)
5. [Pillar C — The API (one contract for MCP, CLI, TUI, GUI, Web)](#5-pillar-c--the-api)
6. [Pillar D — Log pipeline v3](#6-pillar-d--log-pipeline-v3)
7. [Pillar E — The GUI app](#7-pillar-e--the-gui-app)
8. [Pillar F — Integrations: skills, MCP, harnesses](#8-pillar-f--integrations-skills-mcp-harnesses)
9. [Security model](#9-security-model)
10. [Cross-platform abstraction layer](#10-cross-platform-abstraction-layer)
11. [Target repository layout](#11-target-repository-layout)
12. [Data model (SQLite schema)](#12-data-model-sqlite-schema)
13. [Config schema v3](#13-config-schema-v3)
14. [Migration of existing users](#14-migration-of-existing-users)
15. [Phased delivery plan](#15-phased-delivery-plan)
17. [Packaging, release & distribution](#17-packaging-release--distribution)
18. [Observability of agent-runtime itself](#18-observability-of-agent-runtime-itself)
19. [Performance budgets](#19-performance-budgets)
20. [Risks & mitigations](#20-risks--mitigations)
21. [Open questions / decisions to confirm](#21-open-questions--decisions-to-confirm)
22. [Appendix](#22-appendix)

---

## 0. Executive summary

We split the single `agent-runtime` binary into cooperating components:

1. **`agentd`**: one daemon per OS user, started on demand by the first
   agent (or by systemd socket activation), and running until explicitly
   stopped. It owns every managed process across **all projects**, the log
   pipeline, the event bus, and the state database. MCP sessions, the CLI, the
   TUI, the GUI and (later) a web UI are all **clients** of it.
2. **`agent-runtime-shim`**: a tiny per-process supervisor, the same idea as
   `containerd-shim` or `conmon`. It holds the child's stdio pipes and exit
   status, so managed processes **and their logs** survive a daemon crash,
   restart or upgrade. This fixes today's biggest limitation: after
   `AdoptOrphans()` there is no output capture and no exit code.
3. **A project registry** in `~/.local/share/agent-runtime/state.db`
   (SQLite). A project gets a stable ULID and is resolved from its working
   directory through a **locator chain**: canonical path → (device, inode) →
   git common-dir → git root-commit + normalized remote. Moves, re-clones and
   worktrees then resolve correctly without writing anything into the repo.
   Config lives at `~/.local/share/agent-runtime/projects/<id>/agent-runtime.yaml`,
   with revision history in the DB. A checked-in repo config is still
   supported as an opt-in, **trust-gated** layer.
4. **A versioned API** defined in Protobuf and served with **Connect-RPC**
   over a Unix socket (HTTP/2 cleartext), and optionally over authenticated
   loopback TCP for the web UI. One schema produces the Go SDK (`pkg/client`),
   the TypeScript client for GUI and web, and the docs. MCP tools become a thin
   mapping over this API.
5. **A desktop GUI** built with **Wails (Go) + a TypeScript/Svelte frontend**.
   The frontend is written once against the Connect API and ships twice: inside
   the Wails desktop shell now, and as the web UI later. The Go side of the GUI
   is only a window, a tray icon, notifications, and a proxy from the webview to
   the daemon socket.

Ten phases, roughly 16–20 engineer-weeks for one senior engineer (see
[§15](#15-phased-delivery-plan)). Each phase ships something usable and keeps
`make race` green.

---

## 1. Where we are today (baseline audit)

This section records what the code does now, so the plan builds on it instead
of rewriting it.

### 1.1 What already exists and is reused

| Area | Location | Status | Reuse in v3 |
|---|---|---|---|
| Process manager, per-process isolation, process-group kill | `internal/process/{manager,lifecycle,process}.go` | Solid, race-tested | **Kept**. Spawning moves behind a `Spawner` interface so the shim can do the exec |
| Supervision: readiness, health, restart/backoff | `internal/process/{health,restart,supervision}.go` | Solid | Kept as is; runs in the daemon |
| Ring buffers + queries + waiters | `internal/logs/*` | Solid, benchmarked | Kept as the hot tail cache in front of segment files |
| SQLite log archive (async writer, janitor, retention) | `internal/logstore/sqlite.go` | Solid | Evolves into the **log index** (FTS5), fed from segments |
| Event bus + capped subscriptions | `internal/events`, `internal/runtime/events.go` | Solid | Kept; becomes the source for `WatchEvents` streams |
| Facade interface | `internal/runtime/facade.go` | Good seam | Replaced by the generated Connect service interfaces; the facade methods map roughly 1:1 onto RPCs |
| Daemon (per project) + JSON-over-UDS RPC | `internal/daemon/*` | MVP: **one daemon per project**, socket in `<project>/.agent-runtime/run.sock`, one request per connection, no streaming, no auth beyond file perms | Lifecycle code (flock, setsid, WaitReady) is reused; **RPC is replaced** |
| Orphan adoption | `internal/process/adopt.go` | Works, but no stdout/stderr and exit code is `-1` | Superseded by shim reconnection; kept as a fallback for pre-v3 orphans |
| Security policy + audit | `internal/policy`, `internal/config/security.go` | Solid | Kept; becomes per-project policy in the daemon; audit moves to the DB |
| cgroup v2 limits | `internal/process/resource_linux.go` | Uses `/sys/fs/cgroup/agent-runtime.scope/<id>`, which needs root or pre-existing delegation | Moves under the daemon's **systemd-delegated** subtree |
| Profiles (framework detection) | `internal/profile` | Solid | Kept |
| Integrations (claude/codex/gemini/opencode) + embedded skills | `internal/integrate/*` | Solid, scope-aware (uncommitted work in tree) | Becomes data-driven **harness descriptors** behind `IntegrationService` |
| Bubble Tea setup wizard | `internal/cli/tui.go` (uncommitted) | New | Seed of the future TUI; rewired to `pkg/client` |
| Streamable HTTP MCP + bearer auth + rate-limit | `internal/httpserve` | Solid | Kept for remote MCP; auth middleware reused for the web listener |
| Metrics (Prometheus) | `internal/runtime/metrics.go` | Solid | Moves into the daemon; one endpoint per user |

### 1.2 Structural limits v3 must remove

1. **The runtime is bound to one project.** `runtime.New(loaded *config.Loaded, …)`
   takes one config and one `ProjectDir`. Paths such as `.agent-runtime/logs.db`
   and `.agent-runtime/audit.log` are joined onto the project dir. v3 needs a
   **multi-tenant core**: one engine hosting N project runtimes.
2. **Process lifetime is tied to the daemon's pipes.** `lifecycle.go` uses
   in-process `io.Pipe`s, so a daemon restart cuts every child's stdout/stderr.
   The children keep running but become unobservable.
3. **State lives in the repo** (`agent-runtime.yaml`, `.agent-runtime/`). This
   is the user's point 2.
4. **The RPC cannot stream.** Live tail, event watch and process-table watch
   all need server streaming for GUI/TUI/web.
5. **Session identity is thin.** Subscriptions use a `client` string. There is
   no registry of connected sessions, so "which agent started this" and "who is
   watching this" can't be answered.
6. **Config is read once at startup.** No hot reload, no validation API, no
   history, no diff, no "which processes need restart after this edit".

### 1.3 Invariants that carry over (non-negotiable)

These come from the old `AGENTS.md` / `docs/v2-contract.md` and still hold:

1. `StartProcess` returns immediately.
2. Processes outlive the request **and now also the session and the daemon**.
3. Output is always consumed asynchronously.
4. Logs are never pushed into an agent's context. Streams exist only for
   clients that explicitly subscribe (GUI/TUI/web). MCP stays pull-only.
5. Nothing blocks anything else: per-process isolation, per-client isolation,
   per-project isolation (new).
6. Supervision is declarative and per-app. A failing subsystem degrades
   gracefully and never takes the daemon down.
7. **New:** the daemon is the **only writer** of `state.db` and of each
   project's log index. Clients never open the DB directly.
8. **New:** nothing is written into a project repository unless the user asks
   for it explicitly (export to repo, install a project-scope integration).

---

## 2. Target architecture

### 2.1 Component diagram

```
                                   ┌───────────────────────────────────────────┐
  Claude Code ─stdio─┐             │                agentd  (1 per OS user)    │
  Codex       ─stdio─┤             │                                           │
  Gemini CLI  ─stdio─┼─► agent-runtime serve ──┐   ┌─────────────────────────┐  │
  opencode    ─stdio─┘   (MCP bridge, 1/session)│   │ API layer (Connect-RPC) │  │
                                                ├──►│  System  Project Config │  │
  agent-runtime CLI  ──────────────────────────┤   │  Process Log Event      │  │
  agent-runtime tui (future) ──────────────────┤   │  Session Integration    │  │
                                                │   │  Audit                  │  │
  GUI (Wails) ─ webview ─► /api proxy ─────────┤   └───────────┬─────────────┘  │
                                                │               │                │
  Web UI (future) ─https/loopback+token────────┘   ┌───────────▼─────────────┐  │
                         ▲                         │ Core engine             │  │
                         │ UDS: $XDG_RUNTIME_DIR/  │  ProjectRegistry        │  │
                         │  agent-runtime/agentd.sock  ProjectRuntime × N   │  │
                         │                         │   ├ process.Manager     │  │
                                                   │   ├ supervision         │  │
                                                   │   ├ policy + audit      │  │
                                                   │   └ config watcher      │  │
                                                   │  SessionRegistry        │  │
                                                   │  EventBus (global)      │  │
                                                   │  LogPipeline            │  │
                                                   └───┬──────────────┬──────┘  │
                                                       │              │         │
                                          state.db ◄───┘              │ shim    │
                                          logs/<proj>/index.db        │ control │
                                          logs/<proj>/segments/*      │ (UDS)   │
                                   └──────────────────────────────────┼─────────┘
                                                                      │
                                   ┌──────────────────┬───────────────┴───┐
                                   ▼                  ▼                   ▼
                              shim (proc A)      shim (proc B)       shim (proc C)
                              holds pipes,       ...                 ...
                              writes segments,
                              records exit
                                   │
                                   ▼
                              npm run dev → node (process group + cgroup)
```

### 2.2 Binaries

| Binary | Purpose | Notes |
|---|---|---|
| `agent-runtime` | CLI, MCP bridge (`serve`), future TUI (`tui`), `daemon` control, `doctor`, `migrate` | What agents launch. Small and fast to start. Never hosts processes in v3 mode |
| `agentd` | The daemon | May also be reachable as `agent-runtime daemon run` so single-binary installs keep working |
| `agent-runtime-shim` | Per-process supervisor | Static, minimal deps, target RSS ≤ 4 MB. Versioned protocol |
| `agent-runtime-gui` | Desktop app | Wails. Separate package because it pulls in WebKitGTK |

Decision: **separate binaries in one Go module**, one goreleaser build.
`agent-runtime` embeds a "locate or spawn agentd" routine that finds `agentd`
next to itself, then on `$PATH`. The shim is a separate binary rather than a
subcommand of `agentd` so an upgrade of `agentd` never changes the executable
the running shims were launched from, and shim RSS stays small. The shim must not
import the daemon's dependency graph (SQLite, Connect, etc.).

### 2.3 Request flow examples

**Agent starts an app**

```
Claude Code → (stdio MCP) start_process{app:"api"}
  → bridge: session S already registered for workspace W (via MCP roots / cwd)
  → Connect: ProcessService.Start{workspace_id:W, app:"api", session_id:S}
  → agentd: ProjectRuntime(W).Start → resolve spec, policy check, audit
     → Spawner.Spawn → fork/exec agent-runtime-shim --bundle <dir>
        → shim: setsid, PR_SET_CHILD_SUBREAPER, create pipes, exec child into cgroup
        → shim: write segments; listen on shim.sock; report pid
     → daemon: attach to shim.sock, begin ingest, emit process.started
  ← StartResult{process_id, instance_id, pid, readiness}    (immediate)
```

**Daemon restarts while processes run**

```
agentd (old) SIGTERM with --keep-processes (default for upgrade/restart)
  → stop accepting, flush index, close shim control connections (shims keep running)
agentd (new) boots
  → scans $XDG_RUNTIME_DIR/agent-runtime/shims/*/ + instances table (state=running)
  → for each: connect shim.sock, Hello{protocol}, get {pid, status, last_seq}
  → resume ingest from index cursor → no log gap
  → processes that exited while daemon was down: shim holds exit status → recorded
```

**GUI tails logs**

```
GUI webview → fetch /api/agentruntime.v1.LogService/TailLogs (Connect streaming)
  → Wails asset-server middleware → agentd.sock (h2c) → server stream
  ← ring-buffer backlog, then live entries, with heartbeats
```

---

## 3. Pillar A — The background daemon (`agentd`)

### 3.1 Scope: per user, not per project

- **One daemon per OS user** hosts every project. This replaces today's
  per-project daemon (`<project>/.agent-runtime/run.sock`).
- Reasons: one socket to discover, one place for the GUI to connect, global
  views ("everything running on my machine"), shared resources (one log
  janitor, one metrics endpoint), and port-conflict detection across projects.
- Isolation between projects is kept *inside* the daemon: each project has its
  own `ProjectRuntime` (manager, policy, audit, config watcher, log index
  handle), and a panic in one project's goroutine is recovered and scoped to
  that project (see §3.9).

### 3.2 Paths (XDG, Linux)

| Purpose | Path | Mode |
|---|---|---|
| Socket | `$XDG_RUNTIME_DIR/agent-runtime/agentd.sock` (fallback `/tmp/agent-runtime-$UID/`) | dir `0700`, sock `0600` |
| PID + lock | `$XDG_RUNTIME_DIR/agent-runtime/agentd.pid`, `agentd.lock` (flock) | `0600` |
| Shim runtime dirs | `$XDG_RUNTIME_DIR/agent-runtime/shims/<instance_id>/` | `0700` |
| Durable data | `$XDG_DATA_HOME/agent-runtime/` (default `~/.local/share/agent-runtime/`) | `0700` |
| Daemon-wide settings | `$XDG_CONFIG_HOME/agent-runtime/agentd.yaml` | `0600` |
| Daemon's own logs | `$XDG_STATE_HOME/agent-runtime/agentd.log` (rotated) | `0600` |
| Caches (profile detection, git probes) | `$XDG_CACHE_HOME/agent-runtime/` | `0700` |

`$XDG_RUNTIME_DIR` is tmpfs and is wiped on logout or reboot. Shim state that
must outlive a reboot (it can't: processes die on reboot) is not needed there.
Everything durable goes under `XDG_DATA_HOME`.

`internal/platform/paths` owns this table. Nothing else calls
`os.UserHomeDir()` directly.

### 3.3 Startup models (all three supported, in order of preference)

1. **systemd user service with socket activation** (recommended on systemd hosts)
   - `agent-runtime daemon install` writes `~/.config/systemd/user/agentd.socket`
     and `agentd.service`, then runs `systemctl --user daemon-reload && enable --now agentd.socket`.
   - The first connection to the socket starts `agentd`. The daemon picks up the
     inherited fd via `LISTEN_FDS` (use `github.com/coreos/go-systemd/v22/activation`).
   - The service unit sets `Delegate=yes`, `KillMode=process` (**critical**: do not
     let systemd kill shims when agentd restarts), `Restart=on-failure`,
     `Type=notify` (sd_notify READY=1 once the API is serving).
   - `loginctl enable-linger $USER` is offered, never done silently, so
     processes can survive logout. The GUI and `doctor` show the linger state.
   - Unit templates live in `packaging/systemd/` and are embedded with go:embed.
2. **On-demand spawn by the client** (default fallback; works everywhere)
   - `agent-runtime serve` (or any client) calls `client.EnsureDaemon()`:
     dial socket → if refused/absent, take `agentd.lock` via flock, spawn
     `agentd` with `setsid` and a double fork, redirect to `agentd.log`, then poll
     for readiness (reuse today's `daemon.WaitReady`, capped at 5 s).
   - A spawn race between two agents starting at once is settled by the flock.
     The loser just dials.
3. **Foreground** (`agentd --foreground`) for development, containers and CI.

### 3.4 Daemon lifecycle states

```
          start                   api serving           stop (keep)          exit
 (none) ────────► booting ──────────────────► ready ────────────► draining ───────► (none)
                    │  open state.db            │                   │ close API
                    │  run migrations           │ reload (SIGHUP)   │ flush index
                    │  reconnect shims          ▼                   │ detach shims
                    │  load projects        reloading ─► ready      │ (processes keep running)
                    │  start janitor                                │
                    └─ failure → exit 1 with reason in agentd.log   │ stop (all) → stop every
                                                                      process first, then exit
```

Commands:

| Command | Behaviour |
|---|---|
| `agent-runtime daemon status` | pid, version, uptime, socket, #projects, #processes, #sessions, linger state, systemd-managed? |
| `agent-runtime daemon start` | ensure running (systemd if installed, else spawn) |
| `agent-runtime daemon stop` | **default `--keep-processes`**: shims and children keep running, the next daemon re-attaches |
| `agent-runtime daemon stop --all` | gracefully stop every managed process (concurrently, bounded by timeout), then exit |
| `agent-runtime daemon restart` | stop (keep) + start. Used by upgrades |
| `agent-runtime daemon install / uninstall` | systemd user units |
| `agent-runtime daemon logs [-f]` | tail `agentd.log` |

Idle policy (in `agentd.yaml`): `idle_exit: 0` (never, the default) or a
duration. The daemon exits only when **zero** live processes **and** zero
connected clients have held for that long. Only meaningful with socket
activation or on-demand spawn.

### 3.5 The shim (`agent-runtime-shim`)

This component lets processes outlive the daemon **with full observability**.

**Responsibilities**

1. Receive a *bundle* (JSON spec file in `shims/<instance_id>/spec.json`: argv,
   workdir, env, cgroup path, limits, stdin mode, pty yes/no, log segment dir,
   segment size, protocol version).
2. `setsid()` so it is not in the daemon's session, then
   `prctl(PR_SET_CHILD_SUBREAPER)` so grandchildren that daemonize (e.g. `npm`
   → `node`) re-parent to the shim, not to init. The shim can then reap and
   account for the whole tree.
3. Create stdout/stderr pipes (or a PTY when `pty: true`, needed for
   interactive shells in the GUI), then exec the child as a process-group
   leader into its cgroup (`UseCgroupFD`, already used in
   `resource_linux.go`).
4. Read the pipes and write **framed records** to append-only segment files
   (format in §6.2). Each record gets a monotonic `seq` that the shim assigns.
5. Serve a control socket `shims/<instance_id>/shim.sock` with a tiny
   length-prefixed protobuf protocol:
   - `Hello{protocol_version}` → `{pid, pgid, status, started_at, last_seq, exit?}`
   - `Subscribe{from_seq}` → push records live (daemon fast path; the segment
     files are the durable path)
   - `Signal{sig, group:bool}`
   - `Stdin{bytes}` / `Resize{rows,cols}` (PTY)
   - `Stop{grace}` → SIGTERM group, wait, SIGKILL (the shim enforces grace, so it
     still works with the daemon down)
   - `Release{}` → daemon has recorded the exit; shim cleans up and exits
6. On child exit: write `exit.json` (`code`, `signal`, `exited_at`, `oom_killed`
   from `memory.events`), flush segments, **stay alive** until the daemon sends
   `Release` or `shim_linger` (default 24 h) passes. This fixes the current
   "exit code unknown (-1)" after adoption.
7. No network, no SQLite, no config parsing. If the shim crashes the child
   keeps running (it is in its own process group). The daemon notices the dead
   shim socket, marks the instance `orphaned`, and falls back to today's
   pid-poll adoption (`adopt.go`). Logs from then on are lost, and the UI says so.

**Why not alternatives**

| Option | Verdict |
|---|---|
| Keep pipes in daemon (today) | Daemon restart = log loss + unknown exit codes. Rejected |
| Redirect child stdout straight to files (no shim) | Loses stream separation guarantees + ordering, no stdin, no exit code if the daemon is down, no PTY. Rejected |
| `systemd-run --user --scope` per process | systemd-only, loses stdout unless you parse journald, and hard on macOS/Windows. Kept as an **optional** backend (`spawner: systemd`) later, not the default |
| Pass fds to the new daemon via `SCM_RIGHTS` during upgrade | Only covers planned restarts, not crashes. Could be added later as an optimisation. Not needed with a shim |

**Shim protocol versioning**: `protocol_version` is an integer. The daemon
supports `[N-1, N]`. A daemon that finds a shim with an unsupported version
treats it like an orphan (signal and poll only) and flags it in the UI.

### 3.6 Multi-tenant core: `ProjectRuntime`

Refactor `internal/runtime.Runtime` into two pieces:

```go
// internal/core/engine.go
type Engine struct {
    store     *store.DB            // state.db (single writer)
    projects  *project.Registry    // identity + resolution (Pillar B)
    runtimes  sync.Map             // workspaceID -> *ProjectRuntime (lazy)
    sessions  *session.Registry
    bus       *events.Bus          // global; events carry project/workspace ids
    logs      *logpipe.Pipeline
    spawner   platform.Spawner     // shim spawner on Linux
}

// internal/core/project_runtime.go  (≈ today's runtime.Runtime, minus globals)
type ProjectRuntime struct {
    ws        *project.Workspace
    cfg       atomic.Pointer[config.Resolved] // hot-swappable
    manager   *process.Manager
    policy    *policy.Policy
    audit     *audit.Writer       // → state.db audit table
    watcher   *config.Watcher     // fsnotify on the effective config files
}
```

- A `ProjectRuntime` is created lazily on first use of a workspace and kept
  while it has live processes or connected sessions. Otherwise it is unloaded
  after 10 minutes. Its registry is rebuilt from `state.db` when reloaded.
- **Process IDs become globally unique** (`proc_<ulid>`), so a single ID is
  enough in every API call. The workspace is stored on the process row.
- The existing `process.Manager` stays per project, so `max_exited_processes`,
  eviction and shutdown semantics are unchanged.
- `runtime.Facade` is removed at the end of Phase 3. The generated Connect
  handler interfaces replace it.

### 3.7 Sessions and multi-client semantics

A **session** is one connected client: an MCP bridge instance, a CLI
invocation, the GUI, a TUI, or a web tab.

```
Session {
  id            sess_<ulid>
  kind          mcp | cli | gui | tui | web
  harness       claude-code | codex | gemini | opencode | cursor | unknown   (from MCP clientInfo)
  client_pid    pid of the bridge (via SO_PEERCRED)
  workspace_id  the workspace the session is "in" (nullable for GUI/global)
  started_at, last_seen_at
}
```

- The bridge registers on connect (`SessionService.Register`) and keeps a
  **heartbeat stream** open. When the stream drops (the agent exits, or the
  terminal closes), the daemon marks the session `closed`.
- **Process ownership is per project, not per session.** Any session in the
  same workspace sees and controls the same processes. This is what "multiple
  sessions/agents can see logs and status of the same service" means.
- Every process records `started_by_session`, and every audit row records
  the session, so the GUI can show "started by Claude Code (session …) 12 min
  ago" and "watched by: Codex, GUI".
- **Lifetime per process** (new, optional field `lifetime`):
  - `persistent` (default): survives session close. This matches the
    requirement "if session is closed, services won't exit".
  - `session`: tied to the starting session with a **lease**. When the
    session closes, a 30 s grace timer runs (covers agent restarts), then the
    process is stopped. Useful for throwaway test runs and `open_shell`.
    `open_shell` defaults to `session`.
- **Concurrency between agents**: all mutations go through the per-process
  lock in `process.Manager`. Conflicting intents (agent A restarts while agent B
  stops) resolve last-writer-wins at the manager, and both are audited.
  Optional later: advisory "claims" (`ProcessService.Claim`) so one agent can
  mark a process as busy (for example, "running migration, do not restart").

### 3.8 Daemon-side config for the daemon itself (`agentd.yaml`)

```yaml
# $XDG_CONFIG_HOME/agent-runtime/agentd.yaml
listen:
  unix: ""                     # default $XDG_RUNTIME_DIR/agent-runtime/agentd.sock
  tcp:                         # disabled by default; required for the web UI / remote tools
    addr: ""                   # e.g. 127.0.0.1:7350
    token_file: ""             # default $XDG_CONFIG_HOME/agent-runtime/token (0600, generated)
idle_exit: 0                   # 0 = never
shim:
  linger: 24h                  # how long an exited shim waits for Release
logs:
  segment_size: 16MiB
  retention:
    max_age: 7d
    max_total: 5GiB            # across all projects; LRU by project last-used
    per_process_max: 512MiB
  index:
    enabled: true              # FTS5 index for search
metrics:
  addr: ""                     # e.g. 127.0.0.1:9341
notifications:
  desktop: true                # crash/unhealthy toasts via the GUI (or notify-send fallback)
defaults:                      # defaults applied to every project unless overridden
  shell_env: login
  stop_grace: 5s
  security:
    mode: trusted
```

### 3.9 Fault isolation inside the daemon

- Every goroutine started for a project runs under `safego.Go(projectID, fn)`,
  which recovers panics, logs them with a stack, increments
  `agentd_panics_total{project}`, and marks that `ProjectRuntime` as
  `degraded`. The rest of the daemon keeps serving.
- Per-client isolation: every RPC has a deadline, streaming RPCs have bounded
  send queues (drop and send a `gap` marker instead of blocking the
  producer), and there is a max concurrent streams per session (default 32).
- State DB write failures (disk full): the daemon moves to `read-only` mode.
  Process supervision keeps working in memory, and the problem shows in
  `SystemService.Health` and in the GUI banner.

---

## 4. Pillar B — Project identity & central storage

### 4.1 The problem

Today config and state live at `<project>/agent-runtime.yaml` and
`<project>/.agent-runtime/`. Moving them to `~/.local/share/agent-runtime`
means we need a **stable, collision-free way to map a working directory to a
project** that handles:

| Scenario | Required outcome |
|---|---|
| Same repo opened from a subdirectory (`repo/services/api`) | Same project |
| Repo moved / renamed on disk | Same project, config carries over |
| Repo deleted and re-cloned to the same or another path | Same project (after confirmation if ambiguous) |
| Two independent clones of the same remote (e.g. `~/work/app` and `~/tmp/app`) | **Two workspaces** of one project: shared config, separate processes |
| `git worktree add ../app-feature` | A separate workspace of the same project: shared config, separate processes |
| Non-git directory | Works, keyed by path + inode |
| Monorepo with many apps | One project (apps inside) |
| A fork with a different remote but the same history | Different project by default. Offer "link config from …" |
| New commits | **No change**. Identity must not depend on HEAD |

### 4.2 Why the naive keys fail

| Key | Failure |
|---|---|
| Absolute path | Breaks on move/rename |
| Latest commit hash | Changes on every commit. Unusable |
| Remote URL alone | Collides for two clones or worktrees; missing for local-only repos; changes when the remote is renamed or the protocol switches between ssh and https |
| Root commit hash alone | Shared by forks and all clones. Multiple root commits after subtree merges |
| Marker file with UUID committed into the repo | Writes into the repo (rejected by requirement), and copies/forks inherit the UUID |
| inode alone | Changes on re-clone and across filesystems |

### 4.3 Chosen design: a two-level model plus a locator chain

```
Project  (logical app; owns config + history)        proj_<ulid>
   └── Workspace  (a concrete checkout on disk)      ws_<ulid>
          locators: path, (dev,ino), git_common_dir, git_worktree_dir
   fingerprints (on Project): git root commit(s), normalized remote URL(s)
```

- **Project** holds the config, its revisions, integration settings, and a
  display name.
- **Workspace** holds the running processes, the log history, and the
  per-workspace config overlay (for example, a different port for a worktree).
  All runtime state is keyed by workspace.

#### 4.3.1 Workspace root detection (from any cwd)

Order, first match wins:

1. Explicit: `--project <dir|id>` flag, `AGENT_RUNTIME_PROJECT` env var, or the
   MCP `roots/list` response. The bridge asks the harness for roots. Claude Code
   and others report the workspace folder, which beats guessing from the cwd.
2. Nearest ancestor containing a **legacy or checked-in** `agent-runtime.yaml`
   (keeps monorepo sub-configs working).
3. `git rev-parse --show-toplevel` (worktree-aware). Implement with a pure-Go
   file walk looking for a `.git` directory or `.git` file (worktree link), with
   no `git` process on the hot path. Use `go-git` only for root-commit
   discovery, and cache the result.
4. Nearest ancestor with a common project marker (`go.mod`, `package.json`,
   `pom.xml`, `pyproject.toml`, `Cargo.toml`, …) **only if** it is a
   registered workspace already. Otherwise the cwd itself.

The root is always canonicalised with `filepath.EvalSymlinks` + `Abs`.

#### 4.3.2 Resolution algorithm

```
resolve(root):
  1. exact:   SELECT workspace WHERE path = root AND exists(root)            → hit (update last_seen)
  2. moved:   SELECT workspace WHERE dev = st_dev(root) AND ino = st_ino(root)
              AND NOT exists(workspace.path)                                → rebind path, audit "moved"
  3. git:     if root is a git checkout:
                common = git_common_dir(root)   # shared by all worktrees of one clone
                a) SELECT workspace WHERE git_common_dir = common            → sibling worktree:
                     new workspace under same project
                b) fp = {root_commits, normalized_remotes}
                   candidates = projects matching fp (root commit ∩ AND (remote ∩ OR no remotes))
                   - exactly 1 candidate whose workspaces are ALL missing on disk
                       → rebind that workspace (re-clone case), audit "reclone"
                   - ≥1 candidate with live workspaces
                       → new workspace under that project (second clone)
                   - several ambiguous projects → create a new *unconfirmed* workspace,
                       surface a "Link to existing project?" prompt (GUI/CLI/MCP tool result hint)
  4. new:     create project + workspace; if a repo agent-runtime.yaml exists → import flow (§4.6)
```

Normalisation of remotes: lowercase host, strip `.git`, collapse
`git@host:owner/repo`, `ssh://git@host/owner/repo` and `https://host/owner/repo`
to `host/owner/repo`, and drop credentials. Store all remotes; `origin` is only
a display hint.

Root commits: `git rev-list --max-parents=0 HEAD` (can be several). Compute once
per workspace and refresh on demand. Shallow clones: if the root commit can't be
found (shallow), fall back to remotes only and mark the fingerprint `weak`.

Performance: resolution runs on every bridge start. Budget **< 5 ms warm**
(cache by `(root, dev, ino)` → workspace in memory; git probing only on miss).

#### 4.3.3 User-facing controls

- `agent-runtime project ls|show|rename|link|unlink|forget|gc`
- `project link <dir> <project>` attaches a workspace to another project (e.g.
  a fork).
- `project forget` deletes project state (config revisions kept in a
  recycle bin for 30 days).
- `project gc` lists workspaces whose paths no longer exist, for cleanup.
- The GUI has the same actions under Projects → ⋯ menu.

### 4.4 Storage layout

```
~/.local/share/agent-runtime/                 (XDG_DATA_HOME/agent-runtime, 0700)
├── state.db                                  SQLite (WAL): projects, workspaces, sessions,
│                                             processes, instances, config revisions, events,
│                                             audit, integrations, settings, schema_migrations
├── projects/
│   └── proj_01J…/
│       ├── agent-runtime.yaml                THE editable config (source of truth for users/editors)
│       ├── workspaces/
│       │   └── ws_01J….yaml                  optional per-workspace overlay (ports, env)
│       └── env/                              optional secret env files (0600) referenced by env_file
├── logs/
│   └── ws_01J…/
│       ├── index.db                          SQLite FTS5 log index for this workspace
│       └── segments/
│           └── proc_…/run_…/000001.seg       framed log segments written by the shim
├── trash/                                    forgotten projects (30-day retention)
└── backups/state-YYYYMMDD.db                 daily online backup (VACUUM INTO), keep 7
```

**Why YAML file + SQLite, not SQLite only**

- Users, agents and editors need a plain file to edit, diff and grep. The
  skills (`agent-runtime-ready`) teach agents to edit YAML, and this keeps
  that workflow: the agent is told the file path (new MCP tool
  `get_config_path`, and `list_apps` returns it).
- SQLite holds the **index and history**: every accepted revision (content,
  sha256, author session, timestamp, validation result), so the GUI can show
  diffs and roll back, and agents can call `config_history`.
- A **watcher** (inotify through `fsnotify`) on the effective files validates a
  change on save. Valid → new revision, reconcile (§4.5). Invalid → keep the
  last good revision active and raise `config.invalid` with file/line/column.
  The last good revision is always what runs.

**Why per-workspace log DBs rather than one giant DB**

- Write isolation (one busy project's log volume doesn't contend with another's).
- `forget`/`gc` is `rm -rf` of one directory.
- `state.db` stays small and fast (it is on the hot path for every RPC).

### 4.5 Config resolution and hot reload

Effective config for a workspace = merge (lowest → highest):

1. Built-in defaults
2. `agentd.yaml` → `defaults:`
3. **Repo layer** (opt-in, trust-gated): `<workspace>/agent-runtime.yaml` if present
   and trusted (§9.3)
4. **Project layer**: `projects/<id>/agent-runtime.yaml`
5. **Workspace overlay**: `projects/<id>/workspaces/<ws>.yaml`
6. Request-time overrides (e.g. `env` on `StartProcess`), for that start only

Merge rules: maps merge by key; `apps.<name>` merges field-wise; lists
**replace** (predictable) unless the key ends in `+` (e.g. `env+:`), which
appends. Every resolved field carries **provenance** (which layer set it),
following the existing `envSrc` idea. The GUI shows provenance on hover, and
`config explain` does the same in the CLI.

Reconciliation on a new revision (a small planner, similar to `terraform plan`):

| Change | Action |
|---|---|
| New app | Nothing (not auto-started unless `autostart: true`) |
| App removed | Running processes stay, marked `orphaned-config` in the UI |
| `command`, `workdir`, `env*`, `limits`, `type` changed | Mark running process **`stale`** (needs restart). Restart automatically only if `reload: restart` for that app |
| `readiness`, `health_check`, `restart` changed | Applied live (supervision is in the daemon, no restart needed) |
| `runtime.security` changed | Applied live for new calls. Audited |

`ConfigService.Plan` returns this diff before `Apply`, so the GUI can show
"Saving will restart: api, worker".

### 4.6 Import from repo (first contact)

When a workspace is first resolved and `<root>/agent-runtime.yaml` exists:

1. Parse and validate it. Record its sha256.
2. Default mode is `mode: import`: copy it to `projects/<id>/agent-runtime.yaml`,
   and record revision 1 with `source: imported-from-repo`.
3. Alternative mode `mode: repo-linked`: keep using the repo file as layer 3
   (for teams who check the config in). Chosen per project, and asked once
   (GUI dialog, CLI prompt, or returned as a hint in the MCP tool result).
4. The repo file is **never deleted or modified** automatically.
   `agent-runtime project export --to-repo` writes the effective config back
   when asked.
5. `<root>/.agent-runtime/logs.db` (legacy archive) → offered for import into
   the workspace log index (`agent-runtime migrate`, §14). `audit.log` → imported
   into the audit table.

---

## 5. Pillar C — The API

### 5.1 Technology choice

**Protobuf schemas + Connect-RPC (`connectrpc.com/connect`)**, generated with
**Buf** (`buf generate`, `buf lint`, `buf breaking`).

| Need | How Connect meets it |
|---|---|
| Go daemon and Go clients (CLI, bridge, TUI, GUI shell) | `connect-go`, plain `net/http` handlers, fits the existing stdlib style |
| Browser clients (GUI webview, web UI) | `@connectrpc/connect-web`: unary + **server streaming over HTTP/1.1 and HTTP/2**, no Envoy/grpc-web proxy |
| Unix socket transport | `http.Server` on a `net.Listener` from `net.Listen("unix", …)` with h2c. Clients use a custom `DialContext` |
| Also gRPC-compatible | The same handlers speak gRPC, so third-party tools (grpcurl, other languages) work |
| Evolution | `buf breaking` in CI blocks wire-incompatible changes to `v1` |
| JSON debugging | Connect's JSON codec: `curl --unix-socket agentd.sock -H 'Content-Type: application/json' -d '{}' http://agentd/agentruntime.v1.SystemService/GetVersion` |

Rejected: extending the current JSON-over-UDS RPC (no streaming, no codegen,
no browser story); plain gRPC (needs a proxy for browsers); REST + OpenAPI
(streaming is awkward, and two sources of truth once typed clients are
generated); GraphQL (overkill, and subscriptions add complexity).

`pkg/api` (today's hand-written types) is replaced by generated types.
During the transition, `pkg/api` ↔ proto conversion functions keep the MCP
handlers compiling (Phase 3).

### 5.2 Package & versioning rules

- Proto package `agentruntime.v1`, files under `api/proto/agentruntime/v1/`.
- Generated Go: `gen/agentruntime/v1` (+ `…/v1connect`). Generated TS:
  `ui/src/gen/`.
- **Additive changes only in v1.** Breaking changes create `v2` services that
  run side by side. `buf breaking --against .git#branch=master` gates CI.
- Every request that touches runtime state takes an explicit `workspace_id`
  **or** a globally unique `process_id`. There is no implicit "current
  project" on the server; clients resolve it with
  `ProjectService.Resolve{path}` first.
- Pagination: `page_size` + opaque `page_token` everywhere a list can grow.
- Field masks (`google.protobuf.FieldMask`) on `Update*` RPCs.
- Errors: Connect codes plus a structured detail
  `agentruntime.v1.ErrorInfo{reason, rule, hint, docs_url}`. For example,
  `PERMISSION_DENIED` + `reason:"policy_denied" rule:"allowed_commands"` carries
  today's `policy.Denial` over the wire.
- Handshake: every client sends the `X-Agent-Runtime-Client: <kind>/<version>`
  header. `SystemService.GetVersion` returns `{daemon_version, api_version,
  min_client_version, shim_protocol}`. Clients older than the minimum get a
  clear upgrade error.

### 5.3 Services (v1)

Full proto sketches are in the [Appendix](#22-appendix). Summary:

| Service | RPCs | Used by |
|---|---|---|
| `SystemService` | `GetVersion`, `Health`, `GetStats` (today's `runtime_stats`), `GetSettings`, `UpdateSettings`, `Shutdown{keep_processes}` | all |
| `ProjectService` | `Resolve{path}`, `ListProjects`, `GetProject`, `UpdateProject`, `ListWorkspaces`, `LinkWorkspace`, `ForgetProject`, `GC`, `TrustRepoConfig` | all |
| `ConfigService` | `GetConfig{workspace, layer?}` (raw + resolved + provenance), `GetSchema` (JSON Schema for editors), `Validate{yaml}`, `Plan{yaml}`, `Apply{yaml, base_revision}` (optimistic concurrency), `ListRevisions`, `GetRevision`, `Rollback`, `WatchConfig` (stream) | GUI editor, MCP, CLI |
| `ProcessService` | `Start`, `Stop`, `Restart`, `Signal`, `SendStdin`, `Remove`, `Get`, `List{workspace?, filter, page}`, `WatchProcesses` (stream of table diffs), `WaitForExit`, `SetRestartPolicy`, `GetEnv`, `OpenShell`, `Attach` (bidi PTY stream, §7), `GetResourceUsage` / `WatchResourceUsage` | all |
| `LogService` | `GetLogs` (today's semantics), `SearchLogs` (FTS/regex, time range, level, stream, cursor both directions), `TailLogs` (stream: backlog + live), `WaitForLog`, `ClearLogs`, `ExportLogs` (stream chunks: ndjson/txt), `GetLogStats` | all |
| `EventService` | `WatchEvents` (stream, filter by workspace/process/types, `since` cursor), `ListEvents` (paged history from DB), plus MCP-compat `Subscribe`/`Drain`/`Unsubscribe` | GUI/TUI (stream), MCP (pull) |
| `SessionService` | `Register`, `Heartbeat` (client stream: open for the session's lifetime), `ListSessions`, `GetSession`, `Close` | bridge, GUI |
| `IntegrationService` | `ListHarnesses` (detected + status per scope), `PreviewInstall` (diff), `InstallMCP`, `RemoveMCP`, `ListSkills`, `InstallSkills`, `RemoveSkills`, `CheckUpdates` | GUI, TUI, CLI wizard |
| `AuditService` | `ListAudit{workspace, filters, page}` | GUI, MCP `get_audit_log` |

### 5.4 Streaming semantics (shared by all streams)

- The first message is a **snapshot** (e.g. the current process table or the log
  backlog), followed by **deltas**, with a `cursor` on every message.
- Reconnect with `resume_cursor` means no duplicates and no gaps if the
  cursor is still retained. Otherwise the server sends `reset=true` + a fresh
  snapshot.
- A heartbeat message every 15 s keeps proxies and webviews from idling out.
- A slow consumer never blocks: bounded queue per stream (default 1024
  messages). On overflow the server coalesces (process tables) or emits
  `Gap{dropped:n}` (logs/events) and continues.

### 5.5 Transports & listeners

| Listener | Default | Auth | Clients |
|---|---|---|---|
| UDS `agentd.sock` | **on** | `SO_PEERCRED` uid == daemon uid (reject others even if file perms are wrong) | CLI, bridge, TUI, GUI shell |
| TCP loopback | off | Bearer token (from `token_file`), `Origin` allowlist, `Host` header check (DNS-rebinding defence), rate limit (reuse `httpserve.rateLimit`) | Web UI, remote tooling |
| MCP streamable HTTP (existing `serve --http`) | off | Existing bearer auth | Remote MCP clients. Now a bridge to the daemon, not a runtime host |

### 5.6 The MCP bridge (`agent-runtime serve`) in v3

Its job shrinks to translation:

1. `client.EnsureDaemon()`, then check the version handshake.
2. On MCP `initialize`: read `clientInfo` (harness name/version). After
   `initialized`: call `roots/list` if the client supports it (go-sdk
   `ServerSession.ListRoots`), else use the process cwd. Then
   `ProjectService.Resolve` → `workspace_id`, and `SessionService.Register{kind:mcp, harness, workspace}`.
   Keep the heartbeat stream open.
3. Map every existing tool 1:1 onto the API. **Tool names and argument
   shapes stay backward compatible** (the embedded skills and agents' habits
   depend on them). Defaults that change:
   - `list_processes` → scoped to the session's workspace. New optional
     `scope: "workspace" | "project" | "all"`.
   - `process_id` from another workspace is accepted (IDs are global), but
     policy is evaluated against *that* process's project.
4. New tools:
   - `get_project_info`: project id, workspace id, config path(s), trust state,
     and which other sessions are attached. It tells the agent **where the YAML
     lives now**.
   - `validate_config` / `plan_config` / `apply_config{yaml, base_revision}`:
     agents can edit config safely without hunting for files.
   - `search_logs`: bounded FTS/regex search with cursor.
   - `list_sessions`: "who else is working here".
5. `Instructions` is updated: processes persist after the session ends;
   `list_processes` on a new session shows what an earlier session started, so
   **re-attach instead of re-starting**.
6. When the session ends, the bridge exits. The daemon keeps everything
   running (except `lifetime: session` processes, per their lease).

Legacy mode `runtime.daemon: false` (session-scoped, in-process runtime) stays
available behind `agent-runtime serve --embedded` for one minor release to
de-risk the rollout, then is removed.

---

## 6. Pillar D — Log pipeline v3

### 6.1 Data flow

```
child stdout/stderr
   │ (pipes, or PTY master)
   ▼
shim: line framing + seq + ts  ──► segment file (durable, append-only, fsync every 1s or 64KiB)
   │
   └──(shim.sock Subscribe, live)──► agentd ingest
                                        ├─► ring buffer (hot tail; existing internal/logs)
                                        ├─► waiters (wait_for_log; existing)
                                        ├─► level detection (existing levelfilter.go)
                                        ├─► index writer (async, batched) ─► logs/<ws>/index.db (FTS5)
                                        ├─► forwarders (existing stdout/OTLP)
                                        └─► TailLogs streams (per-client bounded queue)
```

If the daemon is down, the shim keeps writing segments. On reconnect the daemon
reads from `index.cursor(instance)` to the segment end, catches up, and then
switches to live subscribe. **No gaps.**

### 6.2 Segment format

```
file header (32 B): magic "ARSEG\x00\x01\x00" | instance_id(16B ulid) | created_unix_ns(8B)
record:
  u32  length (of the rest)          little-endian
  u64  seq                            monotonic per instance, shared by both streams
  i64  ts_unix_ns
  u8   stream  (1=stdout 2=stderr 3=pty 4=system)
  u8   flags   (bit0=partial/truncated line, bit1=binary-escaped)
  []b  payload (≤ 256 KiB; longer lines are split with the partial flag, matching today's cap)
  u32  crc32c (of seq..payload)
```

- Rotation at `segment_size` (16 MiB default). Names are `NNNNNN.seg`.
- Torn-write recovery: on open, scan the last segment; truncate at the first
  bad CRC.
- `stream=4 system` records carry lifecycle markers ("process started pid
  1234", "exited code 1", "restarted"), so the log view reads as a timeline.
- Retention (daemon janitor, hourly): per-process cap, global cap, max age.
  Deletes whole segments, oldest first, and keeps the index in step
  (`DELETE … WHERE seq < first_retained_seq`).

### 6.3 Index (`index.db`, per workspace)

```sql
CREATE TABLE lines (
  rowid       INTEGER PRIMARY KEY,
  process_id  TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  ts          INTEGER NOT NULL,     -- unix ns
  stream      INTEGER NOT NULL,
  level       INTEGER,              -- NULL | 10 trace … 50 fatal (from levelfilter)
  seg_file    INTEGER NOT NULL,     -- segment number
  seg_off     INTEGER NOT NULL,     -- byte offset of record (to fetch raw payload)
  line        TEXT NOT NULL
);
CREATE INDEX lines_proc_seq ON lines(process_id, seq);
CREATE INDEX lines_proc_ts  ON lines(process_id, ts);
CREATE VIRTUAL TABLE lines_fts USING fts5(line, content='lines', content_rowid='rowid',
                                          tokenize='unicode61 remove_diacritics 2');
CREATE TABLE cursors (instance_id TEXT PRIMARY KEY, seq INTEGER NOT NULL);
```

- FTS5 is available in the bundled `modernc.org/sqlite` (built with
  `SQLITE_ENABLE_FTS5`, checked in `go env GOMODCACHE`).
- Regex search: FTS pre-filter when the regex has a literal prefix, else a
  bounded scan of `lines` with Go `regexp`, capped by time range and max rows
  scanned (the response includes `truncated_scan: true`).
- Index lag is exposed in `GetLogStats`. If the indexer falls behind (queue
  full), it **drops indexing, not logs**: segments are authoritative, and a
  re-index job fills the gap later. This follows the existing drop-counter
  policy.

### 6.4 `GetLogs` compatibility

`GetLogs` keeps today's semantics (tail N, stream filter, `contains`, byte
caps, `source` field) and is served from the ring, then the index, then the
segments. `source` gains the value `"segments"`.

### 6.5 Structured logs

If a line parses as JSON (and the first byte is `{`), the indexer extracts
`level`, `msg`, `time`/`ts` and stores the raw line. The GUI renders
structured lines as collapsible key/value rows. The existing
`agent-runtime-logging` skill already pushes apps toward structured output.

---

## 7. Pillar E — The GUI app

### 7.1 Framework decision

**Wails (Go) with a TypeScript + Svelte 5 frontend.** Use Wails v3 if it is
tagged stable when Phase 7 starts; otherwise v2 (stable). The framework
touchpoints are isolated in `cmd/agent-runtime-gui`, so a switch later only
affects that package.

| Candidate | Language | Verdict |
|---|---|---|
| **Wails** | Go + web frontend | **Chosen.** Same language as the rest of the codebase; reuses `pkg/client`; native webview (WebKitGTK on Linux, WebKit on macOS, WebView2 on Windows), so the binary is ~10–15 MB, not Electron's 150 MB; the frontend is reusable as the web UI; mature libraries for the hard widgets (virtualized log lists, Monaco YAML editor with JSON-Schema validation, xterm.js terminal) |
| Tauri 2 | Rust + web frontend | Strong runner-up, with the same frontend-reuse property. It adds a second backend language, and the Rust side would reimplement the daemon client and the UDS proxy. Fallback if Wails blocks us |
| Fyne / Gio | Go, native-drawn | Pure Go and a single toolchain, but a million-line virtualized log view with highlighting, a YAML code editor and a terminal emulator would all be hand-built. That is months of widget work, and none of it is reusable for web |
| Qt 6 (C++) | C++ | Excellent native UI, but it means a C++ toolchain, licensing questions (LGPL dynamic linking), and nothing shared with Go or web |
| egui / iced / Slint | Rust | Nice for tools, but weak text editor and terminal widgets, and no web reuse story (egui can do WASM, but not our API client) |

The deciding factor is the requirement: *"design the API so a future TUI and
web UI can be built on top."* With Wails, the **web UI is the GUI's frontend
served from a different host**, so the GUI and the web UI don't drift apart.

### 7.2 GUI architecture

```
┌──────────────────────── agent-runtime-gui (Go, Wails) ────────────────────────┐
│  main window (webview)                  tray icon (StatusNotifierItem / AppIndicator)
│    │                                      - status dot (all healthy / N unhealthy)
│    │ fetch("/api/…")  (Connect-web)        - quick list: running processes, stop/restart
│    ▼                                      - Open window / Quit GUI (daemon keeps running)
│  AssetServer middleware  ── /api/* ──► reverse proxy (h2c) ──► agentd.sock
│    │                        (adds X-Agent-Runtime-Client: gui/<ver>, session id)
│    └── /*  → embedded frontend (ui/dist, go:embed)
│  native bridge (Wails bindings, small):
│    - OpenFileDialog / SaveFileDialog (export logs, import config)
│    - OpenInEditor(path) ($VISUAL / xdg-open)
│    - Notify(title, body)   (desktop notifications; libnotify via D-Bus)
│    - Clipboard
│    - EnsureDaemon() + daemon install/start controls
└───────────────────────────────────────────────────────────────────────────────┘
```

- **The frontend knows only Connect.** It reaches native features through a
  `Platform` interface with two implementations, `WailsPlatform` (bindings)
  and `BrowserPlatform` (web: download via Blob, notifications via the Web
  Notification API, no tray). This is the only split between GUI and web
  builds.
- The proxy is a `httputil.ReverseProxy` with an `http2.Transport` whose
  `DialTLSContext` dials the unix socket (h2c). Streaming responses must be
  flushed. Set `FlushInterval: -1`.
- The GUI registers a `gui` session so agents can see "GUI connected".
- Closing the window leaves the tray running (configurable). **Quitting the
  GUI never stops the daemon.**

### 7.3 Frontend stack

| Concern | Choice |
|---|---|
| Framework | Svelte 5 + TypeScript + Vite (small bundle, fast; React is fine if the team prefers it, and the decision is local to `ui/`) |
| API | `@connectrpc/connect-web` + generated `ui/src/gen` (`protoc-gen-es`) |
| State | Svelte stores fed by `WatchProcesses` / `WatchEvents` streams; a normalized cache keyed by id |
| Log viewer | Custom virtualized list (fixed row height with wrap-on-demand) over a windowed buffer. Must hold 1 M+ lines at 60 fps |
| YAML editor | Monaco + `monaco-yaml` using the JSON Schema from `ConfigService.GetSchema` (autocomplete, hover docs, inline errors) |
| Diff view | Monaco diff editor (config Plan, revision compare) |
| Terminal | xterm.js + fit addon over the `ProcessService.Attach` bidi stream. Wails supports it; the web UI needs HTTP/2 or a WebSocket fallback (see §21 Q5) |
| Charts | uPlot (tiny and fast) for CPU/mem/restarts sparklines |
| Styling | Tailwind or plain CSS variables; light/dark from OS; high-contrast option |
| Tests | Vitest (unit), Playwright (e2e against the **web build** + a real daemon in CI) |

### 7.4 Screens & features

**Global shell**: left sidebar (Dashboard, Projects, Processes, Logs,
Integrations, Sessions, Audit, Settings), top bar (global search `Ctrl+K`,
daemon status pill, notifications bell), status bar (daemon version, socket,
index lag, disk use).

1. **Dashboard**
   - Cards: running / unhealthy / crashed counts, projects, sessions, disk
     used by logs, daemon uptime.
   - "Needs attention" list: crashed, unhealthy, stale (config changed),
     orphaned processes, invalid configs, untrusted repo configs.
   - Recent events timeline (from `WatchEvents`).

2. **Projects**
   - Table: name, workspaces, running/total apps, last used, config state
     (valid/invalid/untrusted).
   - Project detail: workspaces (path, branch, exists?), apps from config (with
     **Start** buttons), processes, config editor tab, integrations tab
     (project-scope MCP/skills), settings (rename, link/unlink, forget).

3. **Processes** (like Docker Desktop's Containers list)
   - Columns: status dot, name/app, project/workspace, pid, uptime, restarts,
     health, CPU %, RSS, ports (read from `/proc/<pid>/net/tcp*` for the
     process tree; nice to have), started by (session/harness).
   - Sort by any column; filter chips (status, project, harness); text filter;
     multi-select with bulk Stop/Restart/Remove.
   - Row actions: Start/Stop/Restart/Signal ▸/Open shell/Logs/Remove, with a
     confirmation on destructive actions that shows the command.

4. **Process detail** (tabs)
   - **Overview**: command, workdir, profile, lifetime, restart policy (editable
     inline → `SetRestartPolicy`), health state + last probe latency,
     readiness patterns, exit code/signal/OOM, instance history (each restart
     with duration and exit reason), resource sparklines, cgroup limits.
   - **Logs**: see 5.
   - **Environment**: redacted by default, with a reveal toggle that is
     policy-gated and audited; provenance column (shell/runtime/env_file/app/request).
   - **Events**: filtered event timeline for this process.
   - **Shell**: xterm.js `OpenShell` inside the process env, or `Attach` to the
     process's PTY if it was started with `pty: true`.
   - **Config**: the app's resolved config block with provenance, plus "Edit in
     project config".

5. **Log viewer** (single process, or merged multi-process view)
   - Live tail with auto-scroll that pauses on user scroll and a
     "Jump to live (N new)" pill.
   - Filters: stream (stdout/stderr/system), level (from detection),
     time range (relative presets + absolute), instance (current / specific
     restart / all).
   - Search: plain text (FTS), regex toggle, case toggle, "only matching lines"
     vs "highlight in context", next/prev match, match count (from the
     server).
   - Sort: chronological ↑/↓. Merged view interleaves processes by timestamp
     with a color-coded process gutter (useful for `api` + `worker`).
   - Row UX: ANSI color rendering (safe subset), clickable file:line links
     (open in editor), JSON pretty-expand, copy line, copy permalink
     (`agent-runtime://logs/<proc>?seq=…`).
   - Export: current filter → `.log`/`.ndjson` via `ExportLogs` streaming.
   - Bookmarks and "copy for agent" (copies the selected range as a bounded
     markdown block to paste into an agent chat).

6. **Config editor** (per project, workspace overlay switchable)
   - Monaco YAML with schema autocomplete and inline validation from
     `ConfigService.Validate` (debounced 300 ms).
   - **Save** → `Plan` dialog: diff + "will restart: api, worker" + "applied
     live: health_check of api" → **Apply**. Uses `base_revision` optimistic
     concurrency: if an agent edited the file meanwhile, show a 3-way merge
     prompt.
   - History sidebar: revisions with author (session/harness/GUI), time,
     message; diff any two; rollback.
   - "Form mode" (nice to have): a generated form for apps (command array
     editor, env table, health-check builder) that writes YAML.

7. **Integrations** (skills & MCP for agents/harnesses)
   - Harness grid: Claude Code, Codex, Gemini CLI, opencode, Cursor, Windsurf,
     VS Code (Copilot), Zed, Cline… Each shows installed?, version detected,
     MCP configured at global/project scope, and skills installed + version.
   - Actions per harness × scope: Install MCP, Remove MCP, Install/Update
     skills, Remove skills. Every write shows a **file diff preview** first
     (`PreviewInstall`), then applies. A backup of the touched file is kept
     (`<file>.agent-runtime.bak`).
   - "Fix all" button: bring every detected harness to the recommended setup.
   - Skills catalog: embedded skills with version, changelog, and "outdated"
     badges per install location.

8. **Sessions**: connected agents and UIs with harness, workspace, pid,
   connected-since, processes started, last tool call; the option to
   disconnect a session (closes its stream; the agent's bridge reconnects on
   the next call).

9. **Audit**: filterable table (project, session, tool/RPC, result, time), row
   → JSON detail (secrets redacted), export CSV.

10. **Settings**: daemon (autostart via systemd install/uninstall, linger
    toggle guidance, idle exit), log retention sliders with the current usage
    shown, TCP listener enable + token rotate (for web UI), notifications,
    theme, keyboard shortcuts, "Doctor" (runs diagnostics, §18).

**Notifications**: crash, unhealthy, restart budget exhausted, config invalid.
Toasts go in-app and to the desktop (debounced per process, with
"mute this process").

**Keyboard**: `Ctrl+K` palette (jump to process/project, run an action),
`/` search in logs, `g l` logs, `r` restart selected, `s` stop, `Esc` close.
All actions are reachable without a mouse.

### 7.5 Future TUI and Web UI on the same API

- **TUI** (`agent-runtime tui`): Bubble Tea (already a dependency). It uses
  `pkg/client` directly (Go), with the same streams (`WatchProcesses`,
  `TailLogs`). Screens mirror the GUI's Processes / Logs / Integrations. The
  current `tui.go` wizard becomes its Integrations screen via
  `IntegrationService`.
- **Web UI**: `ui/` built with `VITE_TARGET=web`, served by the daemon on the
  TCP listener at `/` (embedded) or by any static host, talking Connect to
  `/api` with a bearer token (login screen pastes the token or a one-time URL
  from `agent-runtime web open`, which prints `http://127.0.0.1:7350/#token=…`
  and opens the browser).

---

## 8. Pillar F — Integrations: skills, MCP, harnesses

### 8.1 Data-driven harness descriptors

Replace the per-agent `switch` statements in `internal/integrate/integrate.go`
with descriptors (embedded YAML, overridable in `$XDG_CONFIG_HOME/agent-runtime/harnesses.d/`):

```yaml
id: claude-code
display_name: Claude Code
detect:
  binaries: [claude]
  paths: ["~/.claude", "~/.claude.json"]
mcp:
  scopes:
    project: { path: "{project}/.mcp.json",            format: json,  key: "mcpServers.agent-runtime" }
    global:  { path: "~/.claude.json",                 format: json,  key: "mcpServers.agent-runtime" }
  entry:
    json: { command: "{exe}", args: ["serve"] }
skills:
  scopes:
    project: { dir: "{project}/.claude/skills" }
    global:  { dir: "~/.claude/skills" }
```

Formats supported: `json`, `jsonc` (byte-preserving, reuse the `hujson`
code from `opencode.go`), `toml` (Codex), `yaml`. Each writer is
**idempotent**, **preserves unrelated content byte for byte**, writes
atomically (temp + rename), and keeps a `.bak`.

`IntegrationService` runs in the daemon (the same user owns these files), so
the CLI wizard, TUI and GUI share one implementation and one audit trail.

### 8.2 MCP entry in v3

Every harness gets the same stdio entry: `agent-runtime serve`, with no
project-specific arguments. The bridge resolves the project from roots/cwd.
Because of this, **global scope becomes the recommended default** (one install
serves every project), and project scope remains for teams who check
`.mcp.json` in.

### 8.3 Skills

- The embedded skills are versioned (`version:` in SKILL.md frontmatter).
  `CheckUpdates` compares installed and embedded versions.
- The skills are updated for v3: config path via `get_project_info`, config
  edits via `apply_config` (or editing the file at the returned path), and the
  persistence semantics ("processes survive your session; look before you start").

---

## 9. Security model

### 9.1 Threat model (local, single-user machine)

| Threat | Mitigation |
|---|---|
| Another local user talks to the daemon | Socket dir `0700` in `$XDG_RUNTIME_DIR`; **`SO_PEERCRED` uid check** on every UDS connection |
| A malicious web page talks to the TCP listener (CSRF, DNS rebinding) | TCP off by default; bearer token required; strict `Host` (must be `127.0.0.1:port`/`localhost:port`) and `Origin` checks; no permissive CORS; token not in cookies |
| A cloned repo's `agent-runtime.yaml` runs commands on first contact | **Repo-config trust gate** (§9.3) |
| Secrets in logs/env shown in the GUI | Redaction on by default (existing `RedactEnv`); reveal is policy-gated and audited; log redaction patterns are optional (future) |
| Agent escapes the project | Existing `restricted` mode, now per project; workdir confinement is resolved against the **workspace** path |
| Tampered shim bundle | Bundle dir `0700` under the runtime dir, which only the user can write; the shim validates that the spec file owner uid matches |
| DB/log files readable by others | Data dir `0700`, files `0600`; `doctor` checks and fixes |

### 9.2 Audit

- Moves from `.agent-runtime/audit.log` (JSONL) to the `audit` table in
  `state.db`, with every RPC that mutates state plus policy denials,
  attributed to a session and harness. Retention is 90 days (configurable).
- `AuditService.ListAudit` + MCP `get_audit_log` (compatible shape).

### 9.3 Repo-config trust (in the spirit of `direnv allow`)

- A repo-layer config is **inactive** until trusted. Trust is recorded as
  `(workspace_id, path, sha256)`.
- On change of the file (new hash) → it becomes untrusted again until
  re-approved. The last trusted revision remains effective, and a banner and
  event say so.
- Trust via: GUI prompt with a diff, `agent-runtime project trust`, or an MCP
  tool that **returns a request for the human**. The agent cannot trust on the
  user's behalf unless `agentd.yaml` has `trust.allow_agent: true`
  (default false).
- Imported configs (§4.6) are copied into the project layer only after the
  same trust confirmation, so the gate can't be bypassed through import.

---

## 10. Cross-platform abstraction layer

Linux is implemented now. The interfaces are designed so macOS and Windows are
new files, not refactors.

```go
// internal/platform
type Paths interface {           // XDG on Linux; ~/Library/... on mac; %LOCALAPPDATA% on Windows
    Runtime() string; Data() string; Config() string; State() string; Cache() string
}
type IPC interface {             // UDS on Linux/mac; AF_UNIX (Win10 1803+) or named pipes on Windows
    Listen(addr string) (net.Listener, error)
    Dial(ctx context.Context, addr string) (net.Conn, error)
    PeerUID(conn net.Conn) (uint32, error)   // SO_PEERCRED / LOCAL_PEERCRED / GetNamedPipeClientProcessId
}
type Spawner interface {         // shim on Linux/mac; shim + Job Object on Windows
    Spawn(ctx context.Context, b Bundle) (ShimHandle, error)
    Reconnect(ctx context.Context, instanceDir string) (ShimHandle, error)
}
type TreeKiller interface {      // pgid+cgroup.kill on Linux; pgid on mac; Job Object on Windows (exists: jobobject_windows.go)
    Signal(h ShimHandle, sig Signal, group bool) error
}
type ServiceManager interface {  // systemd --user; launchd LaunchAgent; Windows Task Scheduler (per-user) or Service
    Install() error; Uninstall() error; Status() (ServiceStatus, error); Start() error; Stop() error
}
type ResourceMonitor interface { // /proc + cgroup v2; libproc on mac; PDH/QueryInformationJobObject on Windows
    Sample(pid int, cgroup string) (Usage, error)
}
```

| Concern | Linux (now) | macOS (future) | Windows (future) |
|---|---|---|---|
| Autostart | systemd user socket + service; fallback spawn | `~/Library/LaunchAgents/…plist` with `Sockets` activation | Task Scheduler "at logon" or a per-user service |
| IPC | UDS | UDS | AF_UNIX (Go supports it on Win10+) or `go-winio` named pipes |
| Subreaper | `PR_SET_CHILD_SUBREAPER` | none: track via pgid + `proc_listchildpids` | Job Object (inherited by children) |
| Tree kill | pgid + `cgroup.kill` | pgid | `TerminateJobObject` |
| Limits | cgroup v2 (delegated) | not available (warn) | Job Object limits |
| PTY | `creack/pty` | `creack/pty` | ConPTY (`creack/pty` supports Windows in newer versions, or `UserExistsError/conpty`) |
| GUI webview | WebKitGTK 4.1 | WKWebView | WebView2 |
| Tray | StatusNotifierItem (D-Bus) | NSStatusItem | Shell_NotifyIcon |

CI builds the Linux code on every commit and cross-compiles (without running)
for `darwin/*` and `windows/*`, so the platform stubs keep compiling.

---

## 11. Target repository layout

```
.
├── api/
│   └── proto/agentruntime/v1/
│       ├── common.proto          ids, pagination, ErrorInfo, Cursor
│       ├── system.proto
│       ├── project.proto
│       ├── config.proto
│       ├── process.proto
│       ├── log.proto
│       ├── event.proto
│       ├── session.proto
│       ├── integration.proto
│       ├── audit.proto
│       └── shim/v1/shim.proto    daemon⇄shim control protocol (internal, versioned separately)
├── buf.yaml, buf.gen.yaml
├── gen/agentruntime/v1/…         generated Go (committed; CI checks `buf generate` is clean)
├── cmd/
│   ├── agent-runtime/            CLI + MCP bridge + (future) tui
│   ├── agentd/                   daemon main
│   ├── agent-runtime-shim/       shim main (tiny)
│   └── agent-runtime-gui/        Wails app (Go side: window, tray, proxy, native bridge)
├── pkg/
│   └── client/                   PUBLIC Go SDK: EnsureDaemon, typed service clients, stream helpers, resume logic
├── internal/
│   ├── core/                     Engine + ProjectRuntime (from internal/runtime)
│   ├── server/                   Connect handlers (one file per service), interceptors (auth, audit, session, deadlines)
│   ├── daemon/                   lifecycle: lock, pid, sd_notify, signals, drain/keep-processes, idle-exit
│   ├── project/                  identity, locators, fingerprints, resolution, git probes
│   ├── store/                    state.db access + migrations (embedded SQL, versioned)
│   ├── session/                  session registry, leases
│   ├── shim/                     shim runtime (used by cmd/agent-runtime-shim) + client (used by daemon)
│   ├── logpipe/                  segments (writer/reader/recovery), ingest, indexer, search, retention
│   ├── logs/                     (existing) ring buffers + waiters: hot tail
│   ├── process/                  (existing) manager, supervision; exec moved behind platform.Spawner
│   ├── events/                   (existing) bus
│   ├── config/                   (existing + v3) schema, layered resolve, provenance, watcher, JSON Schema export, planner
│   ├── policy/                   (existing) policy; audit writer → store
│   ├── profile/                  (existing)
│   ├── integrate/                harness descriptors + writers (json/jsonc/toml/yaml) + skills
│   ├── mcp/                      (existing) tools → now call pkg/client
│   ├── platform/                 paths, ipc, spawner, treekill, servicemgr, resmon (+ _linux/_darwin/_windows)
│   ├── httpserve/                (existing) MCP over HTTP
│   └── cli/                      cobra tree; wizard → IntegrationService
├── ui/                           shared frontend (GUI + web)
│   ├── src/gen/                  generated TS clients
│   ├── src/lib/platform/         WailsPlatform | BrowserPlatform
│   ├── src/lib/stores/           processes, events, sessions (stream-fed)
│   ├── src/components/           LogViewer, ProcessTable, ConfigEditor, DiffView, Terminal, …
│   ├── src/routes/               dashboard, projects, processes, logs, integrations, sessions, audit, settings
│   └── e2e/                      Playwright
├── packaging/
│   ├── systemd/agentd.socket, agentd.service
│   ├── desktop/agent-runtime.desktop, icons/
│   ├── nix/                      flake package + Home Manager module
│   ├── flatpak/                  (GUI)
│   └── nfpm.yaml                 .deb/.rpm
├── docs/                         architecture.md (rewritten), api.md (generated), migration-v3.md, security.md
└── Plan.md
```

---

## 12. Data model (SQLite schema)

`state.db`, WAL mode, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`,
single writer goroutine (same pattern as `logstore`) plus a read pool.
Migrations are numbered embedded SQL files, applied in a transaction, recorded
in `schema_migrations`. A backup is taken **before** any migration.

```sql
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);

CREATE TABLE projects (
  id            TEXT PRIMARY KEY,               -- proj_<ulid>
  name          TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  last_used_at  INTEGER,
  config_mode   TEXT NOT NULL DEFAULT 'import', -- import | repo-linked
  active_rev    INTEGER,                        -- FK config_revisions.id (last good)
  deleted_at    INTEGER                         -- soft delete (trash)
);

CREATE TABLE project_fingerprints (
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,                    -- root_commit | remote
  value       TEXT NOT NULL,                    -- sha | normalized host/owner/repo
  weak        INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, kind, value)
);
CREATE INDEX fp_lookup ON project_fingerprints(kind, value);

CREATE TABLE workspaces (
  id              TEXT PRIMARY KEY,             -- ws_<ulid>
  project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  path            TEXT NOT NULL UNIQUE,         -- canonical realpath
  dev             INTEGER, ino INTEGER,
  git_common_dir  TEXT,                         -- shared by worktrees of one clone
  git_worktree    INTEGER NOT NULL DEFAULT 0,
  branch_hint     TEXT,
  confirmed       INTEGER NOT NULL DEFAULT 1,   -- 0 = ambiguous link pending user confirmation
  created_at      INTEGER NOT NULL,
  last_seen_at    INTEGER,
  missing_since   INTEGER                       -- set by gc when path vanishes
);
CREATE INDEX ws_devino ON workspaces(dev, ino);
CREATE INDEX ws_gitcommon ON workspaces(git_common_dir);

CREATE TABLE config_revisions (
  id           INTEGER PRIMARY KEY,
  project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  layer        TEXT NOT NULL,                   -- project | workspace:<ws_id> | repo:<ws_id>
  content      TEXT NOT NULL,
  sha256       TEXT NOT NULL,
  valid        INTEGER NOT NULL,
  errors_json  TEXT,
  source       TEXT NOT NULL,                   -- file-watch | api | import | rollback
  session_id   TEXT,
  message      TEXT,
  created_at   INTEGER NOT NULL
);
CREATE INDEX rev_proj ON config_revisions(project_id, layer, id DESC);

CREATE TABLE repo_trust (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  path         TEXT NOT NULL,
  sha256       TEXT NOT NULL,
  trusted_at   INTEGER NOT NULL,
  trusted_by   TEXT NOT NULL,                   -- session id / 'cli' / 'gui'
  PRIMARY KEY (workspace_id, path)
);

CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,                -- sess_<ulid>
  kind         TEXT NOT NULL,                   -- mcp|cli|gui|tui|web
  harness      TEXT, harness_version TEXT,
  client_pid   INTEGER,
  workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
  started_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL,
  closed_at    INTEGER
);

CREATE TABLE processes (
  id                 TEXT PRIMARY KEY,          -- proc_<ulid>  (logical, stable across restarts)
  workspace_id       TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  app                TEXT,                      -- NULL for ad-hoc
  spec_json          TEXT NOT NULL,             -- resolved StartSpec (env stored redacted-hash only; real env in bundle)
  profile            TEXT,
  lifetime           TEXT NOT NULL DEFAULT 'persistent',
  restart_policy     TEXT NOT NULL,
  started_by_session TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  config_rev         INTEGER,                   -- revision it was started from (for 'stale' detection)
  created_at         INTEGER NOT NULL,
  removed_at         INTEGER
);
CREATE INDEX proc_ws ON processes(workspace_id, removed_at);

CREATE TABLE instances (
  id           TEXT PRIMARY KEY,                -- run_<ulid>
  process_id   TEXT NOT NULL REFERENCES processes(id) ON DELETE CASCADE,
  pid          INTEGER, pgid INTEGER,
  shim_dir     TEXT,
  cgroup       TEXT,
  status       TEXT NOT NULL,                   -- starting|running|ready|exited|stopped|failed|crashed|orphaned
  started_at   INTEGER NOT NULL,
  ready_at     INTEGER,
  exited_at    INTEGER,
  exit_code    INTEGER, exit_signal TEXT, oom_killed INTEGER,
  exit_reason  TEXT                             -- user-stop|crash|health|restart|daemon-lost|...
);
CREATE INDEX inst_proc ON instances(process_id, started_at DESC);
CREATE INDEX inst_live ON instances(status) WHERE exited_at IS NULL;

CREATE TABLE events (
  id           INTEGER PRIMARY KEY,             -- global monotonic cursor
  ts           INTEGER NOT NULL,
  type         TEXT NOT NULL,
  project_id   TEXT, workspace_id TEXT, process_id TEXT, instance_id TEXT, session_id TEXT,
  payload_json TEXT
);
CREATE INDEX ev_ws ON events(workspace_id, id);
CREATE INDEX ev_proc ON events(process_id, id);

CREATE TABLE audit (
  id           INTEGER PRIMARY KEY,
  ts           INTEGER NOT NULL,
  session_id   TEXT, harness TEXT,
  project_id   TEXT, workspace_id TEXT,
  action       TEXT NOT NULL,                   -- RPC or MCP tool name
  args_json    TEXT,                            -- redacted
  result       TEXT NOT NULL,                   -- ok | error:<code> | denied:<rule>
  duration_ms  INTEGER
);
CREATE INDEX audit_ws_ts ON audit(workspace_id, ts DESC);

CREATE TABLE integrations (
  harness      TEXT NOT NULL,
  scope        TEXT NOT NULL,                   -- global | project:<project_id>
  kind         TEXT NOT NULL,                   -- mcp | skill:<name>
  path         TEXT NOT NULL,
  version      TEXT,
  installed_at INTEGER NOT NULL,
  PRIMARY KEY (harness, scope, kind)
);

CREATE TABLE settings (key TEXT PRIMARY KEY, value_json TEXT NOT NULL, updated_at INTEGER NOT NULL);
```

Retention: `events` 30 days, `audit` 90 days, closed `sessions` 30 days,
`instances` of removed processes 30 days. The janitor handles all of them.

**Env secrets are never stored in `state.db`.** The resolved env lives only in
the shim bundle (`0600`, tmpfs), and `GetEnv` reads it from the live shim or
re-resolves it from config. This keeps today's guarantee that env is never
logged.

---

## 13. Config schema v3

`version: 3`. v1/v2 files load unchanged. New keys are optional.

```yaml
version: 3

project:                       # NEW (optional) — display metadata
  name: my-app

runtime:                       # same keys as v2, now per project (daemon-wide ones moved to agentd.yaml)
  shell_env: login
  env: ["NODE_ENV=development"]
  stop_grace: 5s
  log_buffer_lines: 10000
  max_exited_processes: 50
  security:
    mode: trusted
  # REMOVED in v3 (ignored with a deprecation warning): daemon, log_store, db_path,
  #   db_max_age_days, db_max_mb, metrics, http   → now daemon-wide in agentd.yaml

apps:
  api:
    type: node
    workdir: ./services/api      # relative to the WORKSPACE root (not the yaml file location!)
    command: ["npm", "run", "dev"]
    env_file: .env               # relative to workspace; or "@project/env/api.env" for secrets kept out of the repo
    env: ["PORT=${port:api}"]    # NEW: port templating (see below)
    readiness: ['ready on']
    health_check: { http: "http://localhost:${port:api}/healthz", interval: 10s }
    restart: { policy: on-failure, max_restarts: 10 }
    limits: { cpu: "1.0", memory: 1G }
    lifetime: persistent         # NEW: persistent | session
    autostart: false             # NEW: start when the daemon loads this workspace
    reload: manual               # NEW: manual | restart — what to do when the config for this app changes
    pty: false                   # NEW: allocate a PTY (colors, interactive CLIs)
    depends_on: [db]             # NEW (Phase 9, optional): start ordering + wait-for-ready
    ports:                       # NEW (Phase 9, optional): declared ports for templating + conflict detection
      api: { default: 3000 }
```

- **Relative paths resolve against the workspace root.** In v1/v2 they
  resolved against the YAML file's directory, which was the same place. Now the
  YAML lives in the data dir, so the rule is spelled out. For a repo-linked
  config in a subdirectory (monorepo sub-config), paths resolve against that
  file's directory (backward compatible).
- **Port templating** (`${port:name}`) is what makes worktrees usable. Two
  workspaces of the same project would otherwise collide on `:3000`. The daemon
  allocates a stable port per (workspace, name). It tries `default` for the
  first workspace, then the next free port. The allocation is stored in
  `settings` and shown in the GUI. This is a Phase 9 feature, and the schema
  reserves it now.
- The JSON Schema is **generated from the Go structs** (via
  `invopop/jsonschema` plus doc comments), served by `ConfigService.GetSchema`,
  and published in the repo for editor support (`# yaml-language-server: $schema=…`).

---

## 14. Migration of existing users

### 14.1 Automatic, on first v3 contact with a workspace

1. Resolve → new project + workspace (§4.3).
2. `<root>/agent-runtime.yaml` found → trust prompt → import (default) or
   repo-linked.
3. `<root>/.agent-runtime/` found:
   - A running **v2 per-project daemon** (`daemon.pid` alive and
     `/proc/<pid>/exe` is agent-runtime) → the v3 daemon asks it to hand over:
     v2 has no handover RPC, so v3 reads the v2 `instances` rows from
     `.agent-runtime/logs.db`, adopts the pids via the **legacy adopt path**
     (pid-poll, signal-only), and sends v2's daemon `SIGTERM`. Before that, a
     patched v2.x release makes `Shutdown` skip stopping processes when
     `AGENT_RUNTIME_HANDOVER=1` is set. Adopted processes show a "legacy: no live
     logs, restart to get full capture" badge.
   - `logs.db` → background import of entries into the new index (bounded
     rate, resumable, idempotent by `(process_id, id)`).
   - `audit.log` → imported into `audit`.
   - The directory is left in place. `agent-runtime migrate --cleanup` removes
     it after confirmation.

### 14.2 Explicit command

`agent-runtime migrate [--dry-run] [--all-known] [--cleanup] [<dir>…]`
- `--all-known`: scans `~/.claude.json`, `~/.codex/config.toml`, etc. for
  project paths plus recent dirs, and finds `agent-runtime.yaml` files to import.
- Prints a report: projects created, configs imported, logs imported, legacy
  daemons stopped.

### 14.3 Agent configs

Existing MCP entries (`agent-runtime serve`) keep working unchanged: the same
command now runs as a bridge. `integrate` offers to switch project-scope
entries to one global entry (optional).

### 14.4 Compatibility window

- v3.0 keeps `serve --embedded` (old in-process mode) and reads
  `runtime.daemon` (ignored with a deprecation notice, because the daemon is
  always used).
- v3.2 removes `--embedded` and the per-project daemon code.

---

## 15. Phased delivery plan

Estimates assume one senior engineer who knows the codebase. Two engineers can
run Phases 6/7 (UI) in parallel with Phases 4/5. Each phase ends with
`make vet race test` green, docs updated, and a tagged pre-release.

### Phase 0 — Foundations & spikes (1 week)

- [ ] Restore/rewrite `AGENTS.md` and `docs/architecture.md` for v3 (currently
      deleted in the working tree), and add `docs/adr/` (architecture decision
      records) with ADR-001 per-user daemon, ADR-002 shim, ADR-003
      project identity, ADR-004 Connect-RPC, ADR-005 Wails.
- [ ] Commit or shelve the in-flight uncommitted work (`tui.go`, integrate
      scope changes) so v3 starts from a clean baseline.
- [ ] **Spike A (shim):** 300-line prototype: shim with subreaper, pipes →
      segment file, control socket. Kill -9 the "daemon", restart it,
      reconnect, and verify no lost lines on `yes | head -c 1G`-class output.
- [ ] **Spike B (Connect over UDS + browser):** h2c server on UDS, Go client
      via custom dialer, a Wails window proxying `/api` → UDS with a
      server-streaming RPC rendered live. Confirms that streaming flushes through
      the Wails asset server on WebKitGTK.
- [ ] **Spike C (identity):** resolver prototype against real scenarios: move,
      re-clone, worktree, shallow clone, non-git dir, monorepo subdir.
- [ ] Add tooling: `buf`, `protoc-gen-go`, `protoc-gen-connect-go`,
      `protoc-gen-es`; `make gen`; CI job verifying generated code is up to date.

**Exit:** the three spikes pass their scenarios; ADRs merged.

### Phase 1 — Platform layer & state store (1.5 weeks)

- [ ] `internal/platform/paths` (XDG with fallbacks), plus tests with a fake env.
- [ ] `internal/platform/ipc` (UDS listen/dial, `SO_PEERCRED`).
- [ ] `internal/store`: open/migrate/backup, single-writer queue, read pool,
      and all tables from §12. Migration tests (empty → latest; each step).
- [ ] `internal/project`: locators, fingerprints (pure-Go `.git` walk;
      root commits via `go-git` or a `git` subprocess behind an interface),
      remote normalisation, full resolution algorithm, in-memory cache.
- [ ] Table-driven tests for every scenario in §4.1, using temp git repos
      (`git init`, `worktree add`, `clone`, `mv`).

**Exit:** `agent-runtime project resolve <dir>` (debug command) gives the correct
ids for all scenarios; `state.db` migrations are idempotent.

### Phase 2 — Shim & log pipeline (2.5 weeks)

- [ ] `api/proto/agentruntime/shim/v1/shim.proto` + length-prefixed framing.
- [ ] `internal/shim` runtime: bundle load, setsid, subreaper, cgroup
      placement, pipes/PTY (`creack/pty`), segment writer (rotation, fsync
      policy, CRC), control server (Hello/Subscribe/Signal/Stdin/Resize/Stop/Release),
      exit recording (incl. OOM from `memory.events`), linger.
- [ ] `internal/logpipe`: segment reader + torn-write recovery, ingest (live
      subscribe + catch-up from cursor), indexer (FTS5, async batched, drop
      counter + re-index job), retention janitor, `Search` (FTS + bounded regex).
- [ ] `platform.Spawner` Linux implementation (spawn/reconnect).
- [ ] Refactor `internal/process/lifecycle.go`: exec now goes through
      `Spawner`; `readLoop` consumes shim records instead of `io.Pipe`.
      Supervision/waiters untouched; ring buffer fed from ingest.
- [ ] Tests: shim unit tests (signals, grandchildren reaped, group stop with
      grace while "daemon" is absent), segment fuzzing, chaos test (§16.3).

**Exit:** with the existing single-project runtime, processes and logs
survive a kill -9 of the host process with zero line loss, and exit codes
are recorded while the host was down.

### Phase 3 — Daemon core: multi-tenant engine + Connect API (3 weeks)

- [ ] Write all `v1` protos (§5.3, Appendix). `buf lint` clean.
- [ ] `internal/core`: `Engine`, `ProjectRuntime` (split from `runtime.Runtime`),
      global process ids, per-project policy/audit (audit → store), event bus
      with project/workspace ids, events persisted to `events`.
- [ ] `internal/session`: registry, heartbeat stream, leases for
      `lifetime: session`.
- [ ] `internal/server`: Connect handlers for System/Project/Process/Log/Event/
      Session/Audit; interceptors (peer-cred auth, session attribution,
      audit, deadlines, panic recovery, metrics).
- [ ] Streams: `WatchProcesses`, `TailLogs`, `WatchEvents` with snapshot +
      delta + cursor + heartbeat + gap semantics (§5.4).
- [ ] `internal/daemon` v3 lifecycle: user-level lock, sd_notify, socket
      activation, `stop --keep-processes` vs `--all`, restart, reconnect all
      shims on boot, idle exit.
- [ ] `pkg/client`: `EnsureDaemon` (spawn race via flock), typed clients,
      stream helpers with auto-resume, version handshake.
- [ ] Remove `runtime.Facade` + old JSON RPC (keep `pkg/api` ↔ proto
      converters only as long as the MCP handlers need them).

**Exit:** two CLI sessions in two different projects start/see/stop
processes through one daemon; `daemon restart` keeps all processes and logs;
`grpcurl`/`curl --unix-socket` work against the API.

### Phase 4 — MCP bridge + CLI on the API (1.5 weeks)

- [ ] `agent-runtime serve` → bridge (§5.6): roots/cwd resolution, session
      registration, all existing tools mapped, new tools (`get_project_info`,
      `validate_config`, `plan_config`, `apply_config`, `search_logs`,
      `list_sessions`).
- [ ] Update `Instructions` and both embedded skills (bump skill versions).
- [ ] CLI commands on `pkg/client`: `ps`, `logs [-f]`, `start`, `stop`,
      `restart`, `project …`, `config …`, `daemon …`, `sessions`, `doctor`.
      The REPL keeps working on the client.
- [ ] `serve --embedded` legacy mode.
- [ ] MCP compatibility test suite: replay every existing tool call from
      `internal/mcp/server_test.go` against the bridge and expect equal shapes.

**Exit:** Claude Code + Codex connected to the same project at once:
one starts `api`, the other sees it in `list_processes`, reads its logs, and
restarts it. Close both; `agent-runtime ps` still shows it running.

### Phase 5 — Config v3: central storage, layering, hot reload, trust (2 weeks)

- [ ] Layered resolve with provenance (§4.5), v3 schema keys, deprecation
      warnings for moved keys, workspace-relative paths.
- [ ] Watcher (fsnotify) → validate → revision → reconcile planner →
      `stale` marking / `reload: restart`.
- [ ] `ConfigService` full (Get/Schema/Validate/Plan/Apply with
      base_revision/Revisions/Rollback/WatchConfig).
- [ ] JSON Schema generation from structs, committed at `docs/schema/agent-runtime.v3.json`.
- [ ] Import flow + repo trust gate (§4.6, §9.3).
- [ ] `agent-runtime migrate` (§14), including legacy `logs.db`/`audit.log`
      import and the v2 daemon handover.

**Exit:** editing the YAML in `$EDITOR` hot-reloads, invalid edits are
rejected with line/col while the last good config stays active, and a cloned
repo's YAML does nothing until trusted.

### Phase 6 — Integrations service (1 week)

- [ ] Harness descriptors (embedded + user overrides) for claude-code, codex,
      gemini, opencode, and new: cursor, windsurf, vscode-copilot, zed, cline
      (verify each tool's current config location during the phase).
- [ ] Writers: json, jsonc (reuse hujson code), toml, yaml; atomic + backup
      + byte-preserving; `PreviewInstall` diffs.
- [ ] Skills versioning + `CheckUpdates`.
- [ ] Rewire the Bubble Tea wizard (`tui.go`) to `IntegrationService`.

**Exit:** GUI-independent: `agent-runtime integrate status` shows a full
matrix, and install/remove round-trips leave unrelated file content
byte-identical (golden tests).

### Phase 7 — GUI v1 (4 weeks; can start after Phase 3 with mocks)

- [ ] `cmd/agent-runtime-gui` (Wails): window, `/api` reverse proxy to UDS
      (h2c, flush), native bridge (dialogs, notify, open-in-editor, clipboard),
      tray, single-instance lock, EnsureDaemon + "daemon not running" screen.
- [ ] `ui/` scaffold: Vite + Svelte + TS, generated clients, `Platform`
      abstraction, stream-fed stores with resume, routing, theming, command
      palette.
- [ ] Screens in this order (each shippable): Processes list → Process
      detail (Overview, Logs) → Log viewer (full features) → Projects → Config
      editor (Monaco + schema + Plan/Apply + history) → Integrations →
      Sessions → Audit → Settings → Dashboard → Shell tab (xterm.js + Attach).
- [ ] Desktop notifications (crash/unhealthy), debounced + mute.
- [ ] Playwright e2e against the web build + a real daemon; a small
      WebKitGTK smoke test of the Wails binary in CI (xvfb).

**Exit:** a developer can do everything in the table in §7.4 without the
terminal; 1 M-line log view scrolls smoothly; memory of the GUI stays under
300 MB with 20 processes tailing.

### Phase 8 — Packaging, docs, hardening (1.5 weeks)

- [ ] goreleaser: `agent-runtime`, `agentd`, `agent-runtime-shim` (static, CGO off)
      for linux amd64/arm64, plus darwin/windows cross builds (not yet
      supported, but compiled).
- [ ] GUI builds: AppImage, .deb/.rpm via nfpm (depends on `libwebkit2gtk-4.1`),
      Flatpak manifest; `.desktop` file + icons.
- [ ] systemd units shipped + `daemon install`.
- [ ] **Nix flake** (package + Home Manager module with
      `services.agent-runtime.enable`), since the primary dev box is NixOS.
- [ ] `docs/`: architecture, API reference (generated from proto comments),
      migration guide, security, troubleshooting (`doctor` output
      explained). A new README.
- [ ] Security review (UDS peer-cred, TCP listener, trust gate, file modes),
      fuzzing campaigns (config, segment reader, proto handlers).

**Exit:** a clean-machine install on Ubuntu 24.04, Fedora 41 and NixOS works
from the packages; `agent-runtime doctor` is green.

### Phase 9 — Post-v3.0 enhancements (backlog, prioritise after launch)

- [ ] Web UI (`ui/` web build served by the daemon on the TCP listener, token login).
- [ ] Full TUI (`agent-runtime tui`) on `pkg/client`.
- [ ] Port templating + conflict detection (`${port:name}`).
- [ ] `depends_on` ordering and compose-like "start stack".
- [ ] Listening-port discovery per process (`/proc/net/tcp` × tree pids).
- [ ] Log redaction rules; log-based alerts ("notify me when /panic/ appears").
- [ ] macOS: launchd, `platform/*_darwin.go`, notarised GUI.
- [ ] Windows: named pipes/AF_UNIX, Job Objects, ConPTY, WebView2 GUI, MSI.
- [ ] Remote daemon (connect GUI to a dev VM over SSH-forwarded socket).
- [ ] Optional `spawner: systemd` backend (`systemd-run --user --scope`).

### Timeline summary

| Phase | Weeks | Cumulative |
|---|---|---|
| 0 Foundations & spikes | 1 | 1 |
| 1 Platform & store | 1.5 | 2.5 |
| 2 Shim & logs | 2.5 | 5 |
| 3 Daemon core & API | 3 | 8 |
| 4 MCP bridge & CLI | 1.5 | 9.5 |
| 5 Config v3 | 2 | 11.5 |
| 6 Integrations | 1 | 12.5 |
| 7 GUI v1 | 4 | 16.5 |
| 8 Packaging & hardening | 1.5 | **18** |

Critical path: 0 → 1 → 2 → 3 → 4. Phase 7 can start at week 8 with the API
frozen (using a mock server generated from the protos until then).

### Release milestones

- **v3.0.0-alpha.1** (end of Phase 4): daemon + bridge + CLI; config still read
  from repo (imported); no GUI. Dogfood internally.
- **v3.0.0-beta.1** (end of Phase 6): central config, trust, migrations,
  integrations.
- **v3.0.0-rc.1** (end of Phase 7): GUI.
- **v3.0.0** (end of Phase 8).

---

## 16. Testing strategy

### 16.1 Levels

| Level | What | Where |
|---|---|---|
| Unit | Pure logic: resolver, remote normalisation, config merge/provenance, planner, segment codec, harness writers | package `_test.go` |
| Race | Every package, always (`make race`), plus new stress tests: N sessions × M processes × concurrent start/stop/restart/remove/tail | CI required |
| Integration | Real `agentd` in a temp `XDG_*` sandbox, real shims, real child helpers (reuse `internal/process/testdata/helper`) | `internal/integration/` |
| Chaos | kill -9 daemon / shim / child at random points; disk full (tmpfs with size limit); slow consumer; clock jumps | `internal/integration/chaos_test.go` (build tag `chaos`, nightly) |
| Compatibility | MCP tool shapes vs v2 golden files; config v1/v2 fixtures load in v3; `buf breaking` | CI required |
| Fuzz | Config YAML (existing), segment reader, shim frame decoder, remote-URL normaliser, Connect JSON decoding | `go test -fuzz` nightly |
| UI unit | Stores, log viewer windowing, search highlighting | Vitest |
| UI e2e | Web build + real daemon: start app, tail, search, edit config → plan → apply, install integration into a temp HOME | Playwright, CI |
| GUI smoke | Wails binary launches under xvfb, the proxy streams a `TailLogs` response | CI (Linux) |
| Benchmarks | Ingest throughput, index throughput, search latency, resolve latency | `go test -bench`, tracked over time |

### 16.2 Key acceptance scenarios (automated)

1. Session A starts `api`, disconnects; session B (different harness) lists,
   tails and restarts it.
2. `agentd` kill -9 during 50k lines/s output → restart → gapless `seq`
   sequence in index; exit that happened during downtime recorded with
   correct code.
3. `daemon stop --all` stops 50 processes concurrently within
   `max(stop_grace)+1s`.
4. Repo moved (`mv`), re-cloned, worktree added → resolution outcomes per §4.1.
5. Invalid YAML save → last good stays active; event + error with line/col.
6. Untrusted repo config → `Start{app}` refused with `reason: repo_untrusted`.
7. Other-uid connection to the UDS rejected (test via `unshare`/second user in CI container).
8. GUI: 1 M lines loaded, search "timeout" returns counts < 300 ms, scroll 60 fps (Playwright trace).

### 16.3 Chaos harness sketch

```
for i in 1..200:
   start K processes emitting seq-numbered lines at random rates
   random action: kill -9 agentd | kill -9 a shim | kill -9 a child | SIGSTOP agentd 5s | fill disk
   restart agentd if dead
   assert: every process's indexed seq set is contiguous from first_retained to last_written
           (except after shim death: gap is flagged, not silent)
   assert: no process leaked (cgroup empty after remove), no zombie
```

---

## 17. Packaging, release & distribution

| Artifact | Contents | Channel |
|---|---|---|
| `agent-runtime_<ver>_linux_<arch>.tar.gz` | agent-runtime, agentd, agent-runtime-shim, systemd units, completions | GitHub Releases (existing goreleaser, extended) |
| `.deb` / `.rpm` (CLI) | same, units in `/usr/lib/systemd/user/` | GitHub Releases; later apt/yum repo |
| `agent-runtime-gui` AppImage | GUI (bundles webkit? no: AppImage uses system WebKitGTK; document the dependency) | GitHub Releases |
| `.deb` / `.rpm` (GUI) | GUI + .desktop + icons, depends on `libwebkit2gtk-4.1-0` | GitHub Releases |
| Flatpak | GUI (GNOME runtime provides WebKitGTK); the socket is reached via `--filesystem=xdg-run/agent-runtime` | Flathub (later) |
| Nix | flake: packages + Home Manager module | repo `packaging/nix`, later nixpkgs |
| `go install` | CLI + daemon + shim | as today |

Versioning: all binaries share one version (SemVer) and the API version is
`v1`. `agentd` refuses shims with an unsupported protocol version and clients
below `min_client_version`, and both paths return actionable errors ("run
`agent-runtime daemon restart` after upgrading").

Upgrade flow: package manager replaces binaries → post-install hook (deb/rpm)
or user runs `agent-runtime daemon restart` → running processes keep running
(shims untouched) → new daemon reconnects. The GUI shows "daemon version
differs from GUI; restart daemon?" when versions skew.

---

## 18. Observability of agent-runtime itself

- **Daemon log**: `slog` JSON to `$XDG_STATE_HOME/agent-runtime/agentd.log`
  (rotated, 10 MB × 5), or to journald when running under systemd (detect
  `JOURNAL_STREAM`). `--log-level`, and `SystemService.UpdateSettings{log_level}`
  at runtime.
- **Metrics** (existing Prometheus endpoint, extended): processes by status,
  restarts/crashes, ingest lines/s, index lag, drops (ingest/index/stream), RPC
  latency histograms by method, open streams, sessions by harness, DB size,
  segment disk use, panics by project.
- **pprof**: `agentd --pprof 127.0.0.1:6060` (off by default).
- **`agent-runtime doctor`**: checks and fixes where safe:
  daemon reachable + version skew, socket perms, data dir perms, systemd units
  and linger, cgroup v2 + delegation, inotify watch limits
  (`fs.inotify.max_user_watches`), disk space, WebKitGTK present (for GUI),
  harness integrations status, legacy `.agent-runtime/` dirs pending
  migration, orphaned shims, stale workspaces. Output is human-readable or
  `--json` (the GUI Settings → Doctor renders the JSON).

---

## 19. Performance budgets

| Metric | Budget |
|---|---|
| `agent-runtime serve` cold start to first tool response (daemon running) | < 150 ms |
| … with daemon spawn | < 800 ms |
| Workspace resolve (warm / cold with git probe) | < 5 ms / < 50 ms |
| `StartProcess` RPC latency (excluding the app's own startup) | < 30 ms p99 |
| Ingest throughput per process (shim → segment → daemon ring) | ≥ 100k lines/s (100 B lines) |
| Aggregate ingest | ≥ 300k lines/s across processes on a 4-core laptop |
| Index lag at steady 10k lines/s | < 1 s |
| `SearchLogs` FTS over 10 M lines | < 300 ms p95 |
| `TailLogs` live latency (write → GUI render) | < 100 ms p95 |
| Shim RSS | ≤ 4 MB (target), ≤ 8 MB (hard) |
| Daemon RSS idle / 50 processes | < 40 MB / < 250 MB (ring buffers dominate; configurable) |
| GUI RSS with 20 tailing processes | < 300 MB |

Benchmarks live next to the code and a nightly job records trends.

---

## 20. Risks & mitigations

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | Wails v3 not stable / Linux streaming quirks in WebKitGTK | Med | High | Phase 0 Spike B; v2 fallback; frontend is framework-agnostic (Connect over `/api`), so moving to Tauri only touches `cmd/agent-runtime-gui` |
| R2 | systemd kills shims on daemon restart | Med | High | `KillMode=process` in the unit; shims `setsid` and could move themselves to a sibling cgroup scope (`systemd-run --user --scope` for shims as an option); covered by integration test on systemd CI runner |
| R3 | Identity heuristics link the wrong project | Low-Med | High (wrong commands run) | Ambiguous → never auto-link; `confirmed=0` workspaces require user action; repo-config trust gate; `project link/unlink` easy to fix; everything audited |
| R4 | Log volume fills the disk | Med | Med | Global + per-process caps, janitor, read-only degradation instead of crash, GUI disk meter, `doctor` warning |
| R5 | Scope creep in the GUI | High | Med | Screen order in Phase 7 is ship-in-order; Phase 9 backlog absorbs extras |
| R6 | Breaking agents' learned behaviour (tool names, config location) | Med | Med | Tool names/shapes frozen; `get_project_info` tells agents where config lives; skills updated in the same PR; compatibility test suite |
| R7 | SQLite write contention (state.db) under many sessions | Low | Med | Single-writer queue, WAL, logs in separate per-workspace DBs, events batched |
| R8 | cgroup delegation missing on non-systemd hosts | Med | Low | Limits degrade to "unlimited + warning" (existing policy); tree tracking still works via subreaper + pgid |
| R9 | The shim adds a process per managed process | Certain | Low | ≤ 4 MB RSS each; dev machines run tens, not thousands; documented |
| R10 | Two daemons for one user (e.g. systemd + manual spawn) | Low | High | Single user-level flock; the second instance exits with a clear message; `doctor` detects |
| R11 | Legacy v2 handover leaves unobservable processes | Med | Low | Clearly badged "legacy" in UI with one-click restart to get full capture |

---

## 21. Open questions / decisions to confirm

These have defaults in this plan. Confirm or override before Phase 0 ends.

1. **Default lifetime of processes**: `persistent` (plan default, matches the
   requirement) or `session` with opt-in persistence? Plan: persistent;
   `open_shell` = session.
2. **Clones of the same remote**: shared config (two workspaces, one project,
   the plan default) or fully separate projects? The plan shares, with a
   per-workspace overlay.
3. **Repo-linked config**: should teams be able to keep `agent-runtime.yaml`
   in the repo as the *primary* source (plan: yes, opt-in `repo-linked` +
   trust)?
4. **Frontend framework**: Svelte (plan) vs React (bigger hiring pool, heavier).
5. **Interactive shell over the web UI**: Connect bidi streaming needs
   HTTP/2 end to end; browsers can't do bidi over fetch today. Options: a
   WebSocket endpoint for `Attach` only (plan for web), or half-duplex
   (server-stream output + unary input RPCs). The GUI proxy supports both.
6. **Agent self-trust**: can an agent approve repo-config trust
   (`trust.allow_agent`)? Plan: no by default.
7. **Harness list for v3.0**: plan ships claude-code, codex, gemini, opencode +
   cursor, windsurf, vscode-copilot, zed, cline. Confirm priorities.
8. **Telemetry**: none (plan). Confirm that no opt-in usage analytics are wanted.
9. **License** of GUI deps (Monaco MIT, xterm.js MIT, WebKitGTK LGPL
   dynamically linked): fine for distribution? Plan: yes.

---

## 22. Appendix

### A. Proto sketches (abridged)

```protobuf
// api/proto/agentruntime/v1/common.proto
syntax = "proto3";
package agentruntime.v1;
option go_package = "agent-runtime/gen/agentruntime/v1;agentruntimev1";

import "google/protobuf/timestamp.proto";

message PageRequest  { int32 page_size = 1; string page_token = 2; }
message PageResponse { string next_page_token = 1; int64 total_estimate = 2; }
message Cursor       { string value = 1; }           // opaque, resumable
message Heartbeat    { google.protobuf.Timestamp at = 1; }
message Gap          { int64 dropped = 1; string reason = 2; }

message ErrorInfo {          // attached as a Connect error detail
  string reason   = 1;       // policy_denied | repo_untrusted | not_found | stale_revision | ...
  string rule     = 2;       // e.g. allowed_commands
  string hint     = 3;
  string docs_url = 4;
}
```

```protobuf
// api/proto/agentruntime/v1/process.proto (abridged)
service ProcessService {
  rpc Start(StartRequest) returns (StartResponse);
  rpc Stop(StopRequest) returns (StopResponse);
  rpc Restart(RestartRequest) returns (RestartResponse);
  rpc Signal(SignalRequest) returns (SignalResponse);
  rpc SendStdin(SendStdinRequest) returns (SendStdinResponse);
  rpc Remove(RemoveRequest) returns (RemoveResponse);
  rpc Get(GetProcessRequest) returns (Process);
  rpc List(ListProcessesRequest) returns (ListProcessesResponse);
  rpc WatchProcesses(WatchProcessesRequest) returns (stream ProcessTableUpdate);
  rpc WaitForExit(WaitForExitRequest) returns (WaitForExitResponse);
  rpc SetRestartPolicy(SetRestartPolicyRequest) returns (SetRestartPolicyResponse);
  rpc GetEnv(GetEnvRequest) returns (GetEnvResponse);
  rpc OpenShell(OpenShellRequest) returns (StartResponse);
  rpc Attach(stream AttachInput) returns (stream AttachOutput);          // PTY bidi
  rpc WatchResourceUsage(WatchResourceUsageRequest) returns (stream ResourceUsage);
}

enum ProcessStatus {
  PROCESS_STATUS_UNSPECIFIED = 0;
  PROCESS_STATUS_STARTING = 1; PROCESS_STATUS_RUNNING = 2; PROCESS_STATUS_READY = 3;
  PROCESS_STATUS_EXITED = 4;   PROCESS_STATUS_STOPPED = 5; PROCESS_STATUS_FAILED = 6;
  PROCESS_STATUS_CRASHED = 7;  PROCESS_STATUS_ORPHANED = 8;
}
enum Lifetime { LIFETIME_UNSPECIFIED = 0; LIFETIME_PERSISTENT = 1; LIFETIME_SESSION = 2; }

message StartRequest {
  string workspace_id = 1;
  oneof target { string app = 2; RawCommand raw = 3; }
  repeated string env = 4;                 // request-layer env
  optional Lifetime lifetime = 5;
  optional bool pty = 6;
}
message RawCommand { string command = 1; repeated string args = 2; string workdir = 3; string profile = 4; }
message StartResponse {
  string process_id = 1; string instance_id = 2; int32 pid = 3;
  string profile = 4; repeated string readiness = 5; string command_line = 6;
}

message Process {
  string id = 1; string workspace_id = 2; string project_id = 3; string app = 4;
  ProcessStatus status = 5; Health health = 6; int32 pid = 7;
  string instance_id = 8; int32 restarts = 9;
  google.protobuf.Timestamp started_at = 10; google.protobuf.Timestamp exited_at = 11;
  optional int32 exit_code = 12; string exit_reason = 13;
  string command_line = 14; string workdir = 15; string profile = 16;
  Lifetime lifetime = 17; string restart_policy = 18;
  string started_by_session = 19; bool stale = 20;     // config changed since start
  LogCounts log_counts = 21;
}

message WatchProcessesRequest { string workspace_id = 1; bool all_workspaces = 2; Cursor resume = 3; }
message ProcessTableUpdate {
  oneof kind {
    Snapshot snapshot = 1;       // first message (or after reset)
    Process upsert = 2;
    string removed_id = 3;
    Heartbeat heartbeat = 4;
  }
  Cursor cursor = 10;
  bool reset = 11;
  message Snapshot { repeated Process processes = 1; }
}
```

```protobuf
// api/proto/agentruntime/v1/log.proto (abridged)
service LogService {
  rpc GetLogs(GetLogsRequest) returns (GetLogsResponse);             // MCP-compatible tail
  rpc SearchLogs(SearchLogsRequest) returns (SearchLogsResponse);
  rpc TailLogs(TailLogsRequest) returns (stream TailLogsResponse);
  rpc WaitForLog(WaitForLogRequest) returns (WaitForLogResponse);
  rpc ClearLogs(ClearLogsRequest) returns (ClearLogsResponse);
  rpc ExportLogs(ExportLogsRequest) returns (stream ExportChunk);
  rpc GetLogStats(GetLogStatsRequest) returns (LogStats);
}

enum Stream { STREAM_UNSPECIFIED = 0; STREAM_STDOUT = 1; STREAM_STDERR = 2; STREAM_PTY = 3; STREAM_SYSTEM = 4; }
enum Level  { LEVEL_UNSPECIFIED = 0; LEVEL_TRACE = 10; LEVEL_DEBUG = 20; LEVEL_INFO = 30; LEVEL_WARN = 40; LEVEL_ERROR = 50; LEVEL_FATAL = 60; }

message LogLine {
  string process_id = 1; string instance_id = 2; uint64 seq = 3;
  google.protobuf.Timestamp ts = 4; Stream stream = 5; Level level = 6;
  string text = 7; bool partial = 8;
}

message SearchLogsRequest {
  repeated string process_ids = 1;          // empty + workspace_id = all in workspace
  string workspace_id = 2;
  string query = 3;                         // FTS syntax, or regex when regex=true
  bool regex = 4; bool case_sensitive = 5;
  repeated Stream streams = 6; Level min_level = 7;
  google.protobuf.Timestamp from = 8; google.protobuf.Timestamp to = 9;
  string instance_id = 10;
  enum Order { ORDER_UNSPECIFIED = 0; ORDER_ASC = 1; ORDER_DESC = 2; }
  Order order = 11;
  PageRequest page = 12;
  int32 context_lines = 13;                 // lines of context around each match
}
message SearchLogsResponse {
  repeated LogMatch matches = 1; PageResponse page = 2;
  int64 total_matches_estimate = 3; bool truncated_scan = 4;
  message LogMatch { LogLine line = 1; repeated Range highlights = 2; repeated LogLine before = 3; repeated LogLine after = 4; }
  message Range { int32 start = 1; int32 end = 2; }
}

message TailLogsRequest {
  repeated string process_ids = 1; string workspace_id = 2;
  int32 backlog = 3;                        // lines of history first (default 500)
  repeated Stream streams = 4; Level min_level = 5; string contains = 6;
  Cursor resume = 7;
}
message TailLogsResponse {
  oneof kind { LogBatch batch = 1; Gap gap = 2; Heartbeat heartbeat = 3; ProcessMarker marker = 4; }
  Cursor cursor = 10;
  message LogBatch { repeated LogLine lines = 1; }     // batched ≤ 50 ms / 256 lines for efficiency
  message ProcessMarker { string process_id = 1; string instance_id = 2; string what = 3; } // started/exited/restarted
}
```

```protobuf
// api/proto/agentruntime/v1/config.proto (abridged)
service ConfigService {
  rpc GetConfig(GetConfigRequest) returns (GetConfigResponse);
  rpc GetSchema(GetSchemaRequest) returns (GetSchemaResponse);   // JSON Schema string
  rpc Validate(ValidateRequest) returns (ValidateResponse);
  rpc Plan(PlanRequest) returns (PlanResponse);
  rpc Apply(ApplyRequest) returns (ApplyResponse);
  rpc ListRevisions(ListRevisionsRequest) returns (ListRevisionsResponse);
  rpc GetRevision(GetRevisionRequest) returns (Revision);
  rpc Rollback(RollbackRequest) returns (ApplyResponse);
  rpc WatchConfig(WatchConfigRequest) returns (stream ConfigEvent);
}
message ValidationError { int32 line = 1; int32 column = 2; string path = 3; string message = 4; }
message PlanResponse {
  repeated ValidationError errors = 1;
  repeated AppChange changes = 2;
  message AppChange {
    string app = 1;
    enum Kind { KIND_UNSPECIFIED = 0; ADDED = 1; REMOVED = 2; RESTART_REQUIRED = 3; APPLIED_LIVE = 4; }
    Kind kind = 2; repeated string fields = 3; repeated string affected_process_ids = 4;
  }
  string diff = 3;                            // unified diff vs active revision
}
message ApplyRequest {
  string project_id = 1; string layer = 2;   // "project" | "workspace:<id>"
  string yaml = 3; int64 base_revision = 4;  // optimistic concurrency → FAILED_PRECONDITION + stale_revision
  string message = 5; bool restart_affected = 6;
}
```

```protobuf
// api/proto/agentruntime/shim/v1/shim.proto (internal)
// Framing on shim.sock: u32 length (LE) + protobuf message. One control conn at a time
// (the daemon); a second connection replaces the first (daemon restart).
message ShimRequest {
  uint32 protocol_version = 1;
  oneof op {
    Hello hello = 2; Subscribe subscribe = 3; Signal signal = 4; Stdin stdin = 5;
    Resize resize = 6; Stop stop = 7; Release release = 8;
  }
}
message ShimEvent {
  oneof ev { HelloAck hello = 1; Record record = 2; Exited exited = 3; Error error = 4; }
}
message HelloAck { int32 pid = 1; int32 pgid = 2; string status = 3; int64 started_unix_ns = 4; uint64 last_seq = 5; optional Exited exit = 6; }
message Record   { uint64 seq = 1; int64 ts_unix_ns = 2; uint32 stream = 3; bool partial = 4; bytes payload = 5; }
message Exited   { int32 code = 1; string signal = 2; bool oom_killed = 3; int64 exited_unix_ns = 4; }
```

### B. MCP tool ↔ API mapping

| MCP tool (unchanged name) | API call | Notes |
|---|---|---|
| `start_process` | `ProcessService.Start` | `workspace_id` injected from session |
| `stop_process` | `ProcessService.Stop` | |
| `restart_process` | `ProcessService.Restart` | |
| `process_status` | `ProcessService.Get` | |
| `list_processes` | `ProcessService.List` | default scope workspace; new `scope` arg |
| `get_logs` | `LogService.GetLogs` | same caps/shape |
| `clear_logs` | `LogService.ClearLogs` | |
| `send_stdin` | `ProcessService.SendStdin` | |
| `wait_for_log` | `LogService.WaitForLog` | |
| `wait_for_exit` | `ProcessService.WaitForExit` | |
| `list_apps` | `ConfigService.GetConfig` (apps view) | now also returns config path |
| `remove_process` | `ProcessService.Remove` | |
| `signal_process` | `ProcessService.Signal` | |
| `get_process_env` | `ProcessService.GetEnv` | |
| `open_shell` | `ProcessService.OpenShell` | default `lifetime: session` |
| `set_restart_policy` | `ProcessService.SetRestartPolicy` | |
| `subscribe_events` / `get_events` / `unsubscribe_events` | `EventService.Subscribe/Drain/Unsubscribe` | server-side ring (pull model kept for MCP) |
| `runtime_stats` | `SystemService.GetStats` | |
| `get_audit_log` | `AuditService.ListAudit` | |
| **new** `get_project_info` | `ProjectService.GetProject` + `ConfigService.GetConfig` | tells the agent where the YAML is |
| **new** `validate_config` / `plan_config` / `apply_config` | `ConfigService.Validate/Plan/Apply` | |
| **new** `search_logs` | `LogService.SearchLogs` | bounded |
| **new** `list_sessions` | `SessionService.ListSessions` | |

### C. Event types (v3)

Existing: `process.started`, `process.ready`, `process.exited`, `process.stopped`,
`process.crashed`, `process.restarted`, `process.healthy`, `process.unhealthy`.

New: `process.stale`, `process.orphaned`, `process.removed`,
`config.applied`, `config.invalid`, `config.untrusted`, `project.created`,
`workspace.linked`, `workspace.moved`, `session.opened`, `session.closed`,
`daemon.degraded`, `logs.gap`, `integration.changed`.

### D. Library shortlist

| Purpose | Library |
|---|---|
| RPC | `connectrpc.com/connect`, `golang.org/x/net/http2` (h2c) |
| Protobuf toolchain | `buf`, `google.golang.org/protobuf`, `@bufbuild/protobuf`, `@connectrpc/connect-web`, `protoc-gen-es` |
| SQLite | `modernc.org/sqlite` (already used, pure Go, FTS5 enabled) |
| IDs | `github.com/oklog/ulid/v2` |
| File watching | `github.com/fsnotify/fsnotify` |
| systemd | `github.com/coreos/go-systemd/v22` (activation, sd_notify) |
| PTY | `github.com/creack/pty` |
| Git (root commits only) | `github.com/go-git/go-git/v5` (or `git` subprocess behind an interface) |
| JSON Schema | `github.com/invopop/jsonschema` |
| GUI | `github.com/wailsapp/wails` (v3 or v2), Svelte 5, Vite, Monaco + monaco-yaml, xterm.js, uPlot |
| TUI | Bubble Tea + Lip Gloss (already used), Bubbles (tables/viewports) |
| Existing, kept | cobra, go-sdk (MCP), hujson, yaml.v3, x/sys, x/sync, x/time |

### E. Definition of done (every PR)

- `make fmt vet race test` green; `buf lint` + `buf breaking` green; `make gen` clean.
- New concurrency comes with a stress test.
- Tool/config/API changes update: proto comments, `Instructions`, embedded
  skills (version bump), `docs/`, JSON Schema.
- New subsystem failures degrade (warn + disable) and are visible in
  `SystemService.Health` and `doctor`.
- No writes into project repositories without an explicit user action.
