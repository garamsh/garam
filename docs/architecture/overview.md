# Architecture Overview

gaon is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

## In scope

- Identities for users and agents, including organization membership.
- Org / group / project structure.
- Google OIDC sign-in with a provider abstraction; server-side sessions.
- Invitation-based org membership with org roles (`owner`, `admin`,
  `member`) enforced on org-scoped HTTP APIs.
- An OpenAPI-first HTTP API; server code is generated from the spec.
- Postgres-backed stores (sqlc) with migrations.
- Authorization decisions, expressed as Rego policies (interfaces).
- One identifier system across every resource (GRN).

## Out of scope (today)

- Workflow execution engine, scheduler, queue.
- Notifications or messaging.
- The agent domain (still being planned; code untouched, no API or
  persistence).
- The Rego authz engine wiring and policy persistence (roadmap P1/P2);
  the MVP enforces org roles directly at the HTTP layer.
- OAuth providers beyond Google.

## Module boundaries

```
github.com/garamsh/gaon
├── api/          # OpenAPI spec (source of truth) + generated server
├── cmd/gaon/     # thin binary: wires stores, providers, handlers
├── migrations/   # SQL migrations (golang-migrate); sqlc schema source
├── e2e/          # store round-trips against real Postgres
├── internal/
│   ├── grn/      # identifier — every domain depends on it
│   ├── errorx/   # typed errors — every domain depends on it
│   ├── org/      # organizations, groups, projects, memberships, invitations
│   ├── user/     # human identities
│   ├── agent/    # autonomous identities
│   ├── authn/    # OAuth providers (OIDC) + sessions
│   ├── authz/    # subject/object, roles, policies, decisions
│   └── httpapi/  # handlers implementing the generated ServerInterface
└── (docs/)
```

`grn` and `errorx` sit at the inner ring. Domains cooperate through
`grn.GRN`, never through each other's concrete types. `httpapi` is the
only package that sees several domains at once; it enforces
authentication (session cookie) and org roles at the transport
boundary.

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
| `authz.Subject` | `internal/authz` | actor interface |
| `authz.Object` | `internal/authz` | target interface |
| `authz.Evaluator` | `internal/authz` | policy evaluator (Rego) |
| `authz.Checker` | `internal/authz` | role enrich + evaluate |
| `authz.Manager` | `internal/authz` | grant/revoke + audit |

## Decisions

| Decision | Reason | Where |
|----------|--------|-------|
| GRN organization = `garam` | Company identity for the org segment. Revise if multitenancy arrives. | [grn-model.md](./grn-model.md) |
| Rego package = `gaon.authz` | Policy namespace follows the service codename, not the org segment. | [authz.md](./authz.md) |
| `Evaluator` is an interface | Swap Rego / in-process / hardcoded without touching call sites. | [authz.md](./authz.md) |
| Store interfaces live next to their domain | Implementation swap at the package boundary. | [domains.md](./domains.md) |
| OpenAPI spec is the API source of truth | Handlers implement a generated interface; drift fails CI. | [adr/0001-openapi-first-api.md](./adr/0001-openapi-first-api.md) |
| Sessions are server-side rows | Logout must revoke; opaque tokens, only hashes persisted. | [adr/0002-server-side-sessions.md](./adr/0002-server-side-sessions.md) |
| Invitations are token-keyed, not GRN-addressed | The bearer token is the identifier; no new GRN service_type. | [adr/0003-invitation-tokens.md](./adr/0003-invitation-tokens.md) |

## Non-functional goals

- **Deterministic decisions** — `Manager.Explain` can reproduce why a
  request was allowed or denied.
- **Transport-agnostic errors** — `errorx` flows over HTTP, gRPC, CLI
  without translation at the source; the HTTP mapping lives in
  `internal/httpapi`.
- **Testable in isolation** — every domain exposes an interface; unit
  tests substitute at module boundaries (mocks, in-memory test doubles
  where they exist).
- **Storage-agnostic** — Postgres, SQLite, Redis plug into the same
  `Store` interface without domain code changes.
- **Reproducible generation** — oapi-codegen, sqlc, and mockery output
  is committed; CI regenerates and fails on drift (`make check-gen`).
