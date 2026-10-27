# ADR 0128: A cowork channel's participants narrow what the platform allows, and grant nothing

> Status: accepted
> Date: 2026-10-27

Drafted for #1664, which reserves this number. garam is read at
`34f011b5df0f15b9a56c4628f87ddaad34692521`.

## Context

- ADR-0127 makes the platform the one place a cowork permission is
  decided: cowork asks `checkPermissions` with the caller's own
  credential. Its Consequences rule out "cowork keeping its own access
  lists, a second permission model beside the platform's".
- Messaging (#1664) needs channels, private channels and direct
  messages. A direct message is between the people in it, and a
  private channel between the people invited. Both need to know who
  is in them.
- Making each one a platform policy instead fails twice:
  - writing a policy is `policy:create` and `policy:attach`, which a
    member's role does not hold (`internal/org/role.go`), so a member
    could not start a direct message;
  - every conversation would add a policy and its attachments to the
    organization's policy list, which people read to understand what
    is allowed.
- What would make a participant list a second model is its granting
  something. A list that can only take away leaves the platform as the
  only source of an allow.

## Decision

**A channel's participant list only narrows. To read a channel, or to
post in it, the caller needs the platform's allow and, where the
channel is private or a direct message, a place on its list. Nothing
in cowork allows what the platform denies.** This supersedes
ADR-0127's Consequences in that part: a participant list is kept, and
it is not an access list that grants.

1. **The platform's half.** `cowork:channel-read` and
   `cowork:channel-post` are asked on the organization, to list,
   search or create channels, and on a `cowork-channel`, to read or
   post in one. Every role grants both, and a policy narrows them as
   it narrows any action.
2. **cowork's half.**
   - A public channel has no list.
   - A private channel's list is kept by cowork. Its creator is on it,
     and anyone on it may add another member of the organization.
   - A direct message's list is fixed when it starts: the people in
     it.
   - Leaving takes the caller off the list.
3. **Order.** cowork asks the platform first and the list second, so
   a caller the platform refuses learns nothing about the list.
4. **The record.** A message keeps its author's GRN. Taking someone
   off a list stops their reading from then on, and changes no
   message.

## Consequences

- People start direct messages and private channels without an
  administrator, and the organization's policies stay the place where
  allows come from.
- An administrator who denies `cowork:channel-read` on a channel shuts
  every participant out of it. Being on a list never overrides that.
- cowork holds data that decides a refusal: the lists. A bug that
  drops someone from a list refuses them wrongly, and one that adds
  someone exposes a channel to a member the platform already allows
  to read the organization's channels.
- An administrator cannot read a private channel through the platform
  alone, since the list also has to hold them. Whether an
  administrator may always read every channel is not decided here.
- Ruled out:
  - a platform policy per channel;
  - a list that can grant what the platform denies;
  - asking the platform about each participant on each message.

## Signs the decision was wrong

- Organizations ask for channel access to be managed in the console's
  policy pages, and the two places that decide confuse them.
- A refusal is traced to a participant list that disagrees with what
  the people in the channel expect.
- The platform must answer "may X read channel Y" for someone other
  than the caller, which cowork's own lists would then have to
  answer instead.
