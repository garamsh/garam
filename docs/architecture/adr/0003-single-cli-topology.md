# ADR 0003: Single cobra CLI running independent api/authz servers

> Status: accepted; the process count is superseded by [ADR-0017](./0017-machine-process.md)
> Date: 2026-07-25

## Context

The policy engine is a separate service from the API backend, but the
project wants one deliverable binary and fully independent packages.
Service-to-service calls need a contract; `errorx` kinds are already
aligned with gRPC status codes.

## Decision

- One cobra CLI at `cmd/garam`: `garam serve api` (OpenAPI HTTP
  backend), `garam serve authz` (gRPC policy engine), `garam migrate`
  (DB migrations). Same image, two processes, scaled independently.
- Package independence: `internal/api/` and `internal/authz/` never
  import each other. Shared code is limited to `internal/grn` and
  `internal/errorx`. The api server reaches the engine only through a
  gRPC client.
- gRPC between services; `.proto` sources under `proto/`, generated
  with buf. gRPC status codes carry `errorx` kinds without
  translation.
- Actions are plain strings owned by the domains that define them
  (`const ActionOrgRead = "org:read"` in `internal/org`). The engine
  matches strings only and never imports a domain. If policy-write
  validation needs a known-action registry, each domain exposes its
  actions and the CLI composition root aggregates and registers them —
  the engine core stays uninvolved.

## Consequences

- `authz.Evaluator` gains a gRPC-client implementation used by the
  api server; the in-process evaluator stays for tests and dev.
- Two deployables from one binary keeps local dev simple (run both
  subcommands) while allowing independent scaling later.
- Package-independence is enforceable in review: any import edge
  between `internal/api` and `internal/authz` is a violation.
- Adding a domain means adding its action constants and, optionally,
  registering them at the composition root — no engine change.

## Staged adoption

- **Stage 1 (MVP).** `cmd/garam` exposes `serve api` and `serve authz`
  from day one, but the api server may use the in-process native
  evaluator, importing `internal/authz` for the `Evaluator` seam and
  its implementation. Business-logic independence still applies: no
  handler logic in `authz`, no policy logic in `api`.
- **Stage 2 (P2).** The gRPC server and client land; the api server's
  import of `internal/authz` is replaced by the gRPC client and the
  full no-import rule takes effect. Tracked in the P2 authz issue.
