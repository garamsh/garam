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
- `internal/machine`
- `internal/machineapi`
- `internal/message`
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

A domain that persists nothing has none of the storage files above:
`internal/machine` is its types, its service, and the seam it reads a
trust root through.

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
| `Certificate` | what an agent authenticates with, as this domain hands it back |
| `Service` | creates an agent for the calling operator, and issues the certificate of one assigned to it |
| `Issuer` | signs a certificate for a subject GRN |

garam mints an agent's GRN and never changes it, so which operator runs
the agent is a relation a reassignment moves rather than part of the
identity. That GRN must name an agent of the organization that owns it:
ADR-0014 rests tenancy on its organization segment, so the domain
refuses a pairing that disagrees rather than trusting its callers, and
the schema refuses it again where nothing routes around it. An
assignment stays inside that organization and names an operator
(ADR-0015), which the schema alone decides. Every assignment raises the
epoch, and a state report is accepted only at the current one — the
operator a reassignment replaced reports nothing about the agent it
lost. An agent is never an organization member (ADR-0014), so nothing
here references `users`.

`Service` is what an operator does to the agents it runs. It creates
one in the operator's own organization, garam minting the identifier,
and it answers with the certificate of an agent assigned to that
operator at the current epoch and of no other — the whole of the
authorization on that surface, which ADR-0015 states is not an
optimisation and may not be skipped, so no method reaches an issuer
without it. Comparing the operator on the row is the epoch fence as
well, and it is the tenancy check too, since a GRN carries its
organization. A caller that is not an operator is refused: an agent
authenticates on the same listener and holds nothing.

Signing happens through the `Issuer` seam the composition root
satisfies from `internal/ca`, so the two domains still cooperate
through `grn.GRN` alone and this one never holds key material. The
HTTP surface is `internal/machineapi`, and the queue a message reaches
an agent through is `internal/message`.

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
short by default and bounded at both ends — a service whose configured
lifetime falls outside them does not start — and it carries jitter that
only shortens it, which keeps a fleet from expiring together (ADR-0015).
A lifetime reaching past what the signing CA has left is refused rather
than signed: such a certificate stops verifying the day the CA expires.

The CA private key reaches Postgres only through `Sealer`, whose one
implementation is AES-256-GCM under a key-encryption key read from an
owner-readable file (ADR-0015); the sealed bytes name the key that opens
them, and open only in the organization's row they were written for, so
write access to the database moves no trust root. A subject's own key is
generated per certificate and returned once, never stored. `garam serve
machine` is the process that issues, and it reads the key-encryption
key before it serves: the process that owns the authorities refuses to
start without the key that opens them rather than failing at the first
certificate it is asked for.

That key is as availability-critical as the database — losing it loses
every organization's CA with no recovery short of new authorities and a
reissue for every certificate — and why it is held this way rather than
in a KMS is [ADR-0016](./adr/0016-ca-key-custody.md).

## `internal/machine`

Who is calling on the listener a machine identity authenticates on
(ADR-0014, ADR-0015).

| Type | Responsibility |
|------|----------------|
| `Identity` | the GRN a verified client certificate names |
| `Service` | recovers that GRN and verifies the certificate against the organization's authority |
| `TrustRoot` | supplies the authority a certificate has to chain to |

An agent and an operator are the same kind of caller here and the
listener does not branch on which. The identity is the GRN in the
certificate's SAN URI and nothing beside it: no user row, no session,
no membership, so neither foreign key on `users` is touched and no
handler taking a `*user.User` changes.

The SAN carries exactly one URI and it is parsed whole as a GRN. That
one parse is the whole check: `grn.Parse` splits on every colon and
holds every segment to the character rule
([grn-model.md](./grn-model.md) §identifier), so a URI carrying a
separator that would move a segment — a colon outside the four, a `?`,
a `#` — is not a GRN and names no identity. The rule lives in
`internal/grn` and is pinned there; this boundary calls it rather than
keeping a copy. Which authority the certificate must then chain to is
read off the organization segment of that GRN, so a certificate one
organization's CA signed for another organization's identity is
refused by the trust root rather than by a check.

The package holds no store and reaches no database: `TrustRoot` is the
seam the composition root satisfies from `internal/ca`, which keeps the
two domains cooperating through `grn.GRN` alone. Verification needs the
authority's certificate and never its key.

`garam serve machine` is where the listener runs — its own process and
its own port (ADR-0017), terminating TLS itself with a certificate it
reads from a file, because a forwarded client-certificate header is
trustworthy only where every deployment strips it at the edge
(ADR-0014). What it serves is `internal/machineapi`. It is not part of
the local Docker stack: its key material is per host and the container
would run as a user that does not own it.

## `internal/machineapi`

What `garam serve machine` serves, implementing the ServerInterface
generated from `api/machine.yaml` (ADR-0019).

| Type | Responsibility |
|------|----------------|
| `Server` | the machine listener's handlers: the caller's identity, creating an agent, issuing an agent's certificate, and posting, claiming, renewing and completing a message |

A handler's caller is the identity `internal/machine` resolved from the
client certificate, read back with the comma-ok form and refused where
a request carries none — no session, no user row, no membership. What
an operator may reach is not decided here, and neither is whose queue a
caller may act on: `internal/agent` and `internal/message` decide those,
and this package parses what the path names, hands the caller over, and
renders the answer, turning an `errorx` kind into a status at its own
boundary as `internal/api` does at the browser-facing one. A poll that
found nothing is the one answer that is not a kind: it is an empty
success, so a caller loops on it rather than telling an error apart.

It holds no store and reaches no certificate authority. `internal/api`
and this package never import each other and never run in the same
process (ADR-0017).

## `internal/message`

The durable queue a message reaches an agent through (ADR-0013).

| Type | Responsibility |
|------|----------------|
| `Message` | one message addressed to a recipient, under an identifier of the queue's own |
| `Claim` | one reader's hold on a message, and the token that fences it |
| `Config` | the delivery parameters: lease, ceiling, attempts, and how long a claim waits |
| `Service` | what a machine identity does to its own queue: post, claim, renew, complete |

Messages are their own boundary rather than part of `internal/agent`: a
recipient is addressed by GRN, so nothing here depends on the agent
aggregate, and a message has a lifecycle no agent's contains.

The queue is a table and never a connection, so any garam instance can
hand any recipient's message to a reader and no process holds anything
about a recipient. A claim is exclusive for a lease and fenced by a
token: two readers never hold one message, and a reader that dies leaves
it claimable again once the lease runs out rather than stranded. The
token is what makes that safe — the next claim replaces it, so the
reader it replaced completes nothing. Postgres measures the lease, so no
instance's clock decides when a claim lapses.

Duplicate suppression is garam's (ADR-0013): a message is stored once
per sender and idempotency key, and the key outlives delivery so a
retry arriving afterwards neither stores a message nor requeues the
delivered one. Neither the recipient nor the sender carries a foreign
key — garam answers a sender once the message is stored and reports a
routing failure back as a message rather than rejecting one, so a
message it cannot yet route has to be storable. `Service` keeps that
rule where a caller meets it: posting checks that the recipient is a
GRN and never that it names an agent garam knows. A recipient that is
not a GRN is refused, which is where the line falls: ADR-0013 rejects
nothing because a refusal leaves the sender's reply in an outbox
nobody drains, and that argument is about a reply garam could have
delivered — an address that does not parse is a broken caller rather
than a routing failure, and a queue keyed by GRN could never hand the
row to anyone.

Handing a message out is an attempt, and attempts are bounded: a turn
that failed and a turn that never started are indistinguishable from
here, so what ends a message that fails the same way every time is the
count (ADR-0018). A holder renews its claim while it works, and
renewal carries the claim no further than a ceiling measured from when
it was taken — a renewal reaching past that is held at it, so a holder
that renews forever loses the claim rather than leaving a message
neither completed nor redelivered. Telling the sender that a message
exhausted its attempts is garam's obligation and is not built.

`Service` is what a machine identity does to its own queue, and every
method but posting takes the calling identity beside the recipient
whose queue it names. One comparison decides it: a GRN carries its
organization, so refusing a caller that is not the recipient refuses
another organization's agent by the same line, and no path reaches a
message around it. A claim waits for a message rather than answering
an empty queue at once, and it waits by re-reading the queue: a
message becomes ready when a lease runs out as well as when a sender
stores one, and a lease running out is a moment passing that nothing
announces. The interval is the delivery latency ADR-0013 accepted in
place of a push.

The HTTP surface is `internal/machineapi`; nothing here deletes a
delivered message, and the adapter is not built.

## Store implementation status

| Domain | Interface | postgres | memory | other |
|--------|-----------|----------|--------|-------|
| org | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| user | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.SessionStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authz (policies) | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| agent | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| ca | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| message | yes (`store.go`) | yes (`postgres.go`) | not planned | — |

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