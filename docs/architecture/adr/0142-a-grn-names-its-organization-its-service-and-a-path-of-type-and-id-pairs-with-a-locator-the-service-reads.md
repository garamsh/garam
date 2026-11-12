# ADR 0142: A GRN names its organization, its service and a path of type and id pairs, with a locator the service reads

> Status: accepted; built
> Date: 2026-11-12

Drafted for #1766, which reserves this number and holds the owner's
rulings this records, decided on 2026-11-12 in the escalation on #1754
(comments `issuecomment-6800773587`, `issuecomment-6800845458` and
`issuecomment-6800955659`). It supersedes
[ADR-0001](./0001-grn-segments.md) in part (the five-segment format
and the `default` project segment) and
[ADR-0141](./0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
in part (the GRN forms of worked-example answers 4 and 5). It builds
on
[ADR-0137](./0137-tenant-isolation-is-membership-and-the-policy-engine-fails-closed-on-any-evaluation-error.md)
and
[ADR-0140](./0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md).
garam is read at `85e8eddd0606bc3622ee601d1f3cbc553ee8803f`; a claim
below that names no revision holds for it.

## Context

- **The format fits one service.** `docs/architecture/grn-model.md`
  §Format gives five colon segments, one flat catalogue of types, an
  identifier that is one slug, and no segment naming which service
  owns the resource. `lib/grn/grn.go` requires exactly five segments.
- **Other services are about to name resources.** ADR-0141 made garam
  an issuer whose token's `sub` is a GRN other services will store,
  and it gave the first outside service two planned names,
  `grn:<org>:default:repo:<id>` and
  `grn:<org>:default:service:codehub`. A repository holds files at a
  ref, which the five-segment identifier cannot name, and a resource
  inside another service's resource cannot nest.
- **The owner decided the grammar on #1754.** The rulings are the
  eight items below. garam is not operating (owner, 2026-11-12), so no
  deployment holds a stored GRN that a change of format would strand.
- **ADR-0137 makes the organization the tenant boundary.** Isolation
  is membership of the resource's organization, and `resource.org` is
  required on every resource type. A name that carried its
  organization at a fixed place, ahead of anything a service decides,
  lets that attribute be read from the name.

## Decision

**A GRN is an organization, then the one service that owns the
resource, then one or more `type/id` pairs naming the resource and the
resources it sits inside, and optionally a locator that service reads.
It is an RFC 3986 URI of scheme `grn`.**

```
grn:<org>/<service>/<type>/<id>[/<type>/<id>...][#<locator>]
```

1. **The organization is first.** Everything after it is the owning
   service's, so no service's grammar can name a resource outside the
   organization (ADR-0137), and a service an organization defines
   later resolves inside it. `root` stays the reserved organization
   for what no organization owns, and may not be a real
   organization's identifier (ADR-0001, unchanged).
   - **An organization is owned by `root`.** An `org` entity is
     `grn:root/iam/org/<slug>`. Its `resource.org` is itself
     (ADR-0137), so for that one type the first segment is `root`
     and is not `resource.org`.
2. **The service segment is the service's one name.** A service
   that verifies tokens or calls the policy service uses that name as
   its token audience (ADR-0141 §5) and as its name in the policy
   service's caller list (ADR-0140 §1): `policy`, `codehub` and
   `cowork` are such names. The types the platform owns sit under
   `iam`, the owner's ruling of 2026-11-12: the name says what the
   service is, and sharing it with another vendor's product is
   accepted. The policy service's caller list and the feed's `sub`
   name that caller `platform` today (ADR-0140); `platform` is the
   caller that owns the `iam` service, and whether that caller name
   follows `iam` is left open. A type name and a service name may be
   equal, as `policy` is: position decides which one a segment is.
3. **After the service come `type/id` pairs.** A resource's parent is
   its name with the last pair removed. A name of one pair has no
   parent in the name: its owner is its organization. A resource
   scoped to a project begins with the pair `project/<id>`, and the
   `default` placeholder of ADR-0001 goes: an organization-level
   resource has no project pair, and `root` as an organization is the
   one reserved value.
   - **`project` as a first pair is shared by every service.** It
     names the project of that id and no entity of the service
     holding it. The platform's own project entity is
     `grn:<org>/iam/project/<id>`.
4. **Each segment before `#` keeps the character rule of
   `grn-model.md` §identifier:** lowercase letters, digits and `-`,
   at most 64 characters, and the rule holds for the service and the
   `type` segments as it does for an `id`. **Everything before `#` is
   at most 512 characters.** The bound rests on what holds a GRN
   whole: the platform keys `text` columns by it and keys a table by
   two of them
   (`platform/migrations/000016_password_credentials.up.sql`,
   `platform/migrations/000028_operator_delegations.up.sql`), and a
   PostgreSQL btree index refuses an entry past about a third of a
   page, which the PostgreSQL 17 documentation puts near 2.7 kB at
   the default page size. Two names of 512 characters stay well
   inside that, and the five-segment form's longest, four segments of
   64, is shorter than 512, so no name valid today is refused for
   length. The limit was not measured against a running database in
   the pull request that wrote it.
5. **`#<locator>` names a place inside a resource that the owning
   service reads.** A ref and a file path in a repository is the
   example:
   `grn:acme/codehub/repo/api-server#refs/heads/main:cmd/main.go`
   names a file at a ref, in a form codehub decides. A locator is not
   an entity. It is never fed to the policy service and never an
   entity identifier there, so two names equal before `#` are one
   entity. A policy reaches it as a context value of the request the
   owning service asks, and which key carries it is the
   implementation issue's. A GRN is an RFC 3986 URI of scheme `grn`,
   the part before `#` its path and the locator its fragment, so
   escaping is that RFC's: a `#` inside a locator is `%23`, and a
   character outside what a fragment allows is percent-encoded. No
   locator is bounded here, since no platform table keys on one.
6. **An identifier is immutable and is not reused after its resource
   is deleted.** It holds for every `id` in the path. A rename is a
   new resource, and a deleted name names nothing afterwards, so a
   name stored by another service or in an audit record never
   resolves to a different resource than it did. What enforces the
   rule is a later issue; this decision states it.
7. **There is one grammar.** Once the implementation lands, the
   five-segment form is refused and nothing parses both. The two are
   told apart without ambiguity: before `#` a name of this grammar
   holds one colon, after `grn`, and a five-segment name holds four.
   garam is not operating (owner, 2026-11-12), which is the reason no
   compatibility period is owed: no deployment holds a name that the
   refusal would strand.
8. **Inside one organization a name may be written without
   `grn:<org>/`, where the organization is already fixed.** That is
   one place: a route of `platform/api/openapi.yaml` whose path
   carries `/orgs/{org}`, and the names in a request to it, which
   then read `<service>/<type>/<id>[/<type>/<id>...]`. The platform
   expands the name before it stores, compares, signs or sends it, so
   every name in a table, a token, the policy feed or an audit record
   is the full form. A name that leaves the process, or that crosses
   an organization, is never short.

### The five-segment names, read through this grammar

An earlier document's name reads as follows, and no earlier ADR's
body changes.

- **`grn:<org>:default:<type>:<id>`** is
  `grn:<org>/<service>/<type>/<id>`, `<service>` being the service
  that owns the type: `iam` for `user`, `agent`, `org`, `group`,
  `project`, `operator`, `supervisor`, `policy` and `service`.
- **`grn:<org>:<project>:<type>:<id>`** is
  `grn:<org>/<service>/project/<project>/<type>/<id>`.
- **`grn:root:default:org:<slug>`** is `grn:root/iam/org/<slug>`.
- **ADR-0141's `grn:<org>:default:repo:<id>`** is
  `grn:<org>/codehub/repo/<id>`, and its
  `grn:<org>:default:service:codehub` is
  `grn:<org>/iam/service/codehub`: a service principal is a
  platform record, as cowork's is (ADR-0129). A repository owned by a
  project, which ADR-0141 left to a later step, takes the form
  `grn:<org>/codehub/project/<project>/repo/<id>`; this decision
  settles the form and not whether that step is taken.

## Consequences

- **A resource's organization is read from the first segment of every
  name but an organization's own,** ahead of any service's code,
  which is what ADR-0137's required `resource.org` needs.
- **A name nests without a new kind of segment.** A file in a
  repository in a project is a path and a locator, not a sixth and a
  seventh colon segment.
- **A service names its own types.** The catalogue in `grn-model.md`
  is grouped by owning service, and a service adds a type without the
  platform's catalogue holding it before it is built.
- **Every stored GRN, every `grn:` pattern in a policy and every
  action named for a type changes with the implementation.**
  `lib/grn/grn.go`, the migrations, the Cedar entity identifiers and
  every five-segment example in another responsibility document move
  in the implementation issue, and until it lands the code and those
  documents are in the form this ADR retires.
- **A policy `Resource` pattern keeps its organization prefix.**
  `grn:acme:*` in `authz.md` §Policy document scopes to an
  organization by prefix, and the grammar keeps the organization
  first so that a prefix is still one. What a wildcard may cross is
  decided with the implementation.
- **Immutability and non-reuse cost an identifier a deletion keeps.**
  A deleted slug is not available again, and something has to
  remember it.
- **What this leaves open.** The per-service manifest and the served
  registry of types are a later ADR, and whether a token carries who
  an agent acts for is not decided here. For the implementation issue
  or a later ADR: the context key a locator arrives under, how a
  wildcard in a policy pattern treats `/` and `#`, how an action's
  `<service_type>:<verb>` name reads once types belong to services,
  how a Cedar entity identifier is derived from a nested name, which
  service owns `doc` and `secret` (both planned), and what mechanism
  keeps an identifier from being reused. Whether an organization is
  better named inside itself than by `root` is also open: item 1
  keeps ADR-0001's reading. So is whether the policy service's caller
  name `platform` follows the service name `iam`.

## Signs the decision was wrong

- A service needs a resource that is shared across organizations, and
  the organization segment is bent to hold a placeholder, which would
  show the organization does not belong first.
- A locator is wanted as an entity the policy service decides on,
  which would show that the place inside a resource needs its own
  type and id.
- A real name reaches 512 characters, which would show the bound
  rests on the wrong measure.
- A service reads the platform's `project/<id>` pair as its own
  entity, which would show that a shared first pair is a second
  catalogue.
- A short form is stored or sent, which would show item 8's boundary
  does not hold.

## Alternatives rejected

- **The five-segment form with an owner column,** as
  `grn:<org>:<project>:<service>:<type>:<id>`. It names the service
  and still cannot nest: the identifier stays one slug, the project
  stays a fixed segment that every resource carries and most set to
  `default`, and a locator would need a segment of its own. It adds
  one colon to the form that was already the obstacle.
- **A colon form with a service segment,** as
  `grn:<org>:<service>:<type>:<id>`. A colon separates a fixed number
  of segments, and a name that nests has a variable number, so the
  parser has to know each service's depth to know where a name ends.
  `Parse` already splits on every colon (`grn-model.md` §identifier),
  and a locator holding a colon, as a ref and a path may, would
  re-associate the rest.
- **The service first,** as `grn:<service>/<org>/...`. It lets a
  service's grammar name a resource before any organization, which
  item 1 refuses, and it loses the prefix that scopes an
  organization: `grn:acme:*` in `authz.md` §Policy document selects
  an organization's resources by what comes first, and with the
  service first the same scope is a wildcard in the middle of every
  pattern.
