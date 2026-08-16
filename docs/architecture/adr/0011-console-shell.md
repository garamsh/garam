# ADR 0011: Console information architecture — a sidebar shell over org sections

> Status: accepted
> Date: 2026-08-16

## Context

[ADR-0009](./0009-web-frontend.md) placed a Next.js console under
`/console` and left its structure open. What shipped was a header
carrying a wordmark and a user menu over one `max-w-5xl` main, and a
single organization page stacking groups, projects, members and the
invitation form. Once inside an organization there was no way to reach
another organization, or another part of the same one, except the
browser's back button.

The org API already exposes five org-scoped reads — the organization,
its groups, its projects, its members, its policies — so the console's
structure is organization, then section. Nothing in the UI carried
that structure, and the pages that existed printed GRNs as the
identifier a person navigates by. A GRN addresses a resource in the
API; its last segment is what the API's own paths take
([ADR-0001](./0001-grn-segments.md)).

Two shapes were rejected. A top navigation bar with the organization in
a corner dropdown gives the switcher no room to grow as sections are
added, and hides the current position on a narrow viewport. A
per-organization route group with its own layout would re-fetch the
organization on every section and duplicate the shell per group for no
gain, since one shell serves both the picker and the sections.

## Decision

The console's structure is `Organizations → <organization> →
<section>`, where a section is Overview, Groups, Projects, Members or
Policies. Routes mirror it: `/console/orgs`, `/console/orgs/new`,
`/console/orgs/{org}` and `/console/orgs/{org}/{section}`. `/console`
redirects into the picker.

- The shell is the shadcn `sidebar-07` block adapted to that
  structure: an icon-collapsible sidebar whose header holds the
  organization switcher, whose content holds the sections of the
  selected organization, and whose footer holds the user menu; the
  inset header holds the sidebar trigger and a breadcrumb reading
  `Organizations / <organization> / <section>`. It renders on every
  `/console/*` route, including the invitation page.
- Primitives are brought in by the shadcn CLI and land in
  `web/src/components/ui/` unmodified. The console's own parts live in
  `web/src/app/console/_components/`, per
  [stack-nextjs.md](../../convention/stack-nextjs.md) §1.
- The shell's two reads — `/me` and `/orgs` — happen inside
  `<Suspense>` boundaries in the layout, never awaited at layout
  level, so each route's `loading.tsx` still streams. One
  request-scoped read of `/orgs` feeds the switcher, the breadcrumb
  and the picker.
- Everything that marks the current route — the active organization,
  the active section, the breadcrumb — derives it from the live
  pathname in a Client Component. A layout does not re-render on
  navigation, so a server-rendered answer would be stale.
- The console addresses organizations, groups, projects and users by
  the GRN's last segment and shows names. No GRN is printed.
- Section pages the API cannot yet serve read-only render their
  heading over an empty state rather than being omitted from the
  navigation.

## Consequences

- Adding a section is one entry in the console's section list plus one
  route folder; the sidebar, the breadcrumb and the active marking
  follow from it.
- The organization is fetched per section rather than once per
  organization, because there is no `[org]` layout to hold it. Each
  section reads only what it renders, and a denied or missing
  organization answers 404 the same way in all of them.
- The console no longer surfaces a GRN anywhere, so a visitor cannot
  copy one out of the UI for an API call. The identifier segment the
  API paths take is shown instead.
- The console carries a client bundle it did not before — the sidebar,
  its dropdowns and the breadcrumb are interactive. Pages stay Server
  Components.
- Policies appear in the navigation before the console reads them. The
  section states that in place of a list, rather than the navigation
  lying about what exists.
