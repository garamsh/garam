# site

The public face of garam: the landing page, sign-in and registration,
on its own origin beside [`console/`](../console/README.md), the
signed-in application
([ADR-0131](../docs/architecture/adr/0131-the-session-cookie-is-host-only-and-the-site-hands-a-sign-in-to-another-app-by-a-one-time-code.md)).
Its stack and conventions are the console's: Next.js App Router,
TypeScript strict, Tailwind CSS v4 and shadcn/ui, through the shared
[`design/`](../design/) package
([ADR-0009](../docs/architecture/adr/0009-web-frontend.md),
[stack-nextjs.md](../docs/convention/stack-nextjs.md),
[stack-tailwind.md](../docs/convention/stack-tailwind.md)).

Setup, the dev server, and the Playwright run command live in the
[repository README](../README.md#web-frontend).

## Layout

- `src/app/(marketing)/` — the landing page.
- `src/app/(auth)/` — `/login` takes a username and a password beside
  the buttons that start the backend OAuth flow, and `/register` turns
  an invitation into an account. Both land on the console.
- `src/proxy.ts` — redirects every console path asked of this origin,
  `/device` included, to the console's, keeping path and query.
- `src/features/` — `auth` and the invitation summary `/register`
  shows.
- `src/lib/garam.ts` — server-side API client, a configuration of
  `platform/client/`.
- `src/testing/` — MSW handlers and the Server Component render helper.
- `e2e/` — Playwright specs against the real backend.
