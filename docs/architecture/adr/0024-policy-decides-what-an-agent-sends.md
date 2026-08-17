# ADR 0024: A policy decides what an agent may send

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

An agent GRN has been attachable to a policy since ADR-0008, and
ADR-0014 confirmed it needs no model change. Nothing read the
attachment. An administrator could attach a policy to an agent, see it
stored, list it back, and it decided nothing — the machine listener
imported no evaluator at all, and the only authorization on the message
path was `internal/message`'s comparison of the caller against the
recipient, which governs claiming and never posting.

An attachment that grants nothing is worse than no attachment: it reads
as a control that is not there.

Sending fits the model ADR-0002 already carries, with no extension. The
subject is the sending identity, the action is one action string, and
the resource is the recipient — so a wildcard on the resource says which
recipients an identity may reach, and the engine matches patterns as it
does anywhere else.

Two facts about the surrounding machinery bound what the decision can
be:

- Statement collection is the calling server's half of the union
  (ADR-0007). The engine's own default is deny, so a caller that
  supplies nothing is refused everything.
- The engine resolves the policies of the organization that owns the
  **object** ([authz.md](../authz.md) §Statement collection), and an
  attachment stays inside its policy's organization (ADR-0008).

## Decision

- **The machine listener asks the engine before it stores a message.**
  Subject is the identity the certificate named, action is
  `message:send`, object is the recipient GRN. `internal/machineapi`
  asks, over a seam of its own, exactly as `internal/api` does on the
  browser-facing listener; the two never import each other and never
  share a process (ADR-0017).
- **A machine identity's own GRN grants it its own organization.** The
  listener supplies one Allow statement scoped to `grn:<org>:*`, which
  is the identity-derived half of the union — the role-derived
  statements of ADR-0007, for a caller that holds no roles. An agent
  nobody has written a policy for goes on sending inside its
  organization, and reaches nothing outside it.
- **A policy narrows that with a Deny**, which is how every grant in
  this model is narrowed; deny-overrides applies across the union as it
  does everywhere else.
- **A refusal stores nothing.** ADR-0013 stores a message garam cannot
  route rather than rejecting one, because rejecting destroys a reply
  and tells the sender nothing. A policy refusal is the opposite case:
  garam can route the message and the organization has said not to, so
  storing it would deliver what was refused.
- **That refusal is a 403 and not the inbound message ADR-0013 asks
  for.** The shape ADR-0013 requires is a message back to the sender,
  and garam has no identity to send one under: every message carries a
  sender GRN, `internal/message` mints no identity of its own, and the
  same gap already stands recorded for a message that exhausts its
  attempts ([domains.md](../domains.md) `internal/message`). Inventing
  one here would settle by accident a question that deserves its own
  decision.

## Consequences

- An agent of one organization no longer posts to an agent of another.
  That is a behaviour change: it was accepted and stored until this
  decision, and the assertion that watched it cross is now the
  assertion that it does not.
- **An Allow attached to an agent widens nothing.** The baseline
  already covers the sender's own organization, and outside it the
  engine loads the recipient organization's policies, where an
  attachment naming a foreign subject cannot exist. Every attached
  policy that decides anything on this surface is therefore a Deny.
  Changing that means scoping the check to the subject's organization,
  or admitting cross-organization attachments — both are decisions of
  their own, and neither is taken here.
- **A group-attached policy reaches nothing here.** A check carries the
  subject's groups (ADR-0008) and this listener holds no org store to
  resolve them from, so it sends none. An agent may be a group member;
  a policy attached to that group still decides nothing about what it
  sends.
- **Detaching an active policy's last attachment widens it to the whole
  organization**, which for a Deny turns one agent's restriction into
  every agent's. That follows from ADR-0008 and is not new; this
  surface is where it first has teeth.
- `garam serve machine` now depends on `garam serve authz`. It dials
  the same engine the api server does, reports the channel in its
  readiness, and a deployment that does not configure it fails closed:
  an engine that cannot answer refuses the message rather than storing
  it.
- A sender learns of a refusal only as a non-2xx its adapter cannot
  surface. ADR-0013's mouth writes a refused reply to an outbox nothing
  drains, and a durable retry against a permanent 403 retries forever,
  so the cost of the deviation above is a lost reply and a sender that
  never finds out.

## Rejected alternatives

- **Granting every recipient by default.** An agent with no policy
  would keep working, and an administrator attaching "may send only
  inside acme" would change nothing, because a baseline Allow on every
  resource already matches — the failure this ADR exists to end, in a
  new place. It also leaves "own organization only" inexpressible:
  narrowing is by Deny, and no finite set of Denies names every other
  organization.
- **Denying every recipient by default.** The model's own default, and
  it stops every agent in every deployment on the day it ships,
  including the ones nothing was ever configured for. A control that
  breaks working traffic to close a gap is worse than the gap.
- **Switching enforcement on only where an attachment exists.** It
  keeps working agents working and an Allow meaningful, at the cost of
  a control input the engine does not have: "no policy" and "a policy
  that did not match" are the same answer from `Check`. Asking for the
  difference is a change to the policy model, which this ADR declines
  to make.
- **Putting the check in `internal/message`.** It would sit at the one
  choke point every stored message passes, where a second caller could
  not skip it. It was rejected because the boundary would then reach
  past the layer that composes it to another process, and because the
  next caller of `Post` is garam itself — the refusal message this ADR
  says it cannot yet send — which must not be judged by the policies of
  an agent it is writing on behalf of.
- **Refusing with a stored message instead of a status.** It matches
  ADR-0013's shape and requires an identity garam does not have. Naming
  one in passing is how a sender identity nobody designed becomes
  permanent.
