# ADR 0159: Role links follow the entities in one pass, the platform's tables win a failed write, and a request finishes its own work before it waits

> Status: accepted; built in `platform/internal/policyfeed` and
> `platform/internal/api` (#1878); supersedes in part
> [ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)
> (the sentence "delivered with the entities under the same sequence",
> and the answer at the bound as "unavailable with the fact that the
> change is stored and not yet applied", which gains a reason and the
> stored resource) and
> [ADR-0140](./0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md)
> (§5, "the outbox is a set" of dirty entities, which also holds a
> role link)
> Date: 2026-11-13

Drafted for #1878, which reserves this number. It records four
decisions the pull request took where ADR-0146 item 4 and §Reading
one's own write are silent or cannot be built as written.
[authz.md](../authz.md) §Role links states each as built.

## Context

ADR-0146 item 4 has the platform write an organization's role links to
the policy service from its memberships, as the caller `platform` with
no writer, and says the outbox delivers them "with the entities under
the same sequence". It does not say what a failed link write leaves, or
what the request answers at its 5 second bound beyond "unavailable with
the fact that the change is stored and not yet applied".

- The sequence is the entity batch's: `ApplyEntityChanges` takes a
  sequence per call and carries entities alone. The policy service's
  link calls (`CreatePolicy`, `ActivatePolicy`, `DeletePolicy`) are
  separate RPCs that carry no sequence, so a link cannot ride under
  one.
- A link write crosses the network, and the writer's request holds no
  transaction across it.
- Three of the four requests that write a membership cannot be
  repeated: the identifier of a created organization is random, an
  invitation is spent once, and a deleted organization is gone.
  Answering 503 after the commit and leaving the client to retry
  would make a second organization, or answer 409 or 404 to a person
  whose request had succeeded.
- ADR-0140 §5 makes the outbox a set of dirty entities read through
  `policy_entity_state`. A membership is not an entity of the policy
  service, and the link is read through its own state: the role the
  membership holds, or no row.

## Decision

1. **Links are delivered in the same pass and the same transaction as
   the entities, after them, and not under a sequence.** A pass sends
   its entities, then its links, then deletes every mark. A link is
   ordered by its name instead of a sequence: one policy per member and
   role, so a write is found again (`ALREADY_EXISTS`) and a removal is
   not (`NOT_FOUND`), and neither is a fault. A role held is written
   before the member's other two roles are deleted, so a role that
   changes is never absent.
2. **When a link write fails, the platform's tables win.** The
   membership and its mark stay, the loop retries, and the policy
   service keeps the links it last had: a member added is not yet
   allowed, a member removed is not yet denied, and a role changed is
   the old one's or both. The policy service fails closed or stale,
   never ahead of the tables.
3. **At the bound the answer is unavailable with the reason
   `change_not_applied` and the stored resource, and a request finishes
   its own work before it waits.** The 503 carries `reason`
   `change_not_applied` and `resource`, the GRN of the organization the
   request created, joined, registered into or deleted. The API
   discloses a reason on a 503 for this code alone. A registration
   starts its session and builds its body before the wait, so the
   session exists at the bound. This is the PM's decision in review
   5889668505 of #1881.
4. **The outbox holds a second kind of mark, the role link, read
   through its own state function.** `Iam::RoleLink` has the id
   `<organization GRN> <member GRN>`, and `policy_role_link_state`
   returns the role the membership holds, or no row, which is the
   link's removal. `policy_entity_state` is not widened to it.
   Triggers on `org_members` mark it on every insert, delete and role
   change, so a route written later is covered.

## Consequences

- **Easier.** A new route that writes a membership is fed with no line
  of its own. A client of the three requests that cannot be repeated
  knows what is stored and where to read it, and does not repeat it.
  A role that changes is never left with no link.
- **Harder.** A pass is no longer one batch: its links are calls, each
  of which can fail after the entities applied, and a failure between a
  role's write and the other roles' deletes leaves the member holding
  both until the next pass. The delivery pass holds its `policy_feed`
  row lock across those calls, as ADR-0140's design already does.
- **Ruled out.** A link under the entity sequence, which the policy
  service cannot take without a change to its contract. A wait inside
  a store method, which would hold the writer's transaction across a
  call.
- **A link refused for good stops the feed.** The loop retries a
  failing pass, and a link the policy service refuses for good is never
  skipped, so delivery stops for every organization. Filed as
  garamsh/garam#1882.
- **A link left when the feed was off stays.** `stop` empties the
  outbox and `start` marks the memberships that exist, so a membership
  that ended while the feed was off leaves its link, and its member
  stays allowed. Nothing lists or removes such a link; the operator's
  remedy is in [authz.md](../authz.md) §Role links.
- **A role change is three calls.** With no writer the policy service
  accepts only a role link's create, activate and delete, so a role
  change writes the new policy and deletes the old, never a new
  version.

## Signs the decision was wrong

- The policy service gains a sequenced link write, so a link can be
  ordered against its entities without the order of names. Decision 1
  is then a workaround.
- A client reads `resource` from a 503 and still creates a second
  organization or repeats a spent invitation: the answer did not say
  enough.
- A member is allowed after the membership ended, or denied after it
  began, for longer than the loop's retry explains: the tables did not
  win, or a link was left that a pass should have removed.
- A second 503 needs a reason of its own. Disclosing one code by name
  was then too narrow, and the rule is one `Error` carries a reason
  whatever its status.
- A third mark kind needs a state function of its own. The set of
  marks is then a family of kinds, and ADR-0140 §5 should be written as
  one.
