# ADR 0053: An agent and its operator report state on the machine listener

> Status: accepted
> Date: 2026-09-01

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0012](./0012-agent-operator-model.md) decided that garam keeps two
facts about an agent that nobody else holds: what the operator says
about the pod, and what the agent says about itself. Neither has ever
been reported. `agents.md` opened with that under rule 9, and
`overview.md` §Out of scope named it: an agent is created at `pending`
and `unknown` and stays there for the life of the process, which is
indistinguishable from an agent that never started. The cluster holds
exactly that case — one agent resource in CrashLoopBackOff that garam
reports as it reports a healthy one.

What is missing is only the report. The store has held
`ReportProvisioningState` and `ReportRuntimeState` since the domain was
written, each taking the epoch and refusing any but the current one,
and the browser-facing listener already serves both fields on `Agent`
under `/orgs/{org}/agents`. Nothing above the store calls either, and
neither specification carries a route that would.

The reporters are the question this ADR answers. They are two different
parties, and they are not the same party as the reader:

- The operator reports provisioning. It authenticates on the machine
  listener with a certificate garam issued
  ([ADR-0015](./0015-operator-identity.md)) and holds no session.
- The agent reports its own runtime. It authenticates on the same
  listener, on the same terms — `machine-identity.md` records that an
  agent and an operator are the same kind of caller there and that the
  listener does not branch on which.
- A person reads both, signed in, on the browser-facing listener
  ([ADR-0004](./0004-openapi-first-api.md)).

The browser-facing listener admits a caller only by the `garam_session`
cookie ([ADR-0005](./0005-server-side-sessions.md)), which is a row
keyed to a user. Neither reporter has a user row, a session or a
membership to present, so neither can reach it at all.

## Decision

**Both reports are written on the machine listener, on a route of their
own each, and read on the browser-facing one where they already are.**
Concretely:

- **One surface for both writes.** `api/machine.yaml` describes them.
  The reporters authenticate there and nowhere else, so the surface
  follows the caller, which is what
  [ADR-0019](./0019-machine-openapi-spec.md) splits the two
  specifications on.
- **Two routes, not one.** An operator reports provisioning and an
  agent reports runtime. A single route would have to read the caller's
  service type to decide which of the two facts the body is allowed to
  move, which is the branch `machine-identity.md` says this listener
  does not make; two routes put the difference in the path, where each
  handler's check is the whole of its authorization.
- **Each report carries the epoch, and a stale one is refused.** The
  epoch is the fence ADR-0012 put on the assignment: a report is
  accepted only at the epoch the agent is currently assigned at, so the
  operator a reassignment replaced — and the pod it left running —
  writes nothing. A report at any other epoch is refused under
  `failed_precondition`, and an agent with no assignment has no current
  epoch, so every report about it is stale.
- **The operator route is fenced by the assignment as well.** garam
  answers an operator about the agents assigned to it at the current
  epoch and no others (ADR-0015), and a state report is one of those
  answers: an operator that does not hold the agent is refused under
  `permission_denied`, and an agent that does not exist is refused the
  same way, so a caller learns nothing about the agents it does not
  hold.
- **The agent route is fenced by the identity the connection proved.**
  An agent reports about itself and about no other, which is how
  `claimMessage` and `forwardTurnEvent` already bound an agent to its
  own resources.
- **Nothing is added to the browser-facing listener.** `Agent` already
  carries `provisioningState` and `runtimeState`, and `listAgents` and
  `getAgent` already serve them. What was missing was a writer, not a
  reader.

## Consequences

- garam can tell an agent that never started from one that is running.
  The distinction is only as good as the reports it is given: garam
  learns nothing on its own, and an operator or agent that stops
  reporting leaves the last report standing. Nothing here expires one.
- The lifecycle ADR-0026 described — defined, claimed, running — is
  now observable end to end. `agents.md` no longer opens with rule 9's
  unbuilt marker, and `overview.md` moves state reporting out of
  §Out of scope.
- An agent has to be told its epoch, and garam does not tell it. The
  epoch reaches the operator on `createAgent` and `claimDefinition`,
  and the operator passes it to the agent it constructs, as
  `AgentAssignment`'s own description already says. No route hands an
  agent its own epoch, and this decision adds none.
- Reporting is not deprovisioning. `failed` and `disconnected` are
  states an agent can be reported in; removing an agent stays out of
  scope.
- What calls these routes is gagent-operator's, not garam's. garam
  serves them and dials nobody (ADR-0012).

## Rejected alternatives

- **Put the provisioning report on the browser-facing listener.** The
  operator is not a signed-in person and has no session to present, so
  the route would need a second way to admit a caller on a listener
  whose admission is one cookie. That is the split ADR-0019 made the
  two specifications to avoid.
- **One route taking both facts.** The body would carry which fact it
  moves, and the handler would have to decide from the caller's service
  type whether it may. A stolen agent certificate would then reach the
  operator's fact through the same route, held off by a branch rather
  than by a path — and `machine-identity.md` records that this listener
  does not branch on which kind of caller it has.
- **Take the epoch from the assignment instead of from the report.**
  Reading the current epoch server-side and writing at it makes every
  report current by construction, which retires the fence ADR-0012 put
  there: the pod a replaced operator left running would report as
  readily as the one that replaced it.
- **Answer with the agent as it now stands.** The operator would read
  back the agent's own runtime state, which is the other party's fact
  and none of its business, and the agent would read back the pod state
  its operator reports. A report that garam accepted has nothing to
  return, so both routes answer `204`.
