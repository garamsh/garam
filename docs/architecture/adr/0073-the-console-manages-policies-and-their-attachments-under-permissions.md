# ADR 0073: The console manages policies and their attachments under Permissions

> Status: accepted
> Date: 2026-09-27

## Context

[ADR-0011](./0011-console-shell.md) gave the console a Policies
section, and it held its place with a sentence saying policies were
managed over the HTTP API. `api/openapi.yaml` has carried the whole
lifecycle under `/orgs/{org}/policies` since policies were introduced:
create, list, list and append versions, activate, archive, roll back,
and list, attach and detach attachments. It carries no read of one
policy, no read of the policies naming one subject, no delete and no
explanation of a decision.

#1122 records the product direction approved on 2026-09-27, and #1124
is its second slice: an organization manages policy definitions and
their attachments from the console, and sees whether a policy reaches a
subject directly or through a team. #1123 introduced teams and the
item-page navigation model (ADR-0072).

Four facts of the engine decide what an interface may say
(`docs/architecture/authz.md`):

- an active policy with no attachment applies to every subject of the
  organization;
- an explicit Deny wins over every Allow, and roles grant beside
  policies;
- no evaluation reads a `Condition`;
- garam refuses to detach an active policy's last attachment
  ([ADR-0031](./0031-detaching-an-active-policys-last-attachment-is-refused.md)).

Two facts of the operations limit how they can be used. A policy's
versions and attachments answer an empty list for an identifier no
policy has, so neither proves a policy exists. `rollbackPolicy`'s
summary says it points a policy at an earlier version, while its
`PolicyRollback.version` is "an existing version of the policy" and the
store accepts any stored one; appending a version never moves the
pointer, so without a forward move no new version could ever take
effect. The PM confirmed the schema and implementation reading on
2026-09-27 and tracks the summary as #1129.

## Decision

- **Permissions replaces Policies.** The section is labelled
  Permissions at `/console/orgs/{org}/permissions` and holds two views:
  Policies, the section's own page, and Attachments at
  `…/permissions/attachments`. A policy has a page at
  `…/permissions/{policy}`, its identifier segment, and `…/new` creates
  one. `…/policies` redirects to `…/permissions`, temporarily, as the
  #1123 redirects do.
- **A section may declare named views.** The navigation model gains
  views beside item pages: the breadcrumb names a view by its label
  rather than as an entry, and the sidebar marks the section as the
  location.
- **The console calls the existing operations and nothing else.** No
  delete, no Explain, no permission-set layer, and no change to what an
  operation means. A policy is found in the organization's list, and
  one the list does not hold is not found.
- **Creating grants nothing.** A created policy is a draft, and the
  create page says so. Saving a version says the default is unchanged
  until that version is made the default.
- **Any stored version can be made the default** through
  `rollbackPolicy`: "Make default" for a newer one, "Roll back" for an
  older one. The confirmation names the current and the chosen version.
- **A write that makes a policy evaluate states its scope first.**
  Activating, and changing the version of an active policy, confirm by
  naming the version and whom the policy reaches. Where it reaches
  everyone, the confirmation carries a checkbox the form is not sent
  without, and the Server Action re-reads the attachments before
  writing and asks for it where the page did not. A list garam refuses
  is an unknown scope, confirmed as possibly reaching everyone and
  never described as having no attachment.
- **Attaching is by name.** Teams, people and agents are chosen from
  the organization's lists by name and type, an agent by its identifier
  until #1125; no GRN is typed or shown. Attaching to an active policy
  with no attachment says that it narrows the policy from everyone.
- **The last-attachment refusal names the way on.** It says the policy
  would otherwise apply to everyone, and to attach another subject
  first or archive the policy.
- **A subject's policies are sorted by source, not summed.** For a
  chosen team, person or agent, the Attachments view lists the policies
  attached to it, those attached to a team it belongs to, and the
  active ones with no attachment, each with its state. It says that
  roles grant too, Deny wins and Conditions are not evaluated, and
  calls none of it the subject's access. The subject is addressed as
  `?subject=<kind>:<identifier>`. A team's page links to its view.
- **What was not read is said to be unchecked.** A refused attachment
  list makes a policy neither attached nor organization-wide; a refused
  team membership is named as unchecked where a policy is attached to
  that team, and no absence of inheritance is claimed then. While any
  policy's attachments are unread, an empty source says only that no
  policy the caller can read is in it.
- **A document is authored text, and GRNs in it are what was
  written.** The engine matches resources by GRN pattern, so the
  editor and a version's document show the patterns as typed; that is
  content, not addressing. Navigation, choices and the subject query
  still name teams, people and agents and address them by identifier
  (ADR-0011). The create page starts from an example whose resource is
  the one garam authorizes its action against, so it matches what it
  names.
- **An editor takes input only once it can hold it.** It is disabled
  until the page is interactive, so text typed before then is not lost.
- **A Condition is shown as restricting nothing.** Where a document
  being edited, a stored version, or the version about to be evaluated
  carries one, the console says garam does not evaluate it.

## Consequences

- An administrator manages policies and attachments without calling
  the API by hand, and every write is one existing operation.
- The Attachments view costs one attachments read per policy, and for a
  person or agent one members read per team, since no operation answers
  a subject's policies. The cost grows with the organization.
- The scope re-read before activating is a separate request from the
  write, and the API offers no conditional write joining them: an
  attachment removed between the two is not seen.
- The console cannot retire a policy except by archiving it, and a
  draft nobody wants stays listed as archived.
- Nothing here explains a decision. What the view shows is what is
  stored, and a reader wanting a verdict has none to read until an
  operation offers one.

## Rejected alternatives

- **A policy page one segment deeper.** `/permissions/policies/{policy}`
  beside `/permissions/attachments` would reach past the item-page
  model and change it for one section; a named view fits inside it.
- **Only earlier versions for rollback.** A new version could then
  never take effect, which would make adding one pointless.
- **Treating a refused attachment list as empty.** It would call a
  policy organization-wide on no evidence, which is the reading the
  issue forbids.
- **Hiding policies with no attachment from a subject's attach
  choice.** Attaching one is a legitimate narrowing; the choice groups
  them apart and says what attaching does.
