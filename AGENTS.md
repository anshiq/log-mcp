If task is for Plan.md is implemented, after complete implementation delete that file. strictly no comments in code, don't hold back in ui logic, styling, and implementation logic

## Product focus

Primary focus is the webapp (`ui/` web build served by agentd TCP + `agent-runtime web`). Build web-first; keep every change working for web.

GUI application (`internal/gui` desktop shell inside the single `agent-runtime` binary; `cmd/agent-runtime-gui` is a deprecated shim) is NOT the current preference: maintenance-only, no new features, no redesigns. Work on it may resume in the future. The TUI (`agent-runtime tui` dashboard and setup wizard) has been removed; the webapp is the only interface.

## Target CLI UX

`agent-runtime-gui` is being merged INTO `agent-runtime` (single binary):
- `agent-runtime` -> opens the native GUI application window.
- `agent-runtime web` -> prints the http link to the web interface and opens the browser.
