# ADR-002: Shim Process Supervisor

## Status
Accepted

## Context
Process lifetime is currently tied to the daemon's pipes (`lifecycle.go` uses in-process `io.Pipe`s). A daemon restart cuts every child's stdout/stderr. The children keep running but become unobservable.

## Decision
Introduce `agent-runtime-shim`, a per-process supervisor inspired by `containerd-shim` and `conmon`. The shim holds the child's stdio pipes and exit status, so managed processes and their logs survive a daemon crash, restart, or upgrade.

The shim:
1. Receives a bundle (JSON spec)
2. `setsid()` + `PR_SET_CHILD_SUBREAPER`
3. Creates pipes/PTY, execs child into cgroup
4. Writes framed records to segment files
5. Serves a control socket with length-prefixed protobuf protocol
6. On child exit: writes `exit.json`, stays alive until `Release`

## Consequences
- Processes and logs survive daemon restarts with zero log loss
- Exit codes are recorded even while daemon is down
- Shim protocol is versioned (daemon supports [N-1, N])
- Shim is a separate binary (upgrade of daemon never changes running shims)
- Shim RSS stays small (≤ 4 MB), no SQLite/Connect deps

## Alternatives Considered
- Keep pipes in daemon: Rejected (daemon restart = log loss)
- Redirect stdout straight to files: Rejected (loses stream guarantees)
- `systemd-run --user --scope`: Rejected (systemd-only)
- Pass fds via `SCM_RIGHTS`: Only covers planned restarts, not crashes

## References
- Plan.md §3.5
- Plan.md §2.2 (binaries table)
