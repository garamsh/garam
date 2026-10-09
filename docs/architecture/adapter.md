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
- Readiness
- What garam owns
- Foreign facts this rests on
- Stable identifiers and the outbox
- Activation and runtime status
- Not decided

## What it is

garam is a message API and an agent platform speaks a different
protocol. The adapter is the piece between them: it long-polls garam,
posts into the platform's gateway over loopback, watches the gateway's
event stream, reports back, and forwards the replies the platform
records in its outbox. Where the agent is fenced it first obtains the
activation of the platform's running generation from the lifecycle
authority (§Activation and runtime status). garam writes it as a
library with a thin binary and an orchestrator deploys it
([ADR-0013](./adr/0013-agent-messaging.md)); we own its correctness and
they own its placement.

It runs beside exactly one agent, in that agent's pod. It has to,
because the gateway binds loopback unless mTLS is configured (Sherlock
at `sherlock@40ef7ac`, the historical reading §Foreign facts this rests
on anchors) and because it must outlive the agent process during
shutdown — a native sidecar rather than an ordinary container, for the
reason in [ADR-0018](./adr/0018-delivery-acknowledgement.md).

It reaches garam as the agent, with the certificate garam issued for
that agent, and verifies garam's listener against the garam server root
— one root every organization shares, separate from the authority that
signed the agent, and delivered in the same answer as the certificate
([ADR-0014](./adr/0014-agent-authentication.md),
[ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)).
It presents its certificate per handshake rather than holding what it
read at startup, so a renewed one is presented on the next connection
and this process keeps running.

**Unbuilt: its token mode.**
[ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
moves the adapter from the agent's certificate to two short-lived
tokens, one for garam and one for the control service, each read from
its file per request, with the placement token moved to a
`Garam-Placement` header under `agent-execution.v2`. It is step G2 of
`docs/plans/2026-10-09-machine-identity-migration.md`.

## The delivery loop

1. Claim a message for its agent from garam's queue. The claim carries
   a lease, garam's identifier for the message and the sender garam
   verified, and the content and garam's signature over it
   (§Stable identifiers and the outbox).
2. Post it into the gateway under garam's message identifier, never the
   claim's, as `garam-message.v1` (§Stable identifiers and the outbox).
   Hold the claim under that identifier, in memory.
3. Watch the gateway's event stream for a turn ending.
4. When a turn reports success and names a message, complete that
   message's claim at garam.

The hold is volatile and the identifier is not. A restart lapses the
leases it held, and garam hands those messages out again under the
same identifiers, which the platform recognises: a message whose turn
already committed is answered as completed and its claim completes at
once, without a second turn. After the event stream reopens, the
adapter asks the platform about every message it holds, which recovers
a turn whose event was lost while no stream was open.

## Completion

**A claim is completed by the platform's word that a turn which
consumed its message committed — a turn that succeeded and named it,
or the platform answering the message as completed — and nothing else
completes it. A message whose completion never arrives — because the
turn failed, because the word was lost, because the adapter died — is
redelivered.**

Both halves belong together wherever this is restated. The first alone
reads as exactly-once, and the accept is still not a completion
([ADR-0079](./adr/0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)).

**Delivery is at-least-once.** A redelivered message whose turn
committed is recognised and runs nothing; one whose turn failed, or was
interrupted before it committed, runs again, and the effects that turn
had already caused may repeat. Tolerating that means handling it the
way a person saying the same thing twice is handled.

## Failure and attempts

A turn that fails is an attempt rather than a completion. From outside,
a turn that failed because a model provider was unreachable is
indistinguishable from one that failed on the message itself, and the
failure event rides the same droppable stream, so its absence is
indistinguishable from a turn that never started.

The adapter never has to tell those apart. Success completes;
everything else retries; an attempt count ends what would otherwise
loop forever. The platform notifies no sender (§Foreign facts this
rests on), which is why telling the sender at exhaustion is **garam's
obligation**
([ADR-0018](./adr/0018-delivery-acknowledgement.md) §Consequences,
[overview.md](./overview.md) §Out of scope (today)). **It is recorded
as one and not discharged**: an adapter may not treat an exhausted
message as reported to its sender.

## The lease

The adapter renews its claim while a turn is in flight, under a
ceiling. The ceiling is a backstop for a lost completion event, not a
prediction of how long a turn takes: it only has to exceed a normal
turn. Without it, a missed event renews forever and produces a claim
that is neither completed nor redelivered — a third state, and the
least visible of the three.

**The lease and the attempt count are fixed at their defaults, and the
defaults are narrow.** No deployment moves them: `garam serve machine`
builds the message service on the zero-value configuration, and no
flag, environment variable or chart value names a lease, a ceiling, an
attempt count, a wait or a poll interval. Widening one is an edit to
the Go defaults, which is what the rest of this section is addressed
to.

The reason those defaults are narrow is dated, and the date is later
than it looks: a repeated *turn* is unmeasured — an exact repeated fact
is idempotent in the platform's addressed store, but a turn re-reads
its input and may extract differently.

**The reason expires when that idempotency has been measured against
the real model, not when the store merges.** The two are not the same
moment, and the gap between them is the dangerous one: the store lands
in stages, and one of them removes recall before the stage that reads
it arrives. A default widened in that gap is widened over a platform
that answers nothing.

The platform's own maintainers state when it has been measured; this
document does not track their stages, because a foreign issue number
is not something a reader here can check. What the two paragraphs
above say about their store is what they stated on 2026-08-17, read
from no tree, so it is checked by asking them again rather than at a
commit. Ask them, and widen only on an answer that names a
measurement. Whoever widens this default should find this sentence
before they do.

## Watching

**The adapter's half is unbuilt.** garam's is there: a claim and a
renewal both answer with the watcher count, and the machine listener
serves the route a forward arrives on. Nothing under
`internal/delivery/` reads that count or posts a turn event, so what
follows is the rule the adapter takes when it is built rather than a
description of what runs. [overview.md](./overview.md) §Out of scope
(today) records it, beside the §Readiness half.

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
  browser that reads slowly must not cost the completion signal an
  event.
- **An event garam cannot carry whole is refused**, not shortened. It is
  bounded by what one announcement holds, which `api/machine.yaml`
  states; a refusal is a gap, and this stream already permits one.

## Subscriptions

**One of these exists.** `readTurns` (`internal/delivery/service.go`)
opens the single subscription the loop holds, and it carries turn
endings — consuming and completing together. Forwarding and readiness
are unbuilt (§Watching, §Readiness), so what follows is the rule the
built one already keeps and the rule the others take when they are
built, rather than a description of what runs.

Consuming, completing and forwarding use separate subscriptions. A slow
consumer must not degrade the signal that decides what it completed:
falling behind would drop completion events, which produces
redeliveries, which produce load. The same reasoning keeps readiness on a
subscription of its own (below), and puts watching on a third
([ADR-0022](./adr/0022-watching-an-agent.md)).

## Readiness

**Nothing implements this.** The mechanism below is decided
([ADR-0013](./adr/0013-agent-messaging.md)) and unbuilt: nothing under
`internal/delivery/` or `internal/cli/delivery.go` opens a readiness
subscription. [overview.md](./overview.md) §Out of scope (today)
records it, beside the §Watching half.

**Readiness is a monitoring subscription the adapter opens fresh, of
its own** — not the one it consumes, and not a handle it kept. The
adapter is ready while opening one succeeds and not ready when opening
one fails ([ADR-0013](./adr/0013-agent-messaging.md)).

Opening one exercises the process rather than the socket. A dial that
connects says a port is bound; a subscription that opens says the
gateway served a request, and it says so about the agent this adapter
was placed beside rather than about the pod's network.

It is a subscription of its own for the reason above: readiness decides
whether the agent is treated as healthy, and a consumer that has fallen
behind must not be able to answer that question for it.

Two properties of the gateway are what make the answer mean anything,
and both are named in §Foreign facts this rests on: opening a
subscription enqueues nothing, so asking costs the agent no throughput
however often it is asked, and no listener exists before the tool
registry has loaded, so an answer means the agent can accept work
rather than that a process has started. If either moves, the
subscription goes on opening and stops meaning that, which is what a
row there exists to catch.

Whether anything outside the adapter reads this is a separate question
and an open one: the adapter listens on no port, so it answers no
Kubernetes probe, and what a sidecar beside an agent owes a scheduler
is undecided ([deployment-contract.md](./deployment-contract.md)).

## What garam owns

Whether a message is delivered is garam's record, not the adapter's and
not an orchestrator's ([ADR-0013](./adr/0013-agent-messaging.md)). The
platform at `v0.1.0` can be asked whether it committed a turn for a
message it was handed under garam's identifier, and the adapter uses
that answer only to complete the claim at garam; its own knowledge is
deliberately volatile, so **"has this already been delivered" lives on
garam's side.** A design that keeps it in the pod or in an
orchestrator's status is inventing a distinction that does not exist
there.

## Foreign facts this rests on

Named because this contract would be **wrong** if any of them changed,
not because they were mentioned. Read in Sherlock
([`garamsh/sherlock`](https://github.com/garamsh/sherlock)) at
[`40ef7ac`](https://github.com/garamsh/sherlock/tree/40ef7ac), when the
repository was named `gagent`; each path below is the one it had at
that commit, and none is claimed to hold on the current branch. The
repository carries no tags — none on 2026-09-27 either — so a commit is
the only anchor, and an unanchored citation goes on looking valid after
it has lost what it points at. The sections above restate several of
these rows as `sherlock@40ef7ac`, and the same commit is the anchor for
those restatements.

The last two rows are not that reading's. They arrived on 2026-08-25
from [ADR-0013](./adr/0013-agent-messaging.md) §Decision, which is
where they were read. The 40ef7ac rows that said the gateway mints the
identifier are gone: the contract no longer rests on them, and the
table below holds what replaced them.

| Fact | Where | What breaks without it |
|---|---|---|
| Terminal events carry the ids the turn drained | `internal/gateway/queue.go` | Completion has nothing to key on |
| A subscriber more than a bounded number of events behind loses events silently | `internal/gateway/events.go` | Delivery would be exactly-once, not at-least-once |
| One turn takes up to a bounded batch as a single input | `internal/gateway/queue.go` | The batch would not be the completion unit |
| The inbox is a volatile buffer | `internal/gateway/service.go` | The accept response would be an acknowledgement |
| A failed turn reaches no sender | `internal/gateway/queue.go` | garam would not owe the notification |
| Opening a monitoring subscription enqueues nothing | `internal/gateway/events.go` | Readiness would spend the agent's throughput to measure it |
| No listener exists before the tool registry has loaded — true when [ADR-0013](./adr/0013-agent-messaging.md) §Decision recorded it, and not guaranteed | `cmd/gagent/agent.go` | An answer would stop meaning the agent can accept work |

Read in Sherlock at
[`v0.1.0`](https://github.com/garamsh/sherlock/tree/44aaa555105f9834244eaac4ad18b0ed75dbf0ff)
(`44aaa555105f9834244eaac4ad18b0ed75dbf0ff`), the release this
contract binds:

| Fact | Where | What breaks without it |
|---|---|---|
| A post carries the caller's `id` and `content`, refuses any other field, and keys the message by the transport caller and that id | `internal/gateway/handler.go`, `internal/gateway/queue.go` | Two claims of one message would be two sources |
| A replay of a committed id with the same content answers 200 `completed`; other content under it is 409 | `internal/gateway/queue.go` | A restart's redelivery would run a second turn |
| `GET /agents/{agent}/messages/{id}` answers `pending`, `completed` or `unknown` | `internal/gateway/handler.go` | A lost event could not be recovered |
| `GET /agents/{agent}/status` answers `contract_version` 1 with the agent, epoch, generation, revision, state and start | `internal/gateway/handler.go` | Nothing could be activated |
| The ingress body is bounded at 1 MiB | `internal/gateway/handler.go` | A maximal envelope could be cut |
| An outbox entry is `<id>.json`, written whole under a dot-prefixed temporary name, carrying `created_at` once | `tools/message_send/message_send.go`, `internal/message/message.go` | The forwarder could read a partial entry or invent an age |

This promotes an observability surface into a delivery contract, and
the two carry different obligations: observability may lose an event, a
delivery contract may not. Naming the buffer here is what makes it
contract-bound in a repository that would otherwise have no reason to
know anyone depends on it.

## Stable identifiers and the outbox

What follows is
[ADR-0079](./adr/0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)
as [ADR-0084](./adr/0084-the-adapter-activates-its-generation-and-reports-runtime-status-under-a-contract-garam-owns.md)
pins it for this adapter.

- **The adapter names each message to Sherlock by garam's message
  identifier**, never the claim's, so two claims of one message are one
  source there. A replay under it with an identical payload answers a
  completed source's recorded outcome rather than running another turn,
  and a different payload under it is refused. Completion is reported
  apart from the accept.
- **The content is `garam-message.v1`**: a compact JSON object of
  `contract`, `sender` and `body` in that order, encoded by Go's
  `encoding/json` default with no trailing newline. `sender` is the GRN
  garam's claim verified and `body` the original body as one string, so
  nothing in the body overrides the envelope and a redelivery encodes
  to the same bytes.
- **The adapter does not yet forward what garam signs.** garam's claim
  answers the same content, which garam encodes itself, and a signature
  over it
  ([ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md));
  `tests/integration/message_signature_test.go` holds garam's content
  equal to this adapter's own encoding. The adapter still posts the
  content it encoded and drops the signature. Forwarding both byte for
  byte is ADR-0101's step 2, and it waits for Sherlock's ingress to
  name the member the signature travels in (garamsh/sherlock#993),
  because that ingress refuses a member it does not know.
- **A reply leaves through a durable outbox.** The adapter reads each
  entry as a bounded regular file inside the outbox directory, never
  through a link, and requires its agent to be this adapter's GRN. It
  forwards the entry through `POST /outbox-messages` under
  `durable-delivery.v1` — content as body, target as recipient, the
  entry's identifier unchanged as the idempotency key and its
  `created_at` as `createdAt` — and there is no fallback to
  `POST /messages`.
- **The entry goes only on a verified acknowledgement**: a stored
  message with a non-empty identifier, this agent as sender, the
  entry's target as recipient and its identifier as key, while the file
  is still the one that was read. A 201 carrying a cross-organization
  refusal fails that check. A refusal, an unsupported peer or a wrong
  binding keeps the entry and logs it without a hot retry loop; an
  unreachable garam is retried with bounded backoff under the same
  identity. An entry with no valid `created_at`, or past garam's
  seven-day admission budget, is never deleted, re-keyed or re-stamped.
  An entry the file boundary refuses is set aside until its file is
  replaced; one the system could not open or read is tried again after
  a backoff. A pass offers a bounded batch, the oldest entries not set
  aside.
- **Removal compares, then unlinks by name.** Between the two, a
  writer that can rename in the outbox could put another file under
  the name. That writer can already delete any entry, so the window
  grants it nothing; renaming to a private name first would close it
  at the price of a crash state hiding an unacknowledged reply where
  the forwarder never reads.
- **Entries an earlier generation left are forwarded by the current
  one**, under its own credential and token, keeping their identifier
  and origin generation. Nothing in the entry is rewritten before it
  goes; a delivery or a refusal is logged by its identifier, origin
  and in-reply-to, never its content.
- **An outbox is optional where the agent is legacy.** Where none is
  named nothing is forwarded, as before this contract; the fenced mode
  requires one.
- **What is still not promised.** A turn interrupted before it
  committed may repeat its effects. Delivery stays at-least-once and
  completion stays §Completion's rule.
- **The adapter and Sherlock keep adjacent releases compatible** in
  both upgrade orders, and refuse what they do not support by name.
  This contract binds Sherlock `v0.1.0`
  (`44aaa555105f9834244eaac4ad18b0ed75dbf0ff`) and no tested window
  yet.

## Activation and runtime status

**The adapter's half is built, and the standalone supervisor serves
the two routes; the Operator control service does not yet.** The
Operator implements them in garamsh/garam-agent-operator#218. The
standalone supervisor serves them on a socket of its generation
([agents.md](./agents.md) §Standalone hosts, ADR-0085), and its joint
acceptance with garam and Sherlock is unverified (#1180). This is
[ADR-0084](./adr/0084-the-adapter-activates-its-generation-and-reports-runtime-status-under-a-contract-garam-owns.md)'s
contract, `agent-execution.v1`, which garam owns and the lifecycle
authority serves: the Operator control service when managed, the local
supervisor when standalone.

- **The deployment chooses the mode.** Without control settings the
  adapter is legacy, for an agent never cut over: no activation, no
  token, no status report. With them it is fenced, and what follows
  applies; some but not all is refused at startup, and neither mode
  falls back to the other. garam refuses a cut-over agent's claim made
  without a token, so a misconfigured legacy adapter carries nothing.
  The legacy mode lasts until #1171 retires its source.
- **Control authenticates the agent's leaf**, read per handshake, over
  mTLS; the leaf names the GRN in the path, and garam's token never
  goes to control.
- **A standalone adapter dials its supervisor's socket.**
  `GARAM_ADAPTER_CONTROL_SOCKET` makes every control connection reach
  that unix socket while TLS still verifies the control URL's host;
  it is valid only beside the fenced mode's settings, and nothing else
  in the client changes (ADR-0085).
- **Every protected call carries the activation's token**, as
  `Authorization: Garam-Execution <token>`
  ([machine-identity.md](./machine-identity.md) §Generation and
  lineage), and the adapter makes none before it holds one.
- **It activates the generation the runtime reports, while it reports
  `serving`.** A runtime already draining is not activated (ADR-0085).
  It reads Sherlock's loopback status, then asks the authority with that
  GRN, epoch, generation and revision and the mounted placement token,
  under a request identifier derived from those inputs alone, so a
  restart replays the same intent and receives a newer token. It
  verifies the binding answered and keeps the highest token version.
- **Undecided answers retry; definite ones pause.** An unavailable
  authority or garam is retried with bounded backoff and stops nothing
  local. A refusal, a wrong binding, or garam refusing the token or the
  credential stops protected work and keeps the outbox, with no
  time-based retry, until the runtime's generation, epoch or revision,
  the placement token or the agent's leaf changes. Credential recovery
  is the manager's, never the adapter's.
- **It reports the runtime's status outward** after activation and on
  each change: the activation, GRN, epoch, generation, revision, state
  and start time as the runtime reported them, and when it read them. A
  report never activates anything, and an empty revision is unknown.

ADR-0084 is authoritative for the routes, bodies, refusals, the
identifier's derivation and the settings the adapter reads; ADR-0085
for the control socket setting.

## Not decided

- A durable inbox on the platform's side was unbuilt and unscheduled at
  `sherlock@40ef7ac` — treat it as unavailable rather than refused. If
  it arrives, the completion rule does not change; the redelivery causes
  reduce by one.
