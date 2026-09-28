# agent-runtime

Local async process supervisor for AI coding agents — a per-user background
daemon that starts, watches and signals long-running dev processes (servers,
builds, log tails) on your behalf, so an agent can kick off work, walk away,
and come back to check on it instead of blocking a session on a foreground
command.

## Components

- **agentd** — the always-on daemon (one per OS user). Owns the project
  registry, process supervision, the log pipeline (with FTS5 search) and a
  global event bus, all backed by a single-writer SQLite `state.db`.
- **agent-runtime** — the CLI / MCP bridge agents talk to.
- **agent-runtime-shim** — a tiny (<= 4 MB RSS), dependency-free process shim
  that stays put under a running process across an agentd upgrade.
- **agent-runtime-gui** — a desktop GUI (Wails) over the same daemon API for
  watching projects, sessions, events, resources and audit history by hand.

See [docs/architecture.md](docs/architecture.md) for the full component
diagram, [docs/api.md](docs/api.md) for the daemon API, and
[docs/troubleshooting.md](docs/troubleshooting.md) /
[docs/security.md](docs/security.md) for operational notes.

## Install

Prebuilt binaries and `.deb`/`.rpm` packages are published on the
[releases page](../../releases) for Linux, macOS and Windows (amd64/arm64).

A Nix flake is also provided under `packaging/nix/`.

## Building from source

```sh
go build ./cmd/agent-runtime
go build ./cmd/agentd
go build ./cmd/agent-runtime-shim
```

Requires Go (see `go.mod` for the minimum version).

## License

MIT — see [LICENSE](LICENSE).
