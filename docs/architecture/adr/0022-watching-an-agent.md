# ADR 0022: An agent is watched only while someone is watching

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

A person should be able to open an agent in the console and see it
work — what it is doing now, not what it did. Nothing carries that
today. The agent platform emits turn events on a monitoring stream,
that stream is loopback-only inside the agent's pod, and garam never
dials into an agent's environment (ADR-0012). So the events have to
travel outward: platform, adapter, garam, browser.

Two properties of that stream shape everything downstream, both
recorded in ADR-0013 and read from the platform's source:

- It drops events for a subscriber more than a bounded number behind,
  **while still connected, with nothing marking the gap**.
- A subscription enqueues nothing, so opening one costs the agent no
  work.

The adapter already consumes that stream, for the completion events
ADR-0018 keys on, and already holds a long poll to garam.

## Decision

- **The adapter forwards turn events only while garam says someone is
  watching, and garam tells it on the poll it already holds.** garam
  cannot dial the adapter, so the only available direction is the one
  the adapter opens; the poll exists, and a watcher count on its
  response costs no new connection. When nobody watches, nothing is
  forwarded and nothing is stored.
- **A watch is a subscription the browser opens and closes, not a
  setting.** It begins when a person opens that agent and ends when
  they leave. Nothing accumulates for an agent nobody is looking at.
- **The browser reads it as server-sent events.** The traffic is one
  directional — the agent speaks and the person reads — and SSE
  reconnects on its own. A person sending a message is an ordinary
  request, not a message on this channel, so nothing here needs a
  socket that can carry both.
- **The live view is best effort and never a record.** The stream it
  rests on drops silently under load, so a gap is invisible by
  construction. Anything that needs to be true afterwards — what an
  agent did, which messages it consumed — comes from the queue, which
  is durable. This must be said wherever the view is described, because
  a live feed that looks complete is read as complete.
- **Watching is its own grant, separate from reading.** `agent:read`
  says an agent exists and reports its states; watching exposes what it
  is saying and consuming. An organization may reasonably give one and
  withhold the other, and a grant that arrives by inheritance is a
  grant nobody decided — the same reason ADR-0012's provisioning
  actions were kept out of a wildcard.
- **A watcher sees from when it opened.** Nothing replays. Opening
  mid-turn shows the rest of that turn and no more, which is the cost
  of storing nothing for the agents nobody watches.

## Consequences

- An agent nobody is watching costs nothing: no forwarding, no storage,
  no fan-out. That is the property the design is for.
- A person who opens an agent mid-turn has missed the start of it, and
  no amount of waiting recovers it.
- The adapter gains a second reason to read the monitoring stream, and
  ADR-0018 already requires that consuming and completing use separate
  subscriptions — so watching is a third, for the same reason: a slow
  consumer must not degrade the signal another decision rests on.
- garam holds an open response per watcher, which is a connection per
  person watching rather than per agent. A person leaving a tab open
  holds one until it closes.
- A gap in what a person saw cannot be distinguished from an agent that
  said nothing. The interface should not imply otherwise.
- The events cross a boundary in the clear that the queue's contents do
  not: a turn's content reaches garam because someone is watching. What
  garam may retain of it, and for how long, is not decided here and is
  the first question anything storing it must answer.

## Rejected alternatives

- **Forwarding always and storing recent events.** A viewer opening
  mid-turn would see history, and every agent would pay for an audience
  it may never have — traffic, storage and a retention decision for
  content nobody read.
- **garam subscribing to the agent's stream directly.** The shortest
  path on a diagram, and it requires an inbound route into every
  environment an agent runs in, which ADR-0012 refuses and which would
  end the possibility of an operator someone else supplies.
- **A websocket.** Warranted if the person and the agent exchanged
  turns on one channel. They do not: sending is a request against a
  durable queue, and mixing it into the live channel would make a
  message's delivery depend on a socket that is explicitly best effort.
- **Reusing `agent:read`.** One fewer action, and it grants the content
  of an agent's work to everyone who may see that the agent exists.
- **Deriving the view from the queue instead of the stream.** Durable
  and honest, and it shows only what messages arrived and completed —
  not the working, which is the thing a person opens the view to watch.
