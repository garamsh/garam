# ADR 0072: The console presents a group as a team with a page of its own

> Status: accepted
> Date: 2026-09-27

## Context

[ADR-0011](./0011-console-shell.md) gave the console the sections
Overview, Groups, Projects, Members and Policies, each one page under
`/console/orgs/{org}/{section}`. What the Groups section did was list
the organization's groups and create one; nothing read a group's
members, renamed a group or deleted one, although `api/openapi.yaml`
has carried every one of those operations on `/orgs/{org}/groups/…`
since the group was introduced. A group's member is a GRN naming a
user or an agent (ADR-0014), so a group already holds both.

The product direction #1122 records, approved on 2026-09-27, organizes
the console around teams, people and agents: people and agents are
managed separately and work together in teams, and a team list opens a
separate page for each team rather than expanding in place. #1123 is
its first slice and changes no backend: the group's schema, its
identity and what authorizes each operation stay as they are.

Two facts limit what a page can show. The `Group` schema carries a
name and roles and no description, and the `Agent` schema carries no
name — the console shows an agent's identifier segment, and a friendly
name is #1125's.

## Decision

- **Teams is the console's name for groups.** The section is labelled
  Teams at `/console/orgs/{org}/teams`, and what the console calls a
  team is a group: its GRN, its API paths, its permissions and its
  stored rows are the group's, unchanged. Code in `web/src/features/`
  keeps the API's name, since it mirrors the operations; routes,
  components and copy say team.
- **People is the console's name for the organization's members**, at
  `/console/orgs/{org}/people`, and Agents stays a section of its own.
  The sidebar order is Overview, Teams, People, Agents, then the rest.
- **The team list opens a page per team.** The list names each team
  and links to `/console/orgs/{org}/teams/{team}`, the group's
  identifier segment; creating one is a page of its own at
  `…/teams/new` that opens the team it created. The team's page shows
  its name and members and carries adding, removing, renaming and
  deleting it.
- **A section with item pages extends the navigation model rather
  than leaving it.** The section list declares which sections have
  item pages; the sidebar marks the section as the visitor's location
  on an item page, and the breadcrumb links the section and names the
  item with the name its page read, which the page hands to the
  breadcrumb because the layout cannot read it.
- **A membership is shown by what the organization knows of it.** A
  member GRN is read against the organization's people and agents: a
  person by nickname, an agent by identifier, each with its type. One
  the organization does not hold, or whose type is neither, stays
  listed and removable, marked as not found rather than dropped or
  shown as someone known. Adding offers only people and agents the
  team does not hold, by name and type; no GRN is typed or shown.
- **A refused team is not a missing one.** Inside an organization the
  caller reads, a 403 on the team renders a refusal and a 404 renders
  not-found. An organization the caller cannot read still answers
  404 through the page's org-scoped reads, as ADR-0011 decided.
- **Old addresses redirect.** `…/groups` and `…/members` redirect to
  `…/teams` and `…/people` (`web/next.config.ts`), temporarily rather
  than permanently, since the #1122 direction is still arriving.

## Consequences

- A person manages a team's mixed membership without an API call, and
  removing a membership removes that one row and nothing else.
- Nothing in the console can show a team description or an agent's
  friendly name until the API carries one; the page does not render a
  field for either.
- The team page reads the organization's people and agents on every
  render, so a caller refused either read meets 404 on the team page
  as on the overview.
- The console and the API now name one resource two ways. The mapping
  is this ADR's; a later rename of the backend group is a separate
  decision.

## Rejected alternatives

- **Expanding a team inside the list.** The owner chose a page per
  team for #1122; a page is also what a link, a bookmark and the back
  button address.
- **Folding a refused team into not-found, as for an organization.**
  The team's name is already listed to anyone who reads its
  organization, so 404 hides nothing, and #1123 asks for forbidden and
  not-found to be told apart.
- **Offering agent definitions as members.** A definition's agent has
  a GRN before any operator claims it, but the team page offers the
  agents garam holds; whether a team may take an unclaimed agent is
  left to the agents slices of #1122.
