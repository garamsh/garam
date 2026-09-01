# Messaging

The durable queue a message reaches an agent through (ADR-0013), and
the consent that lets one cross an organization boundary (ADR-0035).
Implemented by `internal/message`.

**garam announces nothing to a sender, and one of the two reports it
owes is built.** It answers a post no consent relation carries across
its boundary with the refusal message below, and announces nothing
else: a message no agent claims, or one that spends its attempts, sits
until the thirty-day sweep deletes it and nobody is told. The
announcement at exhaustion is garam's own obligation, taken because the
platform makes no such report itself (ADR-0018), and it is unbuilt —
[overview.md](./overview.md) §Out of scope (today) records it. What a
sender can do instead is ask, which it could not before: it reads the
state of a message it sent, described below (ADR-0055).

| Type | Responsibility |
|------|----------------|
| `Message` | one message addressed to a recipient, under an identifier of the queue's own |
| `Claim` | one reader's hold on a message, and the token that fences it |
| `Status` | how far one message has got, as the sender that posted it may observe it (ADR-0055) |
| `Consent` | one pair of organizations and the two halves of their cross-organization consent (ADR-0035); it stands while both halves do |
| `Config` | the delivery parameters: lease, ceiling, attempts, and how long a claim waits |
| `Service` | what a machine identity does to its own queue: post, claim, renew, complete, and read the state of what it sent |
| `ConsentStore` | the consent relation: an organization granting or withdrawing its own half, and the send path asking whether both halves stand |
| `ActionSend` | the action a policy names to decide what one identity may send to another |

Messages are their own domain rather than part of the agents one: a
recipient is addressed by GRN, so nothing here depends on the agent
aggregate, and a message has a lifecycle no agent's contains. A message
may cross an organization boundary only where a relation permits it —
one row per consenting pair, held by both organizations and withdrawable
by either (ADR-0035); the refusal reaches the sender as an ordinary
inbound message, never a rejection. Each half is the stamp its side's
last grant wrote: a row is created by the first grant, stands when the
second lands, and is deleted when neither half stands, so the table
holds live consent only. Both organizations of a pair must exist, which
the schema holds by reference rather than by a check a caller could
forget, and an organization that is deleted takes its halves with it.
Granting and withdrawing is the browser-facing listener's surface, under
the org's path; this domain owns the relation itself.

The queue is a table and never a connection, so any garam instance can
hand any recipient's message to a reader and no process holds anything
about a recipient. A claim is exclusive for a lease and fenced by a
token: two readers never hold one message, and a reader that dies leaves
it claimable again once the lease runs out rather than stranded. The
token is what makes that safe — the next claim replaces it, so the
reader it replaced completes nothing. Postgres measures the lease, so no
instance's clock decides when a claim lapses.

Duplicate suppression is garam's (ADR-0013): a message is stored once
per sender and idempotency key, and the key outlives delivery so a
retry arriving afterwards neither stores a message nor requeues the
delivered one. Retention is for space, not for not-keeping
(ADR-0034): a completed message keeps that small row and drops its
body at completion, an undelivered message expires after thirty days,
and a submission's body is bounded at 65536 characters, which UTF-8
spends up to 256 KiB on — so the table's growth is a rate a volume can
be sized against. Neither the recipient nor the sender carries a foreign
key — garam answers a sender once the message is stored and reports a
routing failure back as a message rather than rejecting one, so a
message it cannot yet route has to be storable. `Service` keeps that
rule where a caller meets it: posting checks that the recipient is a
GRN and never that it names an agent garam knows. A recipient that is
not a GRN is refused, which is where the line falls: ADR-0013 rejects
nothing because a refusal leaves the sender's reply in an outbox
nobody drains, and that argument is about a reply garam could have
delivered — an address that does not parse is a broken caller rather
than a routing failure, and a queue keyed by GRN could never hand the
row to anyone.

Handing a message out is an attempt, and attempts are bounded: a turn
that failed and a turn that never started are indistinguishable from
here, so what ends a message that fails the same way every time is the
count (ADR-0018). A holder renews its claim while it works, and
renewal carries the claim no further than a ceiling measured from when
it was taken — a renewal reaching past that is held at it, so a holder
that renews forever loses the claim rather than leaving a message
neither completed nor redelivered.

A sender reads how far one message it sent has got, and reads no
other's (ADR-0055). The four states are the ones the row already
distinguishes: waiting for a reader, held by one whose lease stands,
reported delivered, or handed out no further because the attempts are
spent. Nothing was added to record them — the columns the claim and the
completion write are what the read serves — and the state rests on the
same lease Postgres measures and the same attempt bound the claim
filters on, so what a sender is told and what the queue will do next
cannot disagree. There is no state meaning expired: the sweep deletes
an expired message whole and nothing else deletes one, so absence is
the answer, and a message past the window the sweep has not reached is
still handed out. The read carries no body — a completed message has
none — and neither the recipient nor the key, which the sender
submitted.

`Service` is what a machine identity does to its own queue, and every
method between posting and the read above takes the calling identity
beside the recipient whose queue it names. One comparison decides it: a
GRN carries its organization, so refusing a caller that is not the
recipient refuses another organization's agent by the same line, and no
path reaches a message around it. The read is bounded from the other
side by the same kind of comparison: the caller is the sender the row
is matched on, so an identifier alone reaches nothing. Whom a sender
may post to is not decided here — it is a policy question the machine
listener asks (ADR-0024). What this domain owns of it is the action
constant a policy names, because it is the domain that defines sending.
A claim waits for a message rather than answering
an empty queue at once, and it waits by re-reading the queue: a
message becomes ready when a lease runs out as well as when a sender
stores one, and a lease running out is a moment passing that nothing
announces. The interval is the delivery latency ADR-0013 accepted in
place of a push.

The HTTP surface is the machine listener, and the reader on the other
side of it is the adapter garam ships ([adapter.md](./adapter.md)).
The retention above is this domain's own work rather than a schedule a
deployment carries: the sweep rides the enqueue, so a queue being
written to is a queue being swept (ADR-0034).
