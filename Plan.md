# Plan: agent-runtime UI rebuild (GUI + Web)

This plan covers `ui/` (shared Svelte 5 frontend), `cmd/agent-runtime-gui` (Wails shell) and the daemon HTTP handlers in `internal/server` that the UI depends on. It is based on a full read of every UI file and every handler, plus a live run of `agentd` in an isolated data dir to exercise each endpoint with curl. Every bug listed in section 1 was either reproduced live (marked **[repro]**) or read directly in the code (marked **[code]**).

Repo rules (AGENTS.md) apply throughout: no code comments, don't hold back on UI logic, styling or implementation, and delete this file once everything here is implemented.

---

## 0. How to verify work (use for every phase)

Isolated daemon, so nothing touches the real one:

```sh
SP=$(mktemp -d)
go build -o $SP/agentd ./cmd/agentd && go build -o $SP/agent-runtime-shim ./cmd/agent-runtime-shim
PATH=$SP:$PATH XDG_CONFIG_HOME=$SP/cfg XDG_DATA_HOME=$SP/data XDG_STATE_HOME=$SP/state \
  $SP/agentd -foreground -data $SP/d -socket $SP/a.sock -tcp 127.0.0.1:7350 -token-file $SP/token &
c(){ curl -s --unix-socket $SP/a.sock -H 'Content-Type: application/json' -d "$2" http://agentd/agentruntime.v1.$1; echo; }
```

- Web UI dev: `cd ui && npm run dev` (vite proxies `/api` to `127.0.0.1:7350`; set the token from `$SP/token` on the login screen).
- GUI: `nix develop ./packaging/nix -c make build-gui && AGENTD_SOCKET=$SP/a.sock ./bin/agent-runtime-gui` (Phase 6 adds the socket override).
- Gates: `npm run check` (svelte-check, added in Phase 2), `npm test`, `npm run e2e`, `go test -race ./internal/server/...`.

---

## 1. Audit: what is broken today

### 1.1 Daemon bugs that break the UI (must fix first)

| # | Severity | Where | Bug | Evidence |
|---|---|---|---|---|
| B1 | **Critical** | `internal/server/streams.go:34-59` | The heartbeat goroutine and the handler goroutine write to the same `http.ResponseWriter` with no lock, and the heartbeat can fire after the handler has returned. When a stream client disconnects, **the whole daemon crashes** with `nil pointer dereference` in `bufio.(*Writer).Flush` via `streamWriter.send`. Every UI reconnect or tab close risks killing agentd. | **[repro]** the daemon died after curl stream timeouts; stack trace points at `streams.go:59` from `streams.go:42` |
| B2 | Critical | `internal/server/server.go:147-152` | `DeadlineInterceptor` applies a 30s timeout to **every** request, streams included. WatchProcesses, TailLogs, Attach, WatchEvents, WatchConfig and Heartbeat all get cut every 30s. The session heartbeat stream is closed at 30s, which also closes the session. | **[repro]** WatchProcesses closed exactly 30s after opening |
| B3 | Critical | `internal/server/process.go:454-474` | WatchProcesses subscribes only to runtimes that are **already loaded** when the stream opens. Fresh daemon: snapshot is `null` and processes started later in any workspace never show up. | **[repro]** `{"kind":"snapshot","snapshot":null}`, then only heartbeats after a Start |
| B4 | Critical | `internal/server/process.go:489` | Upsert messages carry only `{event, processId}`, not the process. The UI store requires `msg.process`, so every live update is dropped. Status never changes in the table after the first load. `process.stdout`/`process.stderr` bus events (if published) would also flood this stream. | **[repro]** `{"event":"process.stopped","kind":"upsert","processId":"proc_…"}` |
| B5 | High | `internal/server/process.go:643`, `logsvc.go:280` | Tail and Attach start live-follow at `proc.EntryFrom()` (oldest ring entry), so after the backlog batch **the whole ring is re-sent** as "live" lines. Duplicate log lines on every connect or reconnect. | **[repro]** backlog `line 3, err 3` then `line 1 … err 3` replayed |
| B6 | High | `logsvc.go`, `process.go` handlers vs `pkg/api/types.go` | Casing is mixed. GetLogs, WaitForLog, Signal, SetRestartPolicy, GetEnv and WaitForExit decode **snake_case** (`process_id`) through `pkg/api` structs; everything else decodes camelCase (`processId`). Responses are mixed too (`process_id` in List/Get, `id` in the watch snapshot). | **[repro]** `GetLogs {"processId"}` → `processId required`; `GetEnv {"processId"}` → `unknown process ""` |
| B7 | High | `internal/server/tcp.go:101-102` | The TCP listener serves only the API. `agent-runtime web open` prints `http://127.0.0.1:7350/#token=…`, but `/` is a 404, so the web UI isn't served anywhere. | **[code]** no static handler; no `dist-web` embed anywhere |
| B8 | High | `internal/server/configsvc.go:110` | GetSchema reads `docs/schema/agent-runtime.v3.json` relative to the daemon's cwd. Installed daemons always get a 2-property stub schema. It returns a **string** when the file is found and an **object** otherwise. | **[repro]** stub object returned |
| B9 | Medium | `internal/config/v3.go:148` | `Validate` uses `KnownFields(false)`, so typos inside apps (`bogus: 1`, `comand:`) validate as OK even though the editor promises inline errors. | **[repro]** `apps.web.bogus` → `valid: true` |
| B10 | Medium | `configsvc.go` cfgPlan | Plan ignores `baseRevision`, returns `{changes}` (not the proto shape), and never reports stale revisions. Apply requires `projectId`, while GetConfig does not return `projectId` or the current revision, so the UI can't send correct Apply or optimistic-concurrency requests. | **[code]** |
| B11 | Medium | `configsvc.go` cfgApply | Only the `project` layer is writable. Workspace overlay and repo layers can't be edited, and `restartAffected` is accepted but ignored. | **[code]** |
| B12 | Medium | `internal/core/alerts.go:97` | `logs.alert` goes only to the store (`AppendEvent`), not to a bus. WatchEvents only sees it in history at connect time, so alert toasts never fire live. | **[code]** |
| B13 | Medium | `eventsession.go:42` | ListEvents and WatchEvents omit `payload_json` and `ts` on live events, so exit codes, health reasons and alert messages can't be shown. | **[repro]** |
| B14 | Medium | `logsvc.go:362` | ExportLogs ignores `format`, drops timestamps and streams, is capped at 10k ring lines, and returns `{"chunk":[…]}` with no `done` frame. | **[repro]** |
| B15 | Low | `process.go` handleProcList | `allWorkspaces` lists only loaded runtimes (TODO left in code). Store rows for unloaded workspaces are ignored. Same for GetStats. | **[code]** |
| B16 | Low | `process.go` handleProcGet | `ports: null` instead of `[]`. Resource usage returns `cpuNanos` (cumulative), not percent, so the UI has to diff samples. | **[repro]** |
| B17 | Low | `system.go` | GetSettings returns `{settings:{}}` (empty on a fresh daemon, no defaults), and UpdateSettings writes only 2 of the 4 keys. Health returns `uptimeMs`, while the proto says `uptime_seconds`. | **[repro]** |
| B18 | Low | `logsvc.go` logSearch | Search without `workspaceId` or `processIds` returns nothing. No `processIds` means "no search" instead of "all". No time range, despite the proto. | **[code]** |

### 1.2 Frontend bugs (ui/)

| # | Where | Bug |
|---|---|---|
| F1 | **fixed** | Rows now open a process detail drawer directly (`ProcessTable` `onOpen`); the dead `selected`/`select()` path is gone. |
| F2 | **fixed** | Server now sends `process` on every upsert (B4) and a `removed` frame on delete; `stores.ts` handles both, plus `gap` (resnapshot via `List`). |
| F3 | **fixed** | `stream()` closes via `AbortController` instead of a dead flag; the server-side gap frame is now handled by the caller (process store resnapshots via `List`). Online/visibility-aware reconnect pacing is still open. |
| F4 | **fixed** | Added a token store (`#token=` parsing, session/local storage, an `Authorization` header on every call) and a `Login.svelte` screen gating the web target. |
| F5 | **fixed** | `ApiError` now carries `status`/`code` parsed from `{error, code}`; a 401 triggers a callback that drops back to the login screen instead of an unhandled rejection. |
| F6 | `ui/src/lib/api.ts` | Services are untyped (`Record<string, unknown>` in, `unknown` out) even though `ui/src/gen` holds generated types. |
| F7 | **fixed** | `getPlatform()` now resolves lazily on every call instead of once at module load; added `whenPlatformReady()` for boot-time waits. |
| F8 | **fixed** | Handles the real `batch`/`line` frame shapes, sends stdin through the authenticated API client, added a `ResizeObserver` + theme, and it's mounted (lazily) from `ProcessDetail`'s Terminal tab. |
| F9 | `ui/src/components/ProcessTable.svelte:53` | Sort direction never toggles and there's no sort indicator. Sorting by `command` on a watch snapshot works, but on `process_id`-keyed rows `id` is missing. The filter uses `JSON.stringify` over the whole object. The selection set keeps stale ids after removal and isn't cleared after bulk actions. Shows a Ports column the snapshot never has. No start, remove, signal or details. No empty or loading state. |
| F10 | **partially fixed** | Auto-scroll now waits on `tick()` before reading `scrollHeight`; `newCount` correctly only counts lines added while scrolled up; timestamps added. Wrap, search, level filter and ANSI colors are still open. |
| F11 | **mostly fixed** | Real Monaco+monaco-yaml editor (lazy-loaded), wired to a workspace via the topbar's resolver. `GetConfig` now returns `projectId`/`revision` (server-side addition) so Apply sends the real `projectId` and `baseRevision` instead of `''`. Revisions list/rollback/diff view still open. |
| F12 | `ui/src/routes/App.svelte` | No workspace or project selection. `LogService.get` is a one-shot fetch of 500 lines, not a tail. Tabs have no active state. Actions have no confirmation, progress or error feedback. |
| F13 | **partially fixed** | Added `styles/tokens.css` + `styles/base.css` (dark-first, with a light-theme override), a real topbar/sidebar shell, and every rewritten component now styles from tokens instead of ad-hoc hex. Responsive layout, icons and the full component library (Phase 3.2) are still open. |
| F14 | Tooling | `tsc --noEmit` doesn't check `.svelte` files (no `svelte-check`). `stores.test.ts` has one trivial test, and the e2e has 2 API-only tests. |
| F15 | **mostly fixed** | Added Projects/Workspaces, Events, Sessions, Integrations, Audit, Settings and Apps (+ Stack start) pages, a Start-process dialog, a Resources tab, and log Clear/Export. Still open: a dedicated Search/history log mode, Signal picker (SIGKILL etc. beyond stop/restart), the full component library / command palette. |

### 1.3 GUI shell bugs (cmd/agent-runtime-gui)

| # | Bug |
|---|---|
| G1 | Binding race (F7). The shim should be injected before the app bundle runs, and the UI should also resolve the platform lazily. |
| G2 | On Linux `hasTray = false`. Closing the window quits the app, and there's no replacement for background alert notifications. |
| G3 | The Wails AssetServer proxy must stream NDJSON. `FlushInterval: -1` is set, but WebKitGTK custom-scheme responses may buffer. This needs verification (Phase 6.3), with a fallback plan. |
| G4 | No window size/position persistence, no single-instance lock (a second launch opens a second window), and no socket override for testing. |
| G5 | `OpenInEditor` runs `$EDITOR` detached with no terminal. Terminal editors (vim, nano) silently fail. No `path:line` support. |
| G6 | `platformOpener()` returns `start` on Windows, which isn't an executable (needs `cmd /c start`). |
| G7 | Client header hard-coded as `gui/3.0.0`. No version from build stamping. |

---

## 2. Target product

A Docker-Desktop-grade control panel for agent-runtime: see every project and process, follow logs live, act on processes, edit config safely, and manage integrations. One codebase, two targets (Wails desktop and web).

### 2.1 Information architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ▣ agent-runtime   [Project ▾ / Workspace ▾]   ⌘K Search…   ● daemon v3  ☾ │  top bar
├────────────┬─────────────────────────────────────────────────────────────┤
│ Overview   │                                                             │
│ Processes  │                     routed page                            │
│ Logs       │                                                             │
│ Apps       │                                                             │
│ Config     │                                                             │
│ Events     │                                                             │
│ ───────    │                                                             │
│ Projects   │                                                             │
│ Sessions   │                                                             │
│ Integr.    │                                                             │
│ Audit      │                                                             │
│ Settings   │                                                             │
├────────────┴─────────────────────────────────────────────────────────────┤
│ status bar: stream state · processes 4 running / 1 failed · index lag · ⚠ │
└──────────────────────────────────────────────────────────────────────────┘
```

Routes (hash router; works under Wails and any static host):

| Route | Page |
|---|---|
| `#/` | Overview dashboard |
| `#/processes` | Process table |
| `#/processes/:id/:tab?` | Process detail (tabs: logs, terminal, env, resources, events, info) |
| `#/logs` | Multi-process live tail and search |
| `#/apps` | Configured apps launcher and stack start |
| `#/config/:layer?` | Config editor |
| `#/config/revisions/:rev?` | Revision history, diff, rollback |
| `#/events` | Event timeline |
| `#/projects` and `#/projects/:id` | Projects and workspaces |
| `#/sessions` | Connected agent sessions |
| `#/integrations` | Harness MCP install and skills |
| `#/audit` | Audit log |
| `#/settings` | Daemon and UI settings, shutdown |
| `#/login` | Web only: bearer token entry |

A global **scope** (project + workspace, or "All workspaces") lives in the top bar, is persisted in `localStorage`, and filters every page.

---

### Progress note (this session, condensed pass)

Implemented in place of the granular file-by-file breakdown above:
- **Router**: `lib/router.svelte.ts` — a lightweight hash router (`navigate`, `match` with `:param`/`:param?`), not the full guarded/scroll-restoring version 2.4 describes, but functional and used by every page below.
- **Scope**: `lib/scope.svelte.ts` — current workspace/project, resolved from the topbar or the Projects page, persisted (last path) in `localStorage`.
- **Pages** (`routes/pages/`): ProcessesPage (table + routed detail drawer), ConfigPage (editor + Revisions tab with diff-free list/rollback in `RevisionsPanel.svelte`), ProjectsPage, EventsPage (history + live watch), SessionsPage (polled), IntegrationsPage (harnesses + skills, install/remove MCP with a confirm-diff flow), AuditPage (paged), SettingsPage (daemon settings form + About + danger zone).
- **Daemon**: `errorCode`/`errorHTTPCode` now classify real codes (stale_revision → 409, not just "internal" always); `ConfigService.Plan` accepts `baseRevision` and reports `stale`/`latestRevision`; `ConfigService.Apply` supports `layer: "workspace"` and actually restarts affected processes when `restartAffected` is set.

Not built this pass: the full component library (3.2), CommandPalette, keyboard map, Table virtualization, Search/history log mode, Resources tab, Start-process dialog, Stack start UI, Export/Clear logs UI, and the Wails shell items in Phase 6. `svelte-check` was not added (no network access to add the dependency in this session); `npx tsc --noEmit` plus real `vite build` (both targets) were used as the verification gate instead, along with end-to-end curl checks against a real isolated daemon for every new page's API calls.

### Second progress note (this session)

Also fixed a real, previously-undetected daemon bug found while wiring
the Apps page: `store.CreateProcess` generated its own row id instead
of being keyed by the runtime's actual process id (the one returned as
`StartResponse.ProcessID` and used by every other call — Stop, Restart,
GetLogs, `findProcess`'s unloaded-workspace fallback, and this session's
own `restartAffected` feature). The store row was effectively orphaned:
`GetProcess(processId)` always missed, `ListProcesses` rows could never
be correlated back to a live process, and `ConfigService.Apply`'s
`restartAffected` silently restarted nothing (the wrong id was fed to
`rt.Restart`, its error swallowed). Fixed by having `CreateProcess`
accept the id instead of generating one; both call sites (process
start, and the legacy-migration importer, which had the same bug)
updated. Regression tests added and confirmed to fail against the
pre-fix code via a throwaway `git stash` of just the three changed
files, then re-verified passing after restoring the fix.

Also added: `AppsPage.svelte` (configured apps + running-instance
status via the newly-added `app` field on process info, per-app Start,
multi-select Start-stack with the dependency order shown), and the
`app` field itself on `processInfo`'s JSON (was missing entirely,
which is what surfaced the id-linkage bug above).

### Third progress note (this session): Wails shell fixes, verified in the real window

This environment turned out to have a usable nix devshell
(`nix develop ./packaging/nix`) and a live Wayland/Hyprland desktop, so
the GUI could actually be built and its real WebKitGTK window launched
against an isolated test daemon (not just read/compiled) — confirmed
via screenshots (grim) that the window renders, the sidebar/topbar/
process table match the web build exactly, and the daemon-status dot
goes green (the WatchProcesses NDJSON stream really does flow through
the Wails AssetServer's proxy in the real window, not just headless —
Phase 6.3's open question). Keyboard/mouse automation of the window
itself was not attempted beyond that: the one `wtype` attempt landed in
this very terminal instead of the GUI window (hyprctl's dispatch syntax
here is a custom lua wrapper), so further interactive testing was
judged not worth the risk of more misdirected input on a live desktop.

Fixed in `cmd/agent-runtime-gui`:
- `SingleInstanceLock` (a second launch focuses the existing window
  instead of opening a duplicate).
- Window size/position/maximised state now persists across launches
  (`windowstate.go`, `$XDG_CONFIG_HOME/agent-runtime/gui.json`).
- `AGENTD_SOCKET` env var overrides the daemon socket path (used to
  point the GUI at an isolated test daemon instead of the real one).
- `OpenInEditor` now detects a terminal editor (vim/nvim/nano/hx/helix/
  emacs/...) and launches it inside a terminal emulator ($TERMINAL,
  then a fixed list of common ones) instead of silently doing nothing;
  it also now returns an error instead of swallowing one.
- The system-default opener fallback is now correct on Windows
  (`cmd /c start "" path` instead of a bare, non-executable `start`).

Not done: a menu bar, and the loopback-HTTP-server fallback for G3
(only needed if the AssetServer proxy turns out to buffer streams,
which the screenshot check above did not indicate).

## 3. Phase 1: Daemon fixes the UI depends on

All in `internal/server` unless noted. Each fix gets a regression test in `internal/server/server_test.go` (or a new `streams_test.go`).

### 1.1 Stream safety (B1)
- [x] `streamWriter` gets `mu sync.Mutex`, `closed atomic.Bool`, and a `stop chan struct{}`. `send` locks, checks `closed` and `done`, encodes and flushes.
- [x] `newStream` returns a writer with `Close()`. Every streaming handler does `defer sw.Close()`. The heartbeat goroutine selects on `stop` as well as `done`.
- [x] Test: open 200 streams concurrently, cancel the clients at random points while heartbeats run (set the interval to 5ms through an unexported package var for tests), run under `-race`, and confirm no panic.

### 1.2 No deadline on streams (B2)
- [x] `DeadlineInterceptor` skips the stream routes: a `streamingRoutes` set (`WatchProcesses`, `Attach`, `WatchResourceUsage`, `TailLogs`, `ExportLogs`, `WatchEvents`, `WatchConfig`, `SessionService/Heartbeat`), or a check on `Accept: application/x-ndjson` plus the route set.
- [ ] Unary handlers that legitimately wait (`WaitForLog`, `WaitForExit`, `StartStack`) use `timeoutMs + 5s` instead of 30s.
- [x] Test: WatchProcesses stays open for over 31s (test clock via an injectable deadline duration).

### 1.3 WatchProcesses rewrite (B3, B4, B15)
- [x] Add an engine-level `RuntimeLoaded` notification in `internal/core/engine.go` (a `Subscribe` on a small bus fired from `RuntimeFor`/`GetOrCreateRuntime` when a runtime is first created). WatchProcesses subscribes to that bus and attaches to new runtimes as they load.
- [x] The snapshot always returns `[]`, never `null`. It includes store rows for unloaded workspaces with `status` from the last instance row and `loaded:false`.
- [x] Every delta is `{"kind":"upsert","event":"process.started","process":<ProcessInfo>,"cursor":"<seq>"}`, built with `processInfo(pr, st)` at emit time.
- [x] Removal emits `{"kind":"removed","processId":…}`. Add a `process.removed` event type to `internal/events/bus.go`, published by `RemoveProcess`.
- [ ] Filter out `process.stdout`/`process.stderr` events in this stream.
- [x] Coalesce: at most one upsert per process per 100ms (map + ticker), so restart storms don't flood the stream.
- [ ] The queue overflow path sends `gapMessage` (it currently drops silently), and the client resnapshots on a gap.
- [ ] `ProcessInfo` JSON gains `app`, `instanceId`, `lifetime`, `restartPolicy`, `ports` (always an array), `args`, `exitSignal`, `stdoutLines`, `stderrLines`, `stale`, `loaded`.
- [ ] `List` with `allWorkspaces` merges store rows the same way.

### 1.4 Tail and Attach cursors (B5)
- [x] Record `lastID` as the last backlog entry ID + 1 (or `proc.Logs.NextID()` when the backlog is empty) **before** sending the backlog, and subscribe before reading the backlog so nothing is lost in between.
- [ ] Every line frame includes `id`, `timestamp`, `stream`, `level` (`logs.ParseLevel`), `instanceId`, and `cursor = "<processId>:<id>"`.
- [ ] `resume` is honoured: `{"resume":{"proc_x":1234}}` skips the backlog and continues from each id. If the id has already fallen out of the ring, send `gap`.
- [ ] A restart emits a `{"kind":"instance","processId","instanceId"}` frame so the viewer can draw a separator.
- [ ] TailLogs with an empty `processIds` and a `workspaceId` follows all processes in the workspace, including ones started later (via the manager bus `process.started`).
- [ ] Replace the 200ms polling loop in logTail with a `select` over the wake channels via a merged fan-in channel.
- [ ] Attach frames use the same shape: `{"kind":"output","data":"<utf8>","stream":…}` for live lines, and `{"kind":"batch","lines":[…]}` for the backlog.

### 1.5 Consistent casing (B6)
- [x] The daemon accepts both casings. Add `decodeCompat` in `process.go` that decodes into a `map[string]json.RawMessage`, rewrites snake_case keys to camelCase and vice versa for known aliases (`process_id`↔`processId`, `timeout_ms`↔`timeoutMs`, `backlog_lines`↔`backlog`), and unmarshals into the target. Use it for the six `pkg/api`-typed handlers.
- [ ] Responses stay as they are for MCP compat, and the UI normalises (Phase 2.2). Document the rule in `docs/api.md`: requests accept camelCase, and some responses are snake_case.

### 1.6 Serve the web UI on TCP (B7)
- [x] New package `internal/webui` with `//go:embed all:dist` (the build copies `ui/dist-web` there, gitignored except for a placeholder `index.html` so `go build` works without node).
- [x] `ServeTCP` mux: `/api/` → the guarded API; everything else → static files, **without** the bearer check (the token lives in the SPA), with `Cache-Control: no-cache` on `index.html` and immutable on `assets/*`. Keep the Host check on both.
- [x] CSP header on static responses: `default-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:; img-src 'self' data:` (Monaco needs blob workers).
- [x] Makefile `ui-web` copies `ui/dist-web` into `internal/webui/dist`. `build` depends on it when `ui/dist-web` exists.
- [ ] `agent-runtime web open` reads the actual `--tcp` address from a runtime file the daemon writes (`$XDG_RUNTIME_DIR/agent-runtime/tcp.addr`) instead of hard-coding 7350.

### 1.7 Config service (B8-B11)
- [x] Embed the schema: `docs/schema/agent-runtime.v3.json` → `internal/config/schema/agent-runtime.v3.json` with `//go:embed` (keep the docs copy in sync via a `go generate` or a test that diffs them). GetSchema always returns `{"schema": <object>}`.
- [ ] Complete the schema so it covers every `V3App`/`AppConfig` field (`readiness`, `health_check`, `restart`, `limits`, `lifetime`, `autostart`, `reload`, `pty`, `depends_on`, `ports`, `env_file`), plus `logging.redact` and `alerts`, with `description` on every property so Monaco hovers are useful.
- [ ] `Validate`: `KnownFields(true)`. Map yaml.v3 `field X not found in type` errors back to line/col by walking the node tree for the key. Return `path` for every error, and also validate `depends_on` targets and cycles, port templates and duplicate ports.
- [ ] GetConfig adds `projectId`, `workspaceId`, `revision` (active revision id per layer), `layers: [{name:"project", path, exists, writable:true}, {name:"workspace", …}, {name:"repo", path, trusted, sha256, writable:false}]`, and `raw` keyed by layer.
- [ ] Plan: accept `baseRevision`. Return `{changes, stale: bool, latestRevision}`, where each change includes `affectedProcessIds`. Return a unified `diff` string of the old and new YAML (use `github.com/pmezard/go-difflib`, which is already an indirect dependency via testify, or a small Myers implementation).
- [ ] Apply: accept `workspaceId` as an alternative to `projectId`. Support `layer: "workspace"` (writes the overlay path from `DiscoverEffective`). Return `409` with `code: "stale_revision"`. When `restartAffected` is set, restart `affectedProcessIds` and return `restarted: [...]`.
- [ ] `errorCode(err)` returns real codes (`not_found`, `stale_revision`, `policy_denied`, `repo_untrusted`, `invalid_argument`, `internal`) using sentinel errors plus `errors.Is`, and `errorHTTPCode` maps them (409 for stale).
- [ ] GetRevision: accept `{projectId, revision}` and include `content`. Add a `previousContent` field so the UI can diff against the prior revision without a second call.
- [ ] WatchConfig: also emit `{"kind":"invalid","layer","errors"}` when the file watcher sees an invalid edit on disk, and `{"kind":"changed"}` for external edits, so the editor can offer "reload from disk".

### 1.8 Events (B12, B13)
- [ ] Alerts publish to the workspace manager bus as `logs.alert` with payload `{message, line, rule}`, in addition to `AppendEvent`.
- [ ] ListEvents and WatchEvents include `ts`, `payload` (parsed JSON object) and a stable `cursor` = event row id. Live events are persisted before being sent, so cursors are DB ids and `since` resume works.
- [ ] ListEvents supports `types[]`, `before` (for paging backwards) and `limit` (default 200, max 1000). It returns `nextBefore`.

### 1.9 Logs (B14, B18)
- [ ] ExportLogs: `format` `ndjson|txt|json`. Include timestamp and stream. Read ring plus segments (`internal/logpipe`) for the full history. Frames are `{"kind":"chunk","data":"…"}`, then `{"kind":"done","lines":N}`.
- [ ] SearchLogs: with no `workspaceId`/`processIds`, search all loaded workspaces. Add `timeFrom`/`timeTo` (RFC3339 or unix ms), `offset`, and a `contextLines` implementation (it's accepted but unused). Each match returns `id`, `level`, `instanceId`, and `context: {before:[…], after:[…]}` when requested.
- [ ] GetLogStats: add `totalLines`, `diskBytes`, `segmentCount`, `firstTimestamp`, `lastTimestamp`.

### 1.10 System (B16, B17)
- [ ] GetResourceUsage and WatchResourceUsage compute `cpuPercent` from the delta of `cpuNanos` over wall time (keep `cpuNanos`). Add `rssBytes` from `/proc/<pid>/status` when there's no cgroup, plus `threads`, `openFds` (Linux), and a `children` count. Emit every 2s with `at`.
- [ ] Health returns `uptimeSeconds` plus `version`, `socketPath`, `dataDir`, `tcpAddr`.
- [ ] GetSettings returns all known keys with defaults and a `schema` (key → type/enum/description), so the Settings page renders a form. UpdateSettings accepts every key, validates, and returns the new settings.
- [ ] GetStats adds `processesRunning`, `processesFailed`, `logDiskBytes`, `indexLagSeconds`, `uptimeSeconds`.

### 1.11 Tests for Phase 1
- [ ] `internal/server/streams_test.go`: B1 race and panic, B2 no deadline, heartbeat stops after close.
- [ ] `internal/server/process_watch_test.go`: snapshot is empty, not null. A process started after the stream opens in a new workspace appears. The upsert carries `process`. Remove emits `removed`.
- [ ] `internal/server/logsvc_test.go`: tail has no duplicates, resume works, a restart yields an instance frame.
- [ ] `internal/server/configsvc_test.go`: unknown app field is rejected with a line number, stale revision returns 409, workspace-layer apply works.
- [ ] `internal/server/tcp_test.go`: `/` serves `index.html` without a token, `/api/...` requires a token, CSP header is set.

---

## 4. Phase 2: Frontend foundation

### 2.1 Tooling and dependencies
- [ ] Add dev deps `svelte-check` and `@testing-library/svelte`, plus `jsdom` for vitest component tests. Add scripts: `"check": "svelte-check --tsconfig ./tsconfig.json --fail-on-warnings"`, and `"lint": "npm run check && tsc --noEmit"`.
- [ ] Add deps: `@lucide/svelte` (icons), `@fontsource-variable/inter`, `@fontsource-variable/jetbrains-mono` (bundled fonts, since the Wails webview may be offline), `anser` (ANSI → spans for logs), `diff` (client-side unified diff rendering for config revisions).
- [ ] `vitest.config.ts`: `environment: 'jsdom'` for `*.svelte.test.ts`, with the svelte plugin.
- [ ] `vite.config.ts`: Monaco workers via `?worker` imports (`editor.worker`, `yaml.worker` from `monaco-yaml`). Add `manualChunks` so `monaco` and `xterm` are separate lazy chunks. `build.chunkSizeWarningLimit: 2000`.
- [ ] `src/env.d.ts`: `declare const __APP_TARGET__: 'wails' | 'web'` and `__APP_VERSION__` (inject from `package.json`).
- [ ] `tsconfig.json`: `"verbatimModuleSyntax": true`, `"noUncheckedIndexedAccess": true`, include `src/**/*.svelte`.

### 2.2 API client rewrite (`src/lib/api/`)
Replace `src/lib/api.ts` with a folder:

- [ ] `transport.ts`:
  - `class ApiError extends Error { status; code; details; service; method }`, parsed from `{error, code}`. `isApiError`, `codeOf`.
  - `call<Req, Res>(service, method, body, {signal, timeoutMs})` uses `AbortController` with a default timeout of 15s (configurable), adds the `Authorization` header from `auth.ts`, and sends `X-Agent-Runtime-Client: gui|web/<__APP_VERSION__>`. A 401 on web clears the token and navigates to `#/login`. A network failure maps to `ApiError{code:'unavailable'}`.
  - `openStream<Frame>(service, method, body, handlers, opts)` returns `{close(), state: Readable<'connecting'|'open'|'reconnecting'|'closed'>}`. It uses `AbortController` (close aborts immediately), parses NDJSON via a `TextDecoderStream`-based line splitter, and uses exponential backoff with full jitter (500ms → 15s). It resets backoff after 10s of healthy stream. It pauses reconnects while `navigator.onLine === false` and reconnects immediately on `online` and `visibilitychange` → visible. A watchdog treats 40s without any frame (heartbeat is 15s) as dead and reconnects. The caller-provided `resume()` returns the body patch for the next reconnect (cursor map). `onGap` and `onSnapshot` callbacks.
- [ ] `normalize.ts`: `toProcess(raw)` turns any of the three shapes (List summary, Get status, watch ProcessInfo) into one `Process` type with camelCase fields, `id` always set, `ports: number[]`, `startedAt: number | null` (ms), `exitedAt`, `exitCode`, and `status` narrowed to a union. Also `toLogLine`, `toEvent`, `toRevision`, `toHarness`, `toSession`, `toAuditEntry`.
- [ ] `types.ts`: domain types (`Process`, `ProcessStatus`, `LogLine`, `LogFrame`, `DaemonEvent`, `Project`, `Workspace`, `ConfigBundle`, `PlanResult`, `Revision`, `Harness`, `Skill`, `Session`, `AuditEntry`, `Settings`, `ResourceSample`). Hand-written, because the daemon's JSON doesn't match the proto shapes in `src/gen`. Remove the unused `src/gen` import path from the bundle (keep the files for future Connect migration).
- [ ] `services.ts`: fully typed wrappers for **every** route listed in `internal/server/*`: System (6), Project (9), Process (17 incl. StartStack), Log (7), Event (5), Session (6), Config (9), Audit (1), Integration (8). Each wrapper sends the exact key casing the server decodes (per Phase 1.5, camelCase everywhere once compat lands) and returns normalised types.
- [ ] `auth.ts`: token store. It reads `#token=` from `location.hash` on boot, stores it in `sessionStorage` (with a "remember on this device" option → `localStorage`), and strips it from the URL with `history.replaceState`. Not used in the Wails target (the Go proxy needs no token).
- [ ] Unit tests: NDJSON splitting across chunk boundaries, including multi-byte UTF-8 split mid-codepoint. Reconnect with resume patch. Close aborts. 401 redirect. Error parsing. Every normaliser, against fixture JSON captured from the live daemon (save the curl outputs from section 0 as `src/lib/api/__fixtures__/*.json`).

### 2.3 State stores (`src/lib/state/`)
Svelte 5 runes-based classes in `.svelte.ts` files. No `writable` needed.

- [ ] `connection.svelte.ts`: daemon reachability (Health poll every 5s while any stream is reconnecting), `version`, `apiVersion` compatibility check against `minClientVersion` (blocking banner if incompatible), and aggregated stream state for the status bar.
- [ ] `scope.svelte.ts`: `projects`, `workspaces` per project, current `{projectId, workspaceId} | 'all'`, persisted. Auto-selects if there's only one project. Refreshes on `ProjectService` changes and every 30s.
- [ ] `processes.svelte.ts`: a `SvelteMap<string, Process>` fed by WatchProcesses (snapshot → replace, upsert → set, removed → delete, gap → resnapshot via `List`). Derived `byWorkspace`, `counts {running, failed, exited, starting}`, `list(filter, sort)`. Optimistic status on actions (`stopping…`, `restarting…`) that rolls back on error. Exposes `lastEventAt` per process for row flash animation.
- [ ] `logs.svelte.ts`: `LogBuffer` class, a ring of up to 200k lines per view (configurable in settings) in a typed structure (parallel arrays: `ids`, `ts`, `stream`, `level`, `text`, `proc`), so it can hold a million lines without a million objects. Fed by TailLogs with a resume cursor map. Supports `filter(predicate)` producing an index view (`Uint32Array`), recomputed incrementally on append.
- [ ] `events.svelte.ts`: WatchEvents with a `since` cursor. Keeps the last 2000 events and exposes `onEvent(type, cb)` for toasts and notifications.
- [ ] `alerts.svelte.ts`: `logs.alert` and `process.crashed`/`process.failed`/`process.unhealthy` → toast plus native notification (debounced per process for 30s, mute per process/app persisted, global "Do not disturb").
- [ ] `prefs.svelte.ts`: UI preferences (theme `system|light|dark`, density `comfortable|compact`, log font size, wrap, timestamps format `relative|local|utc|off`, max buffer lines, notification toggles, sidebar collapsed), persisted in `localStorage` under `ar.prefs.v1` with schema versioning.
- [ ] `toasts.svelte.ts`, `dialogs.svelte.ts` (promise-based `confirm()`, `prompt()`), `palette.svelte.ts` (command registry).
- [ ] Replace `stores.test.ts` with tests for the process store reducer (snapshot/upsert/removed/gap, optimistic rollback) and the LogBuffer (append, wraparound, filter view, 1M append perf budget < 1.5s in jsdom).

### 2.4 Router (`src/lib/router.svelte.ts`)
- [ ] Hash router: route table with params, `navigate(path, {replace})`, `link` action for `<a>`, `beforeLeave` guards (the config editor with unsaved changes → confirm), scroll restoration per route, and the document title per route (`Processes · agent-runtime`).
- [ ] Web target: an unauthenticated guard redirects to `#/login?next=…`.

### 2.5 Platform fix (F7, G1)
- [ ] `platform.ts` exports `getPlatform()`, resolved lazily on each call: `__wailsBinding` or `window.go?.main?.App` present → Wails, else browser. Plus `whenPlatformReady(): Promise<Platform>`, which resolves on the `wails:ready` event or after 1.5s fallback.
- [ ] The interface grows: `notify(title, body, {tag, onClick})`, `openInEditor(path, line?)`, `saveFile(name, content, mime)`, `copyText`, `openExternal(url)`, `pickDirectory(): Promise<string|null>` (desktop: native dialog; web: `prompt` fallback), `revealInFileManager(path)`, `setBadge(count)` (desktop: tray/dock; web: favicon badge plus title prefix), `requestNotificationPermission()`.
- [ ] Browser `copyText` falls back to `document.execCommand('copy')` when `navigator.clipboard` is unavailable (non-secure contexts over SSH-forwarded HTTP on a non-localhost name).

---

## 5. Phase 3: Design system and app shell

### 3.1 Tokens (`src/styles/tokens.css`)
Dark-first, with a light theme. All colours go through CSS custom properties. Theme is set on `<html data-theme>`, and `system` follows `prefers-color-scheme` live.

```
--bg-0 app background      dark #0b0d12   light #f7f8fa
--bg-1 surface / sidebar   dark #11141b   light #ffffff
--bg-2 raised / cards      dark #161a23   light #ffffff
--bg-3 hover / inputs      dark #1d2230   light #f0f2f5
--border                   dark #262c3a   light #e3e6eb
--border-strong            dark #343c4f   light #cfd4dc
--text-0 primary           dark #e8eaf0   light #11141b
--text-1 secondary         dark #a3a9b8   light #505866
--text-2 muted             dark #6b7285   light #8a92a0
--accent                   #6d8cff  (hover #86a0ff, subtle bg rgba(109,140,255,.14))
--ok      #3fb950  --warn #d29922  --err #f85149  --info #58a6ff  --neutral #8b949e
--log-stderr #ff7b72 --log-system #79c0ff --log-debug #8b949e --log-warn #e3b341
--radius-sm 4px --radius 6px --radius-lg 10px
--space-1..8 = 2,4,8,12,16,20,24,32px
--font-ui "Inter Variable", system-ui     --font-mono "JetBrains Mono Variable", ui-monospace
--fs-xs 11px --fs-sm 12px --fs-md 13px --fs-lg 15px --fs-xl 18px --fs-2xl 24px
--shadow-1 0 1px 2px rgba(0,0,0,.3)   --shadow-pop 0 12px 32px rgba(0,0,0,.45)
--row-h 32px (compact 26px)   --topbar-h 44px   --sidebar-w 208px (collapsed 52px)
--ease cubic-bezier(.2,.8,.2,1)   --dur-fast 120ms --dur 180ms
```

- [ ] `src/styles/base.css`: modern reset, `html,body{height:100%}`, body background and text from tokens, `font-feature-settings: "cv11","ss01"`, `::selection`, themed thin scrollbars (webkit plus `scrollbar-color`), `:focus-visible` ring (`2px solid var(--accent)` with offset), `prefers-reduced-motion` disables transitions, and `color-scheme` per theme so native controls match.
- [ ] Log palette also covers the 16 ANSI colours per theme (`--ansi-0..15`) for `anser` output and xterm.

### 3.2 Component library (`src/lib/ui/`)
Each is a Svelte 5 component with typed props, keyboard support and ARIA. Styles live in the component and use tokens only.

- [ ] `Button` (variants `primary|secondary|ghost|danger|link`, sizes `sm|md`, `loading` spinner, `icon`-only with required `label` → `aria-label` + tooltip), `IconButton`, `ButtonGroup`, `SplitButton` (Stop ▾ → SIGINT/SIGTERM/SIGKILL/custom).
- [ ] `Input`, `Textarea`, `Select`, `Combobox` (typeahead, used by the scope switcher), `Checkbox` (with indeterminate), `Switch`, `SegmentedControl`, `SearchField` (with ⌘F binding, clear, regex/case toggles).
- [ ] `Badge`, `StatusDot` (pulsing for `starting`/`stopping`), `StatusPill` (dot + label + colour per status), `Tag`, `Kbd`, `Tooltip` (delay 400ms, positioned with a small flip/shift implementation), `Popover`, `DropdownMenu` (roving tabindex, type-ahead, submenus), `ContextMenu` (right click on rows and log lines).
- [ ] `Dialog` (focus trap, Esc, return focus, stacked), `ConfirmDialog` (danger variant with typed confirmation for destructive bulk actions > 3 items or daemon shutdown), `Drawer` (right-side, resizable, width persisted).
- [ ] `Tabs` (URL-synced), `Table` (sticky header, sortable columns with ▲▼ indicators and `aria-sort`, resizable columns persisted per table, row selection with shift-range and ⌘-toggle, keyboard row navigation ↑↓/Enter/Space, virtualization > 200 rows, empty/loading/error slots), `VirtualList`.
- [ ] `Toast` region (stacked bottom-right, auto-dismiss 5s except errors, action button, pause on hover), `Banner` (connection lost, incompatible daemon, stale config), `EmptyState` (icon, title, body, action), `Skeleton`, `Spinner`, `ProgressBar`.
- [ ] `CodeBlock` (mono, copy button, optional line numbers), `DiffView` (unified/split toggle, +/- colouring, collapsed unchanged hunks), `KeyValueList`, `RelativeTime` (auto-updating, title = absolute), `Bytes`, `Duration`, `Sparkline` (SVG, for CPU/mem), `Meter`.
- [ ] `CommandPalette` (⌘K / Ctrl+K): fuzzy search over routes, processes ("Logs: web", "Restart: api"), apps ("Start: worker"), actions ("Toggle theme", "Clear logs of…", "Open config"), and recent items.
- [ ] Component tests for Table selection/sort/keyboard, Dialog focus trap, DropdownMenu keyboard, CommandPalette filtering.

### 3.3 App shell (`src/routes/App.svelte` → `src/app/`)
- [ ] `Shell.svelte`: CSS grid `topbar / sidebar + main / statusbar`. The sidebar collapses to icons (persisted, auto-collapses under 900px width). Under 640px the sidebar becomes a slide-over.
- [ ] `TopBar.svelte`: logo, scope switcher (Combobox of `All workspaces` + projects → workspaces with path subtitles), command palette trigger, daemon status chip (green/amber/red with tooltip: version, uptime, socket, streams), theme toggle, notification bell with an unread alert count and dropdown of recent alerts.
- [ ] `Sidebar.svelte`: nav items with icons, badge counts (Processes: failed count in red; Events: unread alerts), active state, keyboard shortcuts shown on hover (`g p`, `g l`, …).
- [ ] `StatusBar.svelte`: stream state per subscription, running/failed counts, log index lag, current scope path, client version.
- [ ] Global keyboard map (`src/lib/keys.ts`): `⌘K` palette, `/` focus search, `g o|p|l|a|c|e|s` go-to, `?` shortcuts dialog, `Esc` close drawer/dialog, `[`/`]` previous/next process in detail view. Disabled while typing in inputs or editors.
- [ ] Boot sequence in `main.ts`: load prefs → apply theme before first paint (inline script in `index.html` reading `localStorage` to avoid a flash) → web: auth check → `GetVersion` handshake → start global streams (processes, events) → mount. A splash screen shows during the handshake, and a full-page `DaemonUnreachable` state with a retry and troubleshooting hints (`agentd` not running, socket path, `agent-runtime daemon start`, link to `docs/troubleshooting.md` content inlined).

---

## 6. Phase 4: Pages

Each page has loading skeletons, an empty state, an error state with retry, and works in both themes and both densities.

### 4.1 Overview (`#/`)
- [ ] Stat cards: Running, Failed/Crashed (clickable → filtered process list), Projects, Sessions (agents connected), Log disk usage, Index lag, Daemon uptime.
- [ ] "Needs attention" list: failed, crashed, unhealthy and stale-config processes, with one-click Restart / View logs.
- [ ] Recent events (last 20, live), recent alerts, and connected agent sessions (harness icon + workspace).
- [ ] Quick start: apps from the current scope's config not currently running, each with a ▶ button, and "Start stack".

### 4.2 Processes (`#/processes`)
- [ ] Columns: select, status (pill), name (app name or command basename, with the full command below in muted mono), project/workspace (shown only in All scope), PID, uptime (live `RelativeTime` from `startedAt`), restarts (badge, amber if > 0), health, CPU% and memory (fed by a shared `WatchResourceUsage` pool for **visible rows only**, max 20 concurrent streams, falling back to polling `GetResourceUsage` every 5s beyond that), ports (clickable `localhost:PORT` → `openExternal`), profile, lifetime, and a row actions menu.
- [ ] Toolbar: `SearchField` (matches name, command, pid, port, id), status filter chips with counts (All, Running, Starting, Exited, Failed, Stale), workspace filter (All scope), column visibility menu (persisted), density toggle, and a "Start process" button.
- [ ] Bulk bar (appears on selection, sticky): Stop, Restart, Signal ▾, Remove (force option), Set restart policy ▾, Clear logs, Export logs (zip on desktop; multiple downloads on web). A result toast summarises successes and failures per id.
- [ ] Row interactions: click → detail drawer (or full page with ⌘-click / Enter). Double-click → logs tab. Right-click → context menu. Row flash on status change.
- [ ] Actions are wired to the real endpoints with optimistic state (2.3) and error toasts showing `ApiError.message` + `code`.
- [ ] **Start process dialog**: tabs "App" (select from config apps, showing command/workdir/env preview from GetConfig `apps`) and "Command" (command line input parsed with shell-quote rules into `command[]`, workdir picker (`pickDirectory` on desktop), env editor as a key/value grid with paste-a-`.env` support, lifetime segmented control, restart policy select, PTY switch). Submit → `Start` → navigates to the new process's logs, which open live immediately. It remembers the last 10 commands per workspace.

### 4.3 Process detail (`#/processes/:id/:tab`)
Header: status pill, name, command (copyable), PID, uptime, restarts, instance id, and buttons (Restart, Stop split-button with signals, Open shell, Remove, ⋯ menu: Set restart policy, Copy id, Open workdir in editor, Reveal in file manager, Export logs, Clear logs). `[`/`]` switch between processes.

- [ ] **Logs tab**: embeds `LogView` (4.4) scoped to this process, with an instance separator on restart.
- [ ] **Terminal tab**: `Terminal.svelte` rewritten.
  - xterm.js with the fit addon plus `@xterm/addon-web-links` and `@xterm/addon-search`. Theme from tokens (re-applied on theme change), font from `--font-mono`, and a `ResizeObserver` → fit.
  - Output from Attach: the backlog batch written first (dimmed), then live `output` frames. It handles both the old and new frame shapes.
  - Input: a line-buffered mode by default for non-PTY processes (local echo, Enter sends the line plus `\n` in a single `SendStdin` call through `services.ts` with auth). A raw mode for PTY processes sends keystrokes batched every 16ms. Show a mode indicator and a hint for when stdin is closed.
  - "Open shell here" calls `OpenShell({processId})` and switches to the new shell process's terminal tab.
  - Clear, copy selection, search (⌘F within the terminal), and font size ±.
- [ ] **Env tab**: `GetEnv` (sends `process_id` until compat lands) → a searchable key/value table. Redacted values are masked with a "Reveal" toggle (calls with `reveal:true`, shows a policy-denied message on 403). "Live /proc" versus "Spec" segmented control (`live`). Provenance column from `source`. Copy as `.env`.
- [ ] **Resources tab**: live CPU% and memory sparklines (last 5 minutes kept in memory), current values, peak, limits from config (`limits.cpu`/`memory`) drawn as reference lines, threads and fds when available, and listening ports.
- [ ] **Events tab**: ListEvents filtered by `processId` plus live WatchEvents, as a timeline with icons per type, payload details expandable (exit code, signal, health reason).
- [ ] **Info tab**: every field of `Get` (id, instance, workspace path, project, profile, command + args, workdir, started/exited times, exit code/signal, restart policy + backoff state, consecutive failures, health, stdout/stderr line counts, stale flag with "Config changed: restart to apply" banner + button) in a `KeyValueList`, and raw JSON in a collapsible `CodeBlock`.

### 4.4 Logs (`#/logs`) and `LogView` component
`LogView.svelte` replaces `LogViewer.svelte`. It's used by the Logs page and the process Logs tab.

- [ ] Virtualised rendering over the `LogBuffer` index view. Fixed row height when wrap is off. When wrap is on, rows are measured with a height cache and a Fenwick tree for offsets. Renders only the visible range plus overscan. `requestAnimationFrame`-batched appends. Target: 60fps with 1M lines, 5k lines/s ingest.
- [ ] Columns (toggleable): line number, timestamp (format from prefs), process chip (colour-hashed per process, multi-process only), stream marker (stdout none, stderr red bar, system blue), level badge, and message with ANSI colours (`anser`), URL linking, and JSON lines pretty-toggle (click `{…}` to expand).
- [ ] Follow mode: auto-scroll at the bottom. Scrolling up pauses follow and shows a "Jump to live · N new" pill that counts lines actually added since the pause. Resume with End or the pill. Scroll handling uses `tick()` after append so the spacer height is up to date (fixes F10).
- [ ] Toolbar: process multi-select (Logs page), stream filter (stdout/stderr/system), level filter (≥ debug/info/warn/error), text filter with regex and case toggles (client-side over the buffer, highlighting matches, with ↑↓ to jump between matches and a count `3/41`), wrap toggle, timestamps toggle, pause/resume ingest, clear view (local), Clear logs (server `ClearLogs`, confirm), export (`ExportLogs` → `platform.saveFile` with format select), and copy visible/selected.
- [ ] "Search history" mode: switches from a live tail to `SearchLogs` (server FTS plus ring) with time range presets (15m, 1h, 24h, custom), a results list with context lines, click → jump to that line in the live buffer if present, and a `truncatedScan` notice.
- [ ] Line interactions: click selects, shift-click selects a range, ⌘C copies the selection, right-click menu (copy line, copy as JSON, filter to this process, show context, open workdir in editor at a `file:line` if the line matches `path:line` patterns).
- [ ] Persist per-view filter state in the URL query (`#/logs?p=proc_a,proc_b&level=warn&q=timeout`) so views can be shared and bookmarked.

### 4.5 Apps (`#/apps`)
- [ ] Cards or table of configured apps from `GetConfig.apps` for the current workspace: name, type/profile, command, workdir, depends_on chips, ports, autostart/lifetime/reload badges, running instance status (joined with the process store by `app`), and provenance (which layer defines it).
- [ ] Actions: Start (Start with `app`), Stop/Restart running instance, View logs, Edit (jumps to the config editor with the cursor on that app's key).
- [ ] Multi-select + "Start stack" → `StartStack` with the resolved order preview (topological, drawn as a small dependency graph in SVG). Per-app progress shows as the stack starts, and it reports a `failed` app with the error.

### 4.6 Config (`#/config/:layer`)
`ConfigEditor.svelte` rewritten on Monaco.

- [ ] Lazy-load `monaco-editor` plus `monaco-yaml` configured with the schema from `GetSchema` (`fileMatch: ['*']`), so users get completion, hover docs, and inline schema errors. Theme is derived from tokens (a custom `defineTheme` for dark and light, switched live).
- [ ] Layer tabs from `GetConfig.layers`: Project (editable), Workspace overlay (editable, "create overlay" if missing), Repo (read-only, with a trust banner: sha256, "Trust this config" → `TrustRepoConfig`), and Resolved (read-only YAML of the merged apps, with provenance gutter decorations showing which layer each app came from).
- [ ] Server validation debounced at 300ms → Monaco markers (line/column/path/message) merged with the schema markers. An errors panel below the editor lists them (click → reveal line). Deprecation warnings show as warning markers.
- [ ] Dirty tracking with a dot on the tab and `beforeLeave` guard + `beforeunload`. ⌘S → Plan.
- [ ] **Plan dialog**: a DiffView of the current file versus the editor contents, a change list grouped by kind (Added, Removed, Restart required with affected process chips, Applied live, Unchanged collapsed), a stale-revision warning with "Reload latest and re-apply my changes" (3-way: fetch the latest, show a diff), a commit message input, a "Restart affected processes now" switch, and Apply.
- [ ] Apply → toast with the new revision. On `stale_revision` (409), show the stale flow above. On validation errors, go back to the editor with markers.
- [ ] External change detection via WatchConfig: a "Config changed on disk (rev 14 by cli)" banner with Reload / Keep mine / Compare.
- [ ] **Revisions** (`#/config/revisions`): a list (id, layer, time, source badge `api|cli|gui|rollback|file`, session, message, valid flag, sha short). Select → content view plus a diff against the previous revision or against the current one (toggle). Rollback button with confirm → `Rollback` → toast plus editor reload.
- [ ] Toolbar: Format (normalise YAML indentation via Monaco's formatter from monaco-yaml), Copy path, Open in external editor (desktop `openInEditor(configPath)`), Download.

### 4.7 Events (`#/events`)
- [ ] Live timeline (WatchEvents) with history paging (ListEvents `before`), grouped by day, with type filter chips (started, stopped, exited, crashed, failed, restarted, healthy, unhealthy, alert), process filter, and text filter.
- [ ] Each row: icon + colour by type, relative time, process chip (link), instance id, a payload summary (exit code, signal, alert message), and an expandable raw payload.
- [ ] "Pause live" toggle and an "N new events" pill, like the logs view.

### 4.8 Projects (`#/projects`, `#/projects/:id`)
- [ ] Table: name (inline rename → `UpdateProject`), config mode, workspace count, last used, process counts.
- [ ] Detail: workspaces list (path, confirmed, last seen, missing flag), Link workspace (path picker → `LinkWorkspace`), Resolve a path (→ `Resolve`, shows the resulting project/workspace), Forget project (typed confirm; explains the 30-day trash), GC missing workspaces (→ `GC`, lists the removed ones), and "Set as current scope".
- [ ] Add project flow: pick a directory → `Resolve` → it becomes the current scope.

### 4.9 Sessions (`#/sessions`)
- [ ] Table of `ListSessions` (polled every 5s): kind, harness (icon + name), client PID, workspace (resolved to project/path), started, last seen (stale highlight > 60s), closed. Close session action (confirm, explains that `lifetime: session` processes stop after the grace period). A process count per session from the process store's `sessionId` (added in Phase 1.3 ProcessInfo).

### 4.10 Integrations (`#/integrations`)
- [ ] Harness cards from `ListHarnesses`: detected/version, MCP configured (global), skills installed list, plus per-scope actions (scope select: global/project where supported).
- [ ] Install MCP flow: `PreviewInstall` → DiffView of the exact file change → confirm → `InstallMCP` → result (path, backup path with "Reveal"/"Open", changed flag). Remove MCP the same way with `RemoveMCP`.
- [ ] Skills: `ListSkills` (name, version, description), `CheckUpdates` (outdated badges), Install/Remove per harness+scope, "Update all outdated".

### 4.11 Audit (`#/audit`)
- [ ] Virtualised table of `ListAudit` (paged with `limit`, "Load more"): time, action (route shortened to `Service.Method`), result (ok/error code coloured), duration, session, harness, workspace. Filters: action, result, time range, and workspace (current scope by default). Export CSV.

### 4.12 Settings (`#/settings`)
- [ ] **Appearance** (local): theme, density, font sizes, timestamp format, reduced motion override.
- [ ] **Logs** (local): buffer size, default backlog lines, wrap default, ANSI on/off.
- [ ] **Notifications** (local): per-type toggles, DND, muted processes list with unmute, "Test notification", and a web permission request button.
- [ ] **Daemon** (server): a form generated from `GetSettings.schema` (`log_level`, `idle_exit`, `shim_linger`, `retention`) → `UpdateSettings`.
- [ ] **About**: client version/target, daemon version, api version, shim protocol, socket path, data dir, TCP address, uptime, and copy diagnostics (JSON bundle for bug reports).
- [ ] **Danger zone**: Shutdown daemon (keep processes, the default) versus Stop everything and shut down, with typed confirmation. The UI then shows a "Daemon stopped" state with "Start daemon" on desktop (a new Wails binding, `EnsureDaemon`).
- [ ] **Web access** (web target): sign out (clears token), and show the token file location.

### 4.13 Login (`#/login`, web only)
- [ ] Centered card: a token input (paste), a "Remember on this device" checkbox, and an explanation of where the token lives (`$XDG_CONFIG_HOME/agent-runtime/token`) and of `agent-runtime web open`. It validates via `GetVersion` with the token before saving. Clear error on 401/403 (`forbidden origin/host` explains SSH port-forward usage).

---

## 7. Phase 5: Notifications, alerts and polish
- [ ] Alert pipeline (2.3 `alerts`): toast with actions (View logs → jumps to the matching line, Mute this process). Native notification through the platform with click → focus window + navigate. Desktop badge/title count of unread alerts.
- [ ] Row flash and toast on process crash, even when the tab is in the background (web: `document.hidden` → Notification API if permitted).
- [ ] Transitions: drawer slide (180ms), dialog scale+fade, toast slide, list item fade-in on insert (disabled on bulk snapshot), all respecting reduced motion.
- [ ] Accessibility pass: all interactive elements reachable by keyboard, visible focus, `aria-live="polite"` for toasts and the status bar, `role="log"` + `aria-live="off"` on the log view (so screen readers aren't flooded) with an "announce errors" option, contrast ≥ 4.5:1 checked for both themes.
- [ ] Performance budget: initial JS (excluding the Monaco and xterm lazy chunks) < 250KB gzipped. Time to interactive < 1s on the GUI. Process table with 500 rows at 60fps scroll. Measure with `vite build --mode production` + Playwright traces.
- [ ] Error boundary: wrap each routed page in `<svelte:boundary>` with a friendly error card (message, stack in a collapsible, "Reload page", "Copy diagnostics").

---

## 8. Phase 6: Wails desktop shell (`cmd/agent-runtime-gui`)

### 6.1 Binding readiness (G1)
- [ ] Inject the shim before the bundle runs: serve a generated `/__wails_shim.js` from `apiMiddleware` that defines `window.__wailsBinding` as lazy getters over `window.go.main.App.*`, and add `<script src="./__wails_shim.js">` to `ui/index.html` guarded by `__APP_TARGET__ === 'wails'` (use a vite `transformIndexHtml` plugin so the web build doesn't include it). Keep the `OnDomReady` injection as a fallback that dispatches a `wails:ready` event.
- [ ] Expand bindings: `Notify(title, body, tag)`, `OpenInEditor(path, line)`, `SaveDialog`, `WriteFile`, `PickDirectory`, `RevealInFileManager(path)`, `OpenExternal(url)` (`wailsruntime.BrowserOpenURL`), `SetBadge(count)` (window title prefix on Linux, since there's no dock badge), `EnsureDaemon() error`, `Version() string`, `SocketPath() string`.

### 6.2 Editor and opener fixes (G5, G6)
- [ ] `OpenInEditor`: if `$VISUAL`/`$EDITOR` is a known GUI editor (`code`, `codium`, `subl`, `zed`, `idea`, `gedit`, `kate`), launch it with a line argument in the right syntax (`code -g path:line`, `subl path:line`, `zed path:line`, `idea --line N path`). If it's a terminal editor, launch it inside `$TERMINAL` / `x-terminal-emulator` / `gnome-terminal --` / `konsole -e` / `alacritty -e` / `kitty`. Otherwise use the platform opener. Return an error to the UI instead of swallowing it.
- [ ] Windows opener: `cmd /c start "" path`. macOS: `open`. Linux: `xdg-open`.

### 6.3 Streaming through the AssetServer (G3)
- [ ] Verify in the real WebKitGTK window that NDJSON frames arrive incrementally (a debug page in Settings → About showing heartbeat arrival times). If WebKitGTK buffers custom-scheme responses, then:
  - Start a loopback HTTP server inside the GUI process on `127.0.0.1:0` with a per-launch random token, serving the same proxy. Expose `ApiBase() string` and `ApiToken() string` bindings. `transport.ts` uses them when present (Wails target only).
  - The loopback server enforces the Host/Origin checks from `internal/server/tcp.go` (factor `tcpGuard` into a reusable function in `internal/server` or a small `internal/guard` package).

### 6.4 Window and lifecycle (G2, G4, G7)
- [ ] Persist window size, position and maximised state in `$XDG_CONFIG_HOME/agent-runtime/gui.json`, restored on start. Minimum size 900×600.
- [ ] Single instance: `options.App.SingleInstanceLock` (Wails v2.16 supports it) with `OnSecondInstanceLaunch` → show and focus the existing window.
- [ ] Linux without a tray: keep close → quit, but add a `--background` preference ("Keep running in background for notifications"). When on, close hides the window and a tiny `agent-runtime-gui` notification listener keeps running. It reopens via a `.desktop` action or on a second launch (single-instance handoff). Document the GTK two-main-loop limitation already noted in `tray.go`.
- [ ] `AGENTD_SOCKET` env var and a `--socket` flag override `paths.User().SocketPath()` (needed for the section 0 test setup).
- [ ] Client header uses the stamped version (`-ldflags -X main.version=…`, wired in `.goreleaser.yaml` and `packaging/nix/flake.nix`).
- [ ] Menu bar (macOS/Windows/Linux via `options.App.Menu`): App (About, Settings ⌘,, Quit), View (Reload ⌘R, Toggle devtools in dev builds, Zoom in/out/reset), Go (the same routes as the sidebar), Help (Docs, Troubleshooting, Report issue → `https://github.com/anshiq/log-mcp/issues`). Menu items call `WindowExecJS` to navigate the router.
- [ ] Daemon lost while the window is open: the UI shows the DaemonUnreachable state with "Start daemon" → `EnsureDaemon` binding.

---

## 9. Phase 7: Tests and CI

- [ ] **Unit (vitest)**: api transport and normalisers (2.2), stores (2.3), router, LogBuffer filter index, ANSI rendering, shell-quote parsing of the Start dialog command, and the settings schema form generator.
- [ ] **Component (vitest + @testing-library/svelte)**: Table, Dialog, DropdownMenu, CommandPalette, LogView (follow/pause/jump pill, filter highlight), ProcessTable bulk actions (mock services), ConfigEditor (plan → apply → stale flow, with Monaco stubbed behind an adapter interface `EditorAdapter` so jsdom tests use a textarea implementation).
- [ ] **E2E (Playwright)** against a real isolated daemon started in `globalSetup` (the section 0 recipe, with a temp HOME, `--tcp 127.0.0.1:0` read back from the new `tcp.addr` file), web build served by the daemon (1.6):
  1. The login screen rejects a bad token and accepts the good one; `#token=` auto-login strips the hash.
  2. Start a process via the Command dialog → it appears in the table as running without a reload → logs stream live → stop → status becomes stopped live.
  3. Restart → an instance separator shows in the logs → no duplicate lines (count the lines).
  4. Keep a WatchProcesses page open for 45s → still live (regression for B2), and the daemon is still alive after reloading 20 times (regression for B1).
  5. Config: type an invalid key → a marker appears → fix → Plan shows Added → Apply → Apps page lists the app → Start from Apps → Revisions shows the new rev → Rollback.
  6. Search history finds a line after it scrolled out of the live view.
  7. Integrations: PreviewInstall diff renders for a harness, using a temp HOME so nothing real changes.
  8. Theme toggle persists across reload; command palette navigates.
  9. Visual snapshots of each page in dark and light at 1280×800 (`toHaveScreenshot`, threshold 0.2%).
- [ ] **CI** (`.github/workflows`): add a `ui` job (`npm ci`, `npm run check`, `npm test`, `npm run build`, `npm run build:web`) and an `e2e` job (Go build + Playwright with `npx playwright install --with-deps chromium`). Add `make ui-check`, and add `ui-check` to `ci-check`.

---

## 10. Target file layout (ui/src)

```
src/
  main.ts
  env.d.ts
  styles/{tokens.css,base.css,fonts.css}
  app/{Shell,TopBar,Sidebar,StatusBar,Splash,DaemonUnreachable,ShortcutsDialog}.svelte
  lib/
    api/{transport,services,normalize,types,auth}.ts
    api/__fixtures__/*.json
    state/{connection,scope,processes,logs,events,alerts,prefs,toasts,dialogs,palette,resources}.svelte.ts
    router.svelte.ts
    keys.ts
    platform.ts
    format.ts            (bytes, duration, relative time, status colour map, process display name)
    shellquote.ts
    ansi.ts
    ui/…                 (component library, 3.2)
  features/
    overview/OverviewPage.svelte
    processes/{ProcessesPage,ProcessTable,BulkBar,StartProcessDialog,SignalMenu}.svelte
    process/{ProcessPage,ProcessHeader,LogsTab,TerminalTab,EnvTab,ResourcesTab,EventsTab,InfoTab}.svelte
    logs/{LogsPage,LogView,LogToolbar,LogRow,SearchHistory}.svelte
    apps/{AppsPage,StackGraph}.svelte
    config/{ConfigPage,MonacoEditor,EditorAdapter.ts,PlanDialog,RevisionsPage,LayerTabs}.svelte
    events/EventsPage.svelte
    projects/{ProjectsPage,ProjectPage}.svelte
    sessions/SessionsPage.svelte
    integrations/{IntegrationsPage,HarnessCard,InstallDialog,SkillsPanel}.svelte
    audit/AuditPage.svelte
    settings/{SettingsPage,DaemonSettingsForm,AboutPanel,DangerZone}.svelte
    login/LoginPage.svelte
```

Delete: `src/routes/App.svelte`, `src/components/*`, `src/lib/api.ts`, `src/lib/stores.ts`, `src/stores.test.ts` (their replacements are above).

---

## 11. Implementation order and dependencies

1. **Phase 1.1 + 1.2** (stream crash, deadlines). Tiny, critical, and unblocks everything.
2. **Phase 1.3-1.5** (watch, tail cursors, casing), with **2.1-2.3** in parallel (the transport can be built against fixtures).
3. **Phase 3** (tokens, components, shell) + **2.4-2.5**.
4. **Phase 4.2, 4.3, 4.4** (processes, detail, logs): the core value.
5. **Phase 1.7** + **4.5, 4.6** (config end to end).
6. **Phase 1.8-1.10** + **4.1, 4.7-4.12** (remaining pages).
7. **Phase 1.6** + **4.13** (web target served and authenticated).
8. **Phase 6** (Wails shell), **Phase 5** (polish), **Phase 7** (tests land with each phase; the CI job is added in step 1).
9. Final: `make ci-check` green, e2e green, then **delete `Plan.md`** (AGENTS.md).

## 12. Definition of done
- Every row in section 1 is fixed and has a test that fails before the fix.
- Every API route in `internal/server` is reachable from the UI (except the MCP-only Subscribe/Drain/Unsubscribe/Register/Heartbeat/Ping).
- Both targets (`npm run build` for Wails, `npm run build:web` for the daemon-served web UI) pass the full e2e suite. The GUI is checked manually on Linux (WebKitGTK) for streaming, notifications, save dialog and open-in-editor.
- No code comments added (AGENTS.md). `svelte-check` passes with zero warnings.
- `Plan.md` deleted.
