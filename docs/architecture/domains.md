# Domains

Per-domain responsibilities and boundaries.

## Contents
- Common shape
- `internal/grn`
- `internal/errorx`
- `internal/org`
- `internal/authz`
- `internal/authn`
- `internal/user`
- `internal/agent`
- Store implementation status
- Known gap
- Future domains

## Common shape

Each `internal/<domain>/` package contains:

- `<domain>.go` — aggregate types and sentinels.
- `store.go` — `Store` interface.
- `postgres.go` — sqlc-backed implementation (`internal/<domain>/db/` holds the generated queries).
- `*_test.go` — tests.
- `mocks/` — mockery v3 output, for interfaces tests actually consume (testing.md §Mocking strategy).

Domains cooperate through `grn.GRN`, never through each other's
concrete types.

## `internal/grn`

`GRN` value type with `New`, `Parse`, `Validate`, `String`, `IsZero`.
Depends on `errorx` only.

## `internal/errorx`

`Kind` enum aligned with gRPC status codes, `Error` interface,
`New` / `Wrap` / `IsKind` / `KindOf`. No domain dependencies.

## `internal/org`

Top of the tenant hierarchy.

| Type | Responsibility |
|------|----------------|
| `Organization` | top-level tenant |
| `Group` | bundle of users/agents |
| `Project` | container for a unit of work |
| `Membership` | user in an org; carries a `Role` (`owner`, `admin`, `member`) |
| `Invitation` | single-use, expiring bearer token that admits a user into an org with a fixed role |

The `Store` interface covers CRUD on each plus membership and cascade
delete on `Organization`; group and project listing is scoped to one
organization, in both directions — the groups of an org, and the
groups of the org a member belongs to, which a check sends to the
engine to resolve group-attached policies (ADR-0008). Orgs grow by invitation and acceptance, not open signup.
The org domain owns its action constants (`ActionOrgRead`,
`ActionGroupRead`, `ActionProjectRead`, `ActionPolicyCreate`, …) —
including the policy management actions, which the api server enforces
without importing the engine that stores policies — and `Role.Actions`
maps each role to the action patterns it grants; `ActionsFor` does the same for
the role names a `Group` or `Project` carries as data. The authz
evaluator decides. Groups and projects are addressed under their
organization's HTTP paths, and their GRNs carry the owning org in the
organization segment, so a group or project role is scoped to that
resource alone (authz.md §Statement collection).

## `internal/authz`

Authorization model and decision engine (IAM-style, ADR-0002), served
over gRPC (ADR-0003 stage 2), plus the org-scoped policies it
evaluates.

| Type | Responsibility |
|------|----------------|
| `Statement` / `PolicyDocument` | IAM-style rules: `Effect`, `Action[]`, `Resource[]` patterns |
| `StatementEvaluator` | native in-process `Evaluator`: explicit Deny > Allow > default Deny |
| `GRPCServer` | Check/Explain over `proto/garam/authz/v1` |
| `Policy` / `PolicyVersion` | a versioned org rule set and its immutable revisions |
| `PolicyAttachment` | binds a policy to the user or group it applies to (ADR-0008) |
| `Store` / `PostgresStore` | persists policies, their append-only versions, and attachments |
| `PolicyCache` | an org's active policies and attachments, dropped on write and on `NOTIFY` |
| `PolicyGRPCServer` | the policy lifecycle over `proto/garam/authz/v1` |

The api server authorizes through the gRPC client and manages policies
through the `PolicyManager` seam over the same connection
([authz.md](./authz.md) §Managing policies over HTTP); the engine
matches strings only and never imports a domain. Lifecycle, caching, and
invalidation are described in [authz.md](./authz.md) §Policy
persistence.

## `internal/authn`

Authentication: the sign-in provider abstraction and sessions.

| Type | Responsibility |
|------|----------------|
| `Provider` | OIDC authorization-code flow with PKCE; `google.go` and the dev-only `dev.go` (ADR-0010) implement it |
| `Identity` | subject + nickname asserted by a provider |
| `Session` | browser session for a user, expiring |
| `SessionStore` | persists sessions keyed by token hash |

A second provider is a new file implementing `Provider`; call sites
depend only on the interface. Raw session tokens live in cookies; only
SHA-256 hashes reach the database.

## `internal/user`

Human identity.

| Field | Meaning |
|-------|---------|
| `grn.GRN` | user identifier (`service_type = "user"`) |
| `nickname` | display name, not enforced unique |
| `roles` | directly assigned roles |
| `policies` | directly assigned policies; not yet evaluated |

A user can belong to multiple orgs; membership is recorded only in the
org domain (`Membership` / `org_members`), never duplicated on the
user. Roles on the user are direct grants, collected by the api server
alongside group- and project-derived grants at check time
([authz.md](./authz.md)). The `policies` field is stored but not
evaluated: a policy reaches a subject through an attachment the authz
engine owns (ADR-0008), not through this list.

## `internal/agent`

Autonomous identity, provisioned outside garam (ADR-0012).

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Assignment` | which operator runs an agent, and the epoch that binding holds |

garam mints an agent's GRN and never changes it, so which operator runs
the agent is a relation a reassignment moves rather than part of the
identity. Every assignment raises the epoch, and a state report is
accepted only at the current one — the operator a reassignment replaced
reports nothing about the agent it lost. An agent is never an
organization member (ADR-0014), so nothing here references `users`.
Persistence only: no endpoint, no certificate, no message queue.

## Store implementation status

| Domain | Interface | postgres | memory | other |
|--------|-----------|----------|--------|-------|
| org | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| user | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.SessionStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authz (policies) | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| agent | yes (`store.go`) | yes (`postgres.go`) | not planned | — |

The postgres implementations are generated by sqlc from per-domain
`queries.sql` files into `internal/<domain>/db/`; the schema source is
`migrations/`. Domain stores go straight to Postgres — no domain
`memory.go` files are planned. See
[`../plans/roadmap.md`](../plans/roadmap.md).

## Known gap

Domain types (`User`, `Organization`, …) do not satisfy the `authz.Object`
seam; the engine adapts a parsed GRN at its transport boundary. If a
future design needs domain-typed subjects or objects, it should define
the adapter where it is used.

## Future domains

| Domain | Location (planned) | Responsibility |
|--------|---------------------|----------------|
| workflow | `internal/workflow` | workflow definition and execution |
| doc | `internal/doc` | permissioned document |
| audit | `internal/audit` | decision and change events |
| token | `internal/token` | API token exchange (browser sessions live in `internal/authn`) |