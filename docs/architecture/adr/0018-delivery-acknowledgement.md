# ADR 0018: A message is completed by the turn-end event that names it

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

ADR-0013 made garam own idempotency and reconciliation, and said
progress is inferred from replies arriving. It also decided that a
message is fire-and-forget and a reply an independent event, with
nothing pairing them. Those two together leave no per-message
completion signal at all, which is the gap this closes.

The facts below come from the agent platform's source at `gagent@40ef7ac`,
checked rather than inferred, and each was found by someone holding it
beside a fact of ours rather than by reading either alone. That
repository carries no tags, so a commit is the only anchor available —
and without one a citation goes on looking valid after it has lost what
it pointed at, which is worse than a vague claim because the precision
discourages checking:

- The gateway's inbox is an in-memory buffered channel of 64. A 202
  therefore means a message was accepted into a bounded volatile
  buffer, not that it will be processed; a restart drops what is in it
  and emits nothing.
- The 202 body returns the identifier the gateway minted. A caller
  cannot supply one, so a redelivered message arrives as a new message
  and nothing collapses the two.
- `turn_finished` and `turn_failed` carry `message_ids`: the messages
  that turn drained. Only terminal events carry them, one per turn.
- The monitoring stream drops events for a subscriber more than 32
  behind, while it is still connected, with nothing marking the gap.
- A failed turn logs and continues. Nothing reaches the sender.

Certificate rotation is a restart on both sides (ADR-0014, ADR-0015),
so a restart is scheduled rather than exceptional. That is what turns
the first fact from a crash-loop curiosity into a delivery property.

## Decision

- **A claim is completed by a `turn_finished` naming its message, and
  nothing else completes it.** A message whose completion never
  arrives — because the turn failed, because the event was dropped,
  because the adapter died — is redelivered. Both halves belong in one
  sentence wherever this is written down: the first alone reads as
  exactly-once, since the pairing is deterministic and so looks like a
  guarantee, and the drop rule is the only reason it is not.
- **The adapter learns the mapping at POST time and holds it in
  memory.** garam's identifier to the gateway's, from the 202 body.
  That volatility is the mechanism rather than a flaw in it: a restart
  loses the mapping, the leases lapse, and the messages come back.
- **Delivery is at-least-once, from two independent causes.** A
  restart, and a dropped terminal event for a turn that did complete.
  Only the first has a window anyone can state; the second depends on
  how far behind a consumer fell.
- **A turn that fails is an attempt rather than a completion, and
  attempts are bounded.** From outside, a turn that failed because a
  model provider was unreachable is indistinguishable from one that
  failed on the message itself — and `turn_failed` rides the same
  droppable stream, so its absence is indistinguishable from a turn
  that never started. The design never has to tell those apart:
  success completes, everything else retries, and an attempt count is
  what stops a deterministic failure looping forever. At exhaustion
  garam tells the sender, because the platform does not — a message
  that stops without anyone learning why is the failure this project
  keeps finding.
- **The adapter renews its lease while a turn is in flight, under a
  ceiling.** The ceiling is a backstop for a lost terminal event, not a
  prediction of how long a turn takes: it only has to exceed a normal
  turn, and it replaces a worst-case duration the adapter cannot see,
  set by model and tool timeouts it does not configure. Without it a
  missed event renews forever and produces a claim that is neither
  completed nor redelivered — a third state, and the least visible of
  the three.
- **Consuming and completing use separate subscriptions.** A slow
  consumer must not degrade the signal that decides what it completed,
  which is the same reasoning ADR-0013 used to keep readiness on a
  subscription of its own.
- **The contract names the platform's drop rule as a dependency it
  rests on.** This promotes an observability surface to a delivery
  contract, and the two carry different obligations — observability may
  lose an event, a delivery contract may not. Naming it makes the
  buffer size contract-bound in a repository that would otherwise have
  no reason to know anyone depends on it.

## Consequences

- An agent may see the same message twice. Whether that is acceptable
  is the platform's decision and it is open; if it comes back no, the
  change here is completing on the 202 instead, and everything else
  stands.
- "Tolerating a repeat" cannot mean the platform collapses duplicates.
  It mints the identifier, so a redelivery is a new message — the
  repeat is handled the way a person saying the same thing twice is
  handled, or not at all.
- Rotation period is a message-loss budget as well as a disruption
  budget, and a termination grace period long enough to finish the
  in-flight turn does not drain the queue behind it.
- garam gains a notification path it did not have: a message exhausted
  by repeated turn failure has to reach its sender, and nothing in the
  queue does that today.
- The queue gains an attempt count and a lease renewal. Neither exists;
  it has `Enqueue`, `Claim` and `Complete`.
- The adapter has to outlive the agent process during a shutdown, or a
  graceful restart loses the last turn's completion and redelivers
  messages that succeeded. That makes it a native sidecar rather than
  an ordinary container, since the kubelet postpones terminating a
  sidecar until the main container has stopped — and it makes the
  termination grace period budget the adapter's completion work on top
  of the agent's drain, because a sidecar that receives its signal only
  after the grace period is spent is killed before it can complete
  anything.
- A dependency on a foreign constant is now load-bearing. If it moves
  and nobody tells us, the symptom is duplicates rising rather than an
  error.

## Rejected alternatives

- **Completing on the 202.** One line, and it is at-most-once: every
  rotation silently drops what is in the buffer. It stays the fallback
  if repeats turn out to be unacceptable, and it would be a decision
  taken rather than a default arrived at.
- **Completing when a reply arrives.** The shape first proposed here.
  ADR-0013 pairs nothing, one turn takes up to eight messages, and a
  turn that answers one of them would complete all eight — and a
  message that produces no reply would never complete at all.
- **Asking the platform for a caller-supplied idempotency key, or a
  durable inbox.** Both are real answers and neither is ours to ask
  for now: the first is a different request from duplicate tolerance,
  and the second is a component that platform does not have, with its
  internals frozen behind a decision of its own.
- **Predicting a worst-case turn duration and setting the lease to
  it.** Requires a number the adapter cannot see, and it is exactly the
  kind of number that goes stale without anything failing.

## Errata

- 2026-08-24 — §Context's "Certificate rotation is a restart on both sides (ADR-0014, ADR-0015), so a restart is scheduled rather than exceptional" lost its scheduled cause to [ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md), which rotates certificates without one. The delivery property stands on the restarts that remain ordinary — a rollout, a crash, a node moving — and at-least-once delivery rests on the dropped event as much as on either.
