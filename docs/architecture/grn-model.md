# GRN Model

Garam Resource Names — one identifier for every resource in garam.
Implementation:
[`lib/grn/grn.go`](../../lib/grn/grn.go), whose doc
comments are authoritative for constructing, rendering,
parsing and validating a GRN, and for `GRN` being a value type.

**Decided and not built.** The grammar below is
[ADR-0142](./adr/0142-a-grn-names-its-organization-its-service-and-a-path-of-type-and-id-pairs-with-a-locator-the-service-reads.md)'s.
`lib/grn/grn.go` enforces the five-segment form described under
§What the code enforces today until the implementation issue lands,
and every other responsibility document still writes its examples in
that form.

## Contents
- Format
- Ownership
- Types by owning service
- identifier
- Locator
- What the code enforces today
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

One grammar: once the implementation lands the five-segment form is
refused and nothing parses both. A name of this grammar holds one
colon before `#`, a five-segment name four.

Examples:

| Case | Name |
|------|------|
| a platform type | `grn:acme/iam/user/alice` |
| an organization | `grn:root/iam/org/acme` |
| a project-scoped resource | `grn:acme/docs/project/frontend/doc/readme` |
| a nested resource | `grn:acme/codehub/project/frontend/repo/api-server` |
| a locator | `grn:acme/codehub/repo/api-server#refs/heads/main:cmd/main.go` |

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
  organization.
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
| `codehub` | `repo` | a repository codehub stores, owned by an organization, or by a project as `project/<id>/repo/<id>` once that step is taken (planned, ADR-0141) | `grn:acme/codehub/repo/api-server` |
| not decided | `doc` | permissioned document (planned) | `grn:acme/docs/project/frontend/doc/readme` |
| not decided | `secret` | reference to a value an external store holds, never the value (planned, ADR-0036) | `grn:acme/iam/secret/model-api-key` |

The owner of `doc` and `secret` is not decided, so the service names
in their examples are illustrative.

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

## What the code enforces today

`lib/grn/grn.go` enforces the earlier form, which the implementation
issue retires:

```
grn:<organization>:<project>:<service_type>:<identifier>
```

Five colon-separated segments; `project` is `default` when the
resource is organization-level; each segment follows the character
rule above at ≤ 64 characters, and `Parse` splits on every colon. The
earlier names read in the new grammar as follows.

| Earlier | Decided |
|---------|---------|
| `grn:<org>:default:<type>:<id>` | `grn:<org>/<service>/<type>/<id>` |
| `grn:<org>:<project>:<type>:<id>` | `grn:<org>/<service>/project/<project>/<type>/<id>` |
| `grn:root:default:org:<slug>` | `grn:root/iam/org/<slug>` |

## Open questions

The per-service manifest and the served registry of types are a later
ADR. Open in ADR-0142: whether the policy service's caller name
`platform` follows the service name `iam`; whether an organization is
better named inside itself than by `root`; the context key a locator
arrives under; how a wildcard in a policy pattern treats `/` and `#`;
how an action's `<service_type>:<verb>` name reads once types belong to
services; how a Cedar entity identifier is derived from a nested name;
which service owns `doc` and `secret`; and what keeps an identifier
from being reused.
