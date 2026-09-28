# ADR 0075: An agent is started from a template copied once, and carries a name of its own

> Status: accepted
> Date: 2026-09-28

## Context

An organization says what an agent should be by writing a definition:
the operator that is to run it and the values that operator constructs
it from ([ADR-0026](./0026-the-console-defines-an-agent.md)). The
definition mints the agent's GRN, and no `agents` row exists under it
until the operator claims it
([ADR-0033](./0033-an-operator-claims-the-agent-a-definition-mints.md)).
The console showed an agent by that GRN's last segment and nothing
else: `web-app.md` and ADR-0072 recorded that a friendly name was
#1125's.

The console direction #1122 records puts people and agents on
separate pages, teams holding both, and templates as a view inside
Agents rather than a section of their own. #1125 asks for templates an
organization keeps and edits, for agent creation that starts from a
blank form or a template and lets the copy be edited first, and for a
friendly name separate from the agent's identity. It keeps out template
propagation, cloning an identity, team, policy or credential, and any
new runtime; the user has since limited agent support to Sherlock.

Two facts bound what a stored value can claim. What an operator does
with a definition's values is the operator's: garam stores keys and
strings and interprets neither (`api/machine.yaml` §Definition). And
the operator this project runs reads one key family, `tools.pins.*`,
which the value-key rule does not admit, at
[`9dfeccf`](https://github.com/garamsh/garam-agent-operator/blob/9dfeccf/internal/garam/client.go)
of `garamsh/garam-agent-operator`; model and ego have no consuming key
there (#1078, #1102).

## Decision

- **A template is an organization's own resource.** It is stored in
  `agent_templates` under a GRN of its own, service type
  `agent-template`, with a name, a description and values bounded as a
  definition's are (migration 000026). It names no operator and no
  agent. `internal/agent` owns it beside the definition, and it is a
  data carrier with exported fields, as `structure.md` asks of what an
  aggregate persists.
- **A definition copies a template once, and nothing links them
  afterwards.** The console reads the chosen template into the define
  form's rows, the person edits them, and the definition is written
  from what the rows hold. No column records the source, so editing or
  deleting a template reaches no agent by construction rather than by a
  rule each write keeps.
- **Only values are copied.** A template carries no identity, operator,
  team membership, policy or credential, so none can be cloned; each
  agent's GRN is minted by its own definition.
- **Templates have their own actions**, `agent-template:read`,
  `…:create`, `…:update` and `…:delete`, authorized against the
  template's GRN where the path names one. Owners and admins hold all
  four and members hold the read, as groups are held. A policy can
  therefore let someone define agents without letting them change what
  others start from.
- **An update replaces the whole template.** `updateAgentTemplate` is a
  `PUT` of name, description and values, which is what the edit form
  sends.
- **A defined agent carries a name.** `agent_definitions.name` is the
  organization's word for the agent: trimmed, one to 100 characters,
  not unique, bounded as an operator's name is
  ([ADR-0074](./0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)).
  000026 names every existing definition by its identifier, and
  `defineAgent` names one that leaves the name out the same way, so a
  caller written before names existed keeps working; a name given
  empty is refused. The console always asks for one.
- **Renaming moves the name and nothing else.** `renameAgentDefinition`
  is authorized on `agent:rename`, which every role holds, because it
  changes no value an operator reads and tells no operator anything.
  The GRN, the operator, the values, a claim's assignment and every
  membership or attachment naming the GRN stand. Changing the operator
  or values stays #1126's.
- **The console places templates inside Agents.** Agents lists every
  defined agent, claimed or not, and every reported agent no definition
  names; a view switch leads to Templates. Agents, an agent, a new
  agent, Templates, a template and a new template each have a page.
- **What was asked for and what was reported stay apart.** An agent no
  operator has claimed is shown as awaiting one, not with a state
  nobody reported. Its page shows the requested operator and values
  under their own heading, apart from the reported states.
- **Settings are a free key/value editor, claimed no further.** The
  forms label the values Advanced settings and an agent's page labels
  them Settings, each saying the values are saved and their application
  is not reported. No model, ego or other typed field is offered, since
  no operator has settled those keys (#1078), and no runtime or provider
  is offered, since agent support is Sherlock alone.
- **An agent is chosen by name where it is chosen.** Teams and
  Permissions read both agent lists, offer a defined agent before any
  operator claims it, and label each by name with its identifier and,
  where no operator has reported it, that it awaits one. A team adds it
  through the existing membership operation, which stores the GRN. A
  refused list is said to be refused, never read as empty.

## Consequences

- A template's later edit never changes an agent, and an agent's
  settings can differ from the template it started from the moment it
  is created.
- The same name can name two agents or two templates; every choice and
  every list shows the identifier beside it.
- Agents, Teams and Permissions each read the definitions beside the
  reported agents, so a caller refused one list still sees the other
  with the gap said.
- Deleting an organization removes its templates, and the deletion
  preview counts them.
- A saved value is stored, not proven applied; #1078 and #1102 decide
  what an operator consumes.

## Rejected alternatives

- **Recording the template a definition came from.** A link is what
  propagation would need and the issue refuses it; the console would
  also have to decide what a deleted source means.
- **Turning existing definitions into templates.** A definition mints
  an identity and names an operator; a template must do neither.
- **Authorizing templates on `agent:define`.** Defining an agent would
  then grant rewriting what every other member starts from.
- **Renaming on `agent:define`.** A rename is not a definition and must
  not read as one in a policy.
- **A unique name.** The identifier is already unique; a unique name
  would refuse two agents an organization wants to call alike.
- **A top-level Templates section.** The owner chose Templates inside
  Agents.
