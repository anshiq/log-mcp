# ADR-001: Per-User Daemon

## Status
Accepted

## Context
The current architecture has one daemon per project (`<project>/.agent-runtime/run.sock`). This creates problems:
- Multiple sockets to discover
- No global views ("everything running on my machine")
- Shared resources are duplicated across daemons
- Port conflicts across projects

## Decision
Move to **one daemon per OS user**, hosting every project. The daemon listens on `$XDG_RUNTIME_DIR/agent-runtime/agentd.sock`.

Projects are isolated *inside* the daemon via `ProjectRuntime` instances, each with its own manager, policy, audit, config watcher, and log index handle.

## Consequences
- One socket to discover, one place for the GUI to connect
- Global views possible
- Shared resources (one log janitor, one metrics endpoint)
- Panic in one project's goroutine is recovered and scoped
- Projects are still fully isolated within the daemon

## Alternatives Considered
- Keep per-project daemons with a discovery service: Adds complexity, doesn't solve the fundamental problem
- Single global runtime (no project isolation): Too fragile

## References
- Plan.md §3.1
- Plan.md §2.1 (component diagram)
