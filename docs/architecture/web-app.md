# Web App

What the application a person uses garam through is responsible for,
the one seam it reaches the backend through, the one it reaches a
hosted operator's control service through, and what it does not
decide.

## Contents
- What it is
- What it serves
- How it reaches garam
- How it reaches a control service
- Design system
- What it does not decide
- Its own toolchain and gate
- Not established
- Decisions this rests on

## What it is

Two Next.js App Router applications implement it, TypeScript strict,
Tailwind and shadcn/ui ([ADR-0009](./adr/0009-web-frontend.md)), each
on an origin of its own
([ADR-0131](./adr/0131-the-session-cookie-is-host-only-and-the-site-hands-a-sign-in-to-another-app-by-a-one-time-code.md)):
`site-web/` serves the landing page and the way in, and `platform-web/`
the signed-in application. Both build on `lib/design/`, the shared
tokens and primitives, and on `lib/platform-sdk-ts/`, the one client of
the backend they share. What follows names the console where only the
console is meant; the site's image is built from the same Dockerfile.
It is the one part of this
repository that is not Go, it carries a toolchain and a gate of its
own, and each app ships as an image a version tag publishes
([ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)):
`garam/console` and `garam/site`, which also take the app's development
builds
([ADR-0134](./adr/0134-an-image-lives-at-its-publishing-repository-and-its-tag-says-what-a-build-is.md))
under a tag that is not a release version
([ADR-0071](./adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)).

It stays its own image for what each of the two holds while it runs.
This one's final stage is the Node base the build ran on
(`deploy/local/Dockerfile.web`): a shell, `wget`, and the runtime that
executes the `server.js` and the `node_modules` subset copied into it —
the local stack reaches this application's probe through that shell and
the garam processes' by running the shipped binary
(`deploy/local/docker-compose.yml`). The other image is `FROM scratch`
holding five copied paths (`deploy/local/Dockerfile`): certificates, one
account and no root, `platform/migrations/`, and the `garam` binary that
`migrate` and every serve subcommand run from.
Merging the two gives each process what the other holds — a shell and an
interpreter inside the image whose processes read the key-encryption key
that seals the message signing keys
(`deployment-contract.md` §What the binary enforces about key material),
the schema migrations
and that binary
inside the image whose process renders a browser's input — and that is
what a proposal to build and pull one image instead of two has to
answer.
[ADR-0020](./adr/0020-published-artifacts.md) §Rejected alternatives
refused a single image on that ground, and
[ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md) §Decision
leaves the two standing where it supersedes that ADR.

**This document is authoritative for what the application is
responsible for and where its boundary falls.** Five artifacts already
own things a reader might come here for, and none of them is restated:

- `platform/api/openapi.yaml` is the source of truth for every operation
  the application calls ([ADR-0004](./adr/0004-openapi-first-api.md)),
  and `platform-web/src/types/api.ts` is generated from it rather than
  written ([ADR-0019](./adr/0019-machine-openapi-spec.md)).
- [api-surface.md](./api-surface.md) describes the browser-facing
  listener as a **surface**: what admits a caller to it and what
  decides what that caller may do. This document describes the
  **client** of that surface. Where both could answer a question about
  admission or authorization, that one answers it.
- `deploy/charts/garam/README.md` is authoritative for what a
  deployment sets — `web.apiUrl` included, and for what happens when it
  is absent at runtime
  ([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)).
- `docs/convention/stack-nextjs.md` governs how the application is laid
  out inside `platform-web/src/`: which folder holds what, and the two
  entry points a feature declares (§13. Feature entry points and file
  naming). What may import what there is
  [structure.md](./structure.md) §Boundaries and dependency direction's
  ([ADR-0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)),
  and `platform-web/eslint.config.mjs`'s `garam/import-direction` rule
  is what fails `make -C platform-web lint` on a crossing. Nothing here
  restates a layout rule, and a question about where a file belongs is
  that file's, not this one's.
- `deploy/local/Dockerfile.web` is authoritative for what the image
  carries. `output: "standalone"` in `platform-web/next.config.ts`
  decides what the build emits; that Dockerfile decides what has to be
  copied in beside it and says what a path missing from the result costs
  at runtime.

What is left is this document's: which routes exist and what each one
is for, the single seam the application reaches garam through, and the
decisions it does not make.

## What it serves

Three parts of the interface and two probes, over two origins. The
site holds two of the three, the Next.js route groups `(marketing)` and
`(auth)`: a route group contributes no URL segment, so neither is in
any path. The console holds the third, `console`, an ordinary segment,
which is why every console path starts with it. Each app answers a
path the other serves with a redirect to the other's origin, keeping
the path and query, so a bookmark from before the split still lands
(`site-web/src/proxy.ts`, `platform-web/src/proxy.ts`). Both answer the
probes and the `/api/*` rewrite.

| Path | For | Served by | Reached by |
|---|---|---|---|
| `/` | the public landing page | site | anyone |
| `/login` | sign in — a password form, and a provider button only where the backend answers with that provider; signing in lands on the console path the visitor was sent here from, and a visitor already holding a session is sent on to it | site | anyone |
| `/register` | turn an invitation into a password account ([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)), landing in the organization it joined; reached from an invitation link the token is filled in, and one no longer usable is said instead of the form | site | anyone holding an invitation token |
| `/console/*` | the signed-in application | console | a visitor carrying a session cookie |
| `/device` | the page `garam login` names ([ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md)); it forwards to `/console/device` with the code it was given, as `code` or RFC 8628's `user_code` | console | anyone |
| `/healthz`, `/readyz` | the two probes, spelled as every other process spells them ([ADR-0023](./adr/0023-web-probes.md)) | both | anything that can open the port |
| `/api/*` | the same-origin rewrite onto the browser-facing listener ([ADR-0009](./adr/0009-web-frontend.md)) | both | the browser |

The sign-in page asks `/auth/providers` at runtime and renders a
provider button only for a name it answers with, so a build cannot
offer a door the backend would answer 404 for
(`site-web/src/app/(auth)/login/page.tsx`, reading through
`site-web/src/features/auth/api/list-providers.ts`). A read that fails
yields no names, so the password form stays the way in. Which ADR holds
that decision is open (§Not established).

The console's structure is `Organizations → organization → section`,
carried by a collapsible sidebar shell over every `/console/*` route
([ADR-0011](./adr/0011-console-shell.md)). Its org-scoped sections are
declared in one place,
`platform-web/src/app/console/_components/navigation.ts`, which is what
the sidebar, the breadcrumb, the command palette and the active marking
all read. The sidebar is
[ADR-0116](./adr/0116-people-and-agents-are-listed-apart-under-organization-and-a-page-explains-only-what-is-not-self-evident.md):
Home, Projects, and Organization (People, Agents, Teams, Permissions,
Operators, Hosts and Settings). The same file's `CONSOLE_NAV` is where
each one is placed, with a link only for a route that exists, and
`CONSOLE_SECTIONS` names the action each section's list is read under;
adding one is an entry there, a place in a group and a route folder.
Consents is a section in no group: Settings links it and the palette
finds it. Activity is not listed: garam records nothing it would show
(§Not established). Below the groups the sidebar links the
organization's workspace in cowork, at `COWORK_WEB_URL/{org}`, where
that variable is set; cowork is another app, so it is a plain link and
no section
([ADR-0127](./adr/0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)
§10).

A section is left out of the sidebar and the palette where the server
denies the caller its read. The organization's layout asks once for
every section, in one permission check
(`platform-web/src/app/console/_components/org-shell-read.tsx`), and the
sidebar says once, in one line, which sections it left out. Until the
answer arrives, or where it never does, every section is listed. A
section whose entries each have a page of their own declares it there
too, and People, Agents, Teams, Projects, Operators and Permissions
do: `…/people/{person}` is a person's page, `…/projects/{project}` a
project's,
`/console/orgs/{org}/teams/{team}` is a team's page and `…/teams/new`
creates one, `…/agents/{agent}` is an agent's and `…/agents/new`
creates one,
`…/operators/{operator}` is
an operator's, with no create page because `new` is a valid operator
identifier
([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)),
and `…/operators/{operator}/connect` is that operator's setup; below
an operator, `connect` and `keys` are reserved for its own pages, which
sit under the identifier and so take none,
and `…/permissions/{policy}` and `…/permissions/new` are a policy's.
On such a page the sidebar marks the section as the location, and the
breadcrumb links the section and names the entry with the name the
page read, handed to it through
`platform-web/src/app/console/_components/item-crumb.tsx` because the
layout cannot read the entry
([ADR-0072](./adr/0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)).
A section may also declare named views beside its entries, which the
breadcrumb names by label: Permissions declares Attachments, at
`…/permissions/attachments`
([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)).
garam keeps no template of its own, so none has a page (ADR-0117).

The shell's links prefetch on intent, not on sight. The sidebar's
sections and the breadcrumb are `IntentLink`s
(`platform-web/src/app/console/_components/intent-link.tsx`), which
prefetch a route once the pointer rests on the link, it takes focus, or
it is touched. Next.js prefetches every visible link by default, and on
a console page the shell's links were most of its server work: measured
against the built app, 19 prefetch renders per page view, against 3.5
once the shell prefetches on intent (#1209). The cost is a link followed
with no intent shown first, which waits for its render after the click:
one server render and one round trip. Links in a page's own content,
such as Home's lede, keep the default.

The rest of the shell, and where each part is decided:

- **The organization switcher** names the caller's role in the
  organization on screen. That organization's layout reads it from its
  members list behind `<Suspense>`
  (`platform-web/src/app/console/_components/org-shell-read.tsx`), so no
  page waits on it, and a refused or unanswered read shows no role.
- **The top bar** carries the breadcrumb and a command palette, opened
  by ⌘K, Ctrl+K or `/`
  (`platform-web/src/app/console/_components/command-palette.tsx`). It
  finds the organization's people, agents, teams, policies and projects
  by name once something is typed and typing rests, through a Server
  Action that asks garam's `searchOrg` then and answers five of each
  kind with a link to the list for the rest
  (`platform-web/src/features/orgs/api/search-org-objects.ts`); no page
  load reads them for it. It also reaches the sections the caller may
  read, the page's actions, the caller's organizations and the theme.
  `.` opens it on the page's actions alone, each with its consequence in
  one line: a confirmation dialog given a `command` lists itself while
  its page is mounted, and choosing it opens that dialog. On a phone
  the breadcrumb keeps the organization's name beside the page's.
- **The theme** is system, light or dark, chosen in the user menu or
  the palette and kept in the browser alone.
  `platform-web/src/lib/theme.ts` applies it before the first paint, so
  a dark page is never served light first.
- **A failure below the shell** renders
  `platform-web/src/app/console/error.tsx`, which says what happened in
  words and offers the error's digest as a reference, never the thrown
  message. `notFound()` and any console address no route serves render
  `platform-web/src/app/console/not-found.tsx` inside the shell.

What a section can do differs, and the difference is the state of the
API behind it rather than of the console:

| Section | What the console does with it |
|---|---|
| Home | **On Roster.** The organization as the person signed in meets it (ADR-0113): a sentence generated from what follows, its names linked; Needs you, which says nothing waits on them while garam has no work items; their teams, which `listGroups` answers in one request with `member=me`, with the people and agents in each read per team; and the agents they can read that are failing. A part whose read was refused or not answered is said and never shown as empty, and the sentence leaves it out |
| Teams | **On the design system.** The organization's groups, presented as teams ([ADR-0072](./adr/0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)): lists them, creates one, and on a team's page reads its members, adds and removes people and agents, renames it and deletes it. A member is shown by the name and type the organization's people and agents give it, and one neither holds stays listed and removable, marked as not found; where the caller is refused the people or agents list, those members are marked not checked and the team stays readable. An agent is offered and shown by its name, or by its identifier where it has none, with the identifier beside it ([ADR-0119](./adr/0119-a-managed-agent-carries-a-display-name-on-its-own-record-set-at-creation-and-renamed-on-agent-rename.md)) |
| People | **On Roster.** The organization's people, apart from its agents ([ADR-0116](./adr/0116-people-and-agents-are-listed-apart-under-organization-and-a-page-explains-only-what-is-not-self-evident.md)), each with their role, filtered by team, kept in the address. The chosen people, or one row's, are added to a team or given a policy, one request each since garam takes one member per request, offered only for the teams and policies the server does not deny, asked in one check; the policy dialog says that a first attachment narrows a policy that applied to the whole organization. Inviting a person ([ADR-0006](./adr/0006-invitation-tokens.md)) is the header's one action, kept there inactive with the reason where the server denies it. A person's page names their role and since when. A list the caller is refused is said, never shown as empty |
| Agents | **On Roster.** The organization's agents, apart from its people (ADR-0116), each shown by the name the organization gave it or by its identifier where it has none ([ADR-0119](./adr/0119-a-managed-agent-carries-a-display-name-on-its-own-record-set-at-creation-and-renamed-on-agent-rename.md)), failing ones first, each reading as one state derived from its two reports with both named under it, filtered by team and by what runs it, read from the assignment's assignee, kept in the address, and added to a team or given a policy as People's are. Creating one is the header's one action, and the empty state's, kept inactive with the reason where the server denies it. An agent's page, for every agent the agents list holds, shows in its header the two states its operator and the agent itself report ([ADR-0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)), what holds it, an operator or a standalone host ([ADR-0082](./adr/0082-a-standalone-host-runs-its-agent-under-a-supervisor-an-administrator-approves-by-its-keys.md)), the generation garam last activated where the agent read answers one and nothing where it does not (#1576), and when it last reported. Replace host approves a new standalone host for an agent a standalone host holds, from exactly the assignment the header read, so garam refuses it if the agent has moved since. A new agent is created on its own page from a template the control service published, and no form there sends garam a definition or a value ([ADR-0090](./adr/0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md)); below the wizard, Templates lists the control service's published templates and profiles and publishes a template version, shown only where a hosted operator's grant reads them. An agent's page carries the report, saying its operator's control service created it or a standalone host enrolled it, and its name, renamed or cleared there and replaced by a sentence where the server denies `agent:rename` on that agent; a refused agents list is said rather than shown as none. The hosted operator's control service is reached for its published templates and profiles, publishing a template version, creating an agent from one, and an agent's pending, rendered and runtime-reported execution (§How it reaches a control service). garam's retired templates are not shown (ADR-0116) |
| Projects | **On the design system.** Lists the projects the organization owns, each with its identifier, and creates one from a dialog the list's header opens, which is also its empty state's next step — an agent's GRN names the project it was minted in ([ADR-0001](./adr/0001-grn-segments.md)), so an organization holding none cannot reach that path from the console. A project's page reads its members by name, adds and removes people and agents, renames it and deletes it once its name is typed, on the same membership parts a team's page uses (`platform-web/src/app/console/_components/membership/`), with Your access. A list the caller is refused is said, never shown as empty |
| Hosts | **On Roster.** The organization's standalone and direct hosts ([ADR-0082](./adr/0082-a-standalone-host-runs-its-agent-under-a-supervisor-an-administrator-approves-by-its-keys.md), [ADR-0111](./adr/0111-a-directly-attached-runtime-is-activated-by-a-one-agent-host-authority-on-the-generation-it-reports-and-its-store-lock.md)): its supervisors, each with the agent it runs, at which epoch and when that agent last activated, and the approvals that stood them up, with their state. A host is approved from the output of `garam standalone prepare` pasted whole, read against what that command prints and never said back, with the host's name, how it runs its agent and how long the approval lasts, at most the day garam allows. An unspent approval is withdrawn and an active supervisor revoked, each after a dialog saying what ends. Where the server denies `agent:enroll` each stays, inactive, with the reason. A list the caller is refused is said, never shown as empty |
| Operators | **On the design system.** Lists the operators the organization registered ([ADR-0045](./adr/0045-an-organization-registers-an-operator.md)) one row per Operator: its cluster manager and the control service whose record names it, or "Not connected", and a control service whose cluster manager garam no longer holds under a cluster manager that is "Not registered", paired by that record alone ([ADR-0124](./adr/0124-an-operator-is-registered-as-a-pair-in-one-step-and-its-control-service-names-its-manager.md)); each identity by name, with the identifier under it and its control endpoint's host, and the images each reported running and its last contact (#1047). Every hosted operator whose grant carries `operator:read` is asked, each through the operation-authority seam under its own authority, and the answers are merged by GRN, since a service answers only for the controllers that reached it: an operator a service that answered lists among its grant's controllers but left out, or one that reported no image, is not reported; one no answering service serves is not checked, with the reason in the console's words where a service refused or did not answer; a control service's own identity is named as one rather than as a controller; none is shown as an empty list; no service is asked where every operator is a control service, an organization with none hosted shows no notice, and the columns are left out only where the server denies the caller `operator:read`; connects one by registering its cluster manager and control service together in a dialog over the list, or the control service of a cluster manager no control service names yet, which that cluster manager's setup page preselects, offered unless the server denies the caller registering, which opens the setup page, since registering issues nothing ([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)); that page issues a pair's two keys in one step, each shown once and saved as its own file, gives each the Secret it reads it from, and waits for both first sign-ins, resuming from the keys garam holds by issuing only the one missing; a single identity's page issues, rotates and replaces its one key ([ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)). An operator's page has Overview, Keys, Control service, Delegation and Settings: what garam knows of it, and the same report in full: each image's name and reference, its digest where one was given, shortened and copied whole, when they were reported, and its last contact; its keys, rotated from its setup page and revoked after a confirmation; its control endpoint; its delegation; and its name and description. A danger zone deletes it once its identifier is typed, saying that work already running in its cluster does not stop ([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)) |
| Consents | **On the design system.** Shows each cross-organization consent relation the organization is a side of as a card naming where the pair stands and both halves, the other organization by its identifier in a copy field alone ([ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)); grants this organization's half from the card of a pair the other side holds, after a confirmation saying messages cross from then on; opens a new pair from a dialog the header opens, which is also its empty state's next step; and withdraws a half after a confirmation saying what stops. A list the caller is refused is said, never shown as empty |
| Permissions | **On the design system.** The organization's policies and their attachments ([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)), through the existing policy operations only. An agent is chosen by its identifier ([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)). The Policies view lists policies and opens a page per policy, where one is created as a draft; its document is edited as statements or as JSON, one text, and published as the next version — appended and made the default in one step, confirmed after a diff against the default — or saved without publishing; any stored version is made the default, it is activated, it is deleted once its name is typed ([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)), and it is attached to and detached from teams, people and agents chosen by name. Each of these writes is offered unless the server denies it to the caller ([ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)). A write that makes a policy evaluate states whom it reaches first, and one reaching everyone, or reaching a scope garam will not list, needs an explicit confirmation. The Attachments view shows each policy's attachments and, for one chosen subject, the policies attached to it, to its teams and to no one, without calling that its access. A list the caller is refused is shown as unchecked, never as empty; a Condition is shown as not evaluated. Neither view explains a decision, since no operation does |
| Settings | **On the design system.** The organization's name, renamed in place, beside its identifier to copy; a link to Consents, which no sidebar entry lists (ADR-0116); and a danger zone that deletes the organization once garam has counted what the deletion takes, read again when the dialog opens, and the person has typed the organization's name ([ADR-0065](./adr/0065-garam-states-what-deleting-an-organization-destroys-before-the-act.md)). The count names each kind the organization holds and says when its certificate authority goes with it; where garam did not answer the count, the dialog says so and has no confirm field, so the delete cannot be confirmed. Each write is offered unless the server denies it to the caller, and a caller denied the rename reads the name instead, with no sentence saying so |

Every section's empty state is one line, and the page's create action
where it has one, the header's repeated (ADR-0116). What an empty state
can carry is `platform-web/src/app/console/_components/empty-state.tsx`,
which is authoritative for it.

`/console/orgs/{org}/members`, the one list People and Agents were
until ADR-0116, and a person's page under it redirect to People
(`platform-web/next.config.ts`); no other former address is kept
(#1592).

Organizations are created, and an invitation is accepted at
`/console/invitations/{token}` — which is itself behind sign-in,
because accepting one joins an existing account to an organization.
The page reads the invitation first (`GET /invitations/{token}`, which
needs no session) and names the organization and role it admits to;
one already used, expired or unknown is said, with nothing to accept.
Accepting lands in that organization. A visitor without a session is
sent through the console's `/auth/sign-in` to the site, whose `/login`
takes the path as its `next` parameter, which
`/login` and `/register` carry between them and return to once a
session exists, so the token survives either way in
(`platform-web/src/lib/return-path.ts`, which returns only to a console
path, on the console's origin). The session reaches the console from
the site by a one-time code the console redeems on its own host
(`platform-web/src/app/auth/`, [sign-in.md](./sign-in.md)). A session
the backend rejects behind a cookie the browser still holds is sent the
same way:
`platform-web/src/proxy.ts` stamps each console request with its path,
and the redirect reads it back. The Google button
posts the same path in its form body to `POST /auth/login/{provider}`,
which holds it with the login's state, so the provider round trip lands
on it too.

A CLI is signed in at `/console/device`, behind sign-in for the same
reason, so the proxy and the return path above bring a signed-out
visitor back to it with its code. The page shows the code,
prefilled from the address, and one line naming whom approving signs
the CLI in as. Approve, its one primary action, or Deny sends
`POST /auth/device/approval` on the person's session (ADR-0120), and
a code garam holds nothing pending for is said beside it. No read
answers a pending code's client name, so the page names none.
`/console/sessions`, reached from the user menu, lists the person's
browser and CLI sessions (`GET /me/sessions`) with when each signed in
and expires, marks the one the page was opened on, and revokes any
after a confirmation (`DELETE /me/sessions/{session}`). Revoking the
current one signs that browser out. garam records no last use, so none
is shown. The code is not a token: approving a code someone else
started signs their CLI in as the approver, so the page says whom, and
it sends no referrer, since the code can ride in its address.

Where the token may appear is fixed:

- **In the invitation link and the sign-in's own URLs only:** the
  site's `/login?next=…`, `/register?next=…` and `/handoff?next=…`, and
  the console's `/auth/sign-in?next=…` and `/auth/handoff?next=…`.
  cowork-web's routes of those names, and the site's with `app=cowork`,
  carry a path on cowork (ADR-0135), from which no invitation is read.
  Every one is served or redirected to with `referrer: no-referrer`,
  and a link carrying one does not prefetch. The invitation link is
  the console's and the sign-in the site's, so a redirect between them
  crosses origins (ADR-0131 §Consequences).
- **Nowhere else:** in no redirect to an origin garam does not serve,
  in no client storage, and in no other URL.

No organization, group, project or user is addressed by its GRN
anywhere in the interface. A policy document is authored text naming
resources by GRN pattern, and the console shows it as written, which
is content rather than addressing
([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)). The console shows names and addresses
resources by the identifier segment the API's own paths take
([ADR-0011](./adr/0011-console-shell.md),
[ADR-0001](./adr/0001-grn-segments.md)).

## How it reaches garam

**One module.** `platform-web/src/lib/garam.ts` holds the only call to
the backend anywhere under `platform-web/src/`, and
`site-web/src/lib/garam.ts` under `site-web/src/`, each a configuration
of `lib/platform-sdk-ts/`'s one client; every read and every Server
Action goes through it. That is what makes the boundary checkable rather
than asserted: one file to read to know everything the application asks
for.

**It words what it refuses.** A Server Action garam refused returns
the sentence `platform-web/src/lib/refusal-copy.ts` writes for the
answer's `reason` where garam sent one it documents, naming the place
its `path` points at. Otherwise the sentence is the one for the HTTP
status and `kind`, read for the operation that asked where the pair
means something particular to it. It is never one matched from the
English of `message`. That text reaches a person only quoted inside
the generic sentence for a refusal nothing maps.

**It checks what it hands back.** Every read gives the seam a parser
for what `platform/api/openapi.yaml` says the operation carries, so a
body that does not match is refused there rather than reaching a page as
a field nothing proved. A parser may demand no key that operation does
not carry either, so no field `platform/api/openapi.yaml` does not
describe can be what the console refuses a body for. A value it can: a
GRN field the console cannot read is refused here rather than thrown at
whichever page reached it first. That refusal is a third answer beside
the two the seam already has: a refusal is the backend deciding, an
outage is the backend not answering, and this is the backend answering
outside its own specification. Nothing downstream checks a field again.

It is reached over two paths, and only one of them is the rewrite:

- **The browser** reaches the backend under `/api/*` on this origin, so
  the `garam_session` cookie stays same-origin and no CORS layer is
  needed (ADR-0009). This is the path the OAuth round trip and the
  provider sign-in link take.
- **A server-side render** calls the configured address directly,
  because it is not a browser and has no origin to keep. The rewrite
  exists for the browser, not because it is how the application reads
  data.

The two take that address at different times, and that is what binds a
published image to one deployment. A server-side render reads it when
the process starts, so a deployment sets it; the browser's rewrite is
resolved when the image is built, so what is in it is whatever that
build was given and no environment moves it
([ADR-0056](./adr/0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)).
`.github/workflows/release.yml` builds the console's and the site's
images with `http://garam-api:8080` — the api Service a release named
`garam` renders and no other — so a deployment running a published
image installs under that release name or makes the name resolve.
`deploy/charts/garam/README.md` §Choosing a release name is
authoritative for what a deployment does about it, and `make ci`
compares the value the release bakes against what the chart renders.

The address is server-side only and never reaches a browser. It is
declared once, as a typed schema in `platform-web/src/env.ts`, parsed at
import so a missing value fails at start rather than inside the first
request that needs it. That file is also the authority
`make -C deploy/charts ci` reads the application's environment names
from
([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)),
so adding a variable here is a change the chart has to answer for.

**A page asks what it offers.**
`platform-web/src/lib/permission-check.ts` puts the actions a page
offers to `checkPermissions` in one request through the seam above, and
answers each one allowed, denied or unknown
([api-surface.md](./api-surface.md) §What decides the rest). Unknown is
an action the server answers it cannot decide, or a check that could not
be asked or answered outside its contract; the last is logged on the
server. What a page does with each answer is §What it does not decide.

Sign-in state is read from the backend's `/me` and never inferred from
the cookie (ADR-0009).

## How it reaches a control service

**Its peer is the merged Operator wire.** The console's create, read
and publish calls are the control routes at garam-agent-operator `dev`
`8e11169334086f00225654de1a57d5f16006412f`: create (#244,
`5e4680077f03c4a1c39ae0d1d65e1ffb203c9577`) and the template, profile
and execution reads, template publication and CORS answer (#277,
`5ef745aabdd152b988ef04b50d283fd094183380`). garam#1217 records a run
against them. The operators' reports are `GET /v1/orgs/{org}/operators`
at garam-agent-operator
`ba691a69ad0612ff5653d2facce5668ea12cbec9` (#364). The console's calls
are tested against a stub of that wire, with garam real:
`platform-web/e2e/control-service.spec.ts` for the agent and template
calls, and `platform-web/e2e/operators.spec.ts` for the reports. A
recovery control is not built ([operators.md](./operators.md) §Delegated
operation authority).

**One module, in the browser.**
`platform-web/src/lib/operation-authority.ts` holds the only call to a
control service. The browser makes it, because the control service's
CORS answer is what admits the console's origin; the console's own
server never calls one.

- **One authority per call.** A Server Action mints it through
  `platform-web/src/lib/garam.ts` like any other write, and the browser
  holds it only for that call: never in storage, a URL or a log line. It
  is sent as `Authorization: Garam-Operation`, with no cookie, and a
  redirect is an error rather than followed.
- **The URL is built once.** It is the registered endpoint and the
  route, and its `pathname + search` is the request target the
  authority binds; the same object is what is sent, so nothing
  normalizes one and not the other.
- **Only the registered endpoint.** The console calls the endpoint an
  administrator registered on the operator's page and no other
  address. An operator with none is not reached.
- **The model carries its embedding.** A template version names the
  model's embeddings endpoint beside the model, absent where it names
  none, and publishing from a version on screen sends it unchanged.
  Which models need one is the control service's to decide and refuse
  (garam-agent-operator ADR 0052 and ADR 0053 at
  `8e11169334086f00225654de1a57d5f16006412f`); the console shows the
  refusal.
- **It words what it refuses.** A refusal reads the sentence
  `platform-web/src/lib/control-refusal-copy.ts` writes for the `kind`
  the control service named, or for the HTTP status where it named none,
  read for the operation that asked where that means something
  particular to it. A kind that file does not list, and a status it
  does not map, read one fixed sentence saying what to try. The control
  service's `message` is never shown. The kinds listed are the ones the
  Operator documents for the routes the console calls, at the commits
  that file cites. Nothing detects a kind the Operator adds later; the
  fixed sentence is what a person reads for it.
- **It answers in the section that asked.** A refused mint, a refused
  call, an unreachable service and an answer outside the agreed wire
  each disable that section's controls and say so. Teams, People,
  Agents and every garam read stay as they are, because no other
  section waits on a control service.

## Design system

**The foundation is built.** The tokens, fonts, theme, shell,
primitives and page templates exist
(`lib/design/src/styles/globals.css`, `lib/design/src/ui/`,
`lib/design/src/components/`, `platform-web/src/components/`,
`platform-web/src/app/console/_components/`). Each area's migration
lands under epic #1366, in its plan's order. Areas on the design system
are marked in the table above; until an area is, §What it serves
describes it as it is.
[ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
holds the full decision and the reason for each part. This section is
what a page owes once it is built on the system.

**Roster is built in part.**
[ADR-0113](./adr/0113-the-console-is-roster-one-policy-shaped-ui-where-people-and-agents-are-members.md)
replaces ADR-0103's grouped sidebar, its set of pages and its hiding
of a denied action, and
[ADR-0116](./adr/0116-people-and-agents-are-listed-apart-under-organization-and-a-page-explains-only-what-is-not-self-evident.md)
replaces its Members list and its sidebar. Built (§What it serves):
- the sidebar, Home in place of the overview, and the palette that
  finds objects and the page's actions;
- People and Agents, two lists, each member's kind told by the shape
  of its mark (`platform-web/src/components/member-mark.tsx`);
- a uniform object header, the Detail template's mark and facts;
- Your access on the pages of an agent, a person, a team, a project,
  a policy and an operator
  (`platform-web/src/app/console/_components/your-access.tsx`): one line
  names what the caller may not do, and an expander holds each action's
  grant, or the permission a denied one lacks and who holds it, never
  the reason the inactive control already gives;
- Hosts, and replacing the standalone host an agent runs on.

Your access is asked in one permission check for the page's actions,
which the page reads its own controls from too, then one explanation
per action, in parallel, and who holds an action only where the
engine denied it, first page alone. It reads the grants from the
organization down to the caller, names a policy the caller cannot
read only as such, and falls back to the check's answer where an
explanation fails. An agent's page makes 11 requests to garam, five
of them Your access's: more where a hosted operator's control service is
asked or an action is denied, beside the shell's own reads.

Unbuilt:
- a denied action kept visible as an inactive control everywhere:
  People and Agents keep their denied actions so
  (`platform-web/src/app/console/_components/inactive-button.tsx`), and
  the other pages still leave theirs out;
- replacing the host of an agent an operator holds, which its control
  service performs (ADR-0107): the agent's page keeps Replace host,
  inactive, with that reason.

Until those are, the role-aware rules below and under §What it does
not decide are the console as it runs. The design page that drew
Roster is reference material, not a specification, and none of its
mockups is kept in this repository:
https://claude.ai/artifact/6fgTkEzZgDxq8UBoHe9nsa (v2, §Roster and
§Recommendation, read 2026-10-18). The visual system below stays as
it is under Roster.

- **Principles.** Content over chrome, one accent, mono and copyable
  identifiers that are never a title, friction only for secrets and
  destruction, keyboard first, Korean and English equally first-class,
  and no raw server string on screen.
- **Tokens.** These are oklch tokens on `@theme`, in light and dark:
  - cool-grey neutrals;
  - one indigo accent;
  - ok, warn, danger and info status colours, and status is always a
    dot plus a label.

  Pretendard Variable is the UI face and Geist Mono the face for
  identifiers, keys, JSON and timestamps. Radius grows from control to
  card to dialog, density is 36px rows and 32px controls, icons are
  Lucide at a 1.5 stroke, and motion is short, transform and opacity
  only, and off under reduced motion.
- **Shell.** It is ADR-0011's shell, extended:
  - a collapsible sidebar holding the org switcher with the caller's
    role;
  - Roster's navigation (§What it serves);
  - the user and the theme control at the bottom;
  - a top bar holding the breadcrumb and the command palette, which
    keeps the organization's name on mobile.

- **Density.** A page shows a sentence of explanation only where its
  action is not self-evident, and a one-line consequence lives in the
  dialog that confirms the action: a dialog given a `command` and no
  description says its consequence. Each page has one primary action,
  at the end of its header row
  (`platform-web/src/app/console/_components/page-header.tsx`). A table
  keeps the columns a reader acts on (ADR-0116).
- **Templates.** Every console page is a List, a Detail, a Settings
  page or a Wizard, as ADR-0103 §Page templates lays each out. A
  Detail page keeps every tab mounted and hides the inactive ones, so
  what a person typed in a tab, a refusal and an inline outcome
  included, is still there on return.
- **Primitives and patterns.** A page composes the primitives
  ADR-0103 lists rather than copying markup. It follows the patterns
  for a secret shown once, a destructive action, lifecycle stages, the
  policy editor, the operator onboarding wizard and feedback. Success
  shows inline and an error attaches to its field, with no error
  toasts. Every write confirmed in a dialog goes through
  `platform-web/src/app/console/_components/action-dialog.tsx`, which
  stays open until garam answers, keeps a refusal until it closes and,
  for an act nothing undoes, asks for a typed name. A call garam's
  answer never comes back from, such as a Server Action's own POST
  answered 503 by a proxy, stays in that dialog, and in every other
  dialog that submits through a Server Action, as a refusal saying garam
  did not answer, so the change may or may not have been made and is to
  be checked before trying again. It does not close as if cancelled
  (`platform-web/src/lib/unsent-action.ts`, #1631).
  A read garam refused is
  said by `platform-web/src/app/console/_components/refused-read.tsx`,
  in one wording.
- **Component base.** shadcn/ui stays on Radix. Base UI is declined
  for now, for the reasons ADR-0103 §Component base gives.

## What it does not decide

The boundary is as much of the responsibility as the contents.

- **No authorization decision; it asks the server instead.**
  `platform-web/src/proxy.ts` sends a request to `/console/*` carrying
  no session cookie to the site's `/login`, and that is a fast path
  rather than a gate: a cookie can be expired or revoked, so the
  console verifies the session against `/me`, and a 401 from any read
  sends the visitor to sign in regardless. Every org-scoped decision
  belongs to the policy engine
  ([ADR-0002](./adr/0002-iam-style-policy-model.md),
  [authz.md](./authz.md)).
  - **The console asks the server whether the caller may take an
    action.** It asks through a permission-check API that answers
    from the check enforcement runs: authz `Check`, or `CheckCurrent`
    for an action a mint, a delegation write or a standalone host
    approval decides. It answers unknown only where a mint's check
    allows and the grant is left to decide
    ([api-surface.md](./api-surface.md) §What decides the rest). It
    hides or disables what the answer denies. The server still
    enforces every write, so a refusal that is reached still renders.
  - **An unanswered check shows the control.** The console infers no
    permission from a role it read
    ([ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md) §Role-aware UI).
  - **Asked once per page.** The API is `checkPermissions`, asked
    through `platform-web/src/lib/permission-check.ts` (§How it reaches
    garam). Each migrated page asks `checkPermissions` once for every
    write it offers. A form on a page that does not ask, and that the
    API will refuse, still renders and is refused.
- **Mints no identity and holds no credential.** No GRN, no session, no
  certificate, no invitation token originates here. The session is a
  server-side row and the cookie carries an opaque token whose hash
  alone is stored, so there is nothing here to read or forge
  ([ADR-0005](./adr/0005-server-side-sessions.md)); an invitation is
  the token itself, and possession is the whole of the claim
  ([ADR-0006](./adr/0006-invitation-tokens.md)).
- **Serves no secret's value.** No surface does, and
  [ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)
  names the console when it says so.
- **Owns no schema.** `platform-web/src/types/api.ts` is generated from
  `platform/api/openapi.yaml`, so a change to what an operation carries
  starts in the specification and arrives here (ADR-0004, ADR-0019).
- **Its probes read nothing.** Neither answer reads the API, the
  environment or the filesystem, so a backend outage takes no replica
  out of rotation
  ([ADR-0023](./adr/0023-web-probes.md)). Reporting an outage is the
  reading surfaces' job and not the probes': `/login`, `/register` and
  every console section that reads render a notice in place of what
  they could not fetch.
- **Reaches no listener but one.** The machine listener is a separate
  process on separate credentials
  ([ADR-0017](./adr/0017-machine-process.md)); nothing here holds key
  material or speaks to it, and bringing the application up locally
  needs no certificate.

## Its own toolchain and gate

`make -C platform-web ci` is the gate, and `make ci` runs it with every
other module's, so the one name covers both toolchains. In order:
`install`, `format`, `lint` (which also typechecks), `test`, `build`,
`check-gen`.

Three things are outside it. `make -C platform-web e2e` runs the
Playwright specs in `platform-web/e2e/` against a real built
application, a real backend and a real Postgres, so it needs
infrastructure the gate does not start. `make web-image-e2e`'s subject
is the image a release publishes rather than this tree's application,
and it runs that image, so it needs a container runtime the gate never
asks for. `make install` is a prerequisite rather than a check.

Tests are in two locations by decision, not by accident: Vitest owns
the tests colocated under `platform-web/src/`, Playwright owns
`platform-web/e2e/`, and each config names the other's territory so
neither collects the other's files (ADR-0009,
`docs/convention/stack-nextjs.md` §12. Testing — three layers, two
locations).

## Not established

- **Whether a responsibility document is owed per domain or per index
  entry.** `#605` records that the line defining what "Every domain or
  concern has a responsibility document" binds
  differs from the template garam adopted it from, and that the two
  readings point at different files. This document is owed under
  either, so nothing here turns on the answer, and nothing here
  presumes one.
- **The browser end of watching an agent.** garam's half is built and
  [ADR-0022](./adr/0022-watching-an-agent.md) puts a browser at one end
  of the stream, reading it as it arrives and seeing only from when it
  opened. No console route opens one, so the view that ADR describes
  does not exist yet, and where it would live is undecided.
- **Which ADR holds the sign-in page's provider discovery.** The
  behaviour is live and is stated under §What it serves.
  [ADR-0010](./adr/0010-dev-auth-provider.md) decided it, and
  [ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  superseded that ADR to remove the second provider without carrying
  the discovery decision either way — so what the application does
  rests on a decision whose ADR is superseded.
  `site-web/src/features/auth/api/list-providers.ts` and
  `site-web/src/app/(auth)/login/page.tsx` both cite ADR-0010 for it.
- **What the application promises a visitor across versions.** Nothing
  records it, and [api-surface.md](./api-surface.md) §Not decided
  records the same absence for the API it calls.
- **Who answers for an agent.** ADR-0113 leaves open whether an agent
  carries an accountable person, as Entra Agent ID's sponsor does.
  garam holds no such field, and since ADR-0118 no definition's
  `definedBy` either, so an agent's page names nobody.
- **Where Home's feed and the shared timeline are read from.** Audit
  records message sends alone, with no read route
  ([audit.md](./audit.md)). Watching an agent keeps nothing
  ([ADR-0022](./adr/0022-watching-an-agent.md)). Nothing records a
  membership or policy change for a reader, so ADR-0113's
  timeline has no source yet.

## Decisions this rests on

The decisions the state above is made of. An ADR that changes what the
application serves, what it reaches, or what it may not decide belongs
on this list, which is the quickest test of whether a change is one
this document owes an update.

These decide the application itself:

- [ADR-0009](./adr/0009-web-frontend.md) — a Next.js application in
  `platform-web/` in this repository, reaching the API through a
  same-origin `/api/*` rewrite, with three test layers
- [ADR-0011](./adr/0011-console-shell.md) — the console is a sidebar
  shell over `Organizations → organization → section`, and prints no
  GRN
- [ADR-0023](./adr/0023-web-probes.md) — the application answers the
  same two probes as every other process, as route handlers, reading
  nothing
- [ADR-0072](./adr/0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)
  — the console names groups teams and members people, and a team has
  a page of its own below the Teams section
- [ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)
  — Permissions holds policies and their attachments through the
  existing operations, states a policy's scope before it evaluates, and
  sorts a subject's policies by source without calling them its access
- [ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)
  — operators are listed by name and have a page of their own below the
  Operators section, and the identifier stands beside a name wherever
  the name alone could be ambiguous
- [ADR-0075](./adr/0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
  — a new agent starts from a template copied once
- [ADR-0088](./adr/0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)
  — garam's own templates are retired records the console offers as no
  start; since ADR-0116 it shows none of them
- [ADR-0090](./adr/0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md)
  — the console offers no form that sends garam a definition, and a
  new agent is created through the control service
- [ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)
  — garam holds no agent definition
- [ADR-0119](./adr/0119-a-managed-agent-carries-a-display-name-on-its-own-record-set-at-creation-and-renamed-on-agent-rename.md)
  — an agent carries the name the organization gave it, which the
  console shows in place of its identifier where it has one
- [ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
  — one design system, of principles, tokens, shell, page templates,
  primitives and patterns, and a console that asks the server what a
  caller may do rather than rendering what it will refuse; the
  foundation and the permission check are built, and the areas on the
  design system are marked under §What it serves
- [ADR-0113](./adr/0113-the-console-is-roster-one-policy-shaped-ui-where-people-and-agents-are-members.md)
  — the console is Roster, one UI shaped by the caller's policies,
  with a Home, people and agents as members, and a Your access panel
  on every object page; it replaces ADR-0103's navigation, its set of
  pages and its hiding of a denied action, and is built in part
- [ADR-0116](./adr/0116-people-and-agents-are-listed-apart-under-organization-and-a-page-explains-only-what-is-not-self-evident.md)
  — People and Agents are two lists under Organization, beside Teams,
  Permissions, Operators, Hosts and Settings, and a page explains only
  what is not self-evident, with one primary action; it supersedes
  ADR-0113's Members list and its sidebar

The rest bear on it without being about it:

- [ADR-0001](./adr/0001-grn-segments.md) — the identifier segment is
  what the API's paths take, which is what the console shows instead of
  a GRN
- [ADR-0002](./adr/0002-iam-style-policy-model.md) — the engine decides
  every org-scoped request, so the interface decides none and asks the
  server instead
- [ADR-0004](./adr/0004-openapi-first-api.md) —
  `platform/api/openapi.yaml` is the source of truth the client's types
  are generated from
- [ADR-0005](./adr/0005-server-side-sessions.md) — a session is a row
  and the cookie carries only a token naming it
- [ADR-0006](./adr/0006-invitation-tokens.md) — an invitation is a
  single-use token, and possession is the claim
- [ADR-0017](./adr/0017-machine-process.md) — the machine listener is a
  separate process this application never reaches
- [ADR-0019](./adr/0019-machine-openapi-spec.md) — one specification
  per listener, so the generated client carries no operation it could
  not call
- [ADR-0021](./adr/0021-liveness-and-readiness.md) — what the two probe
  paths mean, which ADR-0023 imports wholesale
- [ADR-0022](./adr/0022-watching-an-agent.md) — a watch is opened and
  closed by a browser, and the view is best effort rather than a record
- [ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md) — the
  callback a login lands on refuses a replayed state as unauthenticated
- [ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)
  — starting a login is refused once the live-state bound is reached,
  which the login page has to render
- [ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)
  — a message crosses an organization boundary only where both
  organizations hold a half of one relation, which is the Consents
  section
- [ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)
  — no surface serves a secret's value, the console named among them
- [ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  — the application is one of the two published images, and its API
  address is baked in at build time
- [ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
  — a password is garam's own credential, `/register` is invitation
  gated, and one refusal covers a wrong password and an unknown name
- [ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
  — the chart describes the deployment, and reads this application's
  environment names from `platform-web/src/env.ts`
- [ADR-0043](./adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)
  — the web workload carries an identity of its own and reads no
  credential
- [ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  — the development-only sign-in button is gone, and local work has no
  way in but real credentials or the e2e seed
- [ADR-0045](./adr/0045-an-organization-registers-an-operator.md) — an
  organization registers an operator, which is the Operators section
- [ADR-0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)
  — an operator and an agent each report a state elsewhere, and the two
  are what the Agents section reads and shows
- [ADR-0056](./adr/0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)
  — the browser's `/api/*` address is fixed when the image is built, so
  a published image reaches the api Service a release named `garam`
  renders
- [ADR-0071](./adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
  — a development build of this image bakes the same address, since it
  is built from what the release builds, and is told apart by its tag
- [ADR-0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)
  — `structure.md` governs what a module under `platform-web/src/` may
  import, so a feature reaches a sibling through its declared entry
  points and the stack file no longer refuses that edge
- [ADR-0134](./adr/0134-an-image-lives-at-its-publishing-repository-and-its-tag-says-what-a-build-is.md)
  — that development build lives in `garam/console`, beside the
  releases, and its tag tells the two apart
