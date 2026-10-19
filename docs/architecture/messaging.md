# Messaging

The durable queue a message reaches an agent through (ADR-0013), the
consent that lets one cross an organization boundary (ADR-0035), and
the signature garam puts on every message it hands out (ADR-0101).
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
| `Fence` | what an agent's call presents to the agent execution fence: the agent, the serial and dates of the token its request presented, and its execution token's hash (ADR-0080, ADR-0100) |
| `Service` | what a machine identity does to its own queue: post, post a durable reply within its budget, record a post a policy denied, claim, renew, complete, and read the state of what it sent |
| `ConsentStore` | the consent relation: an organization granting or withdrawing its own half, and the send path asking whether both halves stand |
| `ActionSend` | the action a policy names to decide what one identity may send to another |
| `SubmissionBudget` | how long after its creation a durable reply may still store a message, fixed by `durable-delivery.v2` (ADR-0083) |
| `Author` | who wrote a message: its sender, or garam for the refusal it writes itself — what a signature names as the sender garam vouches for (ADR-0101) |
| `Keyring` | the keys messages are signed with: opens the one that signs now, before a claim's wait, and answers the published set (ADR-0101) |
| `Signer` | signs one hand-out under the key the keyring opened, inside the transaction that claims it (ADR-0104) |
| `Custodian` | holds a signing key's private half and never hands it back: makes a key, and signs under one it holds — sealed into its row under the key-encryption key, or a version of an OpenBao Transit key (`internal/message/transit`) (ADR-0104) |
| `Holding` | where one key's private half is held: a `SealedKey` or a `TransitVersion`, and nothing else |
| `SigningKeyStore` | the keys messages are signed with, read with the database's clock, rotated under one lock, withdrawn at once |
| `Issuer` | the deployment's issuer: the machine listener's external https origin, compared byte for byte |

Messages are their own domain rather than part of the agents one: a
recipient is addressed by GRN, so nothing here depends on the agent
aggregate, and a message has a lifecycle no agent's contains. An
agent's post, claim, renewal, completion and status read run the
agent's execution fence first, inside the queue's own transaction, so a
superseded generation or lineage writes nothing even on a connection
already open. The fence is the `agent_execution_admit` database
function the agents domain declares; this domain calls it and reads its
answer into its own refusal, and imports no package of that domain
(ADR-0080,
[machine-identity.md](./machine-identity.md) §Generation and lineage).
A message
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
instance's clock decides when a claim lapses. It also stamps when a
message was stored, so the retention window below starts and ends on
the clock that measures it.

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

A durable reply is posted under a horizon garam enforces rather than
one the sender keeps
([ADR-0083](./adr/0083-a-durable-reply-is-admitted-under-a-fixed-budget-from-its-creation-and-its-key-is-answered-after-it.md)).
It carries the instant its outbox entry was created, and a new message
is stored only while that instant is no later than the database's
clock and less than seven days behind it, judged in the inserting
statement after the execution fence's lock wait, so a request delayed
on its way is judged when it writes. That instant is the row's
receipt time; the creation instant decides admission and is not
stored. A key that already stores a message is answered at any time,
a completed one with its body dropped included, and one storing a
message to another recipient, or another body while it holds one,
conflicts. Every message admitted this way has at most seven days of
retries left and thirty of retention, so the sweep never reaches a key
a retry could still need; past the budget, a key that stores nothing
never stores one. The cross-organization refusal of such a reply is
written under the same budget.

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

**Every send garam decides is recorded, with the outcome and never
with a body**
([ADR-0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)).
The store writes the send's event through the audit domain's writer
inside the transaction that writes the outcome, so a message write
runs in a transaction whether or not a fence admits it: an authorized
post's event names the message its key stores, a retry included; a
crossing no consent carries records a refused send with no message,
beside the refusal garam writes back; and a post the policy denies,
which stores nothing, records its refused send alone, after the fence.
A durable submission the budget refuses, or whose key holds another
message, records nothing, and its sweep still commits. The record is
the audit domain's and outlives this one's retention
([audit.md](./audit.md)).

**Every hand-out is signed**
([ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)).
A claim answers the message's content — `garam-message.v1`, which garam
now encodes itself, so every attempt of one message carries the same
characters — and a compact ES256 JWS binding the issuer, the sender
garam vouches for, the recipient as audience and its organization, the
message's identifier and a digest of the content, valid for the lease
ceiling from when the claim was taken on the database's clock. The key
is opened before the queue is asked, and the claim is signed inside the
transaction that takes it, so a key garam cannot open, and a signature
Transit cannot make, each refuse the claim as a server error with
nothing claimed and none of the message's attempts spent. A renewal
hands nothing out and carries no
signature. The sender garam vouches for is the row's author: the caller
the listener authenticated for everything a caller posted, and garam's
own issuer for the cross-organization refusal it writes under the
intended recipient's identity, whose content keeps that stored sender.
ADR-0101 §What a verifier checks is the contract a verifier holds garam
to; `api/machine.yaml` is authoritative for the claim's members and the
key-set route.

The keys are rows of their own, never deleted. With no provider
configured each is sealed under the key-encryption key the machine
process is given and bound to its row. A deployment that
sets `GARAM_TRANSIT_ADDR` holds every key it creates from then on in
one OpenBao or Vault Transit key instead: each garam key is one version
of it, made by `rotate`, its row naming the mount, the key and the
version and holding no private material, and Transit signs every
hand-out under that version
([ADR-0104](./adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md)).
The `kid` is the RFC 7638 thumbprint either way, and the published set
is read from the rows alone. A sealed key that is current when the
provider is turned on goes on signing, and the Transit key replacing it
is announced with the full lead; a key held in a custody the process is
not configured with does not sign there, and retiring it is the
repair. A due key Transit does not make, whatever the reason, waits
for the next read while the current key signs and the set is served;
with none signing, claims are refused with Transit's failure.
Whichever path reads them first — a claim or the key set — creates
the key that is due under a lock every replica takes, deciding on the
database's clock read once the lock is held:
with no key signing, one that signs at once, which is a deployment's
first and the replacement for one retired at once; otherwise the next
one, announced two hours before it signs once the current one has
signed for 90 days less that lead, and one at a time. The published set
holds the key signing, the one announced, and one that stopped while a
signature it made can still be valid, plus the skew allowance and one
cache lifetime. `garam retire-message-signing-key` withdraws a key from
signing and from the set at once, which is how a compromised key is
withdrawn.

The HTTP surface is the machine listener, and the reader on the other
side of it is the adapter garam ships ([adapter.md](./adapter.md)).
The retention above is this domain's own work rather than a schedule a
deployment carries: the sweep rides the enqueue, so a queue being
written to is a queue being swept (ADR-0034).

## Enrollment does not confer message trust

**Unbuilt: common enrollment.**
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
changes Operator API authentication and enrollment, not the queue's
signed hand-outs or ADR-0101's issuer contract.

- Agent API access proves the connector/runtime to Garam; an Operator
  assertion proves an operator to the exchange. Neither establishes
  the sender of incoming content at Sherlock.
- Sherlock owns trusted issuers and authenticated `(iss, sub)`; Garam
  keeps the message-signing private key. Enrollment supplies public
  issuer trust separately from short-lived API access. Neither
  long-lived signing key enters a runtime.
- The Sherlock PM checked issuer verification and authenticated Origin
  at `garamsh/sherlock@2281328366515c1c67a72bdf978c2266b842cdec`,
  ADR-0026 and `signature.go`. Its ADR-0014 still places per-caller
  authorization above Sherlock. The PM retains that boundary under
  Option A of
  [proposal #1067](https://github.com/garamsh/sherlock/issues/1067);
  runtime-local sender/channel authorization needs a demonstrated gap
  and a new owner decision, not completion of Operator exchange. Garam
  continues deciding organization authorization and cross-org consent.
- Keep stable message identifiers, fresh hand-out signatures, content
  digests and durable replay/outbox rules. An assertion's one-use
  `jti` cache is not the message redelivery record.

ADR-0101 stands but for where its keys are held, which ADR-0104
supersedes. `internal/message/signing.go` and
`api/machine.yaml` §message implement signed hand-outs and the public
key set; ADR-0102 introduces no new message channel.
