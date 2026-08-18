# ADR 0033: An operator claims the agent a definition mints

> Status: accepted
> Date: 2026-08-19

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0026](./0026-the-console-defines-an-agent.md) decided that the
console defines an agent — including which operator runs it — and that
the operator reads the definition. It deliberately declined the read's
path, authentication, poll-versus-watch, and matching key, and it left
`POST /agents` standing as a second way an agent comes to exist,
recorded as a defect to reconcile.

What those deferrals cost is now measured rather than predicted. The
2026-08-19 end-to-end verification (QA report on file) drove both
halves against the live cluster: a definition mints an agent GRN that
nothing can claim or certify, and `POST /agents` mints an unrelated
one, so the console's two lists never meet. The gagent-operator's first
CRD (their #2/#3) is waiting on exactly this seam.

Three facts the decision rests on, all already in the tree:

- A definition carries its operator: `NewDefinition` requires the
  operator's GRN and validates it names an operator of that
  organization. The "which definitions are mine" question ADR-0026 left
  open is therefore already answered by the record itself — an operator
  reads exactly the definitions that name it.
- The operator already authenticates to the machine listener as a
  machine identity (ADR-0015), and every machine-surface read is scoped
  to the caller by construction (`GET /identity` answers about the
  caller). A definitions read of the same shape needs no policy check,
  because it can name nothing the caller is not named in.
- The store already holds assignment with an epoch, bumped on
  reassignment — the mechanism a claim transition needs exists.

## Decision

**The machine listener answers an operator with the definitions that
name it, polled, and an operator claims one.** Concretely:

- A read on the machine surface returns the caller's definitions — the
  agent GRN each minted, the values, and the claim state. Poll, not
  watch: the adapter already polls the message surface (ADR-0013), and
  a definition changes on human timescales.
- **Claiming binds the operator to the definition's agent GRN.** A claim
  writes the assignment between that GRN and the claiming operator —
  the epoch bumping as it does on any reassignment — and is what admits
  the operator to that agent's certificate route. Before the claim, the
  route refuses the GRN as unassigned, exactly as it does today.
- **A definition is claimed once.** A second claim of the same agent GRN
  is a conflict, not an override; re-aiming a definition at another
  operator is a new definition state, not a second claim of the old
  one.
- `POST /agents` keeps minting for agents no definition describes. The
  two paths cannot describe the same agent: a claimed GRN was minted by
  its definition and cannot be created, and a created GRN was never a
  definition's. The "two ways" defect ADR-0026 recorded is retired by
  construction rather than by choosing a winner.

## Consequences

- The lifecycle ADR-0026 described becomes real: defined (console),
  claimed (operator), running (reported). garam knows each directly or
  by report, and a console that conflates them is a bug, as before.
- The certificate route's rule changes shape: "assigned" now means
  created-and-assigned *or* claimed-from-a-definition. [ADR-0015](./0015-operator-identity.md)'s
  custody and refusal rules are untouched.
- Nothing here gates the read behind the policy engine, deliberately:
  the answer is scoped by the definition's own operator field, so the
  engine is never asked about a subject whose membership it cannot
  establish ([ADR-0025](./0025-engine-scope-is-a-caller-obligation.md)'s
  obligation does not engage).
- The gagent-operator can build its reconcile loop against this: poll
  definitions, claim, mint the certificate, run the agent. Its first
  API version stays free of any field naming an external system, as
  their #2 recorded.

## Rejected alternatives

- **Let `POST /agents` accept a definition's GRN.** That is the claim
  transition wearing the creation endpoint's clothes: it widens a
  minting route into a binding route and decides the matching key as a
  side effect, which is what this ADR exists to do in the open.
- **Watch instead of poll.** A watch earns its channel when events move
  at machine rate; definitions move at human rate, and the polling
  adapter is already deployed shape. A watch can be added later without
  changing what a claim means.
- **Let an operator re-claim to take over.** A silent takeover makes a
  stolen or duplicated operator credential an agent takeover; a conflict
  forces the re-aim through the console, where the org's intent lives.
