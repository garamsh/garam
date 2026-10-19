# ADR 0082: A standalone host runs its agent under a supervisor an administrator approves by its keys

> Status: accepted — the host's key pairs, spending by certificate
> requests and the supervisor's certificate renewal are superseded by
> [ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md);
> the browser's legacy `operatorGrn` is superseded by
> [ADR-0117](./0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)
> Date: 2026-10-05

Decided by the PM for #1168 on 2026-10-04 and 2026-10-05, reviewing the
worker's contract proposals; ADR-0082 and migration 000031 were
reserved for it. It refines ADR-0077's standalone ownership and
ADR-0078's local lifecycle authority, whose bodies stand.

## Context

ADR-0078 lets a lifecycle authority activate a runtime generation:
managed, the Operator control service under its delegation;
standalone, "the local supervisor's narrowly scoped activation
credential". Nothing named that supervisor. An agent was assigned to
an operator only, the lifecycle authorizer read an operator's grant
only, and the only way a machine identity obtained a first certificate
without one was an operator's enrollment token (ADR-0064), which a
member mints and which spends on the first attempt.

A standalone host has no Operator service, no grant and no controller.
Registering a synthetic operator for it would put a host's authority
into a record and a grant built for a hosted service, and admit it to
every operator route. The host's runtime and the tools it installs run
as one account, so whatever the runtime can read, a tool can read.

## Decision

**A standalone host's lifecycle authority is a `supervisor`: a machine
identity garam registers for exactly one agent, on an administrator's
approval of the host's own keys.**

- **The host prepares, the administrator approves.** Before asking,
  the host generates its supervisor key, its agent key and a bootstrap
  secret, and keeps all three with a request identifier. The
  administrator approves the secret's hash, both public-key
  fingerprints and that identifier for one organization, one host name
  and one agent — a new one, or an existing one whose supervisor and
  epoch the approval names — expiring within a day. Approving is
  `agent:enroll`, held by owner and admin and decided from current
  state, an explicit Deny winning. An identical approval retry answers
  the same binding; a withdrawn or expired one is never revived.
- **Spending proves the approved keys.** The host presents the secret
  and one certificate request per approved key. The approver's
  authority is read again, and garam signs over the host's keys and
  never holds a private key. A new agent is written with an execution
  fenced from birth, its first assignment to the supervisor and the
  first certificate of its lineage, in one transaction. An identical
  retry answers the recorded public result only while that
  registration still holds the agent.
- **Assignment names an assignee.** An agent is held by an operator or
  by its supervisor; the browser's legacy `operatorGrn` never names a
  supervisor, and no operator route reaches a supervisor-held agent.
- **The supervisor activates its own agent on the existing execution
  routes.** Its current registration, its being the agent's assignee
  and current policy are read per request, the registration again
  under the execution lock; it covers activation, deactivation and
  introspection, and not recovery, and its certificate reaches no
  other route but its own identity and renewal. Its first
  activation rests on the approval; a managed creation's still needs
  its reference. Collaboration routes are unchanged: the generation
  and lineage fence (ADR-0080) is the one admission for both modes.
- **The supervisor's credential is bounded like every other.** It
  renews over a new key in the last third of its validity, and the
  certificate a renewal replaced may replay that renewal's public
  answer for the identical request. Its lifetime is the deployment's
  certificate lifetime.
- **Expiry, loss and compromise are an administrator's new approval.**
  A replacement approval names the supervisor and epoch it replaces;
  spending it reassigns the agent at the next epoch, ends the former
  registration and activation, and moves the agent to a new lineage,
  keeping its GRN. Revocation ends the registration, the activation and
  every unspent approval for the agent together. There is no standing
  recovery secret.
- **The supervisor's secret stays out of the runtime's reach.** It is
  kept outside the agent's memory, outbox and workspace, and a runtime
  or a tool must not reach it by mount or account. How the host
  supervises the runtime is decided separately (#1180).

## Consequences

- garam answers a standalone host's identity and authority and holds
  none of its desired configuration or memory.
- The supervisor's local runner, the control seam the adapter obtains
  its activation through, and the joint message, task and reply
  acceptance with Sherlock are unbuilt (#1180, #1169).
- `agent_assignments.operator_grn` is renamed `assignee_grn`, and the
  browser's assignment gains `assigneeGrn`.

## Rejected alternatives

- **A synthetic operator and grant for the host.** It admits the host
  to operator routes and puts a host's authority into a hosted
  service's delegation.
- **A one-time token alone, minted by a member.** A leaked token would
  enrol any key, a lost answer would strand the host, and a member is
  not who stands a lifecycle authority up.
- **A device-style approval service.** It needs a polling service the
  key-bound approval does not.
- **A permanent recovery secret on the host.** It outlives every
  revocation it is meant to survive.
- **The runtime's own certificate as lifecycle authority.** A runtime,
  and every tool it runs, could then activate and recover itself.
