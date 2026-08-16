# ADR 0012: Agents are provisioned by operators garam never dials

> Status: accepted
> Date: 2026-08-16

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

Agents join users as actors in garam. They are not created here: an
operator running inside a cluster reads this API, provisions the agent,
and reports back. One organization may hold several operators, and a
user may bring their own — so an operator is a peer of garam, not a
part of it.

The agent platform is `gagent`, whose decisions bound this one. Its
gateway *serves* message ingress at `POST /agents/{id}/messages` and a
monitoring stream at `GET /agents/{id}/events`; it does not poll and
does not dial out to fetch work (gagent ADR-0007, ADR-0012). Its
outbound "mouth" POSTs each message to one configured endpoint and, on
any non-2xx or transport error, writes the message to a local outbox
that nothing drains — delivery to us is at-most-once, with no retry.
Its gateway binds loopback only unless mTLS is configured. And it
declines authorization outright: "granting an agent its capabilities and
deciding who may use it for what is the orchestration layer's concern"
(gagent ADR-0014), while capability enforcement stays at the action site
in the workspace (gagent ADR-0017).

garam runs outside every such cluster and must keep working when a user
supplies an operator we have never seen.

## Decision

- **An agent is identified by a GRN whose identifier garam mints**, as
  organizations already are: `grn:<org>:<project>:agent:<randomHex8>`,
  unique within the organization. An operator is addressed the same way
  under a new `operator` service_type. Neither identifier ever changes.
- **Assignment is a relation, not part of identity.** A row binds one
  agent to one operator and carries an `epoch`. Reassignment updates the
  row and increments the epoch. This is the shape policy attachments and
  org memberships already take.
- **The epoch fences the moment reassignment is unsafe.** An agent's
  sidecar presents its epoch when it connects; garam serves the current
  epoch and refuses any other. A pod left running by a previous operator
  cannot receive messages, so exactly one instance is reachable even
  while two exist.
- **Delivery is agent-initiated and garam never dials in.** A sidecar in
  the agent's pod holds one outbound connection to garam and hands each
  message to the gateway over loopback. garam addresses an agent by its
  GRN and routes to whichever connection currently holds that GRN's
  epoch.
- **garam's console endpoint answers 2xx only after the message is
  durably stored.** A 2xx is the agent's only signal that the message
  survived, and a message it cannot deliver is lost to an outbox nobody
  reads.
- **Provisioning state and runtime state are separate facts.** The
  operator reports whether the pod exists and is ready; the agent
  reports whether it is connected and what its turns did. Neither
  substitutes for the other.
- **Authorization stops at "who may use this agent".** garam decides
  that, and an agent GRN becomes a policy subject — which ADR-0008 does
  not currently allow and a later ADR must supersede. What an agent may
  do once running is enforced in its workspace, not here.

## Consequences

- Operators become interchangeable. Adding one is registering it and
  moving rows; nothing about an agent's identity, policies or message
  history depends on which operator runs it.
- garam needs machine authentication, which it has never had — today
  every route is behind a session cookie issued by OAuth.
- The connection count to garam scales with agents rather than with
  clusters. That is the price of holding the fencing point ourselves.
- Losing the console endpoint loses messages permanently, so its
  availability is a data-durability property rather than a convenience.
- The monitoring stream has no cursor and no replay, so runtime state
  cannot be rebuilt from it after a disconnect. State is reconciled
  periodically against the operator's report, with events shortening the
  lag rather than being the source of truth.
- Nothing above is implemented. The endpoints, the machine
  authentication and the relation all remain to be built.

## Rejected alternatives

- **Encoding the operator in the agent's GRN**
  (`…:agent:<operator>/<agent>`). Reassignment would change an agent's
  identity, breaking policy attachments and audit trails, which the GRN
  rules forbid — "never expose a transient ID where a stable one is
  expected". A `/` is also not a legal identifier character and would
  break `gagent`'s `/agents/{id}/messages` path.
- **One relay per cluster instead of a sidecar per pod.** Fewer
  connections, but the relay must reach each pod, and `gagent` binds
  non-loopback addresses only under mTLS — so garam would have to
  operate a certificate authority per organization and distribute certs
  into clusters it does not own. The sidecar reaches the gateway over
  loopback and needs none of it.
- **Operators claiming agents under a lease.** Self-organizing and
  automatically failing over, at the cost of TTLs, renewal and the
  partition semantics that make leases subtle. The relation reaches the
  same place for a first system; a lease can be added to the same row
  when automatic failover is a requirement rather than a guess.
- **Pre-assigning agents to operators by a selector.** Overlapping
  selectors put two operators on one agent, and the overlap is only
  visible at runtime. An explicit row cannot overlap.
- **garam dialing agent pods directly.** Requires an inbound network
  path into every cluster, which ends the possibility of a user
  supplying their own operator.
