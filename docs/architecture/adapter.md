# Adapter Contract

What the adapter garam ships must do, and what it rests on to do it.

## Contents
- What it is
- The delivery loop
- Completion
- Failure and attempts
- The lease
- Watching
- Subscriptions
- What garam owns
- Foreign facts this rests on
- Not decided

## What it is

garam is a message API and an agent platform speaks a different
protocol. The adapter is the piece between them: it long-polls garam,
posts into the platform's gateway over loopback, watches the gateway's
event stream, and reports back. garam writes it as a library with a
thin binary and an orchestrator deploys it
([ADR-0013](./adr/0013-agent-messaging.md)); we own its correctness and
they own its placement.

It runs beside exactly one agent, in that agent's pod. It has to,
because the gateway binds loopback unless mTLS is configured and
because it must outlive the agent process during shutdown — a native
sidecar rather than an ordinary container, for the reason in
[ADR-0018](./adr/0018-delivery-acknowledgement.md).

It reaches garam as the agent, with the certificate garam issued for
that agent, and verifies garam's listener against the authority that
signed it: the listener presents a certificate of that same
per-organization authority, so the root an adapter needs is already in
its hands ([ADR-0014](./adr/0014-agent-authentication.md),
[ADR-0037](./adr/0037-the-machine-listener-presents-a-per-organization-certificate.md)).

## The delivery loop

1. Claim a message for its agent from garam's queue. The claim carries
   a lease.
2. Post it into the gateway. The response carries an identifier the
   gateway minted; hold the pairing of that to garam's own, in memory.
3. Watch the gateway's event stream for a turn ending.
4. When a turn reports success and names a message, complete that
   message's claim at garam.

Nothing about this loop is transactional and nothing recovers it across
a restart. That is deliberate: the in-memory pairing is what makes a
restart lapse the lease rather than lose the message.

## Completion

**A claim is completed by a turn that succeeded and named its message,
and nothing else completes it. A message whose completion never
arrives — because the turn failed, because the event was dropped,
because the adapter died — is redelivered.**

Both halves belong together wherever this is restated. The first alone
reads as exactly-once: the pairing between a turn's event and the
messages it consumed is exact, so it looks like a guarantee. The event
stream dropping silently is the only reason it is not.

**Delivery is therefore at-least-once, from two independent causes**: a
restart, and a dropped event for a turn that did complete. Only the
first has a window anyone can state.

An agent may see the same message twice. The platform cannot recognise
the repeat — it mints the identifier, so a redelivery arrives as a new
message — so tolerating one means handling it the way a person saying
the same thing twice is handled, not deduplicating it.

## Failure and attempts

A turn that fails is an attempt rather than a completion. From outside,
a turn that failed because a model provider was unreachable is
indistinguishable from one that failed on the message itself, and the
failure event rides the same droppable stream, so its absence is
indistinguishable from a turn that never started.

The adapter never has to tell those apart. Success completes;
everything else retries; an attempt count ends what would otherwise
loop forever. **At exhaustion garam notifies the sender**, because the
platform notifies nobody — a failed turn leaves a line in a log and
nothing else.

## The lease

The adapter renews its claim while a turn is in flight, under a
ceiling. The ceiling is a backstop for a lost completion event, not a
prediction of how long a turn takes: it only has to exceed a normal
turn. Without it, a missed event renews forever and produces a claim
that is neither completed nor redelivered — a third state, and the
least visible of the three.

**The lease and the attempt count are configurable and default narrow.**
The reason is dated, and the date is later than it looks: a repeated
*turn* is unmeasured — an exact repeated fact is idempotent in the
platform's addressed store, but a turn re-reads its input and may
extract differently.

**The reason expires when that idempotency has been measured against
the real model, not when the store merges.** The two are not the same
moment, and the gap between them is the dangerous one: the store lands
in stages, and one of them removes recall before the stage that reads
it arrives. A default widened in that gap is widened over a platform
that answers nothing.

The platform's own maintainers state when it has been measured; this
document does not track their stages, because a foreign issue number
is not something a reader here can check. Ask them, and widen only on
an answer that names a measurement. Whoever widens this default should
find this sentence before they do.

## Watching

**Forward turn events only while garam says someone is watching, and read
that from the claim.** Every claim and every renewal answers with a
watcher count; forward while it is above zero and forward nothing
otherwise ([ADR-0022](./adr/0022-watching-an-agent.md)). garam cannot
dial the adapter, so the poll the adapter already holds is the only
direction the instruction can travel, and the renewal is where a person
who opened the view mid-turn is first seen.

- **A forward is best effort.** garam answers that the event was
  announced and never that anyone saw it, and nothing is stored, so a
  forward that fails is dropped rather than retried — a retry would
  compete with the turn it is describing.
- **Forwarding uses a subscription of its own**, for the reason below: a
  browser that reads slowly must not cost the completion signal an event.
- **An event garam cannot carry whole is refused**, not shortened. It is
  bounded by what one announcement holds, which `api/machine.yaml`
  states; a refusal is a gap, and this stream already permits one.

## Subscriptions

Consuming, completing and forwarding use separate subscriptions. A slow
consumer must not degrade the signal that decides what it completed:
falling behind would drop completion events, which produces
redeliveries, which produce load. The same reasoning keeps readiness on a
subscription of its own in [ADR-0013](./adr/0013-agent-messaging.md), and
puts watching on a third ([ADR-0022](./adr/0022-watching-an-agent.md)).

## What garam owns

Idempotency and reconciliation are garam's, not the adapter's and not
the platform's ([ADR-0013](./adr/0013-agent-messaging.md)). The
platform cannot be asked whether a message was processed, and the
adapter's knowledge is deliberately volatile, so **"has this already
been handled" can only live on garam's side.** A design that puts it in
the pod or in an orchestrator's status is inventing a distinction that
does not exist there.

## Foreign facts this rests on

Named because this contract would be **wrong** if any of them changed,
not because they were mentioned. Read at `gagent@40ef7ac`; that
repository carries no tags, so a commit is the only anchor, and an
unanchored citation goes on looking valid after it has lost what it
points at.

| Fact | Where | What breaks without it |
|---|---|---|
| Terminal events carry the ids the turn drained | `internal/gateway/queue.go` | Completion has nothing to key on |
| The accept response returns the minted id | `internal/gateway/server.go` | The adapter cannot pair its claims |
| The gateway mints the id; a caller cannot supply one | `internal/gateway/service.go` | A redelivery would be recognisable, and it is not |
| A subscriber more than a bounded number of events behind loses events silently | `internal/gateway/events.go` | Delivery would be exactly-once, not at-least-once |
| One turn takes up to a bounded batch as a single input | `internal/gateway/queue.go` | The batch would not be the completion unit |
| The inbox is a volatile buffer | `internal/gateway/service.go` | The accept response would be an acknowledgement |
| A failed turn reaches no sender | `internal/gateway/queue.go` | garam would not owe the notification |

This promotes an observability surface into a delivery contract, and
the two carry different obligations: observability may lose an event, a
delivery contract may not. Naming the buffer here is what makes it
contract-bound in a repository that would otherwise have no reason to
know anyone depends on it.

## Not decided

- A durable inbox on the platform's side is unbuilt and unscheduled —
  treat it as unavailable rather than refused. If it arrives, the
  completion rule does not change; the redelivery causes reduce by one.
- A caller-supplied message identifier was considered and declined: it
  would remove the adapter's pairing, which is a simplification rather
  than a correctness gain, and the pairing is not what makes the design
  work.
