# Domains

garam's domains, named by the subject matter each owns, and what else under `internal/` is not one.

## Contents
- What a domain is here
- The domains
- The shape a domain takes
- Sub-responsibilities in a boundary of their own
- Delivery surfaces
- Shared code below the domains
- Store implementation status
- Known gap
- Open questions
- Future domains

## What a domain is here

A domain is a boundary named for the subject matter it owns and never
for a technical role ([structure.md](./structure.md) §Boundaries and
dependency direction). garam has nine, and each has a responsibility
document of its own named for the subject: the Go package implementing
a domain is named in that document and is not the domain's name
([ADR-0048](./adr/0048-garams-domains-are-named-by-subject-matter.md)).

Three kinds of folder under `internal/` are not domains, and each has a
section of its own below. A **sub-responsibility** belongs to a
domain's subject and is held in a boundary of its own for a reason
recorded there. A **delivery surface** composes several domains, which
is what stops it being one ([structure.md](./structure.md) §The shape).
**Shared code** sits below the domains that need it.

## The domains

Each is a responsibility document of its own, named for its subject;
the Go package implementing it is in parentheses.

- [organizations.md](./organizations.md) — the tenant hierarchy
  (`internal/org`)
- [users.md](./users.md) — human identity (`internal/user`)
- [sign-in.md](./sign-in.md) — how a person proves who they are, and
  the session that carries the proof afterwards (`internal/authn`)
- [authz.md](./authz.md) — the rules that decide what a subject may do,
  and the engine that decides (`internal/authz`)
- [certificate-authorities.md](./certificate-authorities.md) — the
  certificate authority an organization signs machine identities with
  (`internal/ca`)
- [operators.md](./operators.md) — what an organization records about
  an operator, where that operator stands in obtaining a certificate,
  and what it authenticates with (`internal/operator`)
- [agents.md](./agents.md) — autonomous identity, provisioned outside
  garam (`internal/agent`)
- [machine-identity.md](./machine-identity.md) — who is calling on the
  listener a machine identity authenticates on (`internal/machine`)
- [messaging.md](./messaging.md) — the durable queue a message reaches
  an agent through, and the consent that lets one cross an organization
  boundary (`internal/message`)

## The shape a domain takes

Each domain's package contains:

- `<domain>.go` — aggregate types and sentinels.
- `store.go` — `Store` interface.
- `service.go` — the `Service` contract, where the domain has
  behaviour to coordinate; a domain that is only types and persistence
  has none.
- `queries.sql` — the domain's SQL, hand-written. The only place the
  domain's own queries are written: `migrations/` holds the schema.
- `postgres.go` — hand-written `Store` implementation, calling `db/`.
- `db/` — sqlc output from `queries.sql`. Generated, never edited.
- `*_test.go` — tests.
- `mocks/` — mockery v3 output, generated from the interfaces
  `.mockery.yaml` names.

A store splits into `postgres_<concept>.go` files beside `postgres.go`,
inside the same package, when one Postgres type carries the methods of
more than one aggregate (`org`, `agent`, `authz`) or of more than one
`Store` interface (`operator`, `message`). What splits is a receiver's
methods and not a package's stores: `internal/authn` implements four
`Store` interfaces on four Postgres types and keeps them in one
`postgres.go`. `ls internal/*/postgres_*.go` names the five packages
that split and no others. It stays one package because the aggregates'
fields are unexported and no exported constructor restores a stored
row's state or timestamps, so whatever rehydrates one shares its
package. That is a position the project holds and not a state it
drifted into: no aggregate gains an exported restore path in order to
satisfy a layout rule, so a store beside its aggregate is not a defect,
and `docs/convention/stack-go.md` §1's `internal/<domain>/<name>/`
layout stays out of reach for the eight domains whose store files write
their own aggregates' unexported fields
([ADR-0066](./adr/0066-no-aggregate-gains-an-exported-restore-path-to-satisfy-a-layout-rule.md)).
`internal/watch` is the ninth and is not one of them.

A domain that persists nothing has none of the storage files above:
`internal/machine` is its types, its service, the listener it builds,
and the two seams it reads through — a trust root for the caller, a
certificate for itself.

`queries.sql` and `db/` are a source and its output, not two copies. Every
generated file says so in its first line, and `make check-gen` fails on any
drift between them.

A change announcement reads no row, so it is not in `queries.sql`, and
the two domains that use one do not raise it the same way.
`internal/watch` issues both halves from Go. A policy change is
announced by triggers the schema carries in `migrations/`, and
`internal/authz` issues only the `LISTEN`.

## Sub-responsibilities in a boundary of their own

Two folders under `internal/` hold a responsibility that belongs to a
domain's subject rather than to one of its own. Neither is a domain,
and each is a boundary for a reason recorded below — `structure.md`
§Boundaries and dependency direction makes a boundary a domain, a
feature or a bounded context, so being a feature of another subject and
owning a folder are not in tension.

### Watching an agent work

**Both ends of this are unbuilt.** garam's half is built — the stream a
browser reads, the watcher count on the poll, and the fan-out between
the two listeners — and nothing reaches either end of it: no route
under `web/src/app/console/` opens the stream, and nothing under
`internal/delivery/` forwards an event ([adapter.md](./adapter.md)
§Watching). [overview.md](./overview.md) §Out of scope (today) records
it.

Who is watching an agent work, and what is carried to them while they
are (ADR-0022). Implemented by `internal/watch`. The subject is the
agent; the boundary is its own because a watch has a lifecycle neither
the agents domain nor the messaging one contains — it begins when a
person opens an agent and ends when they leave, and nothing about it
survives that — and because the two surfaces that need it may not reach
each other at all (ADR-0017).

| Type | Responsibility |
|------|----------------|
| `Event` | one turn event as an adapter forwarded it; garam carries it and never reads it |
| `Subscription` | one open watch: the stream its holder reads, and the lease that has it counted |
| `Service` | opens a watch, carries a forwarded event to the watchers of that agent, and reports how many there are |
| `Store` | the watches that are open |
| `Relay` | the announcement that carries an event to the process holding a stream |

Nothing accumulates for an agent nobody watches, which is what the
boundary exists to keep true: the service holds an entry per open
subscription and none per agent, the table holds a row per watch, and an
event announced while nobody watches reaches no one and is not kept. A
watcher sees from when it opened, and one that falls behind loses events
rather than holding up the rest — the platform these events come from
drops them for a slow subscriber already (ADR-0013, `gagent@40ef7ac`),
so the same answer one hop later is not a new failure mode.

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

Which browser is admitted is decided at the browser-facing listener, by
a grant separate from reading an agent and carried by no role, so a
policy allows `agent:watch` or nobody watches (ADR-0022); whose events a
caller may forward, and whose watchers it may count, is decided here by
one comparison against the identity the caller authenticated as.

### The adapter's delivery loop

Claiming a message for one agent, posting it into that agent's gateway,
and completing the claim the turn that succeeded named (ADR-0018).
Implemented by `internal/delivery`. The subject is message delivery;
the boundary is its own because it runs outside garam, beside the
agent, as a client of the machine listener rather than a part of it —
it imports no domain and no delivery surface, reading that listener's
specification through the models generated from it.

What it must do is [adapter.md](./adapter.md), which is the
responsibility document for it; nothing about the loop is restated
here.

## Delivery surfaces

Three folders under `internal/` compose several boundaries, which is
what stops each being one ([structure.md](./structure.md) §The shape).
They are named here as what they are; what each serves and what admits
a caller to it is [api-surface.md](./api-surface.md).

- `internal/api` — the browser-facing listener's handlers, implementing
  the ServerInterface generated from `api/openapi.yaml` (ADR-0004). It
  composes the organizations, users, sign-in, agents, operators and
  messaging domains and the watch boundary.
- `internal/machineapi` — the machine listener's handlers, implementing
  the ServerInterface generated from `api/machine.yaml` (ADR-0019). It
  composes the agents, operators, messaging and machine-identity
  domains and the watch boundary.
- `internal/cli` — the cobra commands and the composition root behind
  them. It is what satisfies every seam a domain declares — the
  `Issuer` the agents and operators domains reach `internal/ca`
  through, the `TrustRoot` and `ServerCertificate` the machine-identity
  domain reads, and the `Groups` the machine listener reads org
  membership through.

A surface parses what a path names, hands the caller over to the domain
that decides, and renders the answer — turning an `errorx` kind into a
status at its own boundary. What an operator may reach, whose queue a
caller may act on and whether a presented certificate may be replaced
are none of its decisions.

The two listeners reach their domains differently, and what each holds
shows it. The machine listener holds `Service` contracts and exactly
one store — the consent relation's, which is the messaging domain's own
data reached the way the queue is. The browser-facing one holds domain
stores directly, beside the three seams it declares: the two it asks
the policy engine through, and `AuthorityReader`.

Neither listener imports the other and they never run in one process
(ADR-0017). Neither imports the policy engine: each holds a gRPC client
of its own, and the conversions onto the wire contract are declared
once in `internal/wire`. Neither imports `internal/ca` either, and
neither issues or opens anything a certificate authority holds. What
the browser-facing one reads of an authority is whether an
organization has one, through `AuthorityReader`, which the composition
root satisfies as it satisfies the `Issuer` the agents and operators
domains sign through. Presence is the whole of that seam: the question
is asked to say what deleting an organization would destroy, it needs
no key to answer, and `garam serve api` still reads no key-encryption
key.

Two decisions live at a surface rather than in a domain, because each
is a question about the caller rather than about the subject. Whom a
machine caller may send to is decided at the machine listener: for a
recipient of the caller's own organization the answer comes from the
policy engine, which the listener unions with what the caller's own GRN
grants it (ADR-0024), and the question carries the caller's groups
because an attachment names a group as readily as an identity (ADR-0008,
ADR-0014). Membership that cannot be read refuses the message rather
than deciding it without, because an unread group and an empty one are
otherwise the same answer. A recipient of another organization is
refused before the engine is asked, unless the consent relation stands
(ADR-0035): the engine is never asked a cross-organization question,
because an unattached active policy of the recipient's organization
would otherwise admit every sender there is
([authz.md](./authz.md) §Policy attachment). A post the relation does
not carry is not rejected — the refusal returns to the sender as an
ordinary inbound message (ADR-0013), written under the intended
recipient's identity because garam holds none of its own (ADR-0024).

A claim and a renewal answer with how many streams garam is holding for
the agent, because garam cannot dial an adapter and the poll it already
holds is the direction that exists (ADR-0022). The field is always
answered: zero is the ordinary answer, and a shape that changed with it
would make the agent nobody watches the exceptional one. A count that
cannot be read answers zero and is logged rather than returned — the live
view may not cost a message one of its delivery attempts, which is the
same reason ADR-0022 keeps watching off the subscription completion rests
on. A poll that found nothing carries no count: there is no turn to
forward the events of, and the answer is an empty success rather than a
kind, so a caller loops on it rather than telling an error apart.

## Shared code below the domains

A concern three or more boundaries share is extracted below them
([structure.md](./structure.md) §When a new boundary is earned). The
middle column below is a count of domains, and #783 records that
column being counted four times and answering with four different
numbers for want of saying what it counts, so the rule is here:

- **Only a domain counts.** The nine sections above are the whole
  list. A delivery surface, a sub-responsibility held in a boundary of
  its own and another shared package import these packages too, and
  none of those importers is a domain.
- **A domain's generated subpackages are not its code.**
  `internal/<domain>/db/` and `internal/<domain>/mocks/` are generator
  output, which `docs/convention/README.md` §Scope places outside what
  the project writes, so an import only one of them carries is not the
  domain's. Any other subpackage is inside the folder the boundary
  names and is that domain, counted once with the rest.
- **A test import is not an import here.** A test import is not a
  dependency of the domain's code, and counting one is what once made
  `internal/keyfile` look like two domains. `go list`'s `.Imports`
  holds what is not a test and `.TestImports` holds the rest, so the
  query below is already scoped to it.

The query names every importer of one shared package; the rules above
say which of those importers the column counts.

```
pkg=github.com/garamsh/garam/internal/pgxerr
go list -f '{{$i:=.ImportPath}}{{range .Imports}}{{if eq . "'"$pkg"'"}}{{$i}}{{end}}{{end}}' ./... |
  sed -n 's|.*/garam/||p' | sort
```

| Package | Domains importing it | Standing |
|---|---|---|
| `internal/errorx` | 9 — every domain | earns its place |
| `internal/grn` | 9 — every domain | earns its place |
| `internal/pgxerr` | 8 — `org` `user` `authz` `ca` `agent` `operator` `authn` `message` | earns its place |
| `internal/listfilter` | 2 — `org` `agent` | under the threshold; placed by ADR-0052 and not by the count |
| `internal/wire` | 1 — `authz` | permitted by the wire-format carve-out, not by the count |
| `internal/keyfile` | 1 — `ca` | under the threshold; belongs with the domain that owns it |
| `internal/health` | 0 | under the threshold; belongs with the surfaces that mount it |

`internal/grn` holds the `GRN` value type with `New`, `Parse`,
`Validate`, `String` and `IsZero`, and depends on `errorx` only.

`internal/errorx` holds the `Kind` enum aligned with gRPC status codes,
the `Error` interface, and `New` / `Wrap` / `IsKind` / `KindOf`. It has
no dependency of its own.

`internal/pgxerr` maps a postgres driver error to an `errorx` error by
reading the sql state the driver carries, and labels it with the
operation the caller names rather than with a store's name. It is a
package of its own rather than part of `errorx` because `errorx` is
transport-agnostic by design, so the same errors flow over any surface,
and a pgx dependency there would end that. Which sqlstate becomes which
kind is the package's own doc comment; it is code, and a copy here would
be a second thing to keep true. Every postgres-backed store answers
through it, which is eight domains: the five that each held their own
copy — `org`, `authz`, `ca`, `user`, `agent` — `operator`, which never
had one, and the sign-in and messaging domains, whose stores read the
driver's error inline until they were routed through it too. Machine
identity, the ninth domain, persists nothing and has no store. The watch
boundary's store answers through it as well and is not a domain, which
is why the query above names nine importers where the column counts
eight.

`internal/listfilter` holds `Filter`, an offset and a limit where a
limit of 0 means no limit, and `Parse`, which builds one from what a
request carries and refuses a page the paging columns cannot hold. It
depends on `errorx` only.

Two domains page with it, which is under the threshold, so the count
does not place it where it places `errorx`, `grn` and `pgxerr`. It
stays below the domains because paging is the subject matter of
neither domain that pages with it, and a concern is owned by the
boundary whose subject it is rather than by the one it was extracted
from
([ADR-0052](./adr/0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md)).
`111bdd5` (#402) extracted the type out of `internal/org`, where it
was `org.ListFilter`, when the agents domain gained pagination; that
is where it was born and, under the decision above, not what owns
it.

`internal/wire` holds the conversions onto the wire formats the request
surfaces share: the statement `Effect` and its authz-proto mapping, and
`errorx` kinds rendered as HTTP statuses and read back from gRPC codes.
One domain imports it, so the threshold is not what places it; the
carve-out is. Its whole exported surface is a wire type and conversions
in both directions, and nothing in it chooses a behaviour, which is
what the carve-out names.

`internal/keyfile` holds the one rule key material is read under
(ADR-0015): a file only the reading process's owner can reach —
owner-only permission bits and an owner equal to the process's UID,
both read from the open descriptor so no window sits between the check
and the read. `Pair` is a certificate and its key held as the two paths
they arrive at rather than as what they held at startup, so every read
answers with the files as they are now and a replaced pair is presented
on the next handshake without a restart (ADR-0040). The key goes
through the rule above on every read; the certificate beside it is
public. It depends on `errorx` only. One domain reads it — the
certificate authorities domain, for the key-encryption key — and the
composition root reads the machine listener's pair and the adapter's
through it.

`internal/health` holds what liveness and readiness mean and the two
probes every serve command mounts (ADR-0021). No domain imports it; the
composition root does.

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
| message.Store | yes (`store.go`) | yes (`postgres.go`) | not planned | — |
| message.ConsentStore | yes (`consent.go`) | yes (`postgres_consent.go`) | not planned | — |
| watch | yes (`store.go`) | yes (`postgres.go`) | not planned | — |

The rows are keyed by the Go package, because a store is named for the
package that declares its interface rather than for the subject above
it.

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

## Open questions

- **Whether the organizations domain holds one responsibility or
  several.** `#608` counts six concepts in `internal/org` —
  `Organization`, `Group`, `Project`, `Membership`, `Invitation` and
  `Role` — each with its own `postgres_<concept>.go`, and the
  browser-facing surface split the same way. Naming the domain settles
  what its subject is and not how many subjects there are; nothing
  recorded here divides it, and until something does, this binds
  nothing.
- **What owns a concern once it is extracted below the domains.**
  [structure.md](./structure.md) §Open questions holds this, and
  `internal/keyfile` and `internal/health` are the two packages waiting
  on it: each is under the threshold that would place it below the
  domains, and no decision here says which boundary should own it
  instead.
  [ADR-0052](./adr/0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md)
  decides what owning is and not what holds a concern once it is
  extracted, so it gives these two the test to be read against rather
  than an answer.

## Future domains

| Domain | Location (planned) | Responsibility |
|--------|---------------------|----------------|
| workflow | `internal/workflow` | workflow definition and execution |
| doc | `internal/doc` | permissioned document |
| audit | `internal/audit` | decision and change events |
| token | `internal/token` | API token exchange (browser sessions live in `internal/authn`) |
| secret | `internal/secret` | a reference to a secret an external store holds — never the value (ADR-0036) |
