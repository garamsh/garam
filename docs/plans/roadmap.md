# Roadmap

garam is built bottom-up: identity first, authorization second,
workflow last. Each tier depends only on the tier below it.

## MVP — Authentication + org permissions (done)

- GRN value type, `errorx`, domain types and `Store` interfaces for
  `org` / `user` / `agent`.
- `internal/authn/` — OAuth provider abstraction (OIDC) with Google as
  the first provider; more providers plug in later without touching
  call sites.
- `org` gains an `Invitation` aggregate: orgs grow by invitation and
  acceptance, not open signup. Minimal org roles (`owner`, `admin`,
  `member`) on membership, expressed as IAM-style policy statements
  and enforced by the authz service (ADR-0002).
- Single cobra CLI (`cmd/garam`): `serve api` (OpenAPI HTTP) and
  `serve authz` (gRPC policy engine) run as independent packages
  (ADR-0003).
- The `agent` domain stayed excluded (still being planned).
- Postgres-backed `Store` implementations via sqlc. No in-memory
  domain stores.
- OpenAPI spec as the source of truth for the HTTP API; server code
  generated from the spec (oapi-codegen, stdlib `net/http`).
- Migrations under `migrations/` (golang-migrate). Dockerised Postgres
  for dev; e2e store round-trips against real Postgres.

## P1 — authz engine (done)

- IAM-style `Statement` model and native `StatementEvaluator`
  (ADR-0002); org roles evaluated as statements.
- gRPC transport (ADR-0003 stage 2): `garam serve authz` runs the
  engine as a separate process; the api server authorizes through the
  gRPC client only.
- Full wildcard semantics (`*`, `?`) in statement patterns.

## P2 — authz persistence & policy versioning (done)

- Postgres `policies` + append-only `policy_versions` (ADR-0002);
  policies became org-managed data the engine owns.
- Evaluation over a per-org parsed statement set, cached in the
  engine and invalidated on policy change (`LISTEN`/`NOTIFY`).
- Policy lifecycle RPCs on the authz service (create, add version,
  list, activate, roll back); every version records its author.
- Statement collection stayed hybrid rather than moving fully
  engine-side: role-derived statements remain caller-supplied and the
  engine unions them with the org's persisted policies (ADR-0007).
- Output: policy state survives restarts; every policy change is
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
