# ADR 0079: Interservice contracts keep adjacent releases compatible and replay by stable identifier

> Status: accepted
> Date: 2026-10-04

Decided by the PM under the owner's explicit delegation of 2026-10-04,
recorded in #1155 (D9 and its contract table). Facts about Sherlock
hold at `07aa5c4`.

## Context

garam's two specifications promise nothing across versions
(ADR-0049), so garam, the Operator and Sherlock can only be upgraded
together. ADR-0077 separates them into services with their own owners
and release cycles, which a lockstep upgrade defeats.

Delivery is at-least-once (ADR-0018). The adapter pairs garam's
message with the identifier Sherlock's gateway mints on accepting it
and holds the pairing in memory (ADR-0018), and that accept answers
before any turn runs
([`internal/gateway/handler.go`](https://github.com/garamsh/sherlock/blob/07aa5c4/internal/gateway/handler.go#L71)).
So a redelivery arrives as a new message, and a completed turn can run
again. ADR-0013's adapter forwards replies with durable retry through
an outbox, which nothing has built.

## Decision

**Each interservice contract is versioned and small, and a supported
release works with the adjacent supported release of each peer in both
upgrade orders.** The contracts are: console to Operator, managed
creation to garam, controller to Operator, standalone bootstrap,
activation and delivery (ADR-0078), the effective configuration report,
adapter to Sherlock, and a runtime's reply to garam.

- **The window is tested, not stated.** Each adjacent pair runs in both
  upgrade orders before a release claims it.
- **Unsupported semantics are refused, naming what is unsupported**,
  and never degraded silently. Compatibility past the adjacent release
  is not offered.
- **A timeout is an unknown outcome.** The caller reads the resource
  back before retrying a mutation, and an edit carries the revision it
  expects.
- **The adapter gives Sherlock a stable, sender-scoped message
  identifier.** A replay with the same identifier and an identical
  payload answers the recorded outcome of a completed source instead of
  running another turn; a different payload under it is refused.
  Completion is reported separately from the accept.
- **A reply leaves through a durable outbox.** The runtime records it
  under a stable identifier; the adapter forwards it with that
  identifier as garam's idempotency key and removes the entry only
  after garam acknowledges it. A retry after a lost answer mints no
  second message, and a failed forward keeps the entry and shows it
  pending or failed.
- **A restart does not strand a reply.** The next legitimate generation
  forwards entries an earlier one left under its own current
  credential and activation token (ADR-0078), keeping each entry's
  original identifier and the generation it came from.
- **No effect is exactly-once.** A turn interrupted before it committed
  may repeat what it did.

## What this does not decide

- Version numbering and how a release names its window. The artifact
  defining each contract carries that once built.
- The specifications' text. Each still states that its `version`
  promises nothing (ADR-0049) until a release carries a tested window
  for the contract it defines; the pull request that builds the test
  changes that sentence.

## Consequences

- **None of this is built.** No contract is versioned, no skew test
  runs, the adapter holds the gateway's identifier in memory, and no
  outbox exists.
- ADR-0049's "promises nothing" is superseded for these contracts, from
  the first release whose window is tested.
- ADR-0018's in-memory pairing and its rejection of a caller-supplied
  key are superseded; its completion rule and at-least-once delivery
  stand.
- Every contract change owes a skew test, which is the price of
  independent upgrades.

## Rejected alternatives

- **Lockstep releases.** Cheaper at first and incompatible with
  separately owned services.
- **Indefinite compatibility.** It keeps a branch per old release
  forever.
- **Deduplicating on the gateway's minted identifier.** A redelivery
  never carries it, so it cannot recognise one.
