# ADR 0137: Tenant isolation is membership, and the policy engine fails closed on any evaluation error

> Status: accepted
> Date: 2026-10-30

Drafted for #1689, which reserves this number. It supersedes
[ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
§4 in part. The rest of ADR-0130 stands. Measurements below are of
`cedar-policy` 4.14.0 on rustc 1.99.1.

## Context

- **ADR-0130's tenant isolation reads an attribute a user cannot
  have.** It writes `forbid … unless { principal.org == resource.org }`.
  A user can belong to multiple organizations (`users.md`), so no
  single `org` describes one.
- **That form fails open, not closed.** With no `org` on `User`, strict
  validation refuses the policy. Evaluated anyway, the attribute access
  errors, Cedar skips a policy that errors, and a role link on the
  resource was answered `Allow` with one error in its diagnostics. A
  `forbid` is only as strong as its evaluation.
- **Containment is not membership, and Cedar's `in` is transitive.**
  A team's parent is its organization, so `principal in resource.org`
  is true of a user whose only parent is a team of that organization.
  That user was answered `Allow` on `agent:read` with a role held by
  the team, and with a role naming the user directly. `authz.md`
  §Role-derived statements says neither a group grant nor a direct role
  admits a non-member, and the state exists: `organizations.md` admits
  a user's GRN to a group without reading a membership. Any form
  reading the containment tree has this defect.
- **ADR-0130's role template cannot be held by a team.**
  `principal == ?principal` linked to a team matches nobody, since no
  request's principal is a team. A team holding a grant is what #1689
  and `authz.md` §An agent on the browser-facing listener describe.
- **`resource in ?resource` widens a team's or a project's role.**
  Today a group's roles reach the group's GRN and nothing else
  (`authz.md` §Role-derived statements). A team's member agents have
  the team as a parent, so `in` answered `Allow` for a team admin on a
  member agent, and `==` answered `Deny`.
- **ADR-0130's GRN examples are not the GRN form.** `lib/grn` renders
  five segments, as `grn:acme:default:user:alice` and
  `grn:root:default:org:acme`, not `grn:acme:user/kim` or `grn:acme`.

## Decision

**A principal reaches an organization by being a member of it, and any
evaluation error is a deny.**

1. **Tenant isolation reads membership, kept apart from containment:**
   `forbid (principal, action, resource) unless { principal in resource.org.members };`.
   Each organization has one `OrgMembers` entity, named by its required
   `members` attribute. A member has that entity as a parent, a user
   may have several, and the schema lets no team or project sit in one,
   so `in` reaches it from a member and from nothing else.
   `resource.org` is a required attribute of every resource type, and
   an `Org` carries itself.
2. **The engine fails closed.** Any evaluation error makes the decision
   a deny, with the error as a reason, whatever Cedar answered.
3. **A role template's principal scope is `principal in ?principal`,**
   so a team or project can hold a grant for its members and a direct
   link still matches, `in` being reflexive.
4. **Two template sets.** An organization's roles use
   `resource in ?resource` and link only to an `Org`. A team's or a
   project's roles use `resource == ?resource` and link only to a team
   or a project.
5. **GRN examples take the real form,** and an identifier stays opaque
   to Cedar: `User::"grn:acme:default:user:alice"`.

## Consequences

- The forbid decides a principal missing from the store by itself,
  with no evaluation error, so that row does not rest on item 2.
- An entity slice has to hold the resource's organization and its
  `OrgMembers`. One without them errors, and is denied by item 2.
- The entity store holds one more entity per organization, and the
  platform's outbox writes a member as a parent of it, as it writes a
  team's member.
- An organization's `forbid` that errors stops the request it was
  meant to narrow, and one that errors on every request stops
  everything it reaches. A broken guardrail is loud rather than silent,
  and schema validation at the write boundary is what keeps it rare.
- A resource missing from the entity store is denied through
  `resource.org` erroring, even where a grant names it.
- An entity store that loses a principal's `OrgMembers` parent denies
  that principal, whatever teams it stays in, which is the direction a
  stale outbox should fail in.
- `resource.org` and a resource's parents state one fact twice, and an
  agent is both a member and a contained resource, so it carries three.
  The outbox of ADR-0130 §7 writes them, and nothing compares them.
- The system tier holds six role templates rather than three, because
  the two scopes differ in the policy itself.

## Signs the decision was wrong

- Requests are denied in production by evaluation errors no one can
  act on, so the fail-closed rule gets an exception.
- A resource's `org` and its parents disagree in the entity store, and
  a decision followed the wrong one.
- A second kind of grouping needs to sit in `OrgMembers`, which would
  bring containment back into what isolation reads.
- A scoped role needs to reach what its team or project contains often
  enough that a third template set is proposed.

## Alternatives rejected

- **Keeping `principal.org == resource.org` with one `org` per user.**
  It cannot state a user in two organizations.
- **An erratum to ADR-0130.** A changed form is a changed decision, and
  rule 2 of `docs/architecture/README.md` answers that with a new ADR.
- **`principal in resource.org`.** It was this ADR's first draft. The
  transitive `in` admits a user whose only parent is a team or project
  of the organization: measured `Allow` on `cedar-policy` 4.14.0, both
  with the team holding the role and with a role naming the user.
- **`principal.orgs.contains(resource.org)`, a `Set<Org>` on each
  principal.** It holds on the team-only and multi-organization cases
  as this form does, and needs no entity type of its own. It differs on
  a principal missing from the store: the attribute access errors,
  Cedar answers `Allow` with one error, and only item 2 denies, where
  this form's forbid answers `Deny` with no error. It also moves
  organization membership out of parents while team membership stays
  there, and gives an agent an `orgs` beside its `org`.
- **`resource in principal.org`.** It reads no `resource.org`, but still
  needs a single `org` on the principal.
- **`resource in ?resource` for every role, narrowing with a
  `forbid`.** No finite `forbid` states "nothing inside this team", and
  the set of what a team contains is the owner's data.
