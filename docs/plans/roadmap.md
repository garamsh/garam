# Roadmap

gaon is built bottom-up: identity first, authorization second,
workflow last. Each tier depends only on the tier below it.

## MVP — Identity skeleton + API

- GRN value type, `errorx`, domain types and `Store` interfaces for
  `org` / `user` / `agent` (done on `develop`).
- Postgres-backed `Store` implementations via sqlc. No in-memory
  domain stores — persistence goes straight to Postgres. (The `authz`
  package keeps its in-memory stores as test doubles only.)
- OpenAPI spec as the source of truth for the HTTP API; server code
  generated from the spec (oapi-codegen, stdlib `net/http`). A thin
  `cmd/` binary wires handlers to the stores.
- Migrations under `migrations/` (golang-migrate). Dockerised Postgres
  for dev; integration tests against real Postgres.
- Output: HTTP CRUD for org/user/agent backed by Postgres, `make ci`
  green.

## P1 — authz landing + adapters

- Land the authz implementation (written, uncommitted on `develop`)
  with the file split required by `docs/convention/go.md`.
- `User.AsSubject() authz.Subject` and `Agent.AsSubject()` adapter
  functions, plus `AsObject()` where needed.
- Domain types stay free of authz method clutter.
- Output: `chk.Allow(ctx, user.AsSubject(u), doc.AsObject(d), "doc:read")`
  compiles and runs.

## P2 — authz persistence & policy versioning

- `postgres.go` for `authz.RoleStore` and `PolicyStore`.
- `policies` + append-only `policy_versions` tables; evaluation over a
  compiled policy set, invalidated on change.
- Output: permission state survives restarts; every policy change is
  versioned and auditable.

## P3 — Workflow core

- `internal/workflow` — workflow definition, state machine, execution.
- Workflows are themselves GRN-addressed resources.
- authz remains the gate at every transition.

## P4 — Operational

- Structured logging (`log/slog`) with `trace_id` propagation.
- Prometheus metrics and OpenTelemetry tracing.
- Container image and a minimal deploy path.

## Sequencing rules

- A later level cannot start until the previous level is green.
- Cross-cutting changes (logging, metrics) enter in P4, not earlier.
- If a level uncovers a design hole, fix it before moving on — no
  exceptions for "we can clean this up later."
