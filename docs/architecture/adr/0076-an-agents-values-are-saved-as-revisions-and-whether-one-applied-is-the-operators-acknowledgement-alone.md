# ADR 0076: An agent's values are saved as revisions, and whether one applied is the operator's acknowledgement alone

> Status: accepted
> Date: 2026-09-28

## Context

A definition says which operator runs an agent and with what values
([ADR-0026](./0026-the-console-defines-an-agent.md)). Its values were
written once: garam could not tell whether an operator had acted on
them, so no update was offered. #1132, the configuration slice of #1126
and #1122, asks for the values to be editable on the agent's page and
for the console to tell saved values apart from what the operator
applied. Only Sherlock is in scope; no runtime selector, capability
matrix or key semantics are added.

What garam learns of an operator is what the operator reports. It
reports its pod's state at the epoch it holds the agent
([ADR-0053](./0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)),
and nothing names a set of values. Garam Agent Operator at
[`9dfeccf`](https://github.com/garamsh/garam-agent-operator/blob/9dfeccf/internal/garam/client.go)
reads one key family, `tools.pins.*`, lists every other key as ignored,
applies what it read when it first constructs an agent, and reports no
revision. garam's key rule — lowercase letters, digits and `-` — cannot
carry a `.`, so every value garam stores today is one that operator
ignores. A ready pod therefore says nothing about which values it runs.

`Agent.updatedAt` is written when the agent is created as well as on
every report, so it cannot say when health was last reported.

## Decision

- **A save is a revision of the one the editor read.** A definition's
  values carry a revision, 1 for the set it was defined with, and a
  save names the revision it was made against. Postgres compares the
  two in the statement that writes, so of two saves against one
  revision the second is refused under `aborted` and writes nothing.
  Only the values move: the agent's GRN, its operator, any claim's
  assignment and every team membership or policy attachment naming the
  GRN stay as they were. Saving is authorized on `agent:configure`,
  granted to the roles that hold `agent:define`.
- **A saved revision is not an applied one.** garam tells no operator
  and learns of application only from the operator holding the agent,
  on `POST /agents/{agent}/configuration-state`: `applied`, naming every
  key of that revision it did not apply, or `rejected`, with a reason
  of at most 500 characters. The route is fenced by the assignment and
  its epoch as the provisioning report is. The provisioning route never
  moves the acknowledgement, so an operator that reports no revision
  leaves application unknown whatever its pod reports.
- **What an acknowledgement may move.** A revision the definition has
  not reached is refused. So is one older than what the same epoch
  already acknowledged, which would write over it. A revision between
  the two is recorded. The same revision again replaces the previous
  report, outcome included, in the order garam receives them, and moves
  the time garam received it; garam keeps no ordering of the operator's
  own.
- **An acknowledgement belongs to the epoch it was made at.** It is
  stored with that epoch and describes the current assignment only
  while the epoch matches; after a reassignment it is the replaced
  operator's claim, and the successor's first acknowledgement is not
  bounded by it.
- **Health and configuration keep separate times.** `healthReportedAt`
  moves only on a provisioning or runtime report, and the
  acknowledgement carries its own received time. Rows written before
  this decision have neither and are shown as unknown: no time is
  inferred from `updatedAt`.
- **An acknowledgement is the operator's claim.** `applied` with ignored
  keys is applied in part, and `applied` with none is still what the
  operator said rather than something garam verified.
- **The agent's page edits the settings and shows their status.** The
  values are edited on the agent's page and saved as the next revision,
  and the page states the saved revision beside what the operator
  reported of it: unknown where nothing current was reported, pending
  where an earlier revision was, and applied, applied in part or
  rejected as the operator said. This supersedes, for the agent's page
  alone, the decision in
  [ADR-0075](./0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
  that the page says application is not reported; the template forms
  still say so, since no operator reads a template.

## Consequences

- An editor who lost a race keeps what they typed and is offered the
  saved revision to retry against.
- No current operator acknowledges a revision, so every agent's
  configuration reads unknown until one does. What would change that is
  an operator release reporting on this route; this record does not
  make one.
- The definition's values are no longer written once. ADR-0026 and
  [ADR-0033](./0033-an-operator-claims-the-agent-a-definition-mints.md)
  stand: neither decided that the values never change, and the claim,
  the operator and the scoping they describe are unchanged.
  ADR-0075 left changing the values to #1126, of which this is the
  configuration slice.
- Existing definitions hold revision 1, saved when and by whom they were
  defined.

## Rejected alternatives

- **Last write wins.** A second editor would replace the first without
  either being told, which is the loss #1132 asks garam to prevent.
- **Carry the revision on the provisioning report.** A readiness report
  and an acknowledgement are different facts with different times, and
  one route would let a legacy report without the field read as
  either.
- **Read the epoch from the assignment when the acknowledgement lands.**
  A report from the operator a reassignment replaced would then be
  stored under its successor's epoch and read as current, which is the
  fence ADR-0053 refused to retire.
- **Relabel `updatedAt` as the health time.** It is written on creation,
  so an agent nothing ever reported on would show a report time.
