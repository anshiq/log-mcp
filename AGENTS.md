If task is for Plan.md is implemented, after complete implementation delete that file. strictly no comments in code, don't hold back in ui logic, styling, and implementation logic

## Product focus

The webapp (`ui/` web build served by agentd TCP + `agent-runtime web`) is the only interface. Build web-first; keep every change working for web.

The TUI (`agent-runtime tui` dashboard and setup wizard) and the desktop GUI (`agent-runtime gui`, Wails shell) have been removed.

## Target CLI UX

- `agent-runtime` -> prints the command help.
- `agent-runtime web` -> prints the http link to the web interface and opens the browser.
