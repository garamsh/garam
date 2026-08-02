# Architecture Overview

gaon is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

## In scope

- Identities for users and agents, including organization membership.
- Org / group / project structure; groups and projects are created,
  read, updated, deleted, and populated with members over the HTTP API
  under their organization's paths.
- Google OIDC sign-in with a provider abstraction; server-side sessions.
- Invitation-based org membership with org roles (`owner`, `admin`,
  `member`) enforced on org-scoped HTTP APIs — including the group and
  project endpoints — through the authz evaluator seam.
- An OpenAPI-first HTTP API; server code is generated from the spec.
- Postgres-backed stores (sqlc) with migrations.
- Authorization decisions, expressed as IAM-style policy documents.
- One identifier system across every resource (GRN).

## Out of scope (today)

- Workflow execution engine, scheduler, queue.
- Notifications or messaging.
- The agent domain (still being planned; code untouched, no API or
  persistence).
- authz policy persistence and versioning (roadmap P2); the gRPC
  engine evaluates caller-supplied statements (ADR-0003).
- OAuth providers beyond Google.

## Module boundaries

- `api/` — OpenAPI spec (source of truth) + generated server.
- `proto/` — authz gRPC service definition + buf-generated code.
- `cmd/gaon/` — thin entrypoint for the cobra CLI.
- `migrations/` — SQL migrations (golang-migrate); sqlc schema source.
- `tests/e2e/` — store round-trips against real Postgres.
- `internal/grn/` — identifier; shared primitive.
- `internal/errorx/` — typed errors; shared primitive.
- `internal/cli/` — cobra commands + wiring (`serve api` | `serve authz` | `migrate`).
- `internal/api/` — handlers implementing the generated ServerInterface.
- `internal/authn/` — OIDC provider abstraction, sessions.
- `internal/authz/` — policy engine: model, evaluator, gRPC server.
- `internal/org/` — organizations, groups, projects, memberships, invitations.
- `internal/user/` — human identities.
- `internal/agent/` — autonomous identities (planning).

`grn` and `errorx` are the only shared packages. Domains cooperate
through `grn.GRN`, never through each other's concrete types. `api`
sees several domains at once and enforces authentication (session
cookie) at the boundary; org-role checks go through the `Evaluator`
seam, implemented by the authz gRPC client (ADR-0003 stage 2).
`internal/api` and `internal/authz` never import each other; the wire
contract is `proto/gaon/authz/v1`.

## Core abstractions

| Type | Package | Responsibility |
|------|---------|----------------|
| `grn.GRN` | `internal/grn` | resource identifier (value type) |
| `errorx.Error` | `internal/errorx` | typed error with kind |
| `user.User` | `internal/user` | human identity |
| `agent.Agent` | `internal/agent` | autonomous identity |
| `org.Organization` | `internal/org` | top-level tenant |
| `org.Group` | `internal/org` | user/agent bundle |
| `org.Project` | `internal/org` | unit of work |
| `org.Membership` | `internal/org` | user in an org, carries a `Role` |
| `org.Invitation` | `internal/org` | single-use token into an org |
| `authn.Provider` | `internal/authn` | OIDC provider (Google first) |
| `authn.Session` | `internal/authn` | authenticated browser session |
| `authz.Object` | `internal/authz` | target interface |
| `authz.Evaluator` | `internal/authz` | policy evaluator (native, IAM-style) |

## Decisions

| Decision | Reason | Where |
|----------|--------|-------|
| GRN org/project = owning resource identifiers | Tenancy is structural; reserved `root` owns organizations. | [grn-model.md](./grn-model.md), [ADR-0001](./adr/0001-grn-segments.md) |
| Policies are IAM-style documents, native evaluator | Deterministic deny-overrides; no runtime dependency. | [authz.md](./authz.md), [ADR-0002](./adr/0002-iam-style-policy-model.md) |
| Single cobra CLI, independent api/authz packages | One binary, two processes; engine reached over gRPC only. | [ADR-0003](./adr/0003-single-cli-topology.md) |
| `Evaluator` is an interface | Swap in-process / gRPC-client without touching call sites. | [authz.md](./authz.md) |
| Store interfaces live next to their domain | Implementation swap at the package boundary. | [domains.md](./domains.md) |
| OpenAPI spec is the API source of truth | Handlers implement a generated interface; drift fails CI. | [adr/0004-openapi-first-api.md](./adr/0004-openapi-first-api.md) |
| Sessions are server-side rows | Logout must revoke; opaque tokens, only hashes persisted. | [adr/0005-server-side-sessions.md](./adr/0005-server-side-sessions.md) |
| Invitations are token-keyed, not GRN-addressed | The bearer token is the identifier; no new GRN service_type. | [adr/0006-invitation-tokens.md](./adr/0006-invitation-tokens.md) |

## Non-functional goals

- **Deterministic decisions** — the engine's `Explain` RPC can
  reproduce why a request was allowed or denied.
- **Transport-agnostic errors** — `errorx` flows over HTTP, gRPC, CLI
  without translation at the source; the HTTP mapping lives in
  `internal/api`.
- **Testable in isolation** — every domain exposes an interface; unit
  tests substitute at module boundaries (mocks, in-memory test doubles
  where they exist).
- **Storage-agnostic** — Postgres, SQLite, Redis plug into the same
  `Store` interface without domain code changes.
- **Reproducible generation** — oapi-codegen, sqlc, buf, and mockery
  output is committed; CI regenerates and fails on drift
  (`make check-gen`).
