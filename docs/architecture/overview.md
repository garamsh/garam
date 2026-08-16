# Architecture Overview

garam is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

## Contents
- In scope
- Out of scope (today)
- Module boundaries
- Core abstractions
- Decisions
- Non-functional goals

## In scope

- Identities for users and agents, including organization membership.
- Org / group / project structure; groups and projects are created,
  read, updated, deleted, and populated with members over the HTTP API
  under their organization's paths.
- Sign-in through a provider abstraction — Google OIDC, plus a
  development-only provider gated by `GARAM_DEV_AUTH` (ADR-0010);
  server-side sessions.
- Invitation-based org membership with org roles (`owner`, `admin`,
  `member`) enforced on org-scoped HTTP APIs — including the group and
  project endpoints — through the authz evaluator seam.
- An OpenAPI-first HTTP API; server code is generated from the spec.
- Postgres-backed stores (sqlc) with migrations.
- Authorization decisions, expressed as IAM-style policy documents.
- Org-managed policy documents: stored, versioned append-only,
  activated and rolled back over the authz service, and cached per org
  in the engine. Org admins drive that lifecycle over the HTTP API
  under `/orgs/{org}/policies`, and attach a policy to a user or a
  group (ADR-0008); an unattached active policy still grants org-wide.
- A web console under `/console` for the org structure the API exposes:
  organizations are listed and created, members are invited by link and
  the invitation accepted, groups and projects are read (ADR-0009). Its
  shell is a collapsible sidebar over `Organizations → organization →
  section`, and it addresses resources by the GRN's last segment rather
  than printing GRNs (ADR-0011).
- One identifier system across every resource (GRN).

## Out of scope (today)

- Workflow execution engine, scheduler, queue.
- Notifications or messaging.
- The agent domain. ADR-0012 settles its shape — identity, operator
  assignment, delivery and state — and ADR-0013 settles how a message
  reaches one, but no package, endpoint or persistence exists, and the
  machine authentication both need does not either.
- Chat channels. ADR-0013 makes garam a message API and leaves Slack,
  LINE and the rest to consumers of it; none is built, and whether any
  ships first-party is undecided.
- Resource-based policies: a statement names no `Principal`, so a
  policy reaches subjects through an identity attachment only
  (ADR-0008).
- Per-org direct user roles: a role stored on the user applies in every
  org it belongs to; scoping needs subject × role × scope persistence.
- Upstream OAuth providers beyond Google. The dev provider (ADR-0010)
  is not one — it asserts a fixed local identity and contacts nothing.
- Editing groups, projects and policies from the web console; those
  endpoints exist, the console reads them but does not write them.

## Module boundaries

- `api/` — OpenAPI spec (source of truth) + generated server.
- `proto/` — authz gRPC service definition + buf-generated code.
- `cmd/garam/` — thin entrypoint for the cobra CLI.
- `migrations/` — SQL migrations (golang-migrate); sqlc schema source.
- `tests/e2e/` — store round-trips against real Postgres.
- `web/` — Next.js frontend; reaches the API through a same-origin
  `/api/*` rewrite (ADR-0009). Its own toolchain and gate (`make web-ci`).
  `/` is the public landing page and `/login` the sign-in page; the
  signed-in application lives under `/console`, whose shell and route
  structure are ADR-0011.
- `deploy/local/` — Docker Compose stack running Postgres, migrations,
  authz, api and (profile-gated) web on one machine; its `postgres`
  service also hosts the e2e tests' database (`make db-up`), kept
  separate from the stack's own.
- `internal/grn/` — identifier; shared primitive.
- `internal/errorx/` — typed errors; shared primitive.
- `internal/cli/` — cobra commands + wiring (`serve api` | `serve authz` | `migrate`).
- `internal/api/` — handlers implementing the generated ServerInterface.
- `internal/authn/` — sign-in provider abstraction (Google OIDC,
  dev-only local), sessions.
- `internal/authz/` — policy engine: model, evaluator, policy store
  and per-org cache, gRPC servers.
- `internal/org/` — organizations, groups, projects, memberships, invitations.
- `internal/user/` — human identities.

`grn` and `errorx` are the only shared packages. Domains cooperate
through `grn.GRN`, never through each other's concrete types. `api`
sees several domains at once and enforces authentication (session
cookie) at the boundary; org-role checks go through the `Evaluator`
seam, implemented by the authz gRPC client (ADR-0003 stage 2).
`internal/api` and `internal/authz` never import each other; the wire
contract is `proto/garam/authz/v1`.

## Core abstractions

| Type | Package | Responsibility |
|------|---------|----------------|
| `grn.GRN` | `internal/grn` | resource identifier (value type) |
| `errorx.Error` | `internal/errorx` | typed error with kind |
| `user.User` | `internal/user` | human identity |
| `org.Organization` | `internal/org` | top-level tenant |
| `org.Group` | `internal/org` | user/agent bundle |
| `org.Project` | `internal/org` | unit of work |
| `org.Membership` | `internal/org` | user in an org, carries a `Role` |
| `org.Invitation` | `internal/org` | single-use token into an org |
| `authn.Provider` | `internal/authn` | sign-in provider (Google OIDC; dev-only, ADR-0010) |
| `authn.Session` | `internal/authn` | authenticated browser session |
| `authz.Object` | `internal/authz` | target interface |
| `authz.Evaluator` | `internal/authz` | policy evaluator (native, IAM-style) |
| `authz.Policy` | `internal/authz` | stored policy, versioned append-only |
| `authz.PolicyCache` | `internal/authz` | per-org parsed statement set |

## Decisions

| Decision | Reason | Where |
|----------|--------|-------|
| GRN org/project = owning resource identifiers | Tenancy is structural; reserved `root` owns organizations. | [grn-model.md](./grn-model.md), [ADR-0001](./adr/0001-grn-segments.md) |
| Policies are IAM-style documents, native evaluator | Deterministic deny-overrides; no runtime dependency. | [authz.md](./authz.md), [ADR-0002](./adr/0002-iam-style-policy-model.md) |
| Single cobra CLI, independent api/authz packages | One binary, two processes; engine reached over gRPC only. | [ADR-0003](./adr/0003-single-cli-topology.md) |
| Statements come from two origins, unioned | Org tables stay the source of truth for roles; the engine stays domain-free. | [authz.md](./authz.md), [ADR-0007](./adr/0007-hybrid-statement-collection.md) |
| `Evaluator` is an interface | Swap in-process / gRPC-client without touching call sites. | [authz.md](./authz.md) |
| Store interfaces live next to their domain | Implementation swap at the package boundary. | [domains.md](./domains.md) |
| OpenAPI spec is the API source of truth | Handlers implement a generated interface; drift fails CI. | [adr/0004-openapi-first-api.md](./adr/0004-openapi-first-api.md) |
| Sessions are server-side rows | Logout must revoke; opaque tokens, only hashes persisted. | [adr/0005-server-side-sessions.md](./adr/0005-server-side-sessions.md) |
| Dev sign-in is a provider, not a bypass | Sessions stay identical to Google's; with the flag off no dev route exists. | [adr/0010-dev-auth-provider.md](./adr/0010-dev-auth-provider.md) |
| Invitations are token-keyed, not GRN-addressed | The bearer token is the identifier; no new GRN service_type. | [adr/0006-invitation-tokens.md](./adr/0006-invitation-tokens.md) |
| The console is a sidebar shell over org sections | One structure for switching orgs, moving between sections, and knowing where you are. | [adr/0011-console-shell.md](./adr/0011-console-shell.md) |
| Agents are provisioned by operators garam never dials | An operator is a peer, so a user can supply their own; a sidecar's outbound connection is the only path in. | [adr/0012-agent-operator-model.md](./adr/0012-agent-operator-model.md) |
| Messages reach an agent by polling, not by push | A queue in Postgres has no connection to route to, so any instance answers any poll. | [adr/0013-agent-messaging.md](./adr/0013-agent-messaging.md) |

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
