# agent-runtime v3 Architecture

## Overview

agent-runtime v3 transforms the system from a per-project, session-scoped MCP supervisor into a **per-user background platform**: an always-on daemon, a central project registry, a versioned API, and a desktop GUI.

## Components

```
┌─────────────────────────────────────────────────────┐
│                   agentd (1 per OS user)            │
│                                                     │
│  ┌─────────────────────────────────────────────┐    │
│  │              Engine (multi-tenant)           │    │
│  │  ┌──────────┐  ┌──────────┐  ┌───────────┐ │    │
│  │  │ Project  │  │ Project  │  │ Project   │ │    │
│  │  │ Runtime  │  │ Runtime  │  │ Runtime   │ │    │
│  │  │ (ws A)   │  │ (ws B)   │  │ (ws C)    │ │    │
│  │  └──────────┘  └──────────┘  └───────────┘ │    │
│  └─────────────────────────────────────────────┘    │
│  ┌──────────────┐  ┌──────────────┐                │
│  │ Session      │  │ Project      │                │
│  │ Registry     │  │ Registry     │                │
│  └──────────────┘  └──────────────┘                │
│  ┌──────────────┐  ┌──────────────┐                │
│  │ LogPipeline  │  │ EventBus     │                │
│  │ (segments +  │  │ (global)     │                │
│  │  FTS5 index) │  │              │                │
│  └──────────────┘  └──────────────┘                │
│  ┌──────────────┐                                   │
│  │ state.db     │  SQLite (WAL), single writer     │
│  └──────────────┘                                   │
└─────────────────────────────────────────────────────┘
            │
     ┌──────┼──────┐
     │      │      │
     ▼      ▼      ▼
   CLI    MCP    GUI     (all use pkg/client)
  agent-  bridge agent-
  runtime serve  runtime
```

## Key Design Decisions

1. **Per-user daemon** — one daemon hosts all projects
2. **Shim layer** — processes outlive the daemon with full observability
3. **Locator chain** — workspace resolution via path, dev/inode, git
4. **Connect-RPC** — versioned API over Unix sockets
5. **Central storage** — state.db under `~/.local/share/agent-runtime`
6. **Config layering** — built-in → daemon defaults → repo → project → workspace
7. **Trust gate** — repo configs are inactive until explicitly trusted

## Directory Layout

```
~/.local/share/agent-runtime/
├── state.db              SQLite (WAL)
├── projects/<proj_*/>
│   ├── agent-runtime.yaml
│   ├── workspaces/<ws_*/>
│   └── env/
├── logs/<ws_*/>
│   ├── index.db          SQLite FTS5
│   └── segments/
├── trash/
└── backups/

$XDG_RUNTIME_DIR/agent-runtime/
├── agentd.sock
├── agentd.pid
├── agentd.lock
├── shims/<instance_id>/
└── agentd.log (in $XDG_STATE_HOME)
```

## API Services

| Service | RPCs | Purpose |
|---------|------|---------|
| SystemService | GetVersion, Health, GetStats, Shutdown | Daemon info |
| ProjectService | Resolve, ListProjects, LinkWorkspace, ForgetProject, GC | Project identity |
| ConfigService | GetConfig, Plan, Apply, ListRevisions, Rollback, WatchConfig | Config management |
| ProcessService | Start, Stop, Restart, Signal, List, WatchProcesses, Attach | Process lifecycle |
| LogService | GetLogs, SearchLogs, TailLogs, ExportLogs | Log access |
| EventService | WatchEvents, ListEvents | Event streaming |
| SessionService | Register, Heartbeat, ListSessions | Session management |
| IntegrationService | ListHarnesses, InstallMCP, InstallSkills | Integrations |
| AuditService | ListAudit | Audit trail |

## Migration Path

v2 → v3 migration is automatic:
1. Resolve → new project + workspace
2. Import repo config (trust-gated)
3. Import legacy `logs.db` and `audit.log`
4. Adopt v2 daemon processes via legacy adopt path
5. v3 daemon reconnects all shims on boot

## Performance Budgets

| Metric | Budget |
|--------|--------|
| agent-runtime serve cold start | < 150 ms |
| Workspace resolve (warm) | < 5 ms |
| StartProcess RPC | < 30 ms p99 |
| Ingest throughput | ≥ 100k lines/s |
| Aggregate ingest | ≥ 300k lines/s |

## Security Model

- UDS socket: `0700` directory, `0600` socket, `SO_PEERCRED` uid check
- TCP listener: off by default, bearer token, strict Host check
- Repo-config trust gate (direnv-style allow)
- Secrets in env redacted; reveal is policy-gated and audited
- DB/log files `0600`; data dir `0700`
