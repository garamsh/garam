# Roadmap

garam is built bottom-up: identity first, authorization second,
workflow last. Each tier depends only on the tier below it.

## Contents
- MVP — Authentication + org permissions (done)
- P1 — authz engine (done)
- P2 — authz persistence & policy versioning (done)
- Web console (in progress)
- P3 — Workflow core (not started)
- P4 — Operational (partly done)
- Collaboration platform (decided, not started)
- Sequencing rules

## MVP — Authentication + org permissions (done)

- GRN value type, `errorx`, domain types and `Store` interfaces for
  `org` / `user`.
- `platform/internal/authn/` — OAuth provider abstraction (OIDC) with
  Google as the first provider; more providers plug in later without
  touching call sites.
- `org` gains an `Invitation` aggregate: orgs grow by invitation and
  acceptance, not open signup. Minimal org roles (`owner`, `admin`,
  `member`) on membership, expressed as IAM-style policy statements
  and enforced by the authz service (ADR-0002).
- Single cobra CLI (`platform/cmd/garam`): `serve api` (OpenAPI HTTP)
  and `serve authz` (gRPC policy engine) run as independent packages
  (ADR-0003).
- The `agent` domain stayed excluded, and was decided after this tier
  (ADR-0012, ADR-0013, ADR-0026, ADR-0033).
- Postgres-backed `Store` implementations via sqlc. No in-memory
  domain stores.
- OpenAPI spec as the source of truth for the HTTP API; server code
  generated from the spec (oapi-codegen, stdlib `net/http`).
- Migrations under `platform/migrations/` (golang-migrate). Dockerised
  Postgres for dev; e2e store round-trips against real Postgres.

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

## Web console (in progress)

Not a tier in the dependency stack — a surface over what P1 and P2
already expose, built in parallel rather than gating P3.

- `console/`: Next.js App Router, Tailwind v4, shadcn/ui, three test
  layers (ADR-0009). Its own toolchain and gate, `make web-ci`, which
  `make ci` runs.
- A public landing page at `/`, sign-in at `/login`, and the signed-in
  application under `/console`.
- Sign-in with a username and a password beside Google, registered
  from an invitation (ADR-0041).
- What each section of an organization reads and writes — Teams,
  People, Agents, Projects, Operators, Consents and Permissions — is
  [web-app.md](../architecture/web-app.md) §What it serves.

## P3 — Workflow core (not started)

- `internal/workflow` — workflow definition, state machine, execution.
- Workflows are themselves GRN-addressed resources.
- authz remains the gate at every transition.

## P4 — Operational (partly done)

- Structured logging (`log/slog`) with `trace_id` propagation — done.
  Handlers log the route pattern, never the concrete path, so an
  invitation token cannot reach a log line.
- Prometheus metrics and OpenTelemetry tracing — metrics done for
  `serve api` and `serve authz`, on the listener each already runs, and
  for `serve machine`, on its probe port
  ([api-surface.md](../architecture/api-surface.md) §The surfaces).
  Spans are minted and propagated but not exported: there is nowhere to
  ship them yet.
- Container image and a minimal deploy path — built. Both images come
  from `deploy/local/Dockerfile` and `Dockerfile.web`, and the chart at
  `deploy/charts/garam/` is the description of a deployment (ADR-0042),
  checked by `make chart` inside `make ci`. What a deployment must plan
  for is
  [deployment-contract.md](../architecture/deployment-contract.md).
  Publishing is built and exercised: `.github/workflows/release.yml`
  publishes both images on a `vX.Y.Z` tag push under the names and the
  tag rule ADR-0039 fixed, and `v0.1.0` ran it on 2026-09-01. Both
  repositories held `0.1.0` when ADR-0039's erratum of 2026-09-02 read
  them, and the release that tag created records each image's digest
  beside it. A development build goes into `garam-dev/garam-server` and
  `garam-dev/garam-web` under a commit-derived or prerelease tag:
  `make image-build` builds one locally and a person pushes it
  (ADR-0071, ADR-0097).

## Collaboration platform (decided, not started)

The owner decided on 2026-10-27 that garam grows into an agent-based
collaboration platform: a document service ("cowork"), product
management, agents working beside people, and organization-level
model configuration. #1655 records the decisions and orders the work.

- The repository becomes a monorepo of services (ADR-0125). #1659
  moves today's tree, #1661 runs CI per affected module, and #1660
  splits the web app into `site/` and `console/`.
- Agents reach models through an AI gateway (#1658).
- `ai-gateway/` and `cowork/` are filed once those decisions are
  recorded. Product management has no design yet.

P3's workflow core is not dropped. Where it lands among these
services is decided when its work starts.

## Sequencing rules

- A later level cannot start until the previous level is green. The web
  console is not a level: it consumes finished tiers and does not gate
  the next one.
- Cross-cutting changes (logging, metrics) enter in P4, not earlier.
- If a level uncovers a design hole, fix it before moving on — no
  exceptions for "we can clean this up later."
