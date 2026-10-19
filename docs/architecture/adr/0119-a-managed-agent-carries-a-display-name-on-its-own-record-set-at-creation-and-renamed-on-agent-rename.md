# ADR 0119: A managed agent carries a display name on its own record, set at creation and renamed on agent:rename

> Status: accepted
> Date: 2026-10-20

Drafted for #1610, for which the Garam PM reserved this number. The
PM decided its shape on #1610 on 2026-10-19, and corrected the roles
that hold `agent:rename` on 2026-10-20. The owner wants the next
agent on aurora named `sherlock`. garam is read at
`03d7ed6e9e9697775fe42f6eabe8934b5a7875c3`; a claim below that names
no revision holds for it.

## Context

- **ADR-0118 took the name away.** An agent's name lived on its
  definition
  ([ADR-0075](./0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)).
  [ADR-0118](./0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)
  deleted every definition, and `agent:rename` with them. Its
  §Consequences reads "An agent carries no name in garam", and its
  §Signs the decision was wrong names this case: a person needs a
  name to tell agents apart, and the name wanted a home of its own.
- **A managed creation carries none.** `managed-enrollment.v2`'s
  creation and `agent-enrollment.v1`'s managed binding name a
  controller, a request identifier and a reference, and nothing a
  person reads. The console shows each agent by its identifier.
- **A retry is the same request.** Both routes answer a retry with the
  agent the first request created, and refuse a retry that binds
  anything else
  ([ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
- **A list and a read are authorized on the object.** A policy denying
  an action on one agent refuses that agent alone
  ([ADR-0115](./0115-a-list-drops-each-object-its-caller-may-not-read-asked-of-the-engine-in-one-batch.md)).

## Decision

**A managed agent carries an optional display name on its own record.
The name is set when the agent is created and renamed on the browser
surface under `agent:rename`.**

1. **The name is the agent's.** `agents.name` is nullable, trimmed
   and 1 to 100 characters, the bound `agent.NameMaxLength` holds. It
   is what the organization calls the agent and nothing else: no
   operator reads it, and its GRN, assignment, lineage and tokens do
   not move with it.
2. **It is set at creation.** The name is optional on
   `managed-enrollment.v2`'s creation and on `agent-enrollment.v1`'s
   managed binding. Each route keeps the name it was asked for beside
   the request, so a retry is the same request only when it asks for
   the same name, or for none where the first asked for none. A
   replacement moves an agent that keeps its name, so a replacement
   binding naming one is refused.
3. **It is renamed on its own action.**
   `PATCH /orgs/{org}/agents/{agent}` sets the name or clears it with
   `null`. It is authorized on `agent:rename` against the agent's own
   GRN, so a per-object Deny applies. `agent:rename` is restored as it
   stood before ADR-0118: a grant of the owner, admin and member roles.
4. **The console shows it, falling back to the identifier.** The
   agents list, an agent's page and the command palette show the
   name, and an agent with none is shown by its identifier. The
   organization search matches the name as well as the identifier.

This supersedes ADR-0118 in part: "An agent carries no name in garam"
and the removal of `agent:rename`. Everything else ADR-0118 decided
stands, the definitions' removal among it.

## Consequences

- A person tells agents apart by a name they chose, and the hosted
  control service can name an agent when it creates one.
- A rename touches the agent's row alone. `updatedAt`, which a state
  report moves, does not move with it.
- Two agents may share a name. The identifier still tells them apart,
  and a selector offers the two together.
- A sentence about an agent with no name still says "an unnamed
  agent". The identifier stands only where a name is shown alone.
- The machine routes grow an optional field, which a caller that
  sends none does not see.

## Signs the decision was wrong

- Operators want to read the name, to label a pod or a workspace: the
  name was configuration after all, and wanted a place in what an
  operator reads.
- People keep renaming agents to repeat what the identifier says: the
  fallback to the identifier was enough, and the name is noise.
- Names collide often enough that people act on the wrong agent: a
  name unique per organization was wanted.
