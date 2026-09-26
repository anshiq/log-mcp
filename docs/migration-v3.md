# Migrating to agent-runtime v3

v3 moves state out of your repos into `~/.local/share/agent-runtime`
(`state.db`, project configs, per-workspace log indexes). Migration is
automatic on first contact, explicit with `migrate`, and never deletes
anything without asking.

## Automatic (first contact)

1. The daemon resolves your directory to a project + workspace (stable
   across moves, re-clones and worktrees).
2. `<root>/agent-runtime.yaml` is offered for import (default) or
   repo-linked mode — after you trust it (GUI dialog, `project trust`,
   or an MCP hint). Untrusted repo configs do nothing.
3. `<root>/.agent-runtime/` is absorbed: a running v2 daemon hands over
   (adopted processes show "legacy" until restarted for full log
   capture), `logs.db` entries land in the workspace FTS index,
   `audit.log` lands in the audit table. The directory is left in place.

## Explicit

```sh
agent-runtime migrate [--dry-run] [--all-known] [--cleanup] [--yes] [<dir>...]
```

- `--all-known` scans harness configs (`~/.claude.json`,
  `~/.codex/config.toml`, opencode) for projects to import.
- `--cleanup` offers to remove legacy `.agent-runtime/` dirs afterwards.
- Existing MCP entries (`agent-runtime serve`) keep working unchanged —
  the same command now runs as a bridge. `integrate` can collapse
  project-scope entries into one global entry.

## Rollback / compatibility

- v3.0 keeps `serve --embedded` (old in-process mode); v3.2 removes it.
- `runtime.daemon` is ignored with a deprecation notice (the daemon is
  always used).
- `project export` is manual: the repo file is never rewritten by the
  daemon. `project forget` soft-deletes (30-day trash); `project gc`
  lists vanished workspaces.
