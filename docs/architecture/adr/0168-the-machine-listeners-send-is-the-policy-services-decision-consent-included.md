# ADR 0168: The machine listener's send is the policy service's decision, consent included

> Status: accepted; built in `platform/internal/machineapi`,
> `platform/internal/cli` and `platform/tests/integration` (#1941)
> Date: 2026-11-16

Drafted for #1941, which reserves this number, under #1796. It takes
the round [ADR-0147](./0147-a-message-between-two-consenting-organizations-is-tenant-isolations-one-exception.md)
left owed: the send path asking the policy service about a message,
where it read the platform's own consent relation and the engine.
[messaging.md](../messaging.md) and [authz.md](../authz.md) state the
rule beside the code it governs.

## Context

The service holds each organization's half of a consent as an
attribute of its `Org` and decides a `message.send` between two agents
by it (ADR-0147, #1805). The machine listener did not ask. It read the
consent relation itself, refused a crossing before the engine, and sent
the engine the sender's statements and groups for a send inside the
organization. Two decisions of one send lived in two places.

The service's answers were measured against the stack for the five
shapes a send takes: an agent of the sender's organization, an agent
of a consenting organization, an agent of one that does not consent, an
agent the service holds no entity for, and an organization. They are
allow, allow, deny with `tenant-isolation`, deny with the reason that
the entity does not exist, and `INVALID_ARGUMENT`.

Three things the service does not give a sender follow from those
answers:

- **Absence inside the organization.** A recipient that is no agent
  garam holds is ADR-0013's routing failure, which the sender is told
  by a stored message and never by a rejection. The service answers a
  missing agent with a deny, and a deny inside the organization is a
  403, so asking would turn a routing failure into a rejection.
- **A recipient that is not an agent.** The service refuses an
  organization as a recipient with `INVALID_ARGUMENT`, an error and
  not a decision, and an error cannot be told from an outage.
- **What an answer lets a sender learn.** A refusal that differed for a
  missing consent, a withdrawn one and an absent agent would tell the
  sender which organizations exist and which agents they hold.

Two ways to keep the listener's own check were rejected. Reading the
relation beside the service would leave two decisions of one send.
Falling back to the relation when the service cannot be reached would
decide a send on a rule the organization's own policies and guardrails
do not bind.

## Decision

1. **A send is the policy service's decision.** The machine listener
   asks it `message.send` of the sender and the recipient, and nothing
   else is read to decide it. The consent relation, the sender's
   statements and its groups are not read on this path.
2. **Inside the sender's organization, an absent agent is admitted
   without asking.** A recipient GRN naming no agent garam holds, a
   non-agent type or an agent identifier with no row, is stored as the
   routing failure ADR-0013 keeps storable. What garam reads to tell is
   the sender's own organization's agents and no other's.
3. **Across the boundary, a recipient that is not an agent is refused
   without asking.** That is a user, an operator, or an organization's
   root GRN.
4. **Every other recipient is the service's answer.** That is an agent
   the sender's organization holds, and every agent GRN across the
   boundary whether or not it exists.
5. **A denial across the boundary is garam's refusal, stored.** The
   sender is answered 201 and a message from the intended recipient
   carries one text that names neither consent nor absence, for items 3
   and 4 alike, so what the sender reads is the same for every reason.
   The send's audit event is a refused one with no message
   (ADR-0106).
6. **A denial inside the organization is a rejection.** The refused
   send is recorded as ADR-0106 states, and the sender is answered 403
   `permission_denied`, "sender may not send to this recipient".
7. **An unreachable service fails the send closed.** Any error of the
   service, an outage and a refusal of the question that is not a
   denial alike, stores nothing and answers 503 `unavailable`, so an
   adapter retries.
8. **`serve machine` needs the service to start.** It builds its
   decision client from `GARAM_POLICY_ADDR` and the credentials the
   feed already uses, and with no address configured fails at start,
   naming the variable. There is no fallback.
9. **The policy matrix stops asking the platform about a send.** The
   platform has no decision of its own for a send to compare, so the
   comparison drops its send rows, and the suite drives the real
   listener against the real service for each case above.

## Consequences

- **One decision per send.** The organization's policies and
  guardrails, both organizations' halves and the engine's tenant
  isolation are the service's, so the platform no longer reads the
  consent relation for a send.
- **A service outage is a send outage.** A send waits on the service,
  and an adapter retries a 503. The listener's other decisions do not
  wait on it.
- **A cross-boundary send to an agent that does not exist is refused
  in the same words as one to a withheld consent.** A sender cannot
  learn which. A send to an absent agent of its own organization is
  stored as before.
- **The matrix counts fall by seven.** The seven platform send rows are
  gone, so it asks 1716 questions, 1706 agree and 10 differ, as the
  ten of [policy-matrix.md](../policy-matrix.md) §Differences do.
- **Tests of the attached policy move.** What an attached policy or a
  group attachment decides for a send is the service's and is held by
  its suites; the listener's tests hold what the listener does with an
  allow, a denial and an error.
- **A deployment without a policy service address cannot start `serve
  machine`.** The local stack configures one, and the chart is not
  changed by this decision.

## Signs the decision was wrong

- A sender needs to tell a missing consent from an absent agent to act
  on a refusal. The neutral text then costs more than the disclosure,
  and the question is whose it is to disclose.
- The existence read of the sender's own organization drifts from the
  service's entities, so a recipient garam holds is one the service
  denies as absent. A same-organization send then answers 403 where
  the row says it should be allowed, and the feed is behind.
- An outage of the service stops sends often enough that an adapter's
  retry is not an answer. The remedy would be a decision the listener
  can make without it, which this decision rules out.
- A second kind of recipient wants to be admitted without asking, and
  the list of items 2 and 3 grows by case rather than by a rule.
