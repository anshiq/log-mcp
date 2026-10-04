If task is for Plan.md is implemented, after complete implementation delete that file. strictly no comments in code, don't hold back in ui logic, styling, and implementation logic

## Product focus

Primary focus is the webapp (`ui/` web build served by agentd TCP + `agent-runtime web`). Build web-first; keep every change working for web.

GUI application (`internal/gui` desktop shell inside the single `agent-runtime` binary; `cmd/agent-runtime-gui` is a deprecated shim) and TUI (`agent-runtime tui` dashboard in `internal/cli/tui_daemon.go`, setup wizard in `internal/cli/tui.go`) are NOT the current preference: maintenance-only, no new features, no redesigns. Work on them may resume in the future.

## Target CLI UX (see Plan.md, source of truth for the merge)

`agent-runtime-gui` is being merged INTO `agent-runtime` (single binary):
- `agent-runtime` -> opens the native GUI application window.
- `agent-runtime web` -> prints the http link to the web interface and opens the browser.
- `agent-runtime tui` -> opens the terminal dashboard.
