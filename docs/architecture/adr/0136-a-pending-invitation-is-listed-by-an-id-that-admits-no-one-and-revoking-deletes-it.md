# ADR 0136: A pending invitation is listed by an id that admits no one, and revoking deletes it

> Status: accepted
> Date: 2026-10-31

Drafted for #1718, which reserves this number. On 2026-10-31 the owner
took listing and revoking pending invitations out of that issue to land
first, because neither depends on the policy service (#1689). The rest
of #1718 waits for #1689 and decides nothing here. garam is read at
`6e80e2a4f775a5ddc252884492e4540e048242d1`.

## Context

- **An invitation that was issued could not be seen or withdrawn.**
  `/orgs/{org}/invitations` answered `createInvitation` alone, so a
  link sent to the wrong person stood until it expired, seven days by
  default and up to thirty.
- **An invitation's only name was its secret.** `invitations.token` is
  the primary key, and holding it is the whole claim
  ([ADR-0006](./0006-invitation-tokens.md)). ADR-0122 lists
  `createInvitation.token` among the answers a command writes to a file
  alone, and the console reveals a link once. A list carrying tokens
  would hand the means to join to everyone who may read it.
- **An invitation is spent by a claim on its row** (#1381). Of two
  uses, one matches `accepted_at IS NULL` and the other matches
  nothing. A registration's claim stands alone while it creates an
  account, and can be released.
- **A role decides who may invite**, and `policy/`'s test reads
  `platform/internal/org/role.go`, which the owner's decision leaves
  unchanged.

## Decision

**An invitation carries a random `id` that admits no one, by which
`listInvitations` names the pending ones and `revokeInvitation` deletes
one. Revoking leaves nothing, so its token is answered as an unknown
one.**

1. **The id.** Migration `000066` adds `invitations.id`, a unique
   `uuid`. The token stays the primary key and stays out of every
   answer but `createInvitation`'s.
2. **Pending** is neither spent nor expired: `accepted_at` empty, a
   registration's claim counting as spent, and `expires_at` ahead.
   `listInvitations` answers those, newest first, with the role, who
   issued it and both instants.
3. **`org:invite` decides both operations.** Listing and withdrawing
   are the rest of issuing, as `agent:enroll` covers approving and
   withdrawing. So an owner and an admin hold them, a member does not,
   and either may revoke what the other issued.
4. **Revoking deletes the row.** Every use of the token then finds
   nothing: `getInvitation`, `acceptInvitation` and registering each
   answer 404, as for a token that never existed. A second revocation
   answers 404 too.
5. **A spent invitation is not revoked.** The delete matches
   `accepted_at IS NULL`, on the row a claim also updates, so a
   revocation and a use never both take effect. An invitation already
   used is refused with 409 `invitation_accepted`. A registration
   holding a claim counts as used, so one that is released afterwards
   is pending again and can be revoked then.
6. **The id is the organization's that issued it.** The organization
   in the path is part of the match, so another's path answers 404.

## Consequences

- An organization sees what it has outstanding and can withdraw a
  link at once, by API, console or CLI.
- A list shows who sent an invitation and what it admits to, and the
  link stays unrecoverable: a lost link means a new invitation.
- Nothing in garam remembers that an invitation was revoked.
  `platform/internal/audit` records message sends and machine
  credentials and no change event (`audit.md` §Open questions), so the
  request line, which names the caller and the route pattern, is all
  that records either operation until such an event exists.
- A revoked link reads as an invalid one rather than saying it was
  revoked, so a holder learns nothing about what the organization did.
- An invitation outlives whoever issued it: it stays pending and
  listed. A later decision on removing a member may change that.

## Signs the decision was wrong

- People holding a revoked link keep asking why it is invalid, which
  would argue for a tombstone and a `revoked` preview state, rejected
  here because a row kept only to say so serves no reader while the
  audit trail cannot read it.
- An organization needs to know who revoked what before a change event
  exists.
- Organizations grant listing without revoking, or the reverse, which
  would argue for actions of their own instead of `org:invite`.
