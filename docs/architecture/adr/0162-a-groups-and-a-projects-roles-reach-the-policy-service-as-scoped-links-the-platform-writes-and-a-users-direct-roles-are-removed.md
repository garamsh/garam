# ADR 0162: A group's and a project's roles reach the policy service as scoped links the platform writes, and a user's direct roles are removed

> Status: accepted; built in `platform/internal/policyfeed`,
> `platform/internal/api` and migration 000079 (#1896); supersedes
> [ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)
> in part (inventory row 2, "A user's direct roles", which is removed
> and not written as a link) and extends
> [ADR-0159](./0159-role-links-follow-the-entities-in-one-pass-the-platforms-tables-win-a-failed-write-and-a-request-finishes-its-work-before-it-waits.md)
> item 4, whose "second kind of mark" is the first of a family
> Date: 2026-11-14

Drafted for #1896, which reserves this number. It records the
platform's half of the scoped link whose identifier
[ADR-0163](./0163-the-platform-alone-writes-under-the-identifier-of-the-scoped-link-it-derives-from-a-groups-or-a-projects-roles.md)
reserves. [authz.md](../authz.md) §Role links states the result as
built.

## Context

ADR-0146 inventory rows 3 and 4 have a group's and a project's roles
written as scoped role links, and row 2 has a user's direct roles
written as a link to the role's system group. Read at
`7fa6edb1848298bfb8c656414e67b68bd25d04a8`, the platform wrote none of
the three: it decided from `groups.roles`, `projects.roles` and
`users.roles` itself, and the policy service held a member's link
alone (ADR-0159, ADR-0160).

- `groups.roles` and `projects.roles` are `text[]` columns of the
  owner's own row, and no table holds a role of one member of a
  group. A role is `owner`, `admin` or `member`, and it grants what
  its actions allow to a member of the group or the project on that
  group's or project's GRN (ADR-0163 Context).
- `users.roles` is a `text[]` column of the user's row. No route of
  the API wrote it: login and registration create a user with none,
  and the only writers of a non-empty value were the store's `Update`
  and tests. It granted a role's actions in every organization its
  user belongs to, which no other origin does.
- ADR-0159 item 4 gave the outbox a second kind of mark, the role
  link, whose id is `<organization GRN> <member GRN>` and whose state
  function is its own. A scoped link has a different id and different
  state, so it is not that kind, and the question of how many kinds
  the outbox holds is answered here.
- A role that is removed, and a group or a project that is deleted,
  leave no row to read the role from. A link is found for removal
  without one only if its identifier can be computed from what the
  mark names.

## Decision

1. **A group's and a project's roles are written as scoped links.**
   For each role the owner holds, the pass writes the policy
   ADR-0163 item 2 names, `grn:<org>/iam/policy/link-scoped-<role>-<hex>`,
   as `CreatePolicy` (`ALREADY_EXISTS` is a link written already) and
   `ActivatePolicy`, with no writer, the owner (`Iam::Team` for a
   group, `Iam::Project` for a project) as principal and as resource,
   and team-or-project scope. The platform computes the identifier in
   `scopedPolicy` and holds the computation to the two literals
   ADR-0163 item 2 states. A role held more than once is one link,
   and a name the platform does not know is no link, as it grants
   nothing.
2. **The outbox holds a third kind of mark, and the kinds are a
   family.** `Iam::ScopedRoleLink` has as its id the GRN of the group
   or the project, which is both the link's principal and its
   resource. The outbox's kinds are now the entity types ADR-0140
   lists, `Iam::RoleLink` and `Iam::ScopedRoleLink`; each link kind is
   read through its own state function, `policy_role_link_state` and
   `policy_scoped_link_state`, and a later kind is added the same way
   with no ADR of its own unless it changes a rule of this family.
   Both link kinds are sent after the entities, in the pass's
   transaction and by name under no sequence (ADR-0159 item 1), and
   both can be set aside (ADR-0160).
3. **What marks it.** Inserting and deleting a group or a project
   mark its scoped link, and so does an update that changes `roles`.
   A write of the name, or of `roles` to the value it holds, marks
   nothing. The entity's own mark, `Iam::Team` or `Iam::Project`, is
   not raised when only `roles` changes, since the entity carries no
   role.
4. **A removed role's link is found by its name.** The state function
   returns the roles the owner holds, each once and known, or no row
   for an owner the tables no longer hold. The pass writes each role
   held and then deletes the policy of each of the three roles not
   held, `NOT_FOUND` being a link removed already, so a role removed,
   a role changed and a deleted group or project are one operation and
   no record of the past is read. As ADR-0159 item 1 has it for a
   member, the role is written before the others go. A link refused
   for good removes the owner's links of all three roles and is set
   aside, and a waiting request is answered at once with
   `change_refused`, as ADR-0160 has it for a member.
5. **A request waits for the owner's mark.** Creating and deleting a
   group or a project wait on its `Iam::ScopedRoleLink` mark as the
   membership routes wait on theirs: `Gate.AwaitScoped`, the bound of
   ADR-0159 item 3 and the same two reasons. An update waits only when
   its body wrote the roles; one that did not is answered at once,
   whatever mark of its owner is set aside, as a request that asked
   nothing of the service. That every request changing a fed entity
   waits is a wider rule that is not decided here.
6. **A user's direct roles are removed.** Migration 000079 drops
   `users.roles`, and the code that read it goes: the field of the
   user, the statement built from it, the `direct-role` source of an
   explanation and the console's wording of it. ADR-0146 row 2 is
   superseded, not carried out: a user's role in an organization is
   its membership's, and a role on a group or a project is that
   owner's. What the column held is not kept, and no write of it
   existed to lose.
7. **The migration marks what exists.** For a feed that is on it
   marks every group and every project, raising a mark that is there,
   so the roles stored before it are delivered; `garam policy-feed
   start` does the same for a feed that is off. The migration takes a
   `SHARE` lock on `groups` and `projects` before `policy_outbox`, the
   order a writer and `start` take. Its way down restores `users.roles`
   empty and deletes the third kind's marks, and leaves the links
   written in the service where they are: no pass removes them.

## Consequences

- **Easier.** A route that writes a group's or a project's roles is
  fed with no line of its own. The service holds the same links the
  platform's tables say, and the policy that decides from them can be
  read in one place. A removal needs no lookup.
- **Harder.** The pass sends three kinds. A role that changes holds
  both for as long as the removal of the old one fails, and a
  group's or a project's link is set aside as a whole when one of its
  three is refused. Every group and project request of a feed that is
  on now waits on the loop, though a request that changes no role
  answers at once.
- **Ruled out.** A scoped link written under any other identifier, or
  under a writer's name (ADR-0163). A lookup of a link before its
  removal. A role that applies in every organization of a user.

## Signs the decision was wrong

- A group or a project whose stored roles and whose links in the
  service differ after a pass that reported no failure: the state
  function and the identifier disagree.
- A role link of a kind the outbox does not hold, written by a route
  that has no mark: the family is not covering a route.
- A user reported to hold a role in an organization that its
  membership and groups do not give it, or to have lost one that a
  direct role gave: a direct role was in use after all.
- A group created over HTTP answering `change_not_applied` while the
  service is up: the wait is on a mark the pass does not clear.
