# ADR 0088: garam's agent templates are retired read-only and not imported

> Status: superseded by [ADR-0117](./0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)
> Date: 2026-10-05

Drafted for #1188. The import contract was proposed on #1188 (comment
5994317407) and replaced by the PM's decision there (comment
5994335489), which this records. Facts about the Operator hold at
garamsh/garam-agent-operator@7c21646.

## Context

ADR-0077 makes the Operator control service the one writable source
of what agents are created from, templates included, and ADR-0075's
copy-once rule carries over. #1170 built the console's reads and
publication of the Operator's templates. garam still served its own
templates, created, replaced and deleted on routes of their own.

An import into the Operator would carry nothing a running agent used:

- A garam template's values were copied once into definitions, and a
  definition's keys are `[a-z0-9-]`, which the legacy poller never
  applied (ADR-0086, `archived:never-applied`).
- The Operator's template is a profile and a configuration of model,
  ego and tool pins, and has no description. None of a garam
  template's fields maps to it, so an import would publish a name
  and a profile chosen at import.
- The Operator's `templates` table is keyed by `(name, version)` with
  no organization, and garam's names are not unique (ADR-0075), so an
  import would first need questions answered that are the Operator's.

The deployment's migration runs as a pre-upgrade hook while the
release before it still serves
(`deploy/charts/garam/templates/migrate.yaml`), so a closure that only
removes routes does not order itself against a write that release is
making.

## Decision

**garam's templates are retired in place: every write is closed, the
rows stay and are read as records, and nothing is imported.**

- **The schema refuses every write.** Migration 000034 puts a trigger
  on `agent_templates` that refuses an insert, an update and a
  delete, as 000002 refuses a rewrite of a policy version. Creating
  it waits for every write already holding the table, which commits
  first; a write that reaches the table afterwards is refused, from
  whichever release sent it.
- **A template still goes with its organization.** A delete reached
  through the organization's own deletion is admitted, told apart by
  the organization's row being gone, so ADR-0065's deletion and its
  preview count stand.
- **The routes and actions are removed.** `createAgentTemplate`,
  `updateAgentTemplate` and `deleteAgentTemplate` leave the browser
  specification, and `agent-template:create`, `…:update` and
  `…:delete` leave the roles. `listAgentTemplates` and
  `getAgentTemplate` stay on `agent-template:read`.
- **The console offers only the Operator's templates.** The define
  form starts blank and offers no garam template; creating from a
  template is the control service's. Templates shows the control
  service's first and garam's below as archived records, each with a
  page that shows what it said and offers nothing to do with it.
- **Nothing is cut over per template.** There are no stages, no wire
  and no new operation, so no contract is added.

## Consequences

- A template's retirement changes no definition and no revision: no
  row of either is written, and a definition already copied keeps
  its values (ADR-0075).
- Rolling back below 000034 reopens the writes; the routes come back
  only with the release that served them.
- A row can be removed only with its organization, until a separately
  approved destructive change removes the table.
- ADR-0075's template actions, its whole-template replacement and the
  console's template forms are superseded; its copy-once rule, its
  names and its separation of requested from reported stand.
- ADR-0077's move of templates is met by retirement rather than by a
  cutover; its per-GRN cutover of definitions is ADR-0086's and
  unchanged.

## Rejected alternatives

- **Importing each template into the Operator.** It publishes nothing
  any agent used, and needs the Operator's organization scope, a
  profile per template and a rule for colliding names first.
- **Removing the routes alone.** It holds only while one release
  serves at a time; the schema guard holds for every writer.
- **A guard read in each write's own transaction.** It binds only the
  release that carries it, which the routes' removal already does.
- **Deleting the rows.** It destroys the record of what each template
  said, and destructive removal is a separate approval.
