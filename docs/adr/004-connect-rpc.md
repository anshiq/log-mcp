# ADR-004: Connect-RPC for the Versioned API

## Status
Accepted

## Context
The current JSON-over-UDS RPC has no streaming, no codegen, and no browser story. The future needs server streaming for live tail, event watch, and process-table watch.

## Decision
Use **Protobuf schemas + Connect-RPC** (`connectrpc.com/connect`), generated with **Buf**.

Key benefits:
- Go daemon and clients via `connect-go`
- Browser clients via `@connectrpc/connect-web` (streaming over HTTP/1.1 and HTTP/2)
- Unix socket transport (h2c cleartext)
- gRPC-compatible (same handlers speak gRPC)
- `buf breaking` gates CI for wire compatibility
- JSON debugging via Connect's JSON codec
- One schema produces Go SDK, TypeScript client, and docs

## Consequences
- `pkg/api` (hand-written types) replaced by generated types
- All services defined in `api/proto/agentruntime/v1/`
- Generated Go: `gen/agentruntime/v1` + `…/v1connect`
- Generated TS: `ui/src/gen/`
- Additive changes only in v1; breaking changes create v2

## Alternatives Considered
- Extend current JSON-over-UDS RPC: No streaming, no codegen
- Plain gRPC: Needs proxy for browsers
- REST + OpenAPI: Streaming awkward, two sources of truth
- GraphQL: Overkill, subscriptions add complexity

## References
- Plan.md §5
- Plan.md §5.2 (package & versioning rules)
