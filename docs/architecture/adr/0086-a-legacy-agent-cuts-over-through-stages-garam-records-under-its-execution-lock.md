# ADR 0086: A legacy agent cuts over through stages garam records under its execution lock

> Status: accepted
> Date: 2026-10-05

Drafted for #1171. The contract was proposed on #1171 (comment
5987172687) and decided by the PM there (comment 5987261323); the
Operator PM agreed to it for garamsh/garam-agent-operator#217, and the
PM confirmed the rollback window (comment 5988124767). The error
reason field was decided by the PM on this pull request's dispatch.
Facts about the Operator hold at garamsh/garam-agent-operator@85cf8c9.

## Context

ADR-0077 moves an agent's desired values to the Operator control
service one GRN at a time, with no dual write. ADR-0078 makes the
first activation the GRN's cutover. Four facts of garam at `739644b`
shape how:

- **A legacy agent's first activation had no precondition.** A holder
  of `agent:activate` could fence a legacy agent with nothing imported,
  and nothing returns an execution to `legacy`.
- **An ownership-only switch leaves the mode `legacy`.** No restart
  means no activation, so the mode cannot record that the Operator owns
  the source.
- **A definition's operator and values are written once.** Only its
  name changes, so a freeze has no value write to race.
- **No key garam holds was ever applied.** Garam's keys are
  `[a-z0-9-]` (`internal/agent/definition.go`), and the legacy poller
  applies only `tools.pins.*` keys (`internal/garam/client.go` and
  `poller.go` at `85cf8c9`).

## Decision

**A legacy agent cuts over through five stages under the GRN it has,
and garam records its two in `agent_cutovers` under the agent's
execution lock.** The contract is `agent-cutover.v1`;
`api/machine.yaml` §cutover is authoritative for its wire.

| # | Stage | Owner | Record |
|---|---|---|---|
| 1 | Inactive import of the source read from garam | Operator | its import, keyed by GRN and source digest |
| 2 | Freeze, verified against the import's digest | garam | attempt `frozen` |
| 3 | Switch | garam | attempt `switched` |
| 4 | Source switch and release to the controller | Operator | its marker, one-way |
| 5 | Retirement | garam | none: `switched` is it |

- **The source and its digest.** The source is the agent's definition,
  or none: an agent no definition describes cuts over with empty
  values. The digest is the SHA-256 of the RFC 8785 JSON of
  `{agentGrn, operatorGrn, values}`, where `operatorGrn` is the
  definition's operator, or the assignee where there is no definition.
  The name is not covered and stays garam's and renamable.
- **The freeze verifies in the same transaction.** It takes the
  execution row and then the definition row, the lock a claim takes,
  rechecks the assignment, the epoch and the grant's expiry, requires
  a legacy execution and no other open attempt, and records `frozen`
  only if the digest of the rows under those locks equals the
  import's.
- **Attempts are keyed by the import's identifier.** A retry answers
  what its stage recorded. At most one attempt per agent is open; no
  request reaches a rolled-back attempt, which is what refuses a late
  replay.
- **Rollback ends at the switch.** A frozen attempt may be rolled back,
  which lists the definition to its operator again. A switched one may
  not, because the Operator's marker cannot return to garam; that is a
  reverse migration.
- **The freeze boundary is server-owned.** From `frozen`,
  `listDefinitions` omits the GRN; a claim stays refused, since only a
  claimed agent has an execution row to freeze. From `switched`, the
  operator's certificate route answers 409, deciding under the
  execution row `FOR SHARE` after signing, so no certificate is
  answered once a switch has committed. A legacy agent's first
  activation is 409 `cutover_required` until its switch. The browser's
  definition reads carry the open attempt's stage. No request field
  selects any of it.
- **The values are preserved.** Nothing here writes or deletes a
  definition. A switched one is archival and read-only, and removing
  it is a separate approval.
- **Authority.** Each of the four routes is authorized as
  `activateAgent` is, and carries an owner's or administrator's
  durable `agent:cutover` reference for this caller, agent and
  assignment, rechecked from current state on every stage.
  `agent:cutover` is a new delegated operation, held by the owner and
  administrator roles, where an explicit Deny wins.
- **The inventory.** `garam cutover-inventory --org <slug>` reads one
  snapshot of the database its configuration names, prints one line
  per agent, unclaimed definition and template with a SHA-256 of each
  value and no value, and closes with counts.
- **A refusal names its reason.** The machine `Error` keeps `kind` as
  the errorx kind and gains an optional `reason`. The cutover routes
  set `digest_mismatch`, `attempt_open`, `attempt_ended`,
  `not_cut_over_eligible` or `reverse_migration_required`;
  `activateAgent` sets `cutover_required` and nothing else. A consumer
  branches on `reason` only on a route whose contract lists it, and a
  new reason is a new contract value.

## Consequences

- `activateAgent` refusing a legacy, unswitched agent changes
  `execution-fence.v1` behaviour. It is coordinated with the Operator
  through garamsh/garam-agent-operator#217 rather than assumed
  compatible (ADR-0079).
- Between garam's switch and the Operator's release, no desired writer
  is active. The agent keeps running on what it was built from.
- Retained definitions keep blocking `operator:delete` until their
  removal is approved.
- Templates are not cut over here; their move is #1188's.
- The Operator's import, its disposition of keys never applied and its
  release are the Operator's, and are unbuilt.

## Rejected alternatives

- **Freeze and verify as separate calls.** They add a frozen but
  unverified state whose failure needs an unfreeze, and the values
  cannot change after the freeze anyway.
- **The execution mode as the cutover state.** An ownership-only
  switch never moves it, and a stray activation moves it with nothing
  imported.
- **Reusing `agent:configure`.** A reference minted for a revision
  change would then authorize moving ownership.
- **Recording the switch after the Operator releases.** A crash in
  between leaves two sources.
- **A refusal's name in `kind`.** `kind` is the errorx kind on every
  route, and a consumer would have to know per route which it reads.
