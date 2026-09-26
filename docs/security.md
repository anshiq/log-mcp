# agent-runtime v3 security model

Local, single-user threat model. The daemon enforces; agents are untrusted
callers of the API.

| Threat | Mitigation |
|---|---|
| Another local user talks to the daemon | Socket dir `0700`, socket `0600`, **`SO_PEERCRED` uid check** on UDS (fail closed) |
| Malicious web page → TCP listener (CSRF, DNS rebinding) | TCP off by default; bearer token; strict `Host` (`127.0.0.1`/`localhost`) + `Origin` checks; no permissive CORS; token never in cookies |
| Cloned repo's `agent-runtime.yaml` runs commands on first contact | **Repo-config trust gate** (direnv-style allow): inactive until trusted; trust is (workspace, path, sha256) — edits re-arm it; agents cannot self-trust (`trust.allow_agent`, default false) |
| Secrets in logs/env in the GUI | Redaction on by default; reveal is policy-gated and audited |
| Agent escapes the project | `restricted` mode per project; workdir confinement against the workspace path |
| Tampered shim bundle | Bundle dir `0700`; shim validates spec owner uid |
| DB/log files readable by others | Data dir `0700`, files `0600`; `doctor` checks and fixes |

## Audit

Every mutating RPC plus policy denials lands in the `audit` table with
session + harness attribution (90-day retention). `AuditService.ListAudit`
and MCP `get_audit_log` read it (args redacted).

## File modes

`doctor --fix` repairs data-dir/socket modes. Never run the daemon as
root: cgroup delegation comes from the user systemd instance
(`Delegate=yes`), not privilege.
