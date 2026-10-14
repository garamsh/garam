# ADR 0103: The console has one design system, and asks the server what a caller may do

> Status: accepted
> Date: 2026-10-14

The console design system for epic
[#1366](https://github.com/garamsh/garam/issues/1366), recorded under
[#1367](https://github.com/garamsh/garam/issues/1367), which reserves
this number. On 2026-10-14 the owner asked for a full UI, UX and visual
redesign of the console, and delegated the decisions to the PM. The PM
decided the design brief in #1366. The PM settled two points the brief
left open in this ADR's dispatch the same day: where Overview sits, and
how Templates appears in the sidebar. No implementation ships in this
change. Every part of the decision below is unbuilt until the pull
request that builds it, in the order of #1366's plan.

## Context

At `dev` `9a808d6` the console's plumbing holds: one seam to garam, one
to a control service, parsed reads, and the UX rules
[web-app.md](../web-app.md) records. What is not designed is the visual
and interaction layer. #1366 records four research tracks from
2026-10-14. Where a fact below is about this repository, it names the
path that shows it.

- **Tokens and type.** `web/src/app/globals.css` carries the shadcn
  `neutral` base: every colour is `oklch(L 0 0)`, chroma 0, in `:root`
  and in `.dark`. `web/src/app/layout.tsx` loads Geist, Geist Mono and
  Poppins. Poppins is `--font-heading`, and the brand colour and type
  scale reach only the marketing page.
- **Dark mode is unreachable.** A `.dark` block exists, and nothing
  under `web/src/` sets that class.
- **Missing primitives are copied.** `web/src/components/ui/` has no
  Select, Tabs or Dialog. #1366 counts 6 hand-styled selects, 2 tab
  sets, 9 inline confirms and 13 form layouts.
- **Navigation is flat**, eight sections in one list
  (`web/src/app/console/_components/navigation.ts`), and detail pages
  are single long scrolls.
- **Policies and templates are edited in raw JSON textareas.** Lists
  have no search, filter or pagination.
- **The running app** (111 screenshots at 1440 and 390 px) shows
  further defects:
  - an invitation token lost through the login redirect;
  - raw server strings on screen, such as "org not found";
  - hex identifiers in user-facing text;
  - destructive actions styled like Save;
  - members shown forms they cannot use;
  - inconsistent time formats.
- **The API serves operations the console does not reach.** These are
  operator keys
  ([ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)),
  organization rename and delete with a preview
  ([ADR-0065](./0065-garam-states-what-deleting-an-organization-destroys-before-the-act.md)),
  project detail, standalone approval and agent watch
  ([ADR-0022](./0022-watching-an-agent.md)).

The design research read Vercel Geist, Linear, GitHub Primer,
Cloudflare, Supabase, Tailscale, Stripe and Temporal Cloud. It found a
shared shape:
- neutral surfaces with one accent;
- dense tables;
- an object header over tabs;
- a separated danger zone;
- a command palette.

That shape is what this decision takes. Each rule below names its
reason. Where a reason rests on something outside this repository, it
names the source and the date it was read.

## Decision

The console is built on one design system: the principles, tokens,
shell, page templates, primitives and patterns below. Every console
page is built from them. The console also asks the server which
actions a caller may take, hides or disables what the server denies,
and the server still enforces every write.

### Principles

1. **Content over chrome.** The data is the page. Borders, fills and
   decoration recede, so that what differs between two rows is what
   the eye meets.
2. **One accent colour; status colours carry meaning only.** A second
   decorative colour would compete with status for attention, and
   status is what an operator scans for.
3. **Identifiers are mono, always copyable, never a title.** A person
   reads a name and copies an identifier. Printing a hex id as a
   heading is one of the screenshot defects. This keeps
   [ADR-0011](./0011-console-shell.md)'s rule that no GRN is printed.
4. **Slow on purpose for secrets and destruction, fast everywhere
   else.** Friction belongs where a mistake cannot be undone, and
   nowhere else.
5. **Keyboard first.** The audience operates infrastructure, and the
   reference products all offer a command palette and shortcuts.
6. **Korean and English are equally first-class.** The typeface, line
   height and copy length are chosen for both, not for Latin with
   Hangul as a fallback.
7. **Human copy: never a raw server string.** A server message is
   written for the API's caller. The console maps it to a sentence for
   a person, and the screenshots show what happens otherwise.

### Typography

- **Pretendard Variable is the only UI face.** Its Latin is from Inter
  and its Hangul from Source Han Sans, so mixed Korean and English text
  shares one set of metrics. It is a variable font under the SIL Open
  Font License (`github.com/orioncactus/pretendard`, v1.3.9, read
  2026-10-14). Geist has no Hangul, so Korean text today falls back to
  whatever the platform has, with different metrics.
- **Geist Mono is used for ids, keys, JSON and timestamps**, with
  `tabular-nums` so that columns of digits align. It is the mono face
  the console already loads, under the same licence (`vercel.com/font`,
  read 2026-10-14).
- **Poppins is dropped.** A second display face serves the marketing
  voice and not the console. It is also a third font download on every
  page.
- **Sizes.** The base size is 14px and tables are 13px. Headings are
  weight 600. #1366's design research found the reference consoles at
  these sizes, and a smaller base fits more rows without a smaller
  heading.

### Colour and status

- **Tokens are oklch, declared on `@theme`, in light and dark.** In
  oklch, equal lightness steps look equal, so a light and a dark
  palette are derived by moving L rather than hand-picked.
- **Neutrals are cool greys**, chroma about 0.005 at hue 260. Chroma 0
  reads as unfinished next to an indigo accent. A trace of the accent's
  hue ties the two together.
- **One indigo accent:** `oklch(0.55 0.19 264)` in light and
  `oklch(0.68 0.16 264)` in dark. The contrast was computed for #1367
  with the WCAG 2.2 relative-luminance formula:
  - light accent against white: 5.04:1;
  - dark accent against `oklch(0.17 0.005 260)`: 6.52:1.

  Both pass WCAG 2.2 SC 1.4.3's 4.5:1 for text. A surface the
  foundation chooses keeps accent text at 4.5:1 or more against it.
- **Status colours** are ok green, warn amber, danger red and info blue,
  each a token pair in light and dark.
- **Status is always a dot plus a label, never colour alone.** WCAG 2.2
  SC 1.4.1 (Use of Color) forbids colour as the only carrier of
  meaning. A label also survives a screenshot pasted into a chat.

### Shape, density, icons and motion

- **Radius:** 6px for controls, 10px for cards and popovers, 12px for
  dialogs. Radius grows with the surface, so nesting reads correctly.
- **Spacing** is Tailwind's 4px scale. A shared scale is what makes
  spacing consistent without a review of every margin.
- **Density:** rows are 36px (32px compact) and controls 32px. The
  console's lists are what an operator scans, and #1366's design
  research found the reference consoles at these densities.
- **Icons** are Lucide at 16px with a 1.5 stroke. Lucide is the
  library `web/components.json` already names. Its default stroke of 2
  is heavy against 14px text.
- **Motion** is 120 to 180 ms, ease-out, on opacity and transform only,
  and none under `prefers-reduced-motion` (WCAG 2.2 SC 2.3.3). Animating
  layout properties causes reflow, and motion longer than that reads as
  waiting.

### Shell and navigation

The shell keeps every decision of
[ADR-0011](./0011-console-shell.md):
- `Organizations → organization → section`;
- the org switcher in the sidebar header and the user in its footer;
- a breadcrumb derived from the live pathname;
- no GRN printed.

It adds to that shell:

- **A dimmed sidebar**, 240px wide, collapsing to 56px. It is dimmed
  so that content stays the brightest surface (principle 1).
- **The org switcher shows the caller's role beside the name.** What a
  page offers depends on that role, so the caller is told it.
- **Navigation is grouped.** The IA decided in #1122 names the
  sections, and a flat list of eight does not say which belong
  together.
  - Overview stands alone at the top, above the groups. It summarises
    them, so it belongs to none.
  - **Workloads:** Agents, Templates
  - **Access:** Teams, People, Permissions
  - **Infrastructure:** Operators, Standalone hosts
  - **Organization:** Projects, Consents, Settings
- **The sidebar shows Templates as its own Workloads entry.** Its URL
  stays `…/agents/templates` and its breadcrumb stays
  `Agents / Templates`, as
  [ADR-0075](./0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
  has them. No route moves and ADR-0075 is not superseded. The sidebar
  entry only makes the view reachable in one step.
- **Standalone hosts and Settings are sections no route serves yet.**
  Each lands with its area's issue under #1366. Standalone hosts reach
  [ADR-0082](./0082-a-standalone-host-runs-its-agent-under-a-supervisor-an-administrator-approves-by-its-keys.md)'s
  approval and revoke. Settings reaches organization rename and delete.
- **The user and the theme are at the bottom of the sidebar.** The
  theme control is what makes dark mode reachable. It follows the
  system until a person chooses.
- **A 48px top bar holds the breadcrumb and `⌘K`.** It keeps the
  organization's name on mobile, where the sidebar is closed. The
  screenshots show the organization hidden there today.
- **Content is at most 1200px wide**, so a line of text stays readable
  on a wide screen. Tables are exempt, because a table's width is its
  columns.

### Page templates

Every console page is one of four templates. A page built from the
same parts in a different order is one a person has to learn again.

- **List:** a header with the count and the primary action, a filter
  bar, a DataTable, and an empty state. The count says how much there
  is before the table says what. The empty state still names the
  absence and no next step, as web-app.md records.
- **Detail:**
  - an object header holding the name, a copyable mono id, the status
    and an actions menu;
  - tabs: Overview, Activity, Settings;
  - a Danger zone card at the end.

  A single long scroll puts settings and destruction beside what a
  person came to read. Isolating destruction at the end applies
  principle 4.
- **Settings:** a section nav, and one explicit Save per section. A
  page-wide Save hides which section a refusal belongs to.
- **Wizard:** a vertical stepper. It resumes from what the server
  already holds, not from browser storage, and ends with a verify
  step. A multi-step setup that cannot resume makes a person start
  again after a failed step. The verify step proves the setup worked
  before the wizard says it did.

### Primitives

These are added, so that a page composes them rather than copying
markup:

- Select or Combobox, Tabs, and Dialog or AlertDialog;
- Field, which carries a label, a hint, an error and `aria-invalid`;
- DescriptionList;
- StatusDot, StageTrack and CopyField;
- SecretReveal (§Patterns);
- Stepper, DataTable (on TanStack Table), Kbd and Banner;
- a command palette (on cmdk);
- a time formatter: relative time on screen, and the absolute time and
  zone in a tooltip. #1366 found ISO strings cut by hand in several
  formats;
- a JSON editor (on CodeMirror, loaded client-side only) with a diff
  view. It replaces the raw textareas for policies and templates.

The pull request that adds a library pins its version. This ADR names
the libraries and no versions.

### Patterns

- **SecretReveal.** It cannot be dismissed until the secret is copied,
  downloaded or acknowledged. A secret is never put in a URL, a toast
  or a revalidated render. This keeps
  [ADR-0036](./0036-a-secret-in-garam-is-a-reference-not-a-value.md)'s
  boundary, and the rule that a credential is shown once:
  - a URL is logged and kept in history;
  - a toast disappears before it is copied;
  - a revalidation can render the secret again.
- **Operator onboarding wizard.** The steps are:
  1. register the operator;
  2. issue a key for each identity, the manager and the control
     service;
  3. place the PEM and the `tokenUrl`;
  4. verify the first exchange.

  The order is ADR-0102's, and the verify step is what tells a person
  the operator can actually authenticate.
- **Policy editor.** One document has a structured view and a JSON
  view, a diff against the active version, and "Publish vN" confirmed
  by name. A policy is authored text
  ([ADR-0073](./0073-the-console-manages-policies-and-their-attachments-under-permissions.md)),
  so the JSON stays editable. The diff and the named publish are
  what make an evaluating change deliberate.
- **Lifecycle.** A stage track shows cutover
  ([ADR-0086](./0086-a-legacy-agent-cuts-over-through-stages-garam-records-under-its-execution-lock.md))
  and recovery, and only a valid transition has a button. A button for
  an invalid transition is a refusal the page could have avoided
  offering.
- **Destructive actions.** An AlertDialog names the act in its button
  ("Revoke key", not "Confirm"). Deleting an organization requires
  typing its name, after the preview ADR-0065 answers. A generic
  "Confirm" is clicked without reading, and an organization's delete
  takes everything it holds with it.
- **Feedback.** Success shows inline next to the action, and an error
  attaches to the field it is about. There are no error toasts. A toast
  is gone before it is read and points at no field. A refused form keeps
  its input, as web-app.md records.

### Role-aware UI

**The console asks the server whether the caller may perform an
action, and hides or disables what the server answers is denied.** It
asks through a permission-check API backed by authz `Check`
([authz.md](../authz.md)). The server still enforces every write, so a
stale or skipped answer can show a control the server then refuses, but
it cannot let a write through.

This revises web-app.md's rule that the console makes no authorization
decision. It is not the console deciding: the server answers, and the
console renders the answer. The old rule's reason still holds and is
kept:
- a control the server will refuse still renders that refusal when it
  is reached, so a page is never wrong about what happened;
- every org-scoped decision still belongs to the policy engine
  ([ADR-0002](./0002-iam-style-policy-model.md)).

What changes is that a member is no longer shown a form the server
will refuse. #1366 records that as a screenshot defect.

- **A denied action is hidden** where the caller has no use for it. It
  is **disabled with the reason** where knowing it exists helps, such as
  an action a member can ask an admin for.
- **An unanswered check shows the control.** When the permission-check
  call fails, the page shows the control and the server's refusal
  stays the backstop. A failed check that hid everything would turn an
  outage into an empty console.
- **The console infers no permission from a role it read.** Roles are
  data and policies are rules (authz.md §Roles vs policies). A role
  read and turned into a decision in the browser would skip the
  policies attached to the caller.
- The permission-check API does not exist yet. It is a backend issue
  under #1366, and the specification that adds it is authoritative for
  its shape.

### Component base

**shadcn/ui on Radix is kept**, which is what the console runs:
- `web/components.json` names the `radix-nova` style;
- `web/package.json` depends on `radix-ui`.

shadcn made Base UI the default for new projects in July 2026. Its
changelog says Radix is not deprecated, that components keep shipping
for both, and that existing projects need not migrate
(`ui.shadcn.com/docs/changelog/2026-07-base-ui-default`, read
2026-10-14). Moving now would change every primitive's API, from
`asChild` to `render`, during the same epic that rewrites every page.
It would also bring behaviour changes no type error reports. The
redesign gains nothing it needs from the move.

### Declined

- **Base UI now.** Declined for the reasons under §Component base, and
  open to a later ADR.
- **Keeping Poppins for headings.** Two faces with different Hangul
  coverage is the inconsistency principle 6 exists to remove.
- **A second brand colour.** It competes with status (principle 2).
- **Error toasts.** Declined for the reasons under §Patterns, Feedback.
- **Deriving permissions in the browser from the role.** Declined for
  the reasons under §Role-aware UI.

## Consequences

- A page is reviewed against a template and a primitive list, rather
  than against the author's taste. A one-off layout is visible as one.
- Every console page changes. #1366's plan sequences the work:
  - the foundation lands before any area migrates;
  - the flow fixes and backend gaps run beside it;
  - the epic is not done until every console route is on the new shell
    and templates.
- The console takes on dependencies it does not carry today: TanStack
  Table, cmdk, CodeMirror and the Pretendard font files. Each one is
  pinned and must pass `make web-ci`.
- A page that shows a write now depends on a permission-check answer as
  well as on its read. The rule that an unanswered check shows the
  control keeps an outage from hiding anything. It also means a member
  can, during an outage, meet a form the server refuses, as today.
- The redesign rests on a backend change: the permission-check API.
  Until it lands, the role-aware rule has nothing to ask, and the
  console behaves as it does today.
- Base UI stays available as a later move. Nothing here rules it out.

## Signs the decision was wrong

- **One accent cannot carry the console's states.** Pages need
  colour-coded categories that are not status, and the single accent
  is overloaded or a second decorative colour creeps in.
- **Density hurts more than it helps.** Usability reports, or the
  mobile e2e project, show 13px tables and 36px rows misread or
  mis-tapped at 390px.
- **Pretendard does not render correctly.** Korean and English mixed
  in one line do not share a baseline, or the font's download cost
  shows in the web image's start time. Either would undo the reason it
  was chosen.
- **The permission-check API is too slow or too chatty.** A list page
  waits on many checks, so the role-aware UI costs more latency than
  showing a refusal did. That points toward a batched check or the old
  rule.
- **Radix falls behind.** A component the console needs ships only for
  Base UI, or Radix primitives stop receiving fixes. That reopens
  §Component base.
- **The templates fit badly.** Area migrations keep needing a fifth
  template, or bend one of the four. That would mean the four were
  drawn from too few pages.
