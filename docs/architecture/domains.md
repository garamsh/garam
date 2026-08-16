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
- `internal/ca`
- Store implementation status
- Known gap
- Future domains

## Common shape

Each `internal/<domain>/` package contains:

- `<domain>.go` — aggregate types and sentinels.
- `store.go` — `Store` interface.
- `queries.sql` — the domain's SQL, hand-written. The only place SQL is written.
- `postgres.go` — hand-written `Store` implementation, calling `db/`.
- `db/` — sqlc output from `queries.sql`. Generated, never edited.
- `*_test.go` — tests.
- `mocks/` — mockery v3 output, for interfaces tests actually consume (testing.md §Mocking strategy).

`queries.sql` and `db/` are a source and its output, not two copies. Every
generated file says so in its first line, and `make check-gen` fails on any
drift between them.

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
identity. That GRN must name an agent of the organization that owns it:
ADR-0014 rests tenancy on its organization segment, so the domain
refuses a pairing that disagrees rather than trusting its callers. Every
assignment raises the epoch, and a state report is accepted only at the
current one — the operator a reassignment replaced reports nothing about
the agent it lost. An agent is never an organization member (ADR-0014),
so nothing here references `users`. Persistence only: no endpoint, no
certificate, no message queue.

## `internal/ca`

The certificate authority an organization signs machine identities with
(ADR-0014).

| Type | Responsibility |
|------|----------------|
| `Authority` | an organization's CA: the certificate it signs with, and its private key in sealed form |
| `Certificate` | an issued client certificate, the private key it was issued with, and the CA that signed it |
| `Sealer` | seals key material so what reaches storage is useless without the key-encryption key the process holds |
| `Service` | issues a certificate for a subject GRN, creating the organization's CA on demand |

Every organization has its own CA, so a certificate issued by one does
not chain against another's and tenancy holds in the trust root rather
than in a check a caller could forget. The subject's GRN travels in a
SAN URI and the certificate carries nothing else — no name, no roles, no
claims. Lifetime is the only revocation there is (ADR-0014), so it is
short by default, configurable, and carries jitter that only shortens
it, which keeps a fleet from expiring together (ADR-0015).

The CA private key reaches Postgres only through `Sealer`, whose one
implementation is AES-256-GCM under a key-encryption key read from an
owner-readable file (ADR-0015); the sealed bytes name the key that opens
them. A subject's own key is generated per certificate and returned
once, never stored. No process constructs the service yet: the listener
that verifies a presented certificate is unbuilt, and the boot-time key
requirement lands with it.

A key the process holds rather than a cloud KMS, considered and
rejected: a KMS is the stronger custody, because the key never enters
garam's memory, and taking it would make issuing depend on a hosted
service in a project that has none — the self-hosting ADR-0012 is built
around. `Sealer` is the seam that keeps the option open: a KMS
implementation replaces the one there is without touching a caller.

## Store implementation status

| Domain | Interface | postgres | memory | other |
|--------|-----------|----------|--------|-------|
| org | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| user | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.SessionStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authz (policies) | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| agent | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| ca | yes (`store.go`) | yes (`postgres.go`) | not planned | — |

The postgres implementations are written by hand against sqlc output,
which sqlc generates from each domain's `queries.sql` into
`internal/<domain>/db/`; the schema source is `migrations/`. Domain
stores go straight to Postgres — no domain `memory.go` files are
planned.

sqlc rather than an ORM, re-examined and unchanged: it parses the real
schema and the real query at build time, so a renamed column fails the
build instead of a request. `migrations/` stays the one schema source,
where an ORM's auto-migrate would make a second. The storage-agnostic
goal is delivered by the `Store` interface, which an ORM's tags and
associations would reach past. And the features already in use —
`LISTEN`/`NOTIFY`, triggers, and the row-level locking a queue needs —
are where an ORM hands the work back anyway. See
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