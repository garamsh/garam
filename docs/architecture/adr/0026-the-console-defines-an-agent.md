# ADR 0026: The console defines an agent and an operator reads that definition

> Status: accepted — the read surface and the reconciliation it deferred are decided by [ADR-0033](./0033-an-operator-claims-the-agent-a-definition-mints.md)
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Decision

- **garam holds what an agent should be, and an operator reads it.** An
  organization's members define an agent in the console: which operator
  is to run it, and with what values. The operator learns of the
  definition by asking garam, and reconciles its cluster to match.
- **garam still dials nothing.** The operator asks; garam answers. This
  is the same direction ADR-0012 fixed and it is unchanged — what moves
  is which side decides an agent should exist, not which side opens the
  connection.
- **A definition is a record, not a command.** garam does not know
  whether the operator acted on it, and a definition that no operator
  ever reads leaves garam's state untouched. What the operator did is
  reported back separately, as the provisioning state ADR-0012 already
  keeps apart from the agent's own runtime state.
- **The browser API stops being read-only for agents**, and the sentence
  in `overview.md` saying provisioning stays outside garam retires with
  this ADR. Provisioning still happens outside garam; **deciding** it no
  longer does.

## Context

ADR-0012 put agent provisioning outside garam and fixed the direction of
every connection: garam never dials into a cluster. It did not say which
side decides an agent should exist, and the surface built afterwards
answered that question by implication — `POST /agents` on the machine
listener is an operator telling garam that an agent now exists, with no
request body, so an operator that never calls it produces an
organization with no agents and garam has nothing to say about it.

That leaves the console unable to answer the question a person opening
it asks first: what agents should this organization have? It could only
show what some operator had already made.

The project owner has decided the other direction: the console is where
an agent is defined, including which operator runs it and with what
values, and the operator reads that definition.

**This changes an integration another repository is building against.**
The operator's maintainers were told, hours before this decision, that
garam serves no such list — an answer correct against the code and
ADR-0012 as they stood. The cost of the reversal was one paragraph in
their documents, because their first API version deliberately carries no
field naming an external system. That constraint was not foresight about
this decision; it existed because no operator can obtain its first
certificate yet. It paid for itself anyway.

## Consequences

- **An agent now has a lifecycle in garam before it exists anywhere.**
  Defined, then made, then running — and garam knows the first directly
  and the rest only by report. A console showing an agent must say which
  of those it is showing, or it will show a definition as though it were
  a running thing.
- **Which operator runs an agent becomes garam's data**, so an operator
  needs to know which definitions are its own. The matching key is not
  decided here.
- **Values travel.** Whatever an operator needs to construct an agent is
  now something garam stores and serves, which puts configuration inside
  a boundary that previously held only identity. What that payload is,
  and whether any of it is secret, is not decided here.
- **The machine listener's `POST /agents` is now one of two ways an
  agent comes to exist**, and two ways is a defect rather than a
  feature. Reconciling them is work this ADR creates and does not do.
- The operator gains a read that must be authorized. An operator
  authenticates as a machine identity (ADR-0015), so the question is
  which definitions that identity may see — and ADR-0025's obligation
  applies to whatever asks the engine.

## Rejected alternatives

- **Keep the operator as the source of truth and let the console
  request.** The console would post an intent somewhere and an operator
  would pick it up, which is this decision with an extra hop and no
  owner of the resulting state. If garam holds the intent, garam holds
  the definition.
- **garam pushes definitions to operators.** It reverses ADR-0012's
  connection direction, requires garam to reach into a cluster it is
  deliberately kept out of, and makes every operator's availability
  garam's problem. Rejected on ADR-0012's original grounds, which this
  decision does not disturb.
- **Define agents in the operator's own custom resources and mirror them
  into garam.** Two writable copies of one fact, converging by luck. The
  same objection that makes `POST /agents` a problem now applies twice.
- **Decide the read surface here.** Its path, its authentication,
  whether it polls or watches, the shape of the values, and the operator
  matching key are all undecided. Naming them without deciding them is
  the honest state; guessing them would put a shape into another
  repository's first API version, which is exactly the cost this ADR's
  Context describes.
