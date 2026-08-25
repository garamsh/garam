# ADR 0045: An organization registers an operator, and a definition may name no other

> Status: accepted
> Date: 2026-08-25

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

Two decisions put an operator at the centre of how an agent comes to
exist, and neither left garam holding a record that one exists.

[ADR-0026](./0026-the-console-defines-an-agent.md) made the console the
place an agent is defined, including which operator is to run it.
[ADR-0032](./0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)
shipped the command that mints that operator's first certificate, from a
GRN whoever runs it composes by hand. Between the two, an operator
became real by being named — twice, in two places, with nothing
comparing the names.

`api/openapi.yaml` said so about its own field: whether the operator
exists "is not knowable here, because nothing registers an operator".
`migrations/000010` said the same about the column: no foreign key,
"and a typo would otherwise be a valid operator".

It is not hypothetical. The definition created through the console on
2026-08-25 names `grn:garam:default:operator:gagent-dev`, and garam
holds no record of that operator. Nothing reported it, and nothing
would have: a definition naming an operator that will never read it
sits there looking like every other definition.

## Decision

**An organization registers an operator, and `internal/operator` holds
the record.** A record is a GRN
(`grn:{org}:default:operator:{identifier}`), the organization that owns
it, the member that registered it, and when. It is created and listed
over the browser API under `/orgs/{org}/operators`, authorized through
the policy engine on `operator:register` and `operator:read` — actions
of their own, named action by action rather than as `operator:*`, for
the reason ADR-0012 makes agent actions so: what an operator's
lifecycle will need is undecided, and a wildcard grants it in advance.
Registering is an owner's and an admin's; reading is every member's,
because defining an agent needs the list to name an operator from.

**`AgentDefinitionCreate` refuses an operator the organization has not
registered.** The refusal names the identifier that named no record. It
is the identifier and not the GRN because every other segment was fixed
before the lookup, and because the console renders the sentence it is
given and prints no GRN ([ADR-0011](./0011-console-shell.md)).

**The identifier is supplied, not minted.** Every other org resource
garam creates takes a name and mints an identifier. An operator cannot:
its GRN is already the subject of a certificate wherever ADR-0032's
command has run, so a minted identifier would register something other
than the operator that exists. What garam applies is the segment
character rule, which is the whole of what a supplied identifier can get
wrong in shape.

**The schema carries the same rule.** `agent_definitions.operator_grn`
is a foreign key to `operators`, which is the copy no call path routes
around — the shape `migrations/000010` named as missing and said why.
It is added `NOT VALID`, so the rows written before the registry stay
as they are: validating them would need garam to delete a definition
somebody wrote or to invent a registration nobody made.

**A registration grants nothing.** It issues no credential, and no
listener reads it to decide anything. It is a record that the
organization says this operator is theirs, which is the thing every
credential mechanism under consideration needs underneath it.

## What this does not decide

Named here rather than left implied, because each is a decision someone
will otherwise read into the shape:

- **How an operator proves itself.** There is no enrolment ticket, no
  token, and no exchange route. A registered operator obtains its first
  certificate exactly as an unregistered one does, through ADR-0032's
  command, which reads no record here. Nothing in this ADR is a step
  toward one mechanism over another; that is the point of taking it
  separately.
- **Revocation.** garam has no revocation of any kind. A certificate
  that has not expired keeps authenticating whatever garam holds, so
  removing a record would stop nothing — which is why **there is no
  delete route**. A route that looked like revoking an operator and did
  not would be worse than the absence, and the absence is honest.
  Deleting the organization still takes its operators, because a
  tenant's rows go with the tenant.
- **Whether `issue-certificate` should require a registered operator.**
  It is a real question: it would catch the typo at the point it is
  made, one step earlier than this ADR does. It is also a change to
  ADR-0032's command — a command that deliberately reads nothing but
  the subject's service type before it touches the database — so it is
  decided on its own, not folded in here.

The seam where an enrolment mechanism attaches is this record and
nothing else: whatever proves an operator's identity will name the GRN
`operators` holds, and will not need the table's shape to change to say
so.

## Consequences

- **`api/openapi.yaml:1732` and `migrations/000010`'s note stop being
  true for definitions**, and both are corrected. The note still holds
  for `agent_assignments`: an operator holding a certificate creates an
  agent on the machine listener whether or not anybody registered it,
  and that is the question above, undecided.
- **A definition can now be refused for a reason the console can
  render.** A person who mistypes an operator learns which identifier
  named nothing, where before the definition was written and no one was
  told anything.
- **The console gains a section**, beside Agents, showing the operators
  the organization has registered. It shows identifiers and no GRN
  (ADR-0011), which is also what a person needs in order to run
  ADR-0032's command.
- **A deployment that already has operators has to register them.**
  Nothing migrates them in — the definitions naming them keep working,
  because the foreign key is `NOT VALID`, and a definition written after
  this needs the record. That is a one-time act per operator, by an
  admin who knows which operators are theirs, which is the only party
  that does.
- **`internal/operator` is a boundary of its own.** It has its own
  lifecycle and identity, `internal/api` composes it, and its surface is
  a registry that can stay still while an enrolment mechanism is decided
  above it. `internal/agent` does not import it: the browser surface
  makes the check, so the two domains stay uncoupled.

## Rejected alternatives

- **Keep the operator implicit and check nothing.** That is the state
  this ADR ends, and its cost is already paid on the live cluster.
- **Register an operator by minting its identifier, as groups and
  projects are.** An operator whose certificate already exists could
  then never be registered under the GRN that certificate names, which
  makes the registry unable to describe every operator that exists.
- **Put the record in `internal/agent`.** It already owns the operator
  service type and the assignment, so the pull is real. But an operator
  is the counterparty rather than the subject there: it exists before
  any agent and outlives every one it ran, and the writer of a
  registration is a console member rather than the operator that reports
  an agent. Folding it in would put two lifecycles behind one `Store`.
- **Put the record in `internal/org`.** `internal/org` holds the human
  structure — memberships, groups, projects, invitations. An operator is
  a machine identity, and a domain that holds both stops being about
  either.
- **Add a delete route now and document what it does not stop.**
  Considered because the issue allowed it. A route whose own
  documentation has to say it revokes nothing is a route that will be
  read as revoking something anyway, by whoever reaches it without the
  documentation. When revocation is decided, the route that removes a
  record can arrive with the thing that makes it mean something.
- **Backfill `operators` from the definitions that already name one,
  and validate the foreign key.** It would leave the schema fully
  validated, and it would do it by recording registrations no one made,
  attributed to no one. A registry whose first rows are fabrications is
  not a record of what an organization decided.
