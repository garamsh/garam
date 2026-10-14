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

`web/` implements it: a Next.js App Router application, TypeScript
strict, Tailwind and shadcn/ui
([ADR-0009](./adr/0009-web-frontend.md)). It is the one part of this
repository that is not Go, it carries a toolchain and a gate of its
own, and it ships as one of the two images a version tag publishes
([ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)),
and its development builds go to `garam-dev/garam-web`
([ADR-0097](./adr/0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md))
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
account and no root, `migrations/`, and the `garam` binary that
`migrate`, `issue-certificate` and every serve subcommand run from.
Merging the two gives each process what the other holds — a shell and an
interpreter inside the image whose processes read the key-encryption key
that seals every organization's certificate authority
([deployment-contract.md](./deployment-contract.md) §What the binary
enforces about key material), the schema migrations and that binary
inside the image whose process renders a browser's input — and that is
what a proposal to build and pull one image instead of two has to
answer. [ADR-0020](./adr/0020-published-artifacts.md) §Rejected
alternatives refused a single image on that ground, and
[ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
§Decision leaves the two standing where it supersedes that ADR.

**This document is authoritative for what the application is
responsible for and where its boundary falls.** Five artifacts already
own things a reader might come here for, and none of them is restated:

- `api/openapi.yaml` is the source of truth for every operation the
  application calls
  ([ADR-0004](./adr/0004-openapi-first-api.md)), and `web/src/types/api.ts`
  is generated from it rather than written
  ([ADR-0019](./adr/0019-machine-openapi-spec.md)).
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
  out inside `web/src/`: which folder holds what, and the two entry
  points a feature declares (§Feature entry points and file naming).
  What may import what there is
  [structure.md](./structure.md) §Boundaries and dependency direction's
  ([ADR-0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)),
  and `web/eslint.config.mjs`'s `garam/import-direction` rule is what
  fails `make web-lint` on a crossing. Nothing here restates a layout
  rule, and a question about where a file belongs is that file's, not
  this one's.
- `deploy/local/Dockerfile.web` is authoritative for what the image
  carries. `output: "standalone"` in `web/next.config.ts` decides what
  the build emits; that Dockerfile decides what has to be copied in
  beside it and says what a path missing from the result costs at
  runtime.

What is left is this document's: which routes exist and what each one
is for, the single seam the application reaches garam through, and the
decisions it does not make.

## What it serves

Three parts of the interface and two probes. Two of the three are
Next.js route groups, `(marketing)` and `(auth)`: a route group
contributes no URL segment, so neither is in any path. The third,
`console`, is an ordinary segment, which is why every console path
starts with it.

| Path | For | Reached by |
|---|---|---|
| `/` | the public landing page | anyone |
| `/login` | sign in — a password form, and a provider button only where the backend answers with that provider; signing in lands on the console path the visitor was sent here from, and a visitor already holding a session is sent on to it | anyone |
| `/register` | turn an invitation into a password account ([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)), landing in the organization it joined; reached from an invitation link the token is filled in, and one no longer usable is said instead of the form | anyone holding an invitation token |
| `/console/*` | the signed-in application | a visitor carrying a session cookie |
| `/healthz`, `/readyz` | the two probes, spelled as every other process spells them ([ADR-0023](./adr/0023-web-probes.md)) | anything that can open the port |
| `/api/*` | the same-origin rewrite onto the browser-facing listener ([ADR-0009](./adr/0009-web-frontend.md)) | the browser |

The sign-in page asks `/auth/providers` at runtime and renders a
provider button only for a name it answers with, so a build cannot
offer a door the backend would answer 404 for
(`web/src/app/(auth)/login/page.tsx`, reading through
`web/src/features/auth/api/list-providers.ts`). A read that fails
yields no names, so the password form stays the way in. Which ADR holds
that decision is open (§Not established).

The console's structure is `Organizations → organization → section`,
carried by a collapsible sidebar shell over every `/console/*` route
([ADR-0011](./adr/0011-console-shell.md)). Its org-scoped sections are
declared in one place, `web/src/app/console/_components/navigation.ts`,
which is what the sidebar, the breadcrumb, the command palette and the
active marking all read. Below the overview the sidebar groups the
sections as Workloads, Access, Infrastructure and Organization, and
the same file's `CONSOLE_NAV_GROUPS` is where each one is placed, with
a link only for a route that exists; adding one is an entry there, a
place in a group and a route folder. A section whose
entries each have a page of their own declares it there too, and Teams,
Agents, Operators and Permissions do: `/console/orgs/{org}/teams/{team}`
is a team's page and `…/teams/new` creates one, `…/agents/{agent}` is
a defined agent's and `…/agents/new` creates one,
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
`web/src/app/console/_components/item-crumb.tsx` because the layout
cannot read the entry
([ADR-0072](./adr/0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)).
A section may also declare named views beside its entries, which the
breadcrumb names by label: Permissions declares Attachments, at
`…/permissions/attachments`
([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)).
A view may have item pages of its own, and the breadcrumb then links
the section and the view above the entry: Agents declares Templates,
at `…/agents/templates`, with `…/agents/templates/{template}` below it
and no create page, since garam's templates are retired read-only
([ADR-0075](./adr/0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md),
[ADR-0088](./adr/0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)).

The shell's links prefetch on intent, not on sight. The sidebar's
sections and the breadcrumb are `IntentLink`s
(`web/src/app/console/_components/intent-link.tsx`), which prefetch a
route once the pointer rests on the link, it takes focus, or it is
touched. Next.js prefetches every visible link by default, and on a
console page the shell's links were most of its server work: measured
against the built app, 19 prefetch renders per page view, against 3.5
once the shell prefetches on intent (#1209). The cost is a link
followed with no intent shown first, which waits for its render after
the click: one server render and one round trip. Links in a page's own
content, such as the overview's section cards, keep the default.

The rest of the shell, and where each part is decided:

- **The organization switcher** names the caller's role in the
  organization on screen. That organization's layout reads it from its
  members list behind `<Suspense>`
  (`web/src/app/console/_components/org-role-read.tsx`), so no page
  waits on it, and a refused or unanswered read shows no role.
- **The top bar** carries the breadcrumb and a command palette, opened
  by ⌘K, Ctrl+K or `/`, that reaches the organization's sections, the
  caller's organizations and the theme
  (`web/src/app/console/_components/command-palette.tsx`). On a phone
  the breadcrumb keeps the organization's name beside the page's.
- **The theme** is system, light or dark, chosen in the user menu or
  the palette and kept in the browser alone. `web/src/lib/theme.ts`
  applies it before the first paint, so a dark page is never served
  light first.
- **A failure below the shell** renders
  `web/src/app/console/error.tsx`, which says what happened in words
  and offers the error's digest as a reference, never the thrown
  message. `notFound()` and any console address no route serves render
  `web/src/app/console/not-found.tsx` inside the shell.

What a section can do differs, and the difference is the state of the
API behind it rather than of the console:

| Section | What the console does with it |
|---|---|
| Overview | counts what the sections it summarises hold, names the failing states inside them rather than folding them into a count, and says which sections it does not summarise and why |
| Teams | the organization's groups, presented as teams ([ADR-0072](./adr/0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)): lists them, creates one, and on a team's page reads its members, adds and removes people and agents, renames it and deletes it. A member is shown by the name and type the organization's people and agents give it, and one neither holds stays listed and removable, marked as not found; where the caller is refused the people or agents list, those members are marked not checked and the team stays readable. An agent is offered and shown by its definition's name with its identifier, including one no operator has claimed yet, and a refused definitions list is said rather than offered as none ([ADR-0075](./adr/0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)) |
| People | reads the organization's members, and issues the invitation a person joins by ([ADR-0006](./adr/0006-invitation-tokens.md)) |
| Agents | lists every agent the organization defined by name, whether or not an operator has claimed it, and every reported agent no definition names, each with the two states its operator and the agent itself report ([ADR-0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)) or, before any report, as awaiting an operator. A new agent is created on its own page from a template the control service published, and no form there sends garam a definition or a value; no garam template is offered as a start ([ADR-0090](./adr/0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md)). An agent's page shows what was requested apart from what was reported and renames it; a definition whose cutover is frozen or switched is marked, there and wherever definitions are listed, as cut-over in progress or managed by the Operator, and its page says its values are archival and read-only in garam, offering the rename alone ([ADR-0086](./adr/0086-a-legacy-agent-cuts-over-through-stages-garam-records-under-its-execution-lock.md)); an agent with no definition, as a control service creates, has a page with the report alone. The hosted operator's control service is reached for its published templates and profiles, publishing a template version, creating an agent from one, and an agent's pending, rendered and runtime-reported execution (§How it reaches a control service); the Templates view shows its templates first, and below them garam's own as archived records, each with a page showing what it said and offering no edit, deletion or start ([ADR-0088](./adr/0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)) |
| Projects | reads the projects the organization owns, and creates one — an agent's GRN names the project it was minted in ([ADR-0001](./adr/0001-grn-segments.md)), so an organization holding none cannot reach that path from the console |
| Operators | lists the operators the organization registered by name, with the identifier under each, its control service's host and where each stands in obtaining a certificate, which garam derives rather than is told ([ADR-0045](./adr/0045-an-organization-registers-an-operator.md), [ADR-0064](./adr/0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)); connects one by registering it in a dialog over the list, offered unless the server denies the caller registering, which keeps the credential and enrollment token it answers behind a closed disclosure for an operator not yet on keys ([ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)), then on the operator's setup page issues its key, shows the private key once, gives the Secret each identity reads it from, and waits for its first sign-in, resuming from the keys garam holds ([ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)). An operator's page has Overview, Keys, Control service, Delegation and Settings: what garam knows of it, with its image and last contact unbuilt until the Operator reports them; its keys, rotated from its setup page and revoked after a confirmation; its control endpoint; its delegation; and its name, description and legacy credential, re-issued after a confirmation saying the previous one and its tokens stop authenticating. A danger zone deletes it once its identifier is typed, saying that work already running in its cluster does not stop ([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)). The delete is not offered while a definition names it, which the dialog names, and a refused definitions list is marked not checked |
| Consents | reads the cross-organization consent relations the organization is a side of, grants its half of one and withdraws it ([ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)) |
| Permissions | the organization's policies and their attachments ([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)), through the existing policy operations only. An agent is chosen by its definition's name with its identifier, including one awaiting an operator ([ADR-0075](./adr/0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)). The Policies view lists policies and opens a page per policy, where one is created as a draft, its versions are read and appended, any stored version is made the default, it is activated and archived, and it is attached to and detached from teams, people and agents chosen by name. A write that makes a policy evaluate states the version and whom it reaches first, and one reaching everyone, or reaching a scope garam will not list, needs an explicit confirmation. The Attachments view shows each policy's attachments and, for one chosen subject, the policies attached to it, to its teams and to no one, without calling that its access. A list the caller is refused is shown as unchecked, never as empty; a Condition is shown as not evaluated. Neither view deletes a policy or explains a decision, since no operation does |

Every section's empty state names the absence and no next step. Where
a section creates in place, that next step is the form standing under
it; Teams carries it as the header's create link, and `/console/orgs`
states one in words, because each one's create path is a route of its
own rather than a form on the page. What an empty state can carry is
`web/src/app/console/_components/empty-state.tsx`, which is
authoritative for it and holds no control.

`/console/orgs/{org}/groups`, `…/members` and `…/policies`, three
sections' former addresses, redirect to Teams, People and Permissions
(`web/next.config.ts`).

Organizations are created, and an invitation is accepted at
`/console/invitations/{token}` — which is itself behind sign-in,
because accepting one joins an existing account to an organization.
The page reads the invitation first (`GET /invitations/{token}`, which
needs no session) and names the organization and role it admits to;
one already used, expired or unknown is said, with nothing to accept.
Accepting lands in that organization. A visitor without a session is
sent to `/login` with the path as its `next` parameter, which
`/login` and `/register` carry between them and return to once a
session exists, so the token survives either way in
(`web/src/lib/return-path.ts`, which returns only to a console path).
A session the backend rejects behind a cookie the browser still holds
is sent the same way: `web/src/proxy.ts` stamps each console request
with its path, and the redirect reads it back. The Google button posts
the same path in its form body to `POST /auth/login/{provider}`, which
holds it with the login's state, so the provider round trip lands on
it too.

Where the token may appear is fixed:

- **In three same-origin URLs only:** the invitation link, and
  `/login?next=…` and `/register?next=…`. All three are served with
  `referrer: no-referrer`, and a link carrying one does not prefetch.
- **Nowhere else:** in no redirect to another origin, in no client
  storage, and in no other URL.

No organization, group, project or user is addressed by its GRN
anywhere in the interface. A policy document is authored text naming
resources by GRN pattern, and the console shows it as written, which
is content rather than addressing
([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)). The console shows names and addresses
resources by the identifier segment the API's own paths take
([ADR-0011](./adr/0011-console-shell.md),
[ADR-0001](./adr/0001-grn-segments.md)).

## How it reaches garam

**One module.** `web/src/lib/garam.ts` holds the only call to the
backend anywhere under `web/src/`; every read and every Server Action
goes through it. That is what makes the boundary checkable rather than
asserted: one file to read to know everything the application asks for.

**It words what it refuses.** A Server Action garam refused returns
the sentence `web/src/lib/refusal-copy.ts` writes for the answer's
`reason` where garam sent one it documents, naming the place its
`path` points at. Otherwise the sentence is the one for the HTTP
status and `kind`, read for the operation that asked where the pair
means something particular to it. It is never one matched from the
English of `message`. That text reaches a person only quoted inside
the generic sentence for a refusal nothing maps.

**It checks what it hands back.** Every read gives the seam a parser
for what `api/openapi.yaml` says the operation carries, so a body that
does not match is refused there rather than reaching a page as a field
nothing proved. A parser may demand no key that operation does not
carry either, so no field `api/openapi.yaml` does not describe can be
what the console refuses a body for. A value it can: a GRN field the
console cannot read is refused here rather than thrown at whichever
page reached it first. That refusal is a third answer beside the two
the seam already has: a refusal is the backend deciding, an outage is
the backend not answering, and this is the backend answering outside
its own specification. Nothing downstream checks a field again.

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
published image to one deployment. A server-side render reads it when the
process starts, so a deployment sets it; the browser's rewrite is
resolved when the image is built, so what is in it is whatever that build
was given and no environment moves it
([ADR-0056](./adr/0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)). Every image
`.github/workflows/release.yml` publishes is built with
`http://garam-api:8080` — the api Service a release named `garam`
renders and no other — so a deployment running a published image installs
under that release name or makes the name resolve.
`deploy/charts/garam/README.md` §Choosing a release name is authoritative
for what a deployment does about it, and `make ci` compares the value the
release bakes against what the chart renders.

The address is server-side only and never reaches a browser. It is
declared once, as a typed schema in `web/src/env.ts`, parsed at import
so a missing value fails at start rather than inside the first request
that needs it. That file is also the authority `make chart` reads the
application's environment names from
([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)),
so adding a variable here is a change the chart has to answer for.

**A page asks what it offers.** `web/src/lib/permission-check.ts` puts
the actions a page offers to `checkPermissions` in one request through
the seam above, and answers each one allowed, denied or unknown
([api-surface.md](./api-surface.md) §What decides the rest). Unknown is
an action the server answers it cannot decide, or a check that could
not be asked or answered outside its contract; the last is logged on
the server. What a page does with each answer is §What it does not
decide.

Sign-in state is read from the backend's `/me` and never inferred from
the cookie (ADR-0009).

## How it reaches a control service

**Its peer is the merged Operator wire.** The console's create, read
and publish calls are the control routes at garam-agent-operator `dev`
`8e1116933408`: create (#244, `5e46800`) and the template, profile and
execution reads, template publication and CORS answer (#277,
`5ef745a`). garam#1217 records a run against them. The console's calls
are tested against a stub of that wire in
`web/e2e/control-service.spec.ts`, with garam real. A recovery control
is not built ([operators.md](./operators.md) §Delegated operation
authority).

**One module, in the browser.** `web/src/lib/operation-authority.ts`
holds the only call to a control service. The browser makes it,
because the control service's CORS answer is what admits the console's
origin; the console's own server never calls one.

- **One authority per call.** A Server Action mints it through
  `web/src/lib/garam.ts` like any other write, and the browser holds it
  only for that call: never in storage, a URL or a log line. It is
  sent as `Authorization: Garam-Operation`, with no cookie, and a
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
  (garam-agent-operator ADR 0052 and ADR 0053 at `8e1116933408`); the
  console shows the refusal.
- **It answers in the section that asked.** A refused mint, a refused
  call, an unreachable service and an answer outside the agreed wire
  each disable that section's controls and say so. Teams, People,
  Agents and every garam read stay as they are, because no other
  section waits on a control service.

## Design system

**The foundation is built, and Operators is the first area migrated.**
The tokens, fonts, theme, shell, primitives and page templates exist
(`web/src/styles/globals.css`, `web/src/components/`,
`web/src/app/console/_components/`), and the Operators list, detail
and connect pages are built on them (#1388). Each other area's
migration lands under epic #1366, in its plan's order, and until a page
migrates, §What it serves describes it as it is.
[ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
holds the full decision and the reason for each part. This section is
what a page owes once it is built on the system.

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
  - Overview above four groups: Workloads, Access, Infrastructure and
    Organization;
  - the user and the theme control at the bottom;
  - a top bar holding the breadcrumb and the command palette, which
    keeps the organization's name on mobile.

  Templates is a sidebar entry under Workloads, while its address and
  breadcrumb stay as ADR-0075 has them. Standalone hosts and Settings
  are sections no route serves yet.
- **Templates.** Every console page is a List, a Detail, a Settings
  page or a Wizard, as ADR-0103 §Page templates lays each out.
- **Primitives and patterns.** A page composes the primitives
  ADR-0103 lists rather than copying markup. It follows the patterns
  for a secret shown once, a destructive action, lifecycle stages, the
  policy editor, the operator onboarding wizard and feedback. Success
  shows inline and an error attaches to its field, with no error
  toasts.
- **Component base.** shadcn/ui stays on Radix. Base UI is declined
  for now, for the reasons ADR-0103 §Component base gives.

## What it does not decide

The boundary is as much of the responsibility as the contents.

- **No authorization decision; it asks the server instead.**
  `web/src/proxy.ts` sends a request to `/console/*` carrying no
  session cookie to `/login`, and that is a fast path rather than a
  gate: a cookie can be expired or revoked, so the console verifies the
  session against `/me`, and a 401 from any read sends the visitor to
  `/login` regardless. Every org-scoped decision belongs to the policy
  engine ([ADR-0002](./adr/0002-iam-style-policy-model.md),
  [authz.md](./authz.md)).
  - **The console asks the server whether the caller may take an
    action.** It asks through a permission-check API backed by authz
    `Check`, and hides or disables what the answer denies. The server
    still enforces every write, so a refusal that is reached still
    renders.
  - **An unanswered check shows the control.** The console infers no
    permission from a role it read
    ([ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
    §Role-aware UI).
  - **Asked by the Operators area alone so far.** The API is
    `checkPermissions`, asked through `web/src/lib/permission-check.ts`
    (§How it reaches garam). The Operators pages ask for every action
    they offer — registering, issuing and revoking keys, updating,
    delegating and deleting — and hide a control only on a deny; every
    form outside that area that the API will refuse still renders and is
    refused.
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
- **Owns no schema.** `web/src/types/api.ts` is generated from
  `api/openapi.yaml`, so a change to what an operation carries starts
  in the specification and arrives here (ADR-0004, ADR-0019).
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

`make web-ci` is the gate, and `make ci` runs it last, so the one name
covers both toolchains. In order: `web-format`, `web-lint`
(which also typechecks), `web-test`, `web-build`, `web-check-gen`.

Three things are outside it. `make web-e2e` runs the Playwright specs
in `web/e2e/` against a real built application, a real backend and a
real Postgres, so it needs infrastructure the gate does not start.
`make web-image-e2e`'s subject is the image a release publishes rather
than this tree's application, and it runs that image, so it needs a
container runtime the gate never asks for. `make web-install` is a
prerequisite rather than a check.

Tests are in two locations by decision, not by accident: Vitest owns
the tests colocated under `web/src/`, Playwright owns `web/e2e/`, and
each config names the other's territory so neither collects the other's
files (ADR-0009, `docs/convention/stack-nextjs.md` §Testing — three
layers, two locations).

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
  `web/src/features/auth/api/list-providers.ts` and
  `web/src/app/(auth)/login/page.tsx` both cite ADR-0010 for it.
- **What the application promises a visitor across versions.** Nothing
  records it, and [api-surface.md](./api-surface.md) §Not decided
  records the same absence for the API it calls.

## Decisions this rests on

The decisions the state above is made of. An ADR that changes what the
application serves, what it reaches, or what it may not decide belongs
on this list, which is the quickest test of whether a change is one
this document owes an update.

These decide the application itself:

- [ADR-0009](./adr/0009-web-frontend.md) — a Next.js application in
  `web/` in this repository, reaching the API through a same-origin
  `/api/*` rewrite, with three test layers
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
  — agents are listed by name whether or not an operator has claimed
  them, a new agent starts from a template copied once, and Templates
  is a view inside Agents
- [ADR-0088](./adr/0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)
  — garam's own templates are archived records the console neither
  edits nor offers as a start
- [ADR-0090](./adr/0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md)
  — the console offers no form that sends garam a definition, and a
  new agent is created through the control service
- [ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
  — one design system, of principles, tokens, shell, page templates,
  primitives and patterns, and a console that asks the server what a
  caller may do rather than rendering what it will refuse; the
  foundation and the permission check are built, and Operators is the
  one area migrated

The rest bear on it without being about it:

- [ADR-0001](./adr/0001-grn-segments.md) — the identifier segment is
  what the API's paths take, which is what the console shows instead of
  a GRN
- [ADR-0002](./adr/0002-iam-style-policy-model.md) — the engine decides
  every org-scoped request, so the interface decides none and asks the
  server instead
- [ADR-0004](./adr/0004-openapi-first-api.md) — `api/openapi.yaml` is
  the source of truth the client's types are generated from
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
- [ADR-0026](./adr/0026-the-console-defines-an-agent.md) — a
  definition the console wrote is not a running thing
- [ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md) — the
  callback a login lands on refuses a replayed state as unauthenticated
- [ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)
  — starting a login is refused once the live-state bound is reached,
  which the login page has to render
- [ADR-0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md)
  — defined, claimed and running are distinct states, and conflating
  them in an interface is a defect
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
  environment names from `web/src/env.ts`
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
  — the browser's `/api/*` address is fixed when the image is built, so a
  published image reaches the api Service a release named `garam` renders
- [ADR-0071](./adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
  — a development build of this image bakes the same address, since it
  is built from what the release builds, and is told apart by its tag
- [ADR-0086](./adr/0086-a-legacy-agent-cuts-over-through-stages-garam-records-under-its-execution-lock.md)
  — a definition whose agent cuts over keeps its values as an archive
  only its name still changes, which the Agents and Operators sections
  mark
- [ADR-0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)
  — `structure.md` governs what a module under `web/src/` may import,
  so a feature reaches a sibling through its declared entry points and
  the stack file no longer refuses that edge
- [ADR-0097](./adr/0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md)
  — that development build lives in `garam-dev/garam-web`, apart from
  the release repository
