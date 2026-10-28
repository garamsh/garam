# console

The signed-in garam application, on its own origin beside
[`site/`](../site/README.md), which serves the landing page and the way
in (ADR-0131): Next.js App Router, TypeScript strict, Tailwind CSS
v4, shadcn/ui. Decisions are recorded in
[ADR-0009](../docs/architecture/adr/0009-web-frontend.md); conventions
in [stack-nextjs.md](../docs/convention/stack-nextjs.md) and
[stack-tailwind.md](../docs/convention/stack-tailwind.md).

Setup, the dev server, and the Playwright run command live in the
[repository README](../README.md#web-frontend).

## Layout

- `src/app/console/` — the authenticated shell and the org dashboard.
- `src/proxy.ts` — redirects visitors without a session cookie to the
  site's `/login`, and `/`, `/login` and `/register` to the site; the
  shell re-verifies the session against `/me`.
- `src/features/` — vertical slices (`auth`, `orgs`, …): API calls,
  components, helpers.
- `src/lib/garam.ts` — server-side API client; attaches the session
  cookie and maps backend errors.
- `src/types/api.ts` — generated from `platform/api/openapi.yaml` by
  `make -C console gen`; never edited by hand.
- `src/testing/` — MSW handlers and the Server Component render helper.
- `e2e/` — Playwright specs against the real backend.
