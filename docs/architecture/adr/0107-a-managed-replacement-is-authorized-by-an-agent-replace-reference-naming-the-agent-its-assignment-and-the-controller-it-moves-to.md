# ADR 0107: A managed replacement is authorized by an agent:replace reference naming the agent, its assignment and the controller it moves to

> Status: accepted
> Date: 2026-10-18

Accepted by the Garam PM on 2026-10-18 under the owner's delegation,
recorded in the review on #1491.

Drafted for #1487, which reserves this number. garam is read at
`d0e290d6d10fd1ad548d1105173e43cb1bdb6ac1`, the merge of #1486. A
claim below that names no revision holds there.

## Context

ADR-0102 §Common agent enrollment admits a replacement by keeping the
GRN, requiring the expected assignee and epoch, raising the epoch and
replacing the lineage. It names the managed reference for a new agent
only: the member's `agent:create`. #1486 built the wire for a
replacement and refuses it as `replacement_unavailable`, because no
reference kind says "this member may move this agent to that
controller". Three facts about the references garam has decide which
can:

- **What each binds** (`internal/operator/authority_service.go`
  `placement`). `agent:create` targets the controller a new agent
  will run on and is decided on the organization; it names no agent
  and snapshots no assignment. `agent:configure`, `agent:recover`,
  `agent:execution-read` and `agent:cutover` target an existing agent,
  are decided on that agent, snapshot its current assignee and epoch,
  and are revoked by `recheck` the moment that assignment moves. None
  of the five names a second controller.
- **Who holds each** (`internal/org/role.go` `machineActions`). A
  member holds `agent:create`; only owners and administrators hold
  `agent:recover`, `agent:enroll` and `agent:cutover`, because each
  fences or takes over what an existing agent runs on.
- **What a grant covers** (`operator.Grant.Covers`). A grant names the
  controllers its operator may act for. A reference on an existing
  agent is checked against the controller the agent is on, at mint and
  at every read; nothing checks the controller it would move to.

A replacement ends the agent's activation, fences every certificate and
token of its lineage, and hands its lifecycle to another controller. It
is a write on an existing agent, as the direct placement's replacement
already is: `agent:enroll` is decided on the replaced agent there
(`internal/agent/standalone_service.go` `Approve`).

## Decision

**A managed replacement under `agent-enrollment.v1` is authorized by an
owner's or administrator's `agent:replace` reference: a new delegated
operation that targets the replaced agent, snapshots its current
assignment and names the controller it moves to.** Admission goes
through the existing `admit` primitive's replacement branch.

- **What the reference binds.** `agent:replace` is an operation on an
  existing agent, so it binds what the others bind: the agent as
  target, decided on the agent, with the assignee and epoch it is on at
  mint. It also binds one destination, a controller of the same
  organization, stored beside the snapshot. It binds no request target:
  the approval it authorizes is not known when it is minted, and the
  approval's own binding is what the two admission stages compare.
- **What the grant must cover.** At mint and at every read the
  audience's current grant covers `agent:replace` for both the
  controller the agent is on and the destination. Covering only the
  first would let an operator move an agent onto a controller its grant
  does not name.
- **What admission compares.** Prepare and admit each read the
  reference fresh and accept a replacement only where its kind is
  `agent:replace`, its audience is the caller, its target is the
  binding's replaced agent, its snapshot is the binding's expected
  assignee and epoch, its destination is the binding's assignee and its
  request identifier is the binding's. A mismatch of kind, audience or
  organization is `ErrReferenceMismatch`; of the rest,
  `binding_conflict`. The approval records the expected assignment, so
  a retry under the same request identifier is compared on it.
- **What admission writes.** Under the agent's execution row (ADR-0080)
  `admit` moves the assignment from exactly the expected one, raises the
  epoch, ends the activation and moves the lineage, as a standalone
  replacement already does. The admission records the replacement as
  that agent's current managed creation, so `mintAgentToken` and the
  first activation read the new controller, epoch and lineage, and a
  token of the old lineage refuses from the next request. A retry
  answers the recorded admission while the agent is still where it put
  it, and `superseded` once it is not.
- **Who holds it.** Owners and administrators, where an explicit Deny
  wins, as for `agent:recover` and `agent:cutover`. A member's
  `agent:create` does not reach it.
- **What it does not change.** `agent:create` keeps targeting one
  controller for a new agent. `agent:cutover` keeps binding one
  cutover route's request target at the existing assignment, and its
  routes are unchanged. Direct placement stays refused (#1355).

## Rejected alternatives

- **Reading `agent:cutover` as also authorizing a move.** Its
  reference already targets the agent, snapshots its assignment and is
  held by owners and administrators, which is most of what a
  replacement needs. It names no destination, so the controller the
  agent moves to would be chosen by the caller and covered by nothing a
  member decided. Its request target binds one of four cutover routes;
  binding an admission route as well would make one action two powers,
  moving an agent's source and moving its assignment, and a policy
  could not grant one without the other. ADR-0086 rejected reusing
  `agent:configure` for cutover on that ground.
- **Extending `agent:create` to name an existing agent.** It is decided
  on the organization and held by members, so the extension would let
  every member who may create an agent fence the lineage of any agent
  of the organization and move it. Deciding it on the agent instead
  only for the replacing form would put two authorization rules under
  one action name, which `recheck` and a policy author would each have
  to tell apart by the binding's shape.
- **Reading `agent:recover` as also authorizing a move.** It replaces
  a lineage at the current assignment and is held by the same roles,
  but it is a short-lived handoff bound to the digest of one recovery
  body, and it names no destination either. A reference minted to
  recover a lost credential would then also move the agent.
- **Two references, `agent:create` for the destination and
  `agent:recover` for the agent.** Each stays spendable for its own
  operation, the pair is not one intent a member approved, and nothing
  binds them to each other.
- **Binding the destination in the body digest.** The digest binds the
  request the Operator receives, not the admission body garam reads,
  so garam could not compare the destination with what it admits.

## Consequences

- A migration adds `agent:replace` to the operations a grant and an
  authority may carry and a destination column that is present exactly
  for it. `operator.Binding`, the mint and `recheck` carry the
  destination; the grant check reads both controllers.
- The console and the Operator need a way to mint the reference before
  a replacement can be asked for. Until they have one, garam admits a
  replacement only for a caller holding such a reference.
- `replacement_unavailable` leaves `agent-enrollment.v1`'s refusal
  vocabulary once replacement is built; a client that branched on it
  sees `binding_conflict`, `superseded` or a reference refusal instead.
- An owner's or administrator's policy that grants actions one at a
  time does not grant `agent:replace` until it names it.

## Signs the decision was wrong

- A replacement is admitted onto a controller the audience's grant does
  not name, or after the agent moved from the snapshot its reference
  holds.
- A policy author needs to grant moving an agent without granting
  something else, or the reverse, and `agent:replace` is not where that
  line falls.
- A member who can create agents but holds no `agent:replace` moves an
  existing one.
- A token or certificate of the replaced lineage is accepted after the
  admission commits.
