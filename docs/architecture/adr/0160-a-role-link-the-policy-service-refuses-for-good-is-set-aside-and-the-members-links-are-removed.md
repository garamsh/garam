# ADR 0160: A role link the policy service refuses for good is set aside, the member's links are removed, and a waiting request is answered at once

> Status: accepted; built in `platform/internal/policyfeed` and
> `platform/internal/api` (#1882, first half); extends
> [ADR-0140](./0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md)
> item 11 to a role link, and supersedes in part
> [ADR-0159](./0159-role-links-follow-the-entities-in-one-pass-the-platforms-tables-win-a-failed-write-and-a-request-finishes-its-work-before-it-waits.md)
> item 2 ("the loop retries" and "a member removed is not yet
> denied") for a link refused for good
> Date: 2026-11-13

Drafted for #1882, which reserves this number. It records three
decisions the pull request took where ADR-0140 item 11 is silent about
a role link, and the fourth that follows from them, the reason a
waiting request is answered with.
[authz.md](../authz.md) §Role links states each as built.

## Context

ADR-0140 item 11 deletes an entity the policy service refuses and logs
an error, so a bad entity denies and does not decide on what it last
said. ADR-0159 item 2 says a failed link write leaves the policy
service with the links it last had and the loop retries. For a link the
service refuses for good, the two do not meet. A refusal that no retry
clears is retried by every pass, fails the pass each time, and holds
back every other organization's entities and links behind it. A
request waiting for the link waits out the 5 second bound for an
answer that cannot come. Nothing of the refusal outlasts a log line.

- `PERMISSION_DENIED` and `INVALID_ARGUMENT` are answers to the request
  itself, which is a function of the membership alone: the same
  request is judged the same each time
  (`policy/crates/server/src/status.rs`, lines 100 to 109, 150 to 153,
  116 to 121 and 166 to 180). Every other code the link calls return
  may clear.
- The link of a member is three policies, one per role. A refused write
  of the new role leaves the old role's policy in the service.
- ADR-0159 item 3 gives the bound's answer the reason
  `change_not_applied`, whose meaning is that the loop goes on
  delivering the change.

## Decision

1. **A link refused with a code of a closed list is set aside, and the
   pass goes on.** The list is `PERMISSION_DENIED` and
   `INVALID_ARGUMENT`; every other code fails the pass and is retried
   as before. The mark is kept and records the code, the reason and the
   time in `policy_outbox` (migration 000076), no pass reads it, and a
   later write of the membership, or `garam policy-feed start`, clears
   it. The loop logs an error once and `/metrics` carries the count.
2. **The member's links are then removed, so the service holds no role
   for the member.** The platform's tables still say the role, and the
   policy service says none: the member is denied there, which fails
   closed. This replaces ADR-0159 item 2's "the service keeps the links
   it last had" for this case, since the old role the service last had
   is one the tables no longer hold.
3. **A request waiting for a set-aside link is answered at once,** 503
   `unavailable` with its own reason, `change_refused`, and the stored
   `resource` as the bound's answer carries. It is not
   `change_not_applied`: nothing the loop does will apply it, and the
   client has nothing to wait for. The status stays 503, as the change
   is stored and a client that does not know the reason reads what it
   always read for a 503.

## Consequences

- One organization's refused link no longer holds back the others, and
  an operator reads the refusal from the table and from `/metrics`
  and not from a log line alone.
- A fault on the platform's side that makes every link write malformed,
  or that the service answers as `PERMISSION_DENIED` (a credential or a
  configuration fault), sets every link aside and so removes every
  member's role in the policy service. It denies rather than allows,
  and it is repaired by mending the fault and running `garam
  policy-feed start`, which marks every membership anew. Until then
  the members are denied.
- A client gets a second 503 reason and the SDKs a sentence for it.
- The set-aside is recorded on a mark, so it follows a mark's rules: a
  membership written again is delivered again, whether or not the
  cause is mended, and a refusal that returns sets it aside again.

## Signs the decision was wrong

- A set-aside count that rises together across organizations, which is
  the platform's own fault answered as a refusal: the list of codes
  is then too wide, and `PERMISSION_DENIED` is the first to narrow.
- A link set aside that a retry would have applied, found when an
  operator clears it and it succeeds with nothing changed.
- A client that waits or polls on `change_refused`, which would show
  the reason's meaning is not read as written.
