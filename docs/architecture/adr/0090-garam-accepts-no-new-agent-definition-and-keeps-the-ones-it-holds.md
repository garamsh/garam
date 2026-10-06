# ADR 0090: garam accepts no new agent definition, and keeps the ones it holds

> Status: accepted
> Date: 2026-10-07

Drafted for #1204, which reserved this number. The scope is the PM's
decision there (comment 6022695241), taken under the owner's
delegation of 2026-10-07. It supersedes the define route
[ADR-0026](./0026-the-console-defines-an-agent.md) and
[ADR-0075](./0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
decided, for new definitions only.

## Context

[ADR-0077](./0077-execution-definitions-move-to-the-operator-control-service-and-each-agent-cuts-over-alone.md)
makes the Operator control service the one writable source of what
an agent runs with. garam still served `defineAgent`: any member
could store a definition holding any key/value payload, and #1157
scenario 6 measured a member's session storing `model`, `ego`,
`instructions`, `tools-pins-github`, `profile` and an
`openAPIV3Schema` fragment that way, each answered 201 and kept
verbatim. Every such definition is legacy from birth, so each one
widened the set of agents a cutover still had to move.

Closing the route earlier would have left an organization unable to
create an agent. That precondition is now met: managed creation
through the control service is built, Garam Agent Operator #244
creates through it, and #1157 scenario 1 is verified on kind (#1157
comment 6022675475).

[ADR-0088](./0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)
already met the same situation for templates, and its reason for a
schema guard holds here unchanged: the deployment's migration runs as
a pre-upgrade hook while the release before it still serves, so
removing the route alone does not order itself against a write that
release is making.

## Decision

**garam accepts no new desired runtime value: no route writes a
definition, and the schema refuses one from any release. The
definitions it holds stay as written.**

- **The route and its action are removed.** `defineAgent` leaves the
  browser specification, so `POST /orgs/{org}/agent-definitions`
  answers 405 to every caller, and `agent:define` leaves every role.
  Reading, renaming, claiming and cutting over a definition are
  unchanged.
- **The schema refuses every new value.** Migration 000035 puts a
  trigger on `agent_definitions` that refuses an insert, and an update
  that changes anything but the name. Creating it waits for every
  write already holding the table, which commits first; a write that
  reaches the table afterwards is refused, from whichever release
  sent it. A rename is admitted (ADR-0075), and a delete is not
  refused, so a definition still goes with its organization.
- **No row is deleted or rewritten.** Every definition stays readable
  on `listAgentDefinitions` and `getAgentDefinition`, claimable by the
  operator it names (ADR-0033) and cut over as ADR-0086 decides; a
  switched one stays as the archival row ADR-0086 keeps.
- **An agent is created through the control service.** The console's
  new agent page offers the control service's templates alone, and no
  console form sends a value to garam.

## Consequences

- The set of legacy agents is closed: what ADR-0086 has to cut over is
  what garam held when 000035 ran, and nothing adds to it.
- ADR-0045's refusal of an unregistered operator on the browser
  surface goes with the route it was made on; the schema's foreign key
  still refuses one.
- Rolling back below 000035 reopens the writes; the route comes back
  only with the release that served it.
- Retiring the stored values, or the table, is a separately approved
  destructive change; until then a definition goes only with its
  organization.
- ADR-0026's console define route and ADR-0075's copying of values
  into a new definition are superseded. ADR-0026's record-not-command
  reading, ADR-0075's names and its separation of requested from
  reported stand.

## Rejected alternatives

- **Refusing only a request that carries values.** A definition with
  no values still mints a legacy agent with a GRN, an operator and a
  claim, which is what ADR-0077 moves to the control service.
- **Removing the route alone.** It holds only while one release
  serves at a time; the schema guard holds for every writer.
- **Deleting the definitions.** Legacy operators still claim them and
  ADR-0086 cuts them over from them, and destructive removal is a
  separate approval.
