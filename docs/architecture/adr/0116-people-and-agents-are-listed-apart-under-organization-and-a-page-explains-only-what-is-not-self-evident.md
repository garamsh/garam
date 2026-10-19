# ADR 0116: People and agents are listed apart under Organization, and a page explains only what is not self-evident

> Status: accepted
> Date: 2026-10-19

Drafted for #1588, which reserved this number. It supersedes
[ADR-0113](./0113-the-console-is-roster-one-policy-shaped-ui-where-people-and-agents-are-members.md)
in part, in two places:
- §Members, where people and agents share one Members list;
- the sidebar ADR-0113 set: Home, then Members (People and agents,
  and Teams), Projects, and Organization.

The rest of ADR-0113 stands: one UI for everyone, Home, Your access,
a denied action kept visible, the command palette, and the kind of
member shown by shape. So does
[ADR-0103](./0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)'s
visual system: its tokens, type, single accent and palette. garam is
read at `3959739c3dc5f08ebff54d978d6e23a009b70117`. A claim below
that names no revision holds for it.

## Context

On 2026-10-19 the owner directed (#1588):
- the console is to be simpler and calmer, with fewer words and
  fewer items, and its design and colours kept;
- People and Agents are separate sections, and both sit under
  Organization.

ADR-0113 §Signs the decision was wrong named this case: "People and
agents need different lists", where filters or columns keep splitting
one kind from the other. The shared list does that today
(`web/src/app/console/orgs/[org]/members/_components/member-list.tsx`):
- a kind switch, All, People and Agents, sits over every view of it;
- the Runs on filter and column apply to agents alone;
- the Last report column reads "Never" for an agent and "—" for a
  person.

The console's pages also carry much static prose. Measured from
source as #1588's pull request states, the five densest pages are an
operator's page, a policy's page, an operator's setup, Attachments
and Members. Much of that prose repeats a consequence the page's
dialog states again, or names internal detail a reader cannot act
on.

## Decision

**The console's navigation is Home, Projects and Organization.**
Organization holds People, Agents, Teams, Permissions, Operators,
Hosts, Templates and Settings, in that order. People and Agents are
two lists, each with its own filters and columns. A person's page
and an agent's page stay as ADR-0113 made them.

**A page explains only what is not self-evident.**
- A page shows a sentence of explanation only where its action is
  not self-evident.
- A one-line consequence lives in the dialog that confirms the
  action, not on the page.
- Each page has one primary action, in the same place on every page.
- An empty state is one line and that action.
- A table keeps only the columns a reader acts on.
- Your access shows only what decides the page's actions. The rest
  is behind an expander.

## Consequences

**`/members` is no longer a section.** People takes its own address,
and the old one redirects there, as `…/people` and `…/agents` once
redirected to Members. An agent's page already sits under Agents.

**Two lists are two sets of filters.** People filters by team.
Agents filters by team and by what an agent runs on. The kind switch
goes. Adding several members of both kinds to a team now takes one
pass per list.

**Templates is a sidebar entry of its own.** It stays a view of
Agents in the address, so its pages keep their paths.

**Pages say less.** A reader who wants what a removed sentence said
finds it in the dialog for that action, or in Your access's
expander. A sentence a dialog does not carry is gone.

**This ADR deletes no backend feature.** It removes what the console
shows of an archived or legacy item only where #1588's QA inventory
names it.

## Signs the decision was wrong

- **People and agents keep being chosen together.** Adding mixed
  members to a team or a policy becomes the common case, and readers
  open both lists for one task. One list, as ADR-0113 had it, would
  then serve better.
- **The removed sentences come back as questions.** Readers ask what
  a control does, or act and are surprised by a consequence only the
  dialog states. The density rule would then have cut what was not
  self-evident.
- **The primary action is not the one used.** Readers mostly use a
  secondary action, so the place kept for the primary one serves the
  wrong control.
- **Organization outgrows a group.** Its eight entries hide one
  another, or a new kind of object has no place in it.

## Declined

- **Keep one Members list with a kind switch.** It is ADR-0113's.
  The switch, the agent-only filter and the column read per kind
  show the list is two lists already.
- **Members as a group holding People and Agents.** The owner placed
  both under Organization, beside the other objects an organization
  holds.
- **A denser visual system to fit more on a page.** The owner keeps
  ADR-0103's design. What changes is how much a page says, not how it
  looks.
