# ADR 0013: Agents exchange messages through a garam API an adapter polls

> Status: accepted
> Date: 2026-08-16

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

Agents message each other and the people they work with, and every
message passes through garam. ADR-0012 settled who provisions an agent
and how it is addressed; this settles how a message reaches it.

`gagent` is the agent platform, and its gateway is a protocol layer
rather than a router: three flows — ingress at
`POST /agents/{id}/messages`, monitoring at `GET /agents/{id}/events`,
and an outbound "mouth" that POSTs each reply to one configured URL. It
serves exactly one agent and answers 404 for any other id, and it
declines routing and authorization outright (gagent ADR-0014).

garam sits outside every orchestrator, and no orchestrator exposes an
agent to it: the gateway binds loopback only unless mTLS is configured.
So the connection has to start at the agent's side in every deployment
shape.

The rest is what the platform's source says rather than what its
documents imply, checked directly because two of these are stated
nowhere:

- A 202 means a message entered an in-memory channel of capacity 64,
  not that it will be processed. A restart drops the queue silently.
- `Enqueue` blocks when that queue is full, and turns run one at a time.
- One turn takes up to `MaxDrainBatch = 8` queued messages as a single
  input, so N messages are not N turns and not N replies.
- The monitoring stream drops events for a subscriber more than
  `SubscriberBuffer = 32` behind, while it is still connected, with
  nothing in the stream to mark the gap.
- The mouth POSTs once. Any failure writes the reply to a local outbox
  that nothing drains, and a `console:` URL configured without `tls:`
  produces that outcome for every reply.
- Certificate material is shared across both directions: the outbound
  target's server certificate must be signed by the same private CA
  that authenticates callers to the gateway.

## Decision

- **Messages reach an agent by long-polling a garam endpoint, held in
  Postgres.** No broker. Any garam instance can answer any poll because
  the queue is a table, so there is no connection to route a message to
  and no shared bus to run.
- **A local adapter bridges the two protocols.** It long-polls garam,
  POSTs into the gateway over loopback, receives the mouth's replies,
  and forwards them to garam with durable retry. It is both a client
  and a server, so it binds a stable address — `console.url` is fixed
  at gagent's startup and has no reload — and it drains the outbox on
  startup, which is the only window where the loopback hop fails.
- **garam writes the adapter as a library with a thin binary; the
  orchestrator deploys it.** We own its correctness, they own its
  placement. The uncertainty this ADR leaves open — reconciliation,
  idempotency, ack timing — all lands inside it, and a second
  implementation would make every later fix a cross-repository
  negotiation.
- **garam is a message API. Channels are consumers of it.** Slack, LINE
  and the rest terminate outside this decision. A webhook-only channel
  needs a public endpoint, which garam has and an agent never does, but
  whether garam ships a first-party channel is a product question, not
  this one.
- **A message is fire-and-forget and a reply is an independent event.**
  Nothing pairs them. Batching makes the correspondence untrue, and a
  model that assumed it would break the first time two messages arrived
  together.
- **garam answers 2xx once the message is durably stored, and never
  4xx for a routing failure.** A non-2xx sends the reply to an outbox
  nobody reads, so rejecting a malformed target destroys the message
  and tells the sender nothing. A failure comes back as an ordinary
  inbound message to the sender instead.
- **Readiness is a freshly opened monitoring subscription, separate
  from the one the adapter consumes.** Opening it exercises the process
  rather than a socket, and keeping it apart stops a slow consumer from
  degrading the signal that decides whether the agent is healthy. It
  rests on two gagent properties: that a subscription enqueues nothing
  — which gagent ADR-0014 makes a consequence of a decision — and that
  no listener exists before the tool registry loads, which is **true
  today and not guaranteed** (gagent#477). The second is written into
  the adapter contract as a named dependency so that if it moves, the
  contract says what broke instead of the probe quietly meaning
  something else.
- **garam owns idempotency and reconciliation.** The platform cannot be
  asked whether a message was processed, so redelivery and duplicate
  suppression are ours.
- **The loopback hop is paid for as a boundary, not as transport.** One
  caller, one pod, HTTP with mTLS: as a network surface it is
  indefensible. What it buys is that `gagent` stays a separable product
  instead of a library we link, which keeps our release cycle off
  theirs and garam's concepts out of their process — the same line
  their ADR-0014 draws from the other side. Anyone measuring the hop
  will find it expensive; the boundary is what is being bought.

## Consequences

- Latency is a poll interval rather than a push. Long-polling keeps it
  under a second, against turns that take seconds.
- The adapter count equals the agent count, and each holds one poll.
- garam runs a certificate authority per organization and issues both
  the adapter's server certificate and the agent's client certificate
  from it. Rotation is a restart; there is no reload.
- Losing garam's ingress endpoint loses replies permanently once the
  adapter's retry budget is spent, so its availability is a durability
  property.
- Reconciliation cannot be built on the monitoring stream, which has no
  cursor and drops silently. Progress is inferred from replies
  arriving, and from nothing else.
- Nothing here is implemented. The endpoint, the queue, the adapter and
  the machine authentication ADR-0012 already named all remain to be
  built.

## Rejected alternatives

- **A broker — NATS or similar.** It solves cross-instance routing,
  which polling does not have, at the cost of running, securing and
  monitoring one more system. Its advantages appear at thousands of
  messages a second and sub-10ms delivery; a turn takes seconds.
- **A stream from garam to the adapter, over SSE or gRPC.** Lower
  latency, and it creates the routing problem polling avoids: a message
  arriving at one garam instance has to reach the instance holding that
  agent's connection.
- **garam dialing the agent.** Requires an inbound path into every
  deployment, which no orchestrator offers and which would end the
  possibility of a user supplying their own.
- **The adapter as a gagent plugin.** It has its own lifecycle across
  restarts, and hosting it inside the platform would put garam's
  concerns in the process gagent ADR-0014 keeps them out of.
- **Rejecting a bad target with 4xx.** Correct-looking and destructive:
  the mouth writes the rejected reply to the outbox and the sending
  agent learns nothing.
- **Treating a probe failure during startup as expected.** It would
  weaken the signal exactly in the window — rollout, restart,
  crashloop — that the probe exists for.
