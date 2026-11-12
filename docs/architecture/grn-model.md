# GRN Model

Garam Resource Names — one identifier for every resource in garam.
Implementation:
[`lib/grn/grn.go`](../../lib/grn/grn.go), whose doc
comments are authoritative for constructing, rendering,
parsing and validating a GRN, and for `GRN` being a value type.

The grammar below is
[ADR-0142](./adr/0142-a-grn-names-its-organization-its-service-and-a-path-of-type-and-id-pairs-with-a-locator-the-service-reads.md)'s.
`lib/grn/grn.go` parses and renders it, holds the one map from a type
to its owning service, and refuses the five-segment form of
[ADR-0001](./adr/0001-grn-segments.md).

## Contents
- Format
- Ownership
- Types by owning service
- identifier
- Locator
- Earlier names
- Open questions

## Format

```
grn:<org>/<service>/<type>/<id>[/<type>/<id>...][#<locator>]
```

A GRN is an RFC 3986 URI of scheme `grn`: the part before `#` is its
path and the locator is its fragment. Anything that does not start
with `grn:` (e.g. `arn:...`) is not a GRN. The `grn` prefix already
names the vendor (Garam); no segment repeats it.

| Part | Meaning | Constraint |
|------|---------|------------|
| `org` | owning organization | slug; `root` is reserved |
| `service` | the one service that owns the resource | slug; the service's one name |
| `type` | resource kind within that service | slug, from the service's types below |
| `id` | unique id within the type | slug, see §identifier |
| `locator` | a place inside the resource, read by the service | see §Locator |

One grammar: the five-segment form is refused and nothing parses both.
A name of this grammar holds one colon before `#`, a five-segment name
four.

A GRN holds `/`, so one written into a single URL path segment is
percent-encoded as RFC 3986 requires, its `/` as `%2F`. A caller that
builds such a path escapes the name, and a server matches the escaped
path, so the segment arrives as the whole name.

Examples:

| Case | Name |
|------|------|
| a platform type | `grn:acme/iam/user/alice` |
| an organization | `grn:root/iam/org/acme` |
| a project-scoped resource | `grn:acme/docs/project/frontend/doc/readme` |
| a nested resource | `grn:acme/cowork/project/frontend/channel/general` |
| a locator | `grn:acme/cowork/page/home#section-2` |

## Ownership

The organization is first, and everything after it is the owning
service's, so no service's grammar can name a resource outside the
organization. Tenancy is structural, not a flag.

- **The service segment is the service's one name:** its token
  audience and its name in the policy service's caller list where it
  has them. The platform's types sit under `iam`. The policy service's
  caller list names the platform `platform` (ADR-0140), which owns the
  `iam` service.
- **A resource's parent is its name with the last pair removed.** A
  name of one pair has no parent in the name; its owner is its
  organization. Where only the first `project/<id>` pair is left, that
  pair names the platform's project and no entity of the service
  holding it, so the parent is the project's own name,
  `grn:<org>/iam/project/<id>`: the parent of
  `grn:acme/cowork/project/web/channel/general` is
  `grn:acme/iam/project/web`. `GRN.Parent()` answers it, and every
  parent it answers parses.
- **A project-scoped resource begins with `project/<id>`** after the
  service. The `default` placeholder of the earlier form is gone: an
  organization-level resource has no project pair. A first
  `project/<id>` pair names the project of that id, in any service;
  the platform's own entity is `grn:<org>/iam/project/<id>`.
- **`root` owns what no organization owns.** Organizations are
  `grn:root/iam/org/<slug>`, and `root` may not be a real
  organization's identifier. An `org` entity's `resource.org` is
  itself (ADR-0137), so for that type the first segment is `root` and
  is not `resource.org`.
- **A short form** drops `grn:<org>/` in one place: the route of an
  operation whose path carries `/orgs/{org}`, and the names in a
  request to it. The name is expanded before it is stored, compared,
  signed or sent, so every stored or transmitted name is the full
  form.

## Types by owning service

This table is the catalogue. Adding or renaming a type changes it here,
and nowhere else. The owning service is the `service` segment of the
type's names.

| Service | Type | Domain | Example |
|---------|------|--------|---------|
| `iam` | `user` | person identity | `grn:acme/iam/user/alice` |
| `iam` | `agent` | autonomous actor | `grn:acme/iam/agent/bot-1` |
| `iam` | `org` | organization, owned by `root` | `grn:root/iam/org/acme` |
| `iam` | `group` | user/agent bundle | `grn:acme/iam/group/eng` |
| `iam` | `project` | unit of work | `grn:acme/iam/project/frontend` |
| `iam` | `operator` | provisions agents from outside (ADR-0012); registered by its organization, which supplies the identifier (ADR-0045) | `grn:acme/iam/operator/field-one` |
| `iam` | `supervisor` | runs one agent on a standalone host; garam mints the identifier and registers it for that agent on an administrator's approval (ADR-0082) | `grn:acme/iam/supervisor/0a1b2c3d4e5f6071` |
| `iam` | `policy` | stored policy document | `grn:acme/iam/policy/a1b2c3d4` |
| `iam` | `service` | a first-party service acting in one organization, one per organization and service, the identifier the service's name (planned, ADR-0129; codehub's is planned, ADR-0141) | `grn:acme/iam/service/cowork` |
| `codehub` | `repo` | a repository codehub stores, owned by an organization, or by a project as `project/<id>/repo/<id>` once that step is taken (planned, ADR-0141, ADR-0149 proposes its manifest; in no catalogue until codehub declares one) | `grn:acme/codehub/repo/api-server` |
| not decided | `doc` | permissioned document (planned) | `grn:acme/docs/project/frontend/doc/readme` |
| not decided | `secret` | reference to a value an external store holds, never the value (planned, ADR-0036) | `grn:acme/iam/secret/model-api-key` |

The owner of `doc` and `secret` is not decided, so the service names
in their examples are illustrative.

**Built, and not built.**
[ADR-0143](./adr/0143-a-service-is-declared-by-one-manifest-that-is-its-cedar-schema-and-garam-serves-what-the-manifests-say.md)
makes a service's Cedar schema file its manifest, and a type's GRN
type its `@grn` annotation there. Built: `policy/schema/iam.cedarschema`
and `cowork.cedarschema` are the manifests, and the catalogue in
`lib/grn` is `catalogue_gen.go`, written by `make -C policy gen` from
them with each type's qualified entity type, its parents and each
action with the types it applies to, and held to them by `make -C
policy check-gen`. A type is therefore added in a manifest, and this
table describes what they declare. Not built: codehub declares no
manifest, so `repo` is in no catalogue and `lib/grn` refuses a name of
`codehub` until it does (#1760). ADR-0149 proposes it, unbuilt.
Built (#1788): `GET /registry` answers the services with their types
and actions, and `GET /registry/grn?grn=<name>` answers a name's
service, type, entity type, parent, locator and the actions that apply
to its type, both from the catalogue alone and reading no entity.

## identifier

Lowercase letters, digits, `-`. Length ≤ 64. Slug over UUID. Never
expose a transient ID where a stable one is expected.

An identifier is immutable and is not reused after its resource is
deleted, so a name another service or an audit record stored never
resolves to a different resource than it did. What enforces that rule
is not yet decided.

One prefix is reserved. A `user` identifier beginning `local-` names an
account garam minted from a username and a password, and no sign-in
provider's subject may claim one: a provider that asserts such a
subject is refused rather than handed the account it names
([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
The prefix is what keeps the two kinds of account in disjoint spaces,
so neither can ever resolve to the other's row.

The same character rule binds every segment before `#`, the service
and the `type` segments included. Everything before `#` is at most 512
characters: the platform keys `text` columns by a GRN and a table by
two, and a PostgreSQL btree entry is bounded near 2.7 kB at the
default page size (ADR-0142 item 4 holds the reasoning and says what
was not measured). A segment that can hold a `/` or a colon would
re-associate the rest of the name on the way back in, so the
character rule is what keeps a GRN meaning one thing; the catalogue
constrains which types exist.

## Locator

`#<locator>` names a place inside a resource that the owning service
reads, such as a ref and a file path in a repository. It is not an
entity: it is never fed to the policy service and never an entity
identifier there, and two names equal before `#` are one entity. A
policy reaches it as a context value. Escaping is RFC 3986's: a `#`
inside a locator is `%23`. No platform table keys on a locator, and
none is bounded here.

`lib/grn/grn.go` parses and renders a locator and nothing else reads
one yet. ADR-0149 proposes, unbuilt, that codehub sends a path as the
context key `path` and no locator, so a repository's GRN carries none.

## Earlier names

ADR-0001's form had five colon-separated segments, with `project`
`default` when the resource was organization-level. No code writes or
accepts it. A stored row that still holds one keeps its text: no
migration rewrites a name, and the stores, the policy service and the
evaluator refuse to parse it. The earlier names read in the new
grammar as follows.

| Earlier | Decided |
|---------|---------|
| `grn:<org>:default:<type>:<id>` | `grn:<org>/<service>/<type>/<id>` |
| `grn:<org>:<project>:<type>:<id>` | `grn:<org>/<service>/project/<project>/<type>/<id>` |
| `grn:root:default:org:<slug>` | `grn:root/iam/org/<slug>` |

## Open questions

The per-service manifest and the served registry of types are built
(ADR-0143, #1788), and ADR-0143 also settles how an action
reads once types belong to services: `<service>:<type>.<verb>`
(ADR-0145, which replaced ADR-0143's hyphen), so an action's name does
change. Left open there: a service an organization defines, and a
served registry of locator shapes. Settled for the
implementation: the policy service's caller name and the feed's `sub`
stay `platform`; a Cedar entity identifier is the whole name before
`#`; a `*` in a policy pattern matches as it did, `/` included, so an
organization's scope is `grn:<org>/*`. Open in ADR-0142: whether an
organization is better named inside itself than by `root`; the context
key a locator arrives under; how a wildcard treats `#`; which service
owns `doc` and `secret`; and what keeps an identifier from being
reused.
