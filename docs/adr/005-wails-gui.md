# ADR-005: Wails for the Desktop GUI

## Status
Accepted

## Context
A desktop GUI is needed that shares the same API as the CLI and web UI. The frontend should be written once and shipped twice: inside the Wails desktop shell and as the web UI later.

## Decision
Use **Wails (Go) + TypeScript/Svelte 5** frontend.

Key benefits:
- Same language (Go) as the rest of the codebase
- Reuses `pkg/client` directly
- Native webview (WebKitGTK on Linux, WebKit on macOS, WebView2 on Windows)
- Binary ~10–15 MB, not Electron's 150 MB
- Frontend is reusable as the web UI
- Mature libraries for hard widgets (virtualized log lists, Monaco YAML editor, xterm.js)

## Consequences
- `cmd/agent-runtime-gui` contains only the Go side (window, tray, proxy, native bridge)
- `ui/` contains the shared frontend
- The proxy is `httputil.ReverseProxy` with `http2.Transport` dialing unix socket (h2c)
- Frontend knows only Connect; native features through a `Platform` interface
- Closing the window leaves the tray running; quitting never stops the daemon

## Alternatives Considered
- Tauri 2 (Rust): Second backend language, reimplement client
- Fyne/Gio (Go): Pure Go but hand-build all widgets
- Qt 6 (C++): C++ toolchain, licensing questions
- egui/iced/Slint (Rust): Weak text editor and terminal widgets

## References
- Plan.md §7
- Plan.md §7.2 (GUI architecture)
