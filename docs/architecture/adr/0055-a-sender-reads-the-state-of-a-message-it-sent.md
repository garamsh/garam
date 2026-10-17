# ADR 0055: A sender reads the state of a message it sent

> Status: accepted
> Date: 2026-09-01

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

The machine listener has one message operation a sender can reach:
`POST /messages`. A sender that is answered `201` knows garam stored the
message durably and learns nothing after that — not whether a reader
claimed it, not whether the claim completed, not whether it stopped
being redelivered.

The one report back to a sender that exists is the cross-organization
refusal, and it is not a status: garam cannot route the message, so it
stores the refusal as an ordinary inbound message addressed back to the
sender ([ADR-0013](./0013-agent-messaging.md),
[ADR-0035](./0035-a-cross-organization-message-needs-both-organizations-consent.md)).
Everything else is silent. A message no agent claims, or one that spends
its attempts, sits until the sweep deletes it thirty days later
([ADR-0034](./0034-a-delivered-message-keeps-its-key-and-drops-its-body.md)),
and nothing tells its sender in that time.
[messaging.md](../messaging.md) opens with that under rule 9 and
[overview.md](../overview.md) §Out of scope (today) names the exhaustion
half.

Two agents cannot coordinate on that. A sender that cannot tell a
message nothing has looked at from one a reader is working on has no
basis for deciding whether to wait, to re-address or to give up, and the
absence of a signal lasts a month.

What is missing is only the reading. Every fact a sender would ask for
is already a column, written by the statements that hand a message out
and complete it: `attempts` and `claimed_at`/`claim_expires_at` since
migration `000009`, `completed_at` since `000008`, `created_at` since
the table existed. Nothing above the store serves any of it and no
route exists that would — the same shape
[ADR-0053](./0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)
found for an agent's two state fields.

[ADR-0018](./0018-delivery-acknowledgement.md) already decided that
garam *tells* a sender when a message exhausts its attempts, because the
agent platform tells nobody. That decision stands and nothing here
builds it.

## Decision

**A sender reads the state of a message it sent, on a route of its own
on the machine listener, and garam announces nothing.**

- **A read, not a report.** garam dials nobody
  ([ADR-0012](./0012-agent-operator-model.md)) and the queue traded a
  push for a poll (ADR-0013), so a sender that can ask needs no delivery
  semantics that do not already exist. A sender asks about one message,
  by the identifier the queue minted and the `201` answered with.
- **Bounded by the sender the connection proved.** The row is matched on
  the caller's own GRN as well as on the identifier, so the path alone
  reaches nothing — the shape ADR-0053 used, and the shape
  `internal/message` already uses for a recipient acting on its own
  queue.
- **Four states, every one of them derived from what the row already
  holds.** No column is added and no migration lands.
  - `completed` — a reader reported it delivered.
  - `claimed` — a reader holds it and the lease has not run out.
  - `exhausted` — the attempts are spent and no reader holds it, so the
    queue will not hand it out again (ADR-0018).
  - `pending` — anything else: it is waiting for a reader, or its last
    claim lapsed and it will be handed out again.

  The bound the state is read against is the same `Attempts` the claim
  predicate filters on, taken from the same `Config`, and the lease is
  compared by Postgres as every other lease decision in this domain is.
  So the state a sender is told and the queue's own decision to hand the
  message out again cannot disagree, and no instance's clock decides
  either.
- **Expiry is the message's absence, not a state.** The sweep deletes an
  expired row whole and nothing else deletes one, so a sender holding
  the `201` that is later answered `not_found` has been told the message
  expired. An `expired` state on a living row would name something the
  queue does not act on: the claim predicate filters on completion and
  on attempts and not on age, so a message past the window that the
  sweep has not reached yet is still handed out. `createdAt` is served
  and `RetentionWindow` is a documented constant, so how close a message
  stands to the window is arithmetic the sender can do.
- **A message another identity sent, and a message that never existed,
  are the same answer.** Three cases collapse into `not_found` — never
  stored, not the caller's, swept — so asking teaches a caller nothing
  about the queue outside its own messages. The identifier is opaque and
  minted by garam, so a caller that did not send the message has no
  standing reason to hold one; that is why this route refuses as
  `not_found` where ADR-0053's operator route refuses as
  `permission_denied` over a path naming an agent GRN a caller may
  legitimately know and not hold. Both answers withhold the same fact.
- **The read never carries the body.** A completed message has none —
  ADR-0034 drops it at completion — so the state a sender most wants to
  reach could never carry one, and a field present in some states and
  absent in others makes its absence carry two meanings, which is what
  migration `000014` refused a sentinel to avoid. The body is also the
  sender's own and the only large field, at up to 256 KiB, and this read
  is polled like everything else on this queue.
- **The answer is what garam knows and the sender does not**: the
  queue's identifier, the state, how many attempts have been spent, and
  when garam stored the message. The recipient, the idempotency key and
  the body were the sender's own submission, and echoing them back is a
  second copy to keep true.

## Consequences

- A sender can tell a message nothing has looked at from one a reader is
  working on, and a delivered one from one the queue has given up on.
  That is the basis two agents need to coordinate, and it arrives
  without garam promising to deliver anything.
- **It does not discharge ADR-0018's obligation.** That decision says
  garam tells a sender at exhaustion; a sender that never asks is still
  told nothing. `messaging.md` keeps its rule 9 marker and
  `overview.md` keeps the entry, both narrowed to what is still absent:
  the announcement, not the fact.
- A sender that wants to know polls, and each poll is one primary-key
  read with the sender compared on the row it finds. No index is added.
- The refusal a cross-organization post is answered with is not the
  caller's own message — its sender is the recipient that could not be
  reached — so its identifier answers `not_found` on this route. That is
  right: the refusal is addressed to the caller and arrives on the
  caller's own queue, and there is nothing about it left to track.
- Nothing is added to the browser-facing listener. A person reading a
  machine identity's queue is a different question, with a different
  admission, and no one has asked it.
- A sender that loses the identifier the `201` carried has no way back
  to the message but to re-post the same idempotency key, which is
  answered with the stored message and its identifier.

## Rejected alternatives

- **Tell the sender: post a message back when one is exhausted or
  expires.** This is ADR-0018's own shape and it is the larger thing.
  garam would have to decide the moment a message is dead, and nothing
  in this domain runs on a clock — the sweep rides an enqueue, so a
  queue nobody writes to is a queue nothing happens in. It would address
  a report to a sender that may be a channel garam holds no row for, and
  store a second message whose own delivery can fail, leaving a report
  about a failure that is itself reported by nothing. The read is what a
  sender needs either way, and it does not foreclose the report.
- **A callback or a webhook.** garam opens no connection to a caller
  (ADR-0012, ADR-0013). Building one for this would reverse the
  direction the whole machine surface is built in, for one signal.
- **Serve the body back.** §Decision. A read that carries the body
  answers differently for a completed message than for a pending one,
  and there is no state in which the sender does not already hold it.
- **Add the state to `Message` and answer the post with it.** Every post
  would answer `pending`, so the field would tell a poster nothing, and
  the read would have to answer with `body` present and empty — which is
  a body a sender may legitimately send, so the state would not be
  readable from the shape.
- **List a sender's messages.** A larger surface than the question: an
  ordering, a page cursor and a bound, and an enumeration of a queue
  where one lookup was asked for. It earns its place when a sender that
  has lost the identifiers it posted under is met more than once.
- **Read by the sender's own idempotency key rather than by the queue's
  identifier.** The key survives a `201` the sender never received,
  which the queue's identifier does not. It also puts a caller-authored
  string of no fixed shape in the path, where the queue's identifier is
  opaque and garam minted it; and re-posting the key already answers
  with the stored message, so the identifier is recoverable without a
  second way to name a message.
