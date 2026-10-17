# ADR 0108: A reply answered outside its contract is offered again on a bounded schedule, and a refusal on its merits is not

> Status: accepted
> Date: 2026-10-18

Drafted for #1224, which reserves this number. The direction that
the refusal is a "later" case routed through the forwarder's existing
backoff, told apart by a type and not by text, is the owner's, carried
in the PM's dispatch of 2026-10-18. Facts about garam hold at
`e457cbc83666492d08909e14936a209d7ad35db1`.

## Context

The adapter's forwarder holds two memories for this process alone
(`internal/delivery/forward.go`, `forwarding`):

- **Set aside**: the entry is not offered again until its file is
  replaced.
- **Later**: the entry is offered again at a time, after its own
  backoff, which doubles from the adapter's retry delay up to its cap
  (`DefaultMaxRetryDelay`, one minute).

Only `400 out_of_range` went to the second. Every other answer that
was not an acknowledgement, the fence or no answer was set aside. That
included what `HTTPQueue.Post` reads as an unsupported peer: an answer
without `Garam-Contract-Version` naming the contract the adapter
posted under, which is how a garam that predates
`POST /outbox-messages` answers (its 404), and how one serving only
another version answers.

In the #1196 run 7 upgrade test (#1157), the adapter ran at N while
garam was at N-1. A legacy agent's reply was answered outside
`durable-delivery.v1` and set aside. After garam was upgraded, the
running adapter never offered it again, and it was forwarded only when
the Pod restarted. Nothing was lost, and the reply waited for an
unrelated restart for an unbounded time.

An unsupported peer is a fact about garam at the moment of the call,
not about the entry. ADR-0079 keeps adjacent releases compatible in
both upgrade orders, so the order that runs the adapter first is one
it has to carry. Every other refusal is about the entry: its key, its
budget, its agent, its channel, its content or its target.

## Decision

**A reply garam answered outside the contract is offered again on the
forwarder's per-entry backoff; a refusal on the entry's merits is set
aside as before.**

- **The classification is the type `HTTPQueue.Post` already returns.**
  An unsupported peer is `ErrUnsupportedPeer`, read with `errors.Is`
  where the forwarder acts on the answer. No message text is read.
- **It goes to "later", not to a third memory.** The entry is offered
  again after its own wait, which doubles up to the cap. So once garam
  serves the contract, the running adapter forwards the entry within
  one cap and one outbox interval, under the same key and creation
  instant.
- **The schedule is bounded by its interval, not by a count.** While
  garam does not serve the contract, each such entry is offered at
  most once per cap. Each offer is logged as a warning by its
  identifier.
- **Merit refusals are set aside unchanged**: `410
  deadline_exceeded`, `409 already_exists`, a stored message that does
  not acknowledge the entry, any other definite refusal, and every
  reason the entry is not this adapter's to offer.

## How this changes ADR-0083

ADR-0083's decision stands, and so does its status. The route, the
budget, the table of statuses and the absence of a fallback are
unchanged. What changes is what the adapter does with one row of that
table, and with the answer the table's header rule makes
unrecognisable:

- **404 from a garam without the route** was read as final. It is now
  read as `out_of_range` already was: the entry is kept, and a later
  attempt may be admitted.
- **An answer without the contract's header**, whatever its status, is
  read the same way.

The 410 row still means no later attempt changes the answer. That is
also what bounds the retries in time: once garam serves the contract,
an entry past its seven-day budget is answered 410 and set aside.

## Consequences

- An adapter upgraded before garam forwards its replies once garam is
  upgraded, without a restart.
- A garam that never serves the contract is offered each such entry
  about once a minute, for as long as the process runs. This is the
  same rate an outage is retried at, and nothing is stored.
- No fallback to `POST /messages` is added, and nothing about an
  entry is rewritten before it is offered again.
- The memory stays bounded by the outbox. An entry put off is
  forgotten once its file is gone, as before.

## Rejected alternatives

- **Matching the refusal's text.** Text is not part of the contract,
  and ADR-0083 kept statuses and kinds apart so the adapter would not
  have to read it.
- **A third memory with a schedule of its own.** It would duplicate
  what "later" already does: a time to offer again and a capped wait.
- **Ending the pass and backing off the whole outbox**, as when garam
  does not answer. That backoff exists for an answer that never came,
  and this one did come. The owner's direction routes the case through
  the per-entry "later".
- **A retry count after which the entry is set aside.** It brings back
  the restart as the only way out, after a delay instead of at once.
