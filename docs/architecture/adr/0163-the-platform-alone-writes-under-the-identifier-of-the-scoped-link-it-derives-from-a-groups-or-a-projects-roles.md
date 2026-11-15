# ADR 0163: The platform alone writes under the identifier of the scoped link it derives from a group's or a project's roles

> Status: accepted; built in `policy/crates/grant` and
> `policy/crates/server` (#1898); extends
> [ADR-0161](./0161-the-platform-alone-writes-under-the-identifiers-of-a-members-role-links-and-only-that-members-link.md)
> to a second reserved form without changing what it holds for a
> member's link
> Date: 2026-11-14

Drafted for #1898, which reserves this number. It records the policy
service's half of #1896: the platform is to write a group's and a
project's roles as scoped role links, and the identifier it writes
them under must be as much its own as a member's is.
[authz.md](../authz.md) §Role links states the result as built.

## Context

[ADR-0161](./0161-the-platform-alone-writes-under-the-identifiers-of-a-members-role-links-and-only-that-members-link.md)
reserves `link-owner-`, `link-admin-` and `link-member-` and admits
under them one body: a member's link in organization scope
(`grant::names_link`). A link in a team's or a project's scope is
refused under those identifiers, and under any other identifier a
writer holding the role may create it first, add a version to it, or
remove it, so the platform cannot write such a link safely.

What the platform holds to derive such a link from, read at
`7efd1205cbc93d6edc17e0abbb3dd40e5a7f6832`:

- `groups.roles` and `projects.roles` are `text[]` columns of the
  group's and the project's own row
  (`platform/migrations/000001_init.up.sql`). There is no table of a
  role held by one member of a group or on another resource.
- A role there is `owner`, `admin` or `member`
  (`scopedRoles` in `platform/internal/api/group.go` over
  `org.ParseRole`), and it grants what its actions allow to a member
  of the group or the project on that group's or project's own GRN
  (`groupRoleStatements` and `projectRoleStatements` in
  `platform/internal/api/statement.go`).
- The policy feed's entities are `Iam::Team` for a group and
  `Iam::Project` for a project
  (`platform/internal/policyfeed/postgres/postgres.go`), and the
  members are the entity's children, so a role link whose principal
  is the team or the project reaches them through the
  `role-scoped-*` templates (ADR-0137).

So the platform holds one shape of scoped link: a team or a project
as principal, and the same team or project as resource. A link whose
principal is an agent or a user, or whose resource is another team
or project, is what a person writes through the console
(#1796, decision 5) and stays a named writer's.

## Decision

1. **A fourth prefix, `link-scoped-`, is reserved** in an
   organization's own policies (`grn:<org>/iam/policy/<id>`, the id
   with no `/`), beside ADR-0161's three. The tail is not read to
   reserve, as there. The prefix opens none of the three, so an
   identifier of a member's link and one of a scoped link never
   meet, and `link-scope-x` and `link-scopedx` are not reserved.
2. **The identifier is
   `grn:<org>/iam/policy/link-scoped-<role>-<hex>`,** `<role>` being
   `owner`, `admin` or `member` and `<hex>` the first 16 bytes of the
   SHA-256, as 32 lower-case hex digits, of one byte string:
   five fields in this order, each written as its length in bytes
   as 8 bytes big-endian and then its bytes in UTF-8:
   1. the role's word,
   2. the principal's entity type,
   3. the principal's id,
   4. the resource's entity type,
   5. the resource's id.

   One public function of `grant`, `scoped_link_policy`, computes it
   from the organization, the role, the principal and the resource.
   The platform recomputes it in Go (#1896) and the two are held to
   these values, which `link_name.rs` tests:

   | role | principal and resource | identifier |
   | --- | --- | --- |
   | `admin` | `Iam::Team`, `grn:acme/iam/group/eng`, both | `grn:acme/iam/policy/link-scoped-admin-057a36867e8b5d54bdfba284ee2f3a02` |
   | `member` | `Iam::Project`, `grn:acme/iam/project/web`, both | `grn:acme/iam/policy/link-scoped-member-0e9bfafcc0092e41678c97122d4c76be` |

   The first row's byte string is `00 00 00 00 00 00 00 05`
   `admin`, `00 00 00 00 00 00 00 09 Iam::Team`,
   `00 00 00 00 00 00 00 16 grn:acme/iam/group/eng`,
   `00 00 00 00 00 00 00 09 Iam::Team` and
   `00 00 00 00 00 00 00 16 grn:acme/iam/group/eng`, written as
   bytes (the 8 length bytes are hexadecimal, 0x16 being 22).
   - Why length prefixes. A concatenation, or a separator that an
     id may contain, makes different fields one input: principal
     `ab` with resource `c`, and principal `a` with resource `bc`.
     A length prefix cannot, so two principals or two resources
     never share an identifier, and a test cuts the same bytes at
     other field boundaries to show it.
   - Why the role is both in the name and in the hash. The name
     shows the role to an operator reading `ListPolicies`, and the
     hash alone then differs for each role, so the identifier is a
     function of all three inputs and not of a name beside a hash of
     two.
   - Why 16 bytes. The slug of a policy is at most 64 characters,
     and `link-scoped-owner-` with 32 digits is 50 characters, as a
     member's identifier is at most 44.
3. **Only the caller `platform` with no writer writes under the
   prefix, and only a team's or a project's link on itself.** A
   `CreatePolicy` there is accepted when the body is a role link in
   team-or-project scope whose principal and resource are one entity
   of type `Iam::Team` or `Iam::Project`, and whose identifier is the
   one item 2 computes from the body's role, principal and resource
   (`grant::names_link`, which tells the two forms apart by the
   body's scope). Any other body is refused, a guardrail, a `permit`,
   a user, an agent or an organization as principal, a resource other
   than the principal, another role, or a tail that is not the hash
   of the body's fields. The form admits what the platform's tables
   hold, item 2 of the Context, and no more.
4. **A write that names a writer under the prefix is refused,
   whatever the writer holds and whatever the write,** as ADR-0161
   item 3 refuses it for a member's identifiers: `CreatePolicy`,
   `AppendPolicyVersion`, `ActivatePolicy`, `RollBackPolicy` and
   `DeletePolicy`, the owner's and the link the identifier names
   included.
5. **The platform's activation and removal, with no writer, are
   accepted only for a policy every version of which is the link its
   identifier names,** as ADR-0161 item 4 holds for a member's. The
   create of a link already there answers `ALREADY_EXISTS`, which the
   platform reads as written, and a replacement is the same create
   and activation again: a policy with no writer refuses a new
   version.
6. **Every refusal is `PERMISSION_DENIED` and is recorded as ADR-0161
   items 5 and 6 say:** the table there holds for a scoped link
   unchanged, with "the member's link" read as the link the
   identifier names, and a writer's refusal is a denial of the
   step's action on the organization while the platform's is not
   recorded.
7. **How it stands to ADR-0161.** It extends it and takes nothing
   back. A member's three prefixes, its body and its tests are as
   they were, and its status line says so. A body of one form under
   the identifier of the other is refused, since the two
   identifiers come from different inputs.
8. **No migration and no start-time check,** as ADR-0161 item 7. A
   policy that already holds an identifier opening `link-scoped-` is
   neither changed nor removed through the service by anyone, and an
   operator reads it with `ListPolicies` and `GetPolicy`.

## Consequences

- A writer cannot make the platform's scoped-link call act on its
  policy, nor give a team or a project a role by naming a policy as
  that team's link.
- #1896 writes a group's or a project's role under this identifier
  and no other, and holds its Go computation to the two values of
  item 2. A change of either side's computation sets every scoped
  link aside under ADR-0160.
- An identifier opening `link-scoped-` that a writer created before
  this decision cannot be removed by a writer. An operator removes it
  from the database, as ADR-0161 says of its own.
- A person's console link in a team's or a project's scope, with an
  agent or a team as principal, is written under an identifier the
  writer chooses, and may not open `link-scoped-`.
- `grant` has two naming functions, `link_policy` and
  `scoped_link_policy`, because a member's link is hashed from the
  member alone and ADR-0161 does not change it.

## Signs the decision was wrong

- The platform needs a scoped link whose principal is not its
  resource, under this prefix: the closed shape of item 3 is then the
  question, and not the prefix.
- A set-aside link whose reason names a `link-scoped-` identifier,
  found among teams or projects nobody squatted: the Go computation
  and this one have drifted.
- An organization that asks to reuse a policy under the prefix: the
  prefix is wider than the identifiers it needs, and the remedy is a
  narrower form with a tail check.
