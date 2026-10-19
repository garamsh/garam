# ADR 0113: The console is Roster, one policy-shaped UI where people and agents are members

> Status: superseded in part by ADR-0116 (Members and the sidebar)
> Date: 2026-10-18

Drafted for #1547, which reserved this number. It supersedes
[ADR-0103](./0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
in part, in three places:
- §Shell and navigation's grouped sections;
- §Page templates as the whole set of pages a console has;
- the first bullet of §Role-aware UI, which hides a denied action
  where the caller has no use for it.

The rest of ADR-0103 stands: its principles, type, colour, shape,
primitives, patterns and component base. garam is read at
`37567a41305da74a3fab2ece97820bac29f5a5b5`. A claim below that names
no revision holds for it.

## Context

On 2026-10-18 the owner made two decisions (#1547):
- there is one UI for everyone, and authority comes only from the
  policies attached to a principal;
- garam is becoming a place where people and agents work together.

ADR-0103 answered a different question: how an administrator
operates agents. Its sidebar groups sections by kind of
infrastructure: Workloads, Access, Infrastructure and Organization.
A member who can only read lands in the same groups as an owner. They
meet controls they cannot use, or none at all where a denied action
is hidden. Work items, when they arrive, would need a group of their
own.

Five directions were drawn for the redesign. They are in the design
page at https://claude.ai/artifact/6fgTkEzZgDxq8UBoHe9nsa (v2, read
2026-10-18): Ledger, Casebook, Switchboard, Roster and Atlas. The
owner chose Roster on 2026-10-18. That page is reference material,
not a specification. Its mockups are not copied into this
repository, and nothing here depends on their pixels.

How other products answer the same questions, read on 2026-10-18:
- **Agents as teammates.**
  - Linear delegates an issue to an agent from the Assignee field,
    and "the human teammate remains responsible"
    (https://linear.app/docs/agents-in-linear).
  - GitHub assigns an issue to Copilot like a user
    (https://github.blog/changelog/2025-10-28-a-mission-control-to-assign-steer-and-track-copilot-coding-agent-tasks/).
  - Asana's AI Teammates "follow the exact same rules as your
    colleagues"
    (https://help.asana.com/s/article/understanding-access-control-for-ai-teammates).
- **Explaining access.** Google Cloud's Policy Troubleshooter reports
  which policies decided a principal's access to a resource
  (https://docs.cloud.google.com/iam/docs/troubleshooting-access).
  AWS's access-denied messages name the policy type behind a deny
  (https://docs.aws.amazon.com/IAM/latest/UserGuide/troubleshoot_access-denied.html).
- **Denied, but visible.** GitHub Primer prefers an inactive button
  to a disabled one. It stays focusable and says why the action is
  blocked (https://primer.style/product/components/button/guidelines).
- **Accountability.** Microsoft Entra Agent ID requires a human
  sponsor for each agent identity
  (https://learn.microsoft.com/en-us/entra/agent-id/agent-owners-sponsors-managers).

What garam already holds that Roster rests on:
- **Explain.** The policy engine's `Explain` RPC reports which
  statements matched a request. Nothing calls it: no specification
  carries the operation ([authz.md](../authz.md) §Explain).
- **The permission check.** `checkPermissions` answers a list of
  actions in one request. Each answer is allow or deny, never why,
  and names no statement or policy (`api/openapi.yaml`).
- **No holder lookup.** No operation answers which principals hold an
  action.
- **Audit.** It records message sends alone, and nothing reads them
  through garam ([audit.md](../audit.md),
  [ADR-0106](./0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)).
- **Watch.** Watching an agent is best effort and never a record. It
  replays nothing and marks no gap
  ([ADR-0022](./0022-watching-an-agent.md), `watchAgent`).
- **Mixed teams.** A team already holds people and agents
  ([ADR-0072](./0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)).
- **Who defined an agent.** An agent definition carries `definedBy`
  (`api/openapi.yaml`, `AgentDefinition`). An agent a control service
  created has no definition in garam ([web-app.md](../web-app.md)
  §What it serves, Agents).

## Decision

**The console is Roster.** It is one UI shaped by the caller's
policies, where people and agents are members of one organization.

### One UI for everyone

- **Every person uses the same screens.** garam has no fixed admin
  tier: every object is a GRN, and authority comes only from the
  policies attached to a principal. The console is never split into
  an admin surface and a member surface.
- **Policies decide what each person sees and can do.** The same page
  serves an owner and a reader.

### Home

**The console opens on Home, computed for the person signed in.** It
holds:
- **Needs you**, which is empty until work items exist;
- **their teams**;
- **things worth a look**: unhealthy objects their policies let them
  read;
- **a generated lede sentence** that summarises the above, taken from
  Casebook. Its links replace the plain failing banner.

### Members

**Agents are members, like people.** People and agents share one
Members list, one profile layout and one activity timeline, as
Linear, GitHub and Asana treat an agent.

**The kind of member is shown by shape, not colour:**
- people are round;
- agents are rounded squares;
- teams are dashed;
- hosts are square.

ADR-0103's single accent stays (its principle 2).

### Your access

**Every object page carries a Your access panel.**
- For each action, it shows the grant behind it.
- For a denied action, it shows the missing permission and who holds
  it.
- It reads access from the root down: the path of scopes, and what
  each grants. This is taken from Atlas, following AWS's access-denied
  messages and Google Cloud's Policy Troubleshooter.

**A denied action stays visible.** It renders as an inactive control
that states its reason, rather than being hidden. This replaces
ADR-0103 §Role-aware UI's first bullet. Two rules of that section
stand:
- an unanswered check shows the control;
- the console infers no permission from a role it read.

### Command palette

**⌘K finds objects and actions**, both taken from Switchboard:
- it finds people, agents, teams and policies;
- `.` opens the current object's actions, each with its consequence
  spelled out.

### What stays from ADR-0103

**ADR-0103's visual system stays whole.** Its tokens, type, the one
accent, the primitives and the patterns are unchanged. So are its
List, Detail, Settings and Wizard templates for the pages that are
one of those. Roster adds Home and Members beside them, and replaces
the grouped sidebar.

[ADR-0011](./0011-console-shell.md)'s shell decisions also stand:
- `Organizations → organization → section`;
- the org switcher;
- a breadcrumb from the live path;
- no GRN printed.

### What comes later

**Collaboration slots in without new navigation.** Work items,
handoff and review go into Needs you, a Work tab and the shared
timeline. None of them exists yet.

### Open questions

- **An accountable person per agent.** This is Entra Agent ID's
  sponsor, or Linear's "remains responsible". garam has no such
  field, and this ADR does not add one; the owner decides.
  - Until then, an agent's page shows **Defined by**, read from the
    definition's `definedBy`.
  - An agent no definition names shows none.
  - #1547 asks for "Defined by" read from audit. Audit records no
    definition event, so the definition is the only source garam
    holds today.
- **The source of Home's feed and the shared timeline.** Measured
  against what garam holds:
  - audit records message sends alone, with no read route;
  - watch is best effort and keeps nothing;
  - nothing records a membership, definition or policy change for a
    reader.

  So the timeline and the "worth a look" feed have no source today.
  Which domain records them, and which action governs reading them,
  is undecided.
- **What Your access needs from the API.** Each of these is a backend
  change of its own, and none is decided here:
  - an operation that reaches `Explain`;
  - a lookup from an action to its holders, for "who holds it".

  The permission check already answers a page's actions in one
  request.

## Consequences

**Navigation and the page model are replaced.**
- Every console route moves into Roster's navigation.
- The table of sections in [web-app.md](../web-app.md) is rewritten
  when the first Roster route lands.
- ADR-0103's grouped sidebar is retired then, not now. Until Roster is
  built, the console stays as web-app.md describes it.

**Members and the timeline are shared components.** Members merges the
People section with the agents a person reads. One profile layout and
one timeline then serve every member page.

**A member who can only read now sees every action.** Each one they
cannot take renders inactive, with its reason. A page with many
denied actions shows more controls than ADR-0103's would.

**Your access depends on backend work that does not exist.** Until
`Explain` and a holder lookup are reachable, the panel can say only
allowed or denied, from the permission check. It cannot name the
grant or the holders.

**Home and the timeline have no data source.** Until the open
question above is decided, Home holds teams and unhealthy objects,
and the timeline holds nothing garam records.

**Adding a kind of member means a new shape.** It never means a new
colour.

## Signs the decision was wrong

- **A reader's pages are mostly inactive controls.** Usability
  reports or screenshots show members scanning past actions they will
  never take. Hiding them, as ADR-0103 did, would then serve better.
- **Home stays empty in practice.** Needs you never fills because work
  items do not land, and things worth a look is almost always empty.
  The console would then open on a page with nothing on it.
- **People and agents need different lists.** Filters, columns or bulk
  actions keep splitting one kind from the other, so the shared
  Members list carries a mode switch on every view.
- **Your access cannot be explained cheaply.** An `Explain` per action
  makes an object page slow, or a holder lookup leaks who holds what
  to callers who should not learn it. The panel would then fall back
  to allow or deny, which is ADR-0103's model.
- **Shape does not carry the kind of member.** People mistake an
  agent for a person in lists or the palette, and a colour or a label
  is needed after all.

## Declined

The design page's §Recommendation ranks the five directions:

- **Ledger as the whole console.** It is the best operations surface,
  and Roster's lists and object header are Ledger's. It organizes by
  kind of infrastructure, though, and gives a member no home and
  agents no place beside people.
- **Atlas's resource tree as navigation.** garam's GRNs are shallow:
  almost every resource sits under the `default` project. A tree also
  has no place for people or work. Its reading of access from the
  root down is kept, inside Your access.
- **Switchboard's query-led split view.** It suits operators rather
  than a whole organization. Its palette is kept.
- **Casebook's serif and top tabs**, for the reasons the first drafts
  gave. Its lede sentence is kept, on Home.
