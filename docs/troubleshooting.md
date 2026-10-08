# Troubleshooting

`agent-runtime doctor` explains itself; this page maps output to actions.

| doctor check | Meaning | Action |
|---|---|---|
| daemon-socket FAIL | No socket: daemon not running | `agent-runtime daemon start` (or `agentd --foreground`) |
| daemon-socket mode | Permissive socket | `doctor --fix`; check `XDG_RUNTIME_DIR` ownership |
| runtime/data/config-dir | Wrong modes or missing | `doctor --fix`; never run as root |
| daemon-reachable FAIL | Socket exists but refuses | `agentd.log` in `$XDG_STATE_HOME/agent-runtime/`; `daemon restart` |
| disk | Log volume pressure | Raise `agentd.yaml` retention caps, `project gc`, web UI disk meter |
| integrations | Harness state | `integrate status`, `integrate <agent> --write` |
| legacy-migration | v2 dirs remain | `migrate --dry-run --all-known`, then `migrate --cleanup` |

## Common issues

- **Two daemons for one user** (systemd + manual): single user-level
  flock — the second exits with a clear message; `doctor` flags it.
- **Orphaned shims** (`shims/*/` without a daemon row): restart the
  daemon; it reconnects via `shim.sock`, or marks `orphaned` (signal +
  pid-poll fallback, UI-badged).
- **Stale workspaces**: `project gc` after moves/deletes.
- **Blank page**: daemon down shows the "daemon not running" screen;
  start it and reload.
- **Invalid YAML save**: last good revision stays active; the web UI/CLI
  shows file/line/column from `config.invalid`.
