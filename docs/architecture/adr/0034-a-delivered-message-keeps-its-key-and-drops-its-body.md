# ADR 0034: A delivered message keeps its key and drops its body

> Status: accepted
> Date: 2026-08-19

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

`internal/message` stores every message durably and nothing ever
deletes one. The retention is deliberate — a completed row suppresses a
sender's retry, which is how [ADR-0013](./0013-agent-messaging.md)
discharges idempotency — and the growth is unbounded: nothing bounds
the count, and `body` is an unbounded `text` with no `maxLength` on the
submission schema.

What forced the question now is the consumer's storage. The cluster
garam is being deployed to keeps its volumes on node-local disks whose
quotas do not grow in place, so "decide retention later" and "the disk
cannot grow" are incompatible positions, and the sizing question lands
on this repository: how fast does this table grow, and does the rate
converge? With no retention it does not.

**The motive has to be stated before the mechanism, because the two
motives behave differently.** Deletion for *space* is served by the
live volume alone. Deletion for *not keeping a record* is not served by
anything this application does: a deleted row goes on existing in every
backup the deployment keeps, which is a number chosen by infrastructure
this repository does not control. Nothing in this repository obliges
garam to make a message unrecoverable, so no such obligation is
invented here.

## Decision

The motive is **space**, stated so a later reader does not assume a
privacy property that was never built. Then:

- **A completed message keeps its identity and drops its body.** The
  row — sender, recipient, idempotency key, timestamps — stays, so a
  retry arriving after delivery is still suppressed exactly as
  ADR-0013 requires. The body, the only large field, is dropped at
  completion. Duplicate suppression never weakens; the growth that
  remains is a small fixed row per message.
- **An undelivered message expires.** A message no reader claims within
  a generous window is deleted whole. The window must exceed the retry
  budget a sender operates under, because a deleted row cannot suppress
  a retry — the budget is a property of the senders, and the window
  names it: thirty days.
- **A body is bounded at submission.** The submission schema gains a
  `maxLength`, so a single message cannot be unbounded either. The
  bound is 64 KiB — well past any plausible agent turn, small enough
  that the table's worst case is arithmetic.
- A sweep in the process that owns the queue performs both deletions.
  No cron, no operator action, nothing the deployment must remember.

## Consequences

- The table's growth converges: bounded bytes per message, and only the
  small rows persist past delivery. The consumer can size a volume
  against a rate, which is the answer the sizing question needed.
- **"Deleted" means deleted from the live volume, and nothing more.**
  A deployment that keeps N days of backups holds every row for N more
  days, and this ADR makes no claim against that. If a privacy
  guarantee is ever wanted, it is a new decision that must include the
  backup window — cheaper built into backup design than retrofitted.
- A completed message cannot be re-read — its body is gone. Nothing in
  the model permits re-reading after completion today, so this changes
  no contract; it makes the end of the contract visible in the schema.
- `docs/architecture/domains.md` records the lifecycle.

## Rejected alternatives

- **Age-based expiry of whole rows only.** Simplest, and it caps the
  table by time rather than by size: a busy month still grows without
  bound within the window. The body-drop is what bounds the bytes.
- **Retain everything and call messages the audit record.** Nothing
  reads them back; an audit record nobody can query is storage, not
  audit.
- **Delete nothing until the store decision lands.** The store question
  (this deployment's volume mechanics) is exactly what the unbounded
  table was blocking; waiting inverts the dependency.

## Errata

- 2026-08-26 — §Decision's "A body is bounded at submission" gives the
  bound as **64 KiB**. The bound is **65536 characters**:
  `maxLength: 65536` in `api/machine.yaml` counts characters, and
  `internal/message/message.go` tests `len([]rune(body)) >
  MaxBodyLength` against the same number, with its own comment naming
  the unit. UTF-8 spends up to four bytes per rune, so the worst case is
  **256 KiB** rather than 64 KiB, and a body in this project's own
  conversation language spends three — roughly 192 KiB. The decision
  stands: a bounded body is what makes the table's worst case
  arithmetic, and it remains arithmetic. What was wrong is the figure
  that arithmetic was performed on, which was out by up to four times.
  The same statement in `api/machine.yaml` and
  `docs/architecture/domains.md` is corrected separately (#565, #564).
