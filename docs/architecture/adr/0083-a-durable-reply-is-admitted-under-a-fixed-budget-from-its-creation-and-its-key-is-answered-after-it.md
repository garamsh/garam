# ADR 0083: A durable reply is admitted under a fixed budget from its creation, and its key is answered after it

> Status: accepted
> Date: 2026-10-05

Drafted for #1185, the server half of #1169. The route, the header,
the seven-day budget and the absence of a fallback were decided in
#1169 (comments of 2026-10-04 at 17:46 and 18:07 UTC); the refusal
statuses were pinned by the PM for #1185 on 2026-10-05.

## Context

An agent's adapter forwards each durable outbox reply to garam and
removes the entry only once garam has acknowledged it, retrying the
same entry under one idempotency key until then (ADR-0079). garam
suppresses a duplicate only while the key's row exists, and the sweep
deletes an undelivered row thirty days after it was stored (ADR-0034).
A sender that retries past that point stores the message a second
time, so the retry horizon has to end inside the retention.

The adapter alone cannot hold that line. Its producer stamps the
entry with its own host's clock, a request can wait an unbounded time
between leaving the adapter and reaching the insert, and an older
garam that took the request on `POST /messages` would apply no bound
at all. Insertion and expiry already run on the database's clock
(#1181).

## Decision

**`POST /outbox-messages` stores a message only while the entry's
creation instant is inside a fixed seven-day budget on the database's
clock, and answers a key that already stores one at any time.**

- **A versioned route of its own.** It requires and answers
  `Garam-Contract-Version: durable-delivery.v1`. It is the same message
  write as `POST /messages`, which is unchanged, and is not a second
  queue. There is no fallback: a garam that predates the route answers
  404, as for any path it does not serve, and stores nothing.
- **The body adds `createdAt`**, the entry's immutable creation instant
  as RFC 3339 text; the idempotency key is the entry's opaque
  identifier and the body its content, both unchanged.
- **The budget is part of the contract.** A new message is stored only
  where `createdAt` is no later than the decision instant and
  `createdAt` plus seven days is later than it. No setting changes the
  seven days, and an upgrade never moves an entry's cutoff.
- **The decision instant is the insert's.** It is `clock_timestamp()`,
  read once inside the inserting statement, after the execution
  fence's lock wait (ADR-0080), so a delayed or contended request is
  judged when it writes. The row's receipt time is that instant;
  `createdAt` decides admission and is not stored, so no migration and
  no deadline column is added.
- **The key is read before the budget.** Whatever the key stores is the
  answer, after the cutoff too, including a completed message whose
  body was dropped (ADR-0034). A key storing a message to another
  recipient, or with another body while it still holds one, conflicts.
  A cleared body cannot be compared, so the key and recipient bind a
  retry to it.
- **Policy, consent and the execution token are `POST /messages`'s.**
  The cross-organization refusal is stored through the same budgeted
  write, and its answer has the intended recipient as sender and the
  caller as recipient, which is how a caller tells it from its own
  message.

Every answer carries the header. The statuses:

| Status | Kind | When |
|---|---|---|
| 201 | — | the message the key stores, stored now or earlier, or the cross-organization refusal |
| 400 | `invalid_argument` | no or another contract version, naming `durable-delivery.v1`; a malformed body, including a missing `createdAt` or one that is not RFC 3339 |
| 400 | `out_of_range` | `createdAt` later than the decision instant; the entry is kept and a later attempt may be admitted |
| 410 | `deadline_exceeded` | the key stores nothing and the budget has run out; no later attempt changes it |
| 409 | `already_exists` | the key stores a different message |
| 401, 403, 409 `failed_precondition` | as `POST /messages` | the execution fence's refusals (ADR-0080) |
| 404 | — | a garam without the route |

The 410 is chosen by the expiry's own error rather than by its kind,
whose shared mapping is 504, so a timeout never reads as expiry.

## Consequences

- Every message this route admits has at most seven days of retries
  left and is retained at least thirty days from its receipt, so 23
  days separate the last admissible attempt from the earliest expiry.
- An adapter can tell acknowledgement, clock skew, expiry, a
  conflicting key, a fence refusal and an unsupported peer apart from
  status and kind alone, without reading message text.
- A producer clock running more than seven days behind the database's
  makes its entries expire on arrival, and one running ahead delays
  them until the database's clock passes; neither extends a stored
  row's horizon.
- Deduplication is not promised across a loss of database history or
  an arbitrary reset of the database's clock, and no effect is
  exactly-once.
- Changing the budget is a new contract version.

## Rejected alternatives

- **A client-only margin under the retention.** Smaller, but it rests
  on the producer's clock and on no request being delayed, which are
  the failures this exists for.
- **A deadline column.** It would duplicate a pure function of
  `createdAt` and a constant, and need a migration.
- **A configurable or much longer budget.** It adds contract states
  and narrows the separation from the retention, with no product need
  shown.
- **`now()` as the decision instant.** It is the transaction's start,
  before the fence's lock wait, so a delayed request would be judged
  early.
- **Answering expiry 409 `failed_precondition`.** The fence answers
  that, and the adapter would have to read message text to tell them
  apart.
- **A version echo on `POST /messages`.** An older server would perform
  the unbounded write before the caller could read the echo.
