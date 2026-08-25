# Domains

Per-domain responsibilities and boundaries.

## Contents
- Common shape
- `internal/grn`
- `internal/errorx`
- `internal/pgxerr`
- `internal/keyfile`
- `internal/listfilter`
- `internal/org`
- `internal/authz`
- `internal/authn`
- `internal/user`
- `internal/operator`
- `internal/agent`
- `internal/ca`
- `internal/machine`
- `internal/machineapi`
- `internal/message`
- `internal/watch`
- Store implementation status
- Known gap
- Future domains

## Common shape

Each `internal/<domain>/` package contains:

- `<domain>.go` — aggregate types and sentinels.
- `store.go` — `Store` interface.
- `service.go` — the `Service` contract, where the domain has
  behaviour to coordinate; a domain that is only types and persistence
  has none.
- `queries.sql` — the domain's SQL, hand-written. The only place the
  domain's own queries are written: `migrations/` holds the schema, and
  the `LISTEN`/`NOTIFY` statements that read no row are issued from Go
  (`internal/authz`, `internal/watch`).
- `postgres.go` — hand-written `Store` implementation, calling `db/`.
  Past the single-file size limit it splits into `postgres_<concept>.go`
  files beside it, one per aggregate the store persists, inside the same
  package: the aggregates' fields are unexported, so the store that
  rehydrates them must share their package.
- `db/` — sqlc output from `queries.sql`. Generated, never edited.
- `*_test.go` — tests.
- `mocks/` — mockery v3 output, for interfaces tests actually consume (testing.md §Mocking strategy).

A domain that persists nothing has none of the storage files above:
`internal/machine` is its types, its service, and the two seams it
reads through — a trust root for the caller, a certificate for itself.

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

## `internal/pgxerr`

`Classify(err, op, notFound, conflict)` maps a postgres driver error
to an `errorx` error. It reads the sql state on a `*pgconn.PgError`:
`pgx.ErrNoRows` returns the caller's `notFound` (or wraps the chain),
23505 returns `conflict` (or `KindAlreadyExists`), 23503 returns
`notFound` (or `KindNotFound` with "<op>: referenced row missing"),
23514 returns `KindInvalidArgument`, P0001 returns
`KindFailedPrecondition`, anything else wraps with `op` and the
chain's kind. `op` is the caller's own label for the operation;
the wrap message is `op` alone, not prefixed with the store name.
Depends on `errorx`, `pgx`, `pgconn`. The five domain postgres stores
that each held their own copy (`org`, `authz`, `ca`, `user`, `agent`)
now answer through here. The `message` store did not have its own
copy — it had inline `errors.Is(err, pgx.ErrNoRows)` checks — and
remains so; the audit that named it as a fifth copy was wrong on
this.

## `internal/keyfile`

The one rule key material is read under (ADR-0015): a file only the
reading process's owner can reach — owner-only permission bits and an
owner equal to the process's UID, both read from the open descriptor so
no window sits between the check and the read. The rule is neither
certificates nor listeners, so it lives here once for every process that
opens one: `internal/ca` reads the key-encryption key through it, and
the composition root reads the machine listener's pair and the adapter's
through it.

`Pair` is a certificate and its key held as the two paths they arrive
at rather than as what they held at startup, so every read answers with
the files as they are now and a replaced pair is presented on the next
handshake without a restart (ADR-0040). The key goes through the rule
above on every read; the certificate beside it is public. Depends on
`errorx` only.

## `internal/listfilter`

`Filter` scopes a domain's list call: an offset and a limit, where a
limit of 0 means no limit. Org, agent and user all page their lists
with it, and a domain may not import a sibling to borrow the type, so
it is extracted rather than reached through one of them. No
dependencies.

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
without importing the engine that stores policies, the consent
management actions (ADR-0035), which it enforces over the relation
`internal/message` stores, and reading an
organization's agents, which it enforces without `internal/agent`
importing this package back — and `Role.Actions`
maps each role to the action patterns it grants; `ActionsFor` does the same for
the role names a `Group` or `Project` carries as data. No role holds a
wildcard over agents, and none holds one over operators: ADR-0012 keeps
provisioning outside garam, so each agent action a role grants is granted
by name and a later one is a decision rather than an increment, and
ADR-0045 leaves an operator's enrolment and its revocation undecided,
which is more of the same.
`agent:watch` is granted by no role at all, which is that reason carried
further — ADR-0022 separates watching from reading so an organization may
withhold it, and a role naming it would settle that for every
organization instead. The authz evaluator decides. Groups and projects are addressed under their
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

Authentication: the sign-in provider abstraction, password
verification, and sessions.

| Type | Responsibility |
|------|----------------|
| `Provider` | OIDC authorization-code flow with PKCE; `google.go` implements it (ADR-0044 removed the development-only second implementation) |
| `Identity` | subject + nickname asserted by a provider |
| `Session` | browser session for a user, expiring |
| `SessionStore` | persists sessions keyed by token hash |
| `OAuthStateStore` | persists the state a login issued, until the callback spends it or its lifetime runs out (ADR-0028); issuing refuses past `MaxLiveOAuthStates` live states (ADR-0030) |
| `Password` | a password as typed; renders as a redaction marker in every form, so it cannot reach a log line, a response, or a message (ADR-0041) |
| `Verifier` | the stored Argon2id form of a password, carrying the parameters it was computed under |
| `PasswordCredentialStore` | persists one verifier per user that has a password |
| `PasswordAttemptStore` | bounds the verifications live at once; recording refuses past `MaxLivePasswordAttempts` (ADR-0041) |

A second provider is a new file implementing `Provider`; call sites
depend only on the interface. Raw session tokens live in cookies; only
SHA-256 hashes reach the database, the state a login issues included.

A password is not a `Provider` and never becomes one: that interface is
an authorization-code flow, and satisfying it would mean inventing a
code. Password sign-in is its own endpoint asserting the same
`Identity`, after which the path is the callback's — a session row and
a cookie, unchanged (ADR-0005, ADR-0041). What is stored is an Argon2id
verifier carrying its own parameters, so raising them invalidates no
row: a sign-in against any other setting re-hashes on its way out.
Accounts minted this way take the reserved `local-` prefix in the user
identifier space, which a provider subject may not claim, so a password
account and a provider account can never resolve to one user row.

A wrong password and a username no credential belongs to are one
refusal, in message and in the work done: the second verifies against a
verifier derived from random bytes rather than returning early.

A callback completes once because the state it carries is spent from a
row, not because the response asked the browser to drop the cookie
holding it (ADR-0028). The state's own lifetime bounds the row, so a
record cannot outlive what it is checked against or lapse before it.
Issuing refuses once `MaxLiveOAuthStates` states are live, which bounds
the write rate the unauthenticated login endpoint can be driven at to
that many rows per flow lifetime, for every replica at once — the bound
is decided in the statement that writes, not held by any one process
(ADR-0030).

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

## `internal/operator`

What an organization records about an operator, and what that operator
authenticates with: the record that the organization has it (ADR-0045),
and which certificate garam last issued it, together with what it takes
to replace that with the next (ADR-0046).

| Type | Responsibility |
|------|----------------|
| `Operator` | an organization's record that it has an operator under this GRN: who registered it and when |
| `Store` | registers one, answers one by GRN, and lists an organization's own |
| `Credential` | the certificate a connection authenticated with — the identity it names, which certificate it is, and how long it is good for |
| `Certificate` | what an operator authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Current` | the certificate garam last issued an operator: the record a renewal is measured against |
| `CertificateStore` | records what garam last issued, and moves that record to the successor only where the presented certificate is still it |
| `Service` | mints an operator's certificate out of band and adopts it, and renews the one an operator presents |
| `Issuer` | signs a certificate for a subject GRN and hands back the garam server root beside it |

The identifier is the caller's rather than garam's, which is what makes
this domain unlike every other org resource. An operator's GRN is
already the subject of a certificate wherever `issue-certificate` has
run (ADR-0032), so a minted identifier would name something other than
the operator that exists. What the domain applies is the GRN segment
rule, and the schema applies it again where nothing routes around it.

A record grants nothing and reports nothing. No listener reads one to
admit a caller: an operator authenticates with the certificate it holds
(ADR-0015), and garam never dials one, so there is no state here for
anybody to report. What the record is for is the question a definition
asks — whether this organization has the operator it names — and
`internal/api` asks it there. The `agent_definitions` foreign key is the
copy of that check no call path routes around.

There is no update and no delete. A record carries nothing to change,
and removing one would stop nothing an operator can already do, which is
a route ADR-0045 declines to offer until revocation is decided.

The certificate half reads no record above it. An operator renews with
what it holds, and whether its organization registered it changes
nothing about that.

An operator's first certificate arrives out of band (ADR-0015,
ADR-0032). Every one after it is issued here, over the connection the
certificate being replaced authenticated, so the out-of-band mint is a
bootstrap rather than a standing obligation.

Three things refuse a renewal, and each is a decision rather than a
guard. A certificate that has expired renews nothing: the listener
refuses it a handshake earlier, and the rule states the same boundary as
the empty end of its window, so what follows expiry is a mint out of
band. A certificate with more than a third of its validity left is
refused as too early, measured off the certificate rather than off a
configured number, so the window holds whatever lifetime signed it. And
only the certificate garam last issued an operator may renew it: garam
records that certificate and moves the record to the successor in the
statement that checks it, so every other copy of that identity stops
being renewable at once. That last one is what bounds a stolen
certificate where there is no revocation — the operator it was taken
from renews, the copy loses the lineage, and it expires. It revokes
nothing: a certificate that can no longer renew keeps authenticating
until it runs out.

The mint adopts whatever it issued, so re-running `garam
issue-certificate` takes the lineage from every certificate that already
names the operator. That is the recovery from an answer nobody received,
and the closest thing to a revocation garam has.

`CertificateStore` holds one row per operator and has no read: what the
rule needs is not the current certificate but whether the presented one
is still it, and the write answers that itself. Signing happens through
the `Issuer` seam the composition root satisfies from `internal/ca`, as
the agent domain's does, so this one never holds key material; the
successor is signed before the record moves, and a caller that lost the
lineage receives nothing. The HTTP surface is `internal/machineapi`, and
`internal/cli` is the other caller — `garam issue-certificate`, the one
bootstrap ADR-0015 leaves out of band. The agent's certificate is not
this domain's: an agent is admitted to its own by the claim its operator
made (ADR-0033) and renews on the route ADR-0040 gave it.

## `internal/agent`

Autonomous identity, provisioned outside garam (ADR-0012).

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Definition` | what an organization says an agent should be: which operator runs it and with what values (ADR-0026). Shares no field with `Agent`, so a reader cannot take one for the other, and writes no `agents` row until the operator it names claims it (ADR-0033) |
| `Assignment` | which operator runs an agent, and the epoch that binding holds |
| `Certificate` | what an agent authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Service` | creates an agent for the calling operator, issues the certificate of one assigned to it or renews an agent's own, and answers an operator with the definitions naming it so it can claim one (ADR-0033) |
| `Issuer` | signs a certificate for a subject GRN and hands back the garam server root beside it |

garam mints an agent's GRN and never changes it, so which operator runs
the agent is a relation a reassignment moves rather than part of the
identity. That GRN must name an agent of the organization that owns it:
ADR-0014 rests tenancy on its organization segment, so the domain
refuses a pairing that disagrees rather than trusting its callers, and
the schema refuses it again where nothing routes around it. An
assignment stays inside that organization and names an operator
(ADR-0015), which the schema alone decides. A definition names one the
organization has registered as well, which the browser surface decides
against `internal/operator` and the schema decides again (ADR-0045);
this domain reads no record of its own and addresses an operator by GRN. Every assignment raises the
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
organization. A caller that is not an operator is refused, with one exception: an
agent asking for its own certificate is renewing over the connection
that certificate authenticated, which grants it nothing it does not
already hold (ADR-0040). An expired certificate has no such connection,
so the operator route is what recovers one.

`Store` answers `Service` and, org by org, the read `internal/api`
serves people: an organization's agents and the assignments holding
them, each a batch read rather than one per agent. An agent no operator
holds contributes no assignment, so the two lists are not one length.
Both aggregates are constructed through this package's exported
constructors, which is what lets a `Store` outside it answer with them
at all.

Signing happens through the `Issuer` seam the composition root
satisfies from `internal/ca`, so the two domains still cooperate
through `grn.GRN` alone and this one never holds key material. The
HTTP surfaces are `internal/machineapi` for the operator and
`internal/api` for the organization's members, which reads and never
writes — provisioning is an operator's, from outside garam. The queue a
message reaches an agent through is `internal/message`.

## `internal/ca`

The certificate authority an organization signs machine identities with
(ADR-0014).

| Type | Responsibility |
|------|----------------|
| `Authority` | an organization's CA: the certificate it signs with, and its private key in sealed form |
| `Certificate` | an issued client certificate, the private key it was issued with, the CA that signed it, and the serial that tells it from another certificate naming the same subject |
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
implementation is AES-256-GCM under a key-encryption key read from a
file through `internal/keyfile` (ADR-0015); the sealed bytes name the
key that opens
them, and open only in the organization's row they were written for, so
write access to the database moves no trust root. A subject's own key is
generated per certificate and returned once, never stored. `garam serve
machine` is the process that issues, and it reads the key-encryption
key before it serves: the process that owns the authorities refuses to
start without the key that opens them rather than failing at the first
certificate it is asked for. `garam issue-certificate` reads the same
key the same way and mints an operator's own first certificate — the
one bootstrap ADR-0015 leaves out of band. It refuses any subject that
is not an operator, and no serve path reaches it, so no serving process
mints an operator's first certificate, which is why the command is a
sibling of `serve` rather than a subcommand under it. That is as far as
the refusal and the command tree reach: `garam serve machine` mints
identities and issues certificates on the listener it serves.

That key is as availability-critical as the database — losing it loses
every organization's CA with no recovery short of new authorities and a
reissue for every certificate — and why it is held this way rather than
in a KMS is [ADR-0016](./adr/0016-ca-key-custody.md).

## `internal/machine`

Who is calling on the listener a machine identity authenticates on
(ADR-0014, ADR-0015).

| Type | Responsibility |
|------|----------------|
| `Identity` | the GRN a verified client certificate names, and the certificate's own serial and dates — which is what a caller asking to replace it is answered by (ADR-0046) |
| `Service` | recovers that GRN and verifies the certificate against the organization's authority |
| `TrustRoot` | supplies the authority a presented certificate has to chain to |
| `ServerCertificate` | supplies the certificate the listener presents, asked per handshake so a replaced one needs no restart (ADR-0040) |

An agent and an operator are the same kind of caller here and the
listener does not branch on which. What the certificate asserts is the
GRN in its SAN URI and nothing beside it: no user row, no session, no
membership, so neither foreign key on `users` is touched and no handler
taking a `*user.User` changes. The identity carries the certificate's
own serial and dates as well, which assert nothing and describe which
certificate authenticated — the one question a caller asking to replace
it is answered by (ADR-0046).

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
authority's certificate and never its key. `ServerCertificate` is the
other seam, satisfied from `internal/keyfile`, and what it answers with
is a different trust root: garam verifies an identity by its
organization's authority, and an identity verifies garam by the one
root every organization shares (ADR-0040).

`garam serve machine` is where the listener runs — its own process and
its own port (ADR-0017), terminating TLS itself with the pair it holds
as two paths and re-reads per handshake, the private key under the
owner-only rule `internal/keyfile` holds, and the root that signed that
pair beside them for every identity it issues a certificate to. It
terminates TLS itself because a forwarded client-certificate header is
trustworthy only where every deployment strips it at the edge
(ADR-0014). What it serves is `internal/machineapi`. It is not part of
the local Docker stack: its key material is per host and the container
would run as a user that does not own it.

## `internal/machineapi`

What `garam serve machine` serves, implementing the ServerInterface
generated from `api/machine.yaml` (ADR-0019).

| Type | Responsibility |
|------|----------------|
| `Server` | the machine listener's handlers, one for every operation `api/machine.yaml` declares |
| `Evaluator` | the seam the listener asks the policy engine through |
| `Groups` | the seam the listener reads a caller's group membership through |

A handler's caller is the identity `internal/machine` resolved from the
client certificate, read back with the comma-ok form and refused where
a request carries none — no session, no user row, no membership. What
an operator may reach is not decided here, and neither is whether the
certificate it presented may be replaced or whose queue a caller may act
on: `internal/agent`, `internal/operator` and `internal/message` decide
those, and this package parses what the path names, hands the caller
over, and renders the answer, turning an `errorx` kind into a status at
its own boundary as `internal/api` does at the browser-facing one. A poll that
found nothing is the one answer that is not a kind: it is an empty
success, so a caller loops on it rather than telling an error apart.

Whom a caller may send to is decided here, because it is a policy
question rather than a property of the queue: the answer comes from the
policy engine over `Evaluator`, and a boundary this package composes
may not reach past it to another process. The listener collects what
the caller's own GRN grants it — its own organization — and the engine
unions that with the policies attached to the caller (ADR-0024). A
refusal is answered before anything is stored, so what a policy refused
is not in the queue.

The question carries the groups the caller belongs to, because an
attachment names a group as readily as an identity (ADR-0008) and an
agent is a group member (ADR-0014). Which groups those are is org data,
so this surface reads them over `Groups` rather than holding a store:
the composition root satisfies it from `internal/org`, as it satisfies
the trust root `internal/machine` reads. Membership that cannot be read
refuses the message rather than deciding it without, because an unread
group and an empty one are otherwise the same answer.

A recipient of another organization is refused before the engine is
asked, unless the consent relation between the two organizations stands
(ADR-0035): the engine is never asked a cross-organization question,
because an unattached active policy of the recipient's organization
would otherwise admit every sender there is
([authz.md](./authz.md) §Policy attachment). The relation is the one
thing that may open the boundary, because it is no policy — both
organizations hold it and either withdraws it — and it is read from
`internal/message`, the domain that owns it. A post the relation does
not carry is not rejected: the refusal returns to the sender as an
ordinary inbound message (ADR-0013), written under the intended
recipient's identity because garam holds none of its own (ADR-0024).
The same-organization half of the precondition is the one
`internal/api` gets from org membership, on a listener that has no
membership to read.

A claim and a renewal answer with how many streams garam is holding for
the agent, because garam cannot dial an adapter and the poll it already
holds is the direction that exists (ADR-0022). The field is always
answered: zero is the ordinary answer, and a shape that changed with it
would make the agent nobody watches the exceptional one. A count that
cannot be read answers zero and is logged rather than returned — the live
view may not cost a message one of its delivery attempts, which is the
same reason ADR-0022 keeps watching off the subscription completion rests
on. A poll that found nothing carries no count: there is no turn to
forward the events of.

It reaches no certificate authority, and it imports neither the engine
nor `internal/org`: `GRPCEvaluator` is its own client, as
`internal/api` keeps its own. The one store it holds is the consent
relation's, which is the message domain's own data and arrives the way
the queue does. `internal/api` and this package never import each other
and never run in the same process (ADR-0017).

## `internal/message`

The durable queue a message reaches an agent through (ADR-0013).

| Type | Responsibility |
|------|----------------|
| `Message` | one message addressed to a recipient, under an identifier of the queue's own |
| `Claim` | one reader's hold on a message, and the token that fences it |
| `Consent` | one pair of organizations and the two halves of their cross-organization consent (ADR-0035); it stands while both halves do |
| `Config` | the delivery parameters: lease, ceiling, attempts, and how long a claim waits |
| `Service` | what a machine identity does to its own queue: post, claim, renew, complete |
| `ConsentStore` | the consent relation: an organization granting or withdrawing its own half, and the send path asking whether both halves stand |
| `ActionSend` | the action a policy names to decide what one identity may send to another |

Messages are their own boundary rather than part of `internal/agent`: a
recipient is addressed by GRN, so nothing here depends on the agent
aggregate, and a message has a lifecycle no agent's contains. A message
may cross an organization boundary only where a relation permits it —
one row per consenting pair, held by both organizations and withdrawable
by either (ADR-0035); the refusal reaches the sender as an ordinary
inbound message, never a rejection. Each half is the stamp its side's
last grant wrote: a row is created by the first grant, stands when the
second lands, and is deleted when neither half stands, so the table
holds live consent only. Both organizations of a pair must exist, which
the schema holds by reference rather than by a check a caller could
forget, and an organization that is deleted takes its halves with it.
Granting and withdrawing is the api server's surface (`internal/api`
under the org's path); this package owns the relation itself.

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
delivered one. Retention is for space, not for not-keeping
(ADR-0034): a completed message keeps that small row and drops its
body at completion, an unclaimed message expires after thirty days,
and a submission's body is bounded at 64 KiB — so the table's growth
is a rate a volume can be sized against. Neither the recipient nor the sender carries a foreign
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
message around it. Whom a sender may post to is not decided here — it
is a policy question, and `internal/machineapi` asks it (ADR-0024).
What this package owns of it is the action constant a policy names,
because it is the package that defines sending. A claim waits for a
message rather than answering
an empty queue at once, and it waits by re-reading the queue: a
message becomes ready when a lease runs out as well as when a sender
stores one, and a lease running out is a moment passing that nothing
announces. The interval is the delivery latency ADR-0013 accepted in
place of a push.

The HTTP surface is `internal/machineapi`, and the reader on the other
side of it is the adapter garam ships ([adapter.md](./adapter.md)).
The retention above is this package's own work rather than a schedule a
deployment carries: the sweep rides the enqueue, so a queue being
written to is a queue being swept (`internal/message/postgres.go`,
ADR-0034).

## `internal/watch`

Who is watching an agent work, and what is carried to them while they
are (ADR-0022).

| Type | Responsibility |
|------|----------------|
| `Event` | one turn event as an adapter forwarded it; garam carries it and never reads it |
| `Subscription` | one open watch: the stream its holder reads, and the lease that has it counted |
| `Service` | opens a watch, carries a forwarded event to the watchers of that agent, and reports how many there are |
| `Store` | the watches that are open |
| `Relay` | the announcement that carries an event to the process holding a stream |

Watching is its own boundary rather than part of `internal/message` or
`internal/agent`. A watch has a lifecycle neither contains — it begins
when a person opens an agent and ends when they leave, and nothing about
it survives that — and the two surfaces that need it, `internal/api` and
`internal/machineapi`, may not reach each other at all (ADR-0017), so it
is extracted rather than held by either.

Nothing accumulates for an agent nobody watches, which is what the
boundary exists to keep true: the service holds an entry per open
subscription and none per agent, the table holds a row per watch, and an
event announced while nobody watches reaches no one and is not kept. A
watcher sees from when it opened, and one that falls behind loses events
rather than holding up the rest — the platform these events come from
drops them for a slow subscriber already (ADR-0013), so the same answer
one hop later is not a new failure mode.

The two ends of a watch are in different processes — a browser opens the
stream on `garam serve api` and an adapter forwards to `garam serve
machine` — and Postgres is the crossing for both the events and the
count (ADR-0027). `Relay` is the event half, as `LISTEN`/`NOTIFY`: it
announces and writes nothing down, so an event crossing between processes
is no more a record than one that never left. An event too large for one
announcement is refused rather than shortened: a shortened turn would put
words in the agent's mouth, where a refused one is the gap this stream
already permits.

`Store` is the count half: a leased row per open watch (ADR-0027).
Postgres measures the lease, so no instance's clock decides when a watch
lapses, and the rows nobody closed are dropped where rows are written.
The holder of a stream carries its lease forward on the same tick it
proves the connection alive on, and a lease it cannot carry forward ends
the stream — a stream nothing forwards to any more shows a working agent
as a quiet one.

The HTTP surfaces are `internal/api` for the browser that watches and
`internal/machineapi` for the adapter that forwards. Which browser is
admitted is decided at the first, by a grant separate from reading an
agent and carried by no role, so a policy allows `agent:watch` or nobody
watches (ADR-0022); whose events a caller may forward, and whose watchers
it may count, is decided here by one comparison against the identity the
caller authenticated as.

## Store implementation status

| Domain | Interface | postgres | memory | other |
|--------|-----------|----------|--------|-------|
| org | yes (`store.go`) | yes (`postgres.go` + `postgres_<concept>.go`) | not planned | — |
| user | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.SessionStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.OAuthStateStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.PasswordCredentialStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authn.PasswordAttemptStore | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| authz (policies) | yes (`store.go`) | yes (`postgres.go` + `postgres_<concept>.go`) | not planned | — |
| operator.Store | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| operator.CertificateStore | yes (`store.go`) | yes (`postgres_certificate.go`) | not planned | — |
| agent | yes (`store.go`) | yes (`postgres.go` + `postgres_<concept>.go`) | not planned | — |
| ca | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| message | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| watch | yes (`store.go`) | yes (`postgres.go`) | not planned | — |

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
| secret | `internal/secret` | a reference to a secret an external store holds — never the value (ADR-0036) |