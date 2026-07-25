# Roadmap

gaon is built bottom-up: identity first, authorization second,
workflow last. Each tier depends only on the tier below it.

## P0 — Domain memory implementations

- `internal/org/memory.go` — in-memory `Store` with cascade delete.
- `internal/user/memory.go` — in-memory `Store`.
- `internal/agent/memory.go` — in-memory `Store`.
- Domain-level `_test.go` files that exercise CRUD via `Store`.
- Output: every domain has at least one usable implementation and a
  test suite that proves it.

## P1 — authz adapters

- `User.AsSubject() authz.Subject` and `Agent.AsSubject()` adapter
  functions, plus `AsObject()` where needed.
- Domain types stay free of authz method clutter.
- Output: `chk.Allow(ctx, user.AsSubject(u), doc.AsObject(d), "doc:read")`
  compiles and runs against the existing in-memory stores.

## P2 — Persistence

- `postgres.go` for `org`, `user`, `agent` (`Store`).
- `postgres.go` for `authz.RoleStore` and `PolicyStore`.
- Migrations under `migrations/`.
- Output: a dockerised Postgres stack with seeded fixtures and a
  green integration suite.

## P3 — Transport

- HTTP layer (probably chi or stdlib `http` with a small router) over
  the domain `Service` interfaces.
- gRPC layer if a separate contract is needed.
- OpenAPI / proto definitions live next to the handlers.

## P4 — Workflow core

- `internal/workflow` — workflow definition, state machine, execution.
- Workflows are themselves GRN-addressed resources.
- authz remains the gate at every transition.

## P5 — Operational

- Structured logging (`log/slog`) with `trace_id` propagation.
- Prometheus metrics and OpenTelemetry tracing.
- GitHub Actions CI running `make ci`.
- Lefthook hooks locally.
- Container image and a minimal deploy path.

## Sequencing rules

- A later P-level cannot start until the previous P-level is green.
- Cross-cutting changes (logging, metrics) enter in P5, not earlier.
- If a P-level uncovers a design hole, fix it before moving on — no
  exceptions for "we can clean this up later."