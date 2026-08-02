# Next.js (App Router) — Architecture & Style Conventions

> Defaults for Next.js projects in 2025. Source: Next.js docs,
> `vercel/next.js` examples, `vercel/commerce`,
> `shadcn-ui/taxonomy`, `alan2207/bulletproof-react`,
> `steven-tey/novel`. 2025 patterns: App Router only, Server
> Components by default, `'use client'` only for interactivity,
> Server Actions for form mutations, Route Handlers for external
> HTTP API, `src/` layout for any non-trivial project. Defaults, not
> laws — divergence → follow code, note it in the PR description.

## Contents
- 0. Folder & file naming
- 1. Project layout (App Router, `src/`)
- 2. App Router file conventions
- 3. Server Component vs Client Component vs Server Action vs Route Handler
- 4. Server Components — pages and layouts
- 5. Server Actions
- 6. Route Handlers — external use only
- 7. Env (single typed loader at project root)
- 8. Multi-layout via route groups
- 9. Parallel routes and intercepts (modal pattern)
- 10. `proxy.ts` (v16+) / `middleware.ts`
- 11. Testing — three layers, three locations
- 12. Project conventions summary
- 13. Hierarchy (stack-specific MUST/NEVER)
- 14. Ecosystem versions (verify live)
- 15. Sources (URL index)

## 0. Folder & file naming

Names describe what they own. Follow the official Next.js 16
docs and the `create-next-app` defaults; the names the official
tooling ships are the canonical names. In particular:

- The official project structure lives at
  `nextjs.org/docs/app/getting-started/project-structure`. The
  `app/` (routes), `public/` (static assets), optional `src/`,
  and the App Router file conventions (`layout.tsx`, `page.tsx`,
  `loading.tsx`, `error.tsx`, `route.ts`, `template.tsx`,
  `default.tsx`, `proxy.ts` / `middleware.ts`) are all official
  and stay.
- `lib/utils.ts` (the shadcn-ui companion file) is allowed. It's
  the conventional home for `cn()` plus a few formatting / type
  helpers and should stay small.
- Folders Next.js itself does **not** ship: `utils/`, `helpers/`,
  `common/`, `misc/`, `shared/`, `stuff/`. These are still
  banned — they were never the Next.js canonical layout.

If a name is in the official docs / `create-next-app` default,
adopt it. If it's not, name the file or folder after what it
owns; don't reach for the vague names above.

## 1. Project layout (App Router, `src/`)

This section follows the official Next.js 16 project structure
(`nextjs.org/docs/app/getting-started/project-structure`) and
adds the bulletproof-react / shadcn-style `src/components/`,
`src/features/`, `src/lib/` shared folders — those are an
**optional layer** on top of the official routes, used when
the project has more than a handful of cross-cutting components
or vertical slices. Small projects can keep `src/` minimal:
just `src/app/`, `src/lib/utils.ts` (if shadcn-ui is in use),
`src/components/`, and a small `src/lib/` for `env.ts` /
`auth.ts` / `db.ts`.

```
my-app/
├── .env.example / .env.local (gitignored)
├── .gitignore
├── eslint.config.mjs
├── package.json
├── tsconfig.json               # paths: "@/*": ["./src/*"]
├── next.config.ts
├── proxy.ts                    # or src/proxy.ts (v16+); was middleware.ts in <v16
├── public/                     # at project root, never inside src/
├── e2e/                        # Playwright E2E tests
├── playwright.config.ts
├── vitest.config.ts
└── src/
    ├── app/                    # ROUTES ONLY (Next.js App Router)
    │   ├── layout.tsx          # root: <html>/<body>, fonts, providers
    │   ├── not-found.tsx
    │   ├── error.tsx           # must be 'use client'
    │   ├── global-error.tsx    # own <html>/<body>
    │   ├── page.tsx
    │   ├── (marketing)/        # route group: public section
    │   │   ├── layout.tsx / page.tsx
    │   │   ├── pricing/page.tsx
    │   │   └── blog/[slug]/page.tsx
    │   ├── (app)/              # route group: authenticated section
    │   │   ├── layout.tsx
    │   │   ├── dashboard/page.tsx + loading.tsx + error.tsx
    │   │   ├── dashboard/_components/{stat-card,activity-feed}.tsx
    │   │   └── settings/[section]/page.tsx
    │   ├── (auth)/             # auth flows, centered card layout
    │   │   ├── layout.tsx
    │   │   └── login/page.tsx
    │   ├── @modal/             # parallel route slot
    │   │   ├── default.tsx
    │   │   └── (..)photos/[id]/page.tsx    # intercept
    │   └── api/                # Route Handlers (external / webhooks only)
    │       ├── auth/[...nextauth]/route.ts
    │       ├── webhooks/stripe/route.ts
    │       └── og/route.ts
    │
    ├── components/             # cross-feature (optional, shadcn-style)
    │   ├── header.tsx / footer.tsx
    │   ├── theme-provider.tsx / theme-toggle.tsx ('use client' only here)
    │   └── ui/                 # shadcn primitives
    │       ├── button.tsx / dialog.tsx
    │
    ├── lib/                    # pre-configured third-party clients
    │   ├── utils.ts            # shadcn cn() + a few helpers
    │   ├── env.ts              # validated env (t3-env or zod)
    │   ├── auth.ts             # NextAuth config + helpers
    │   ├── db.ts               # Prisma / Drizzle client
    │   ├── api-client.ts / react-query.ts / rate-limit.ts
    │
    ├── features/               # vertical slices (optional, bulletproof-react)
    │   ├── auth/{api,components,hooks,types}.ts
    │   ├── billing/{api,components,hooks,types}.ts
    │   └── posts/
    │
    ├── hooks/                  # cross-feature hooks (optional)
    │   ├── use-media-query.ts / use-debounce.ts
    │
    ├── services/<vendor>/      # external API adapters (optional)
    │   └── stripe/
    │
    ├── styles/globals.css
    ├── types/api.ts
    ├── testing/                # test infra: MSW handlers, db mocks, render()
    └── instrumentation.ts      # (optional) OpenTelemetry
```

Two helpers sharing an idea → name the idea:
`lib/format-currency.ts`, `lib/email-validation.ts`. The
canonical location for cross-cutting third-party clients is
`src/lib/<vendor>.ts` (or `src/services/<vendor>/` for heavier
adapters).

## 2. App Router file conventions

A route is **not publicly accessible** until a `page.tsx` or
`route.ts` exists in the segment.

| File | Role | Key constraint |
|---|---|---|
| `layout.tsx` | Shared UI for segment + descendants. State preserved. | Root **must** render `<html>` and `<body>`. |
| `page.tsx` | Route UI; leaf of subtree. Async by default. | Required for segment to be public. |
| `loading.tsx` | Suspense fallback. | Cannot show when layout reads uncached data — wrap in `<Suspense>` or move to `page`. |
| `error.tsx` | Error boundary. | **Must be a Client Component** (`'use client'`). |
| `global-error.tsx` | Replaces root layout on catastrophic error. | Must include own `<html>` and `<body>`. |
| `not-found.tsx` | UI for `notFound()` and unmatched URLs. | Streamed → 200, non-streamed → 404. |
| `route.ts` | HTTP request handler. Exports `GET`/`POST`/`PUT`/`PATCH`/`DELETE`/`HEAD`/`OPTIONS`. | Web `Request`/`Response` only. No JSX. |
| `template.tsx` | Like layout but re-mounts on navigation. | Use sparingly. |
| `default.tsx` | Fallback for unmatched parallel route slots. | Required when using parallel routes. |

### Folder conventions

| Syntax | URL effect | Use |
|---|---|---|
| `folder/` | adds URL segment | Standard route |
| `[param]/` | dynamic | `params.param` |
| `[...rest]/` | catch-all | `params.rest: string[]` |
| `[[...rest]]/` | optional catch-all | `params.rest?: string[]` |
| `(group)/` | **no** URL effect | Organization, multiple root layouts |
| `_folder/` | **no** URL effect | Private — opted out of routing |
| `@slot/` | **no** URL effect | Named slot for parallel routes |
| `(.)folder`, `(..)folder`, … | intercepts route | Modals |

Route group rules: routes in different groups at the same segment
**must not** resolve to the same URL. Navigating across **multiple
root layouts** causes a full reload. `/` must live in one group when
using multiple root layouts.

## 3. Server Component vs Client Component vs Server Action vs Route Handler

| Concern | Server Component | Server Action | Route Handler |
|---|---|---|---|
| Read data | ✅ async render | — | ✅ in handler |
| Mutations from forms | — | ✅ **default** | OK |
| Mutations from `onClick` | — | ✅ via `useTransition` | ✅ |
| Webhooks from third parties | — | ❌ | ✅ **only choice** |
| Public REST/JSON API | — | ❌ | ✅ **only choice** |
| OAuth callbacks | — | ❌ | ✅ |
| Streaming (ReadableStream) | partial (Suspense) | — | ✅ |
| Works without JS | ✅ | ✅ | ❌ |

### Decision rule

1. **External HTTP API / webhook** → `src/app/api/<path>/route.ts`
2. **Form submission / mutation from your own UI** → Server Action
3. **`onClick` mutation with optimistic UI** → Server Action via
   `useTransition` / `useActionState`
4. **Read data for a page** → Server Component (async, fetch inline)
5. **React to a mutation** → Server Action calls `revalidatePath` /
   `revalidateTag`, or `router.refresh()`

### `'use client'` placement

Add to **specific interactive components**, not layouts or whole
pages. Once a file is `'use client'`, all its imports become
client-bundled. Use `import 'server-only'` for modules that must
never reach the client. Non-`NEXT_PUBLIC_` env vars are stripped to
`""` in client bundles.

## 4. Server Components — pages and layouts

```typescript
// src/app/(app)/dashboard/page.tsx
import { db } from '@/lib/db';
import { StatCard } from './_components/stat-card';
import { ActivityFeed } from './_components/activity-feed';

export default async function DashboardPage() {
  const stats = await db.stat.findMany();
  const recent = await db.activity.findMany({ take: 10, orderBy: { createdAt: 'desc' } });

  return (
    <main className="grid gap-4 md:grid-cols-3">
      {stats.map((s) => <StatCard key={s.id} stat={s} />)}
      <ActivityFeed items={recent} />
    </main>
  );
}
```

Pages are `async`. No `'use client'`, no `useEffect`. `params` and
`searchParams` are **promises** in v15+: `const { id } = await params`.
Layouts **do not re-render** on navigation → reading `searchParams`
there is unreliable. Use Client Components for live values.
Layouts reading uncached data (`cookies()`, `headers()`, uncached
`fetch`) **block** `loading.tsx`. Wrap data fetch in `<Suspense>` or
move to `page.tsx`.

## 5. Server Actions

```typescript
// src/features/posts/api/create-post.ts
'use server';

import { revalidatePath } from 'next/cache';
import { redirect } from 'next/navigation';
import { z } from 'zod';
import { auth } from '@/lib/auth';
import { db } from '@/lib/db';

const Schema = z.object({
  title: z.string().min(1).max(200),
  body: z.string().min(1),
});

export async function createPost(formData: FormData) {
  const session = await auth();
  if (!session?.user) throw new Error('unauthorized');

  const data = Schema.parse({
    title: formData.get('title'),
    body: formData.get('body'),
  });

  await db.post.create({ data: { ...data, authorId: session.user.id } });

  revalidatePath('/posts');
  redirect(`/posts`);
}
```

**Critical security:** Server Functions POST to the route where
they're used. A `proxy.ts` matcher that excludes a path silently
disables auth on its Server Actions. **Always re-verify the session
inside the Server Action.**

## 6. Route Handlers — external use only

```typescript
// src/app/api/webhooks/stripe/route.ts
import { NextRequest } from 'next/server';
import Stripe from 'stripe';
import { env } from '@/env';

const stripe = new Stripe(env.STRIPE_SECRET_KEY);

export async function POST(req: NextRequest) {
  const sig = req.headers.get('stripe-signature');
  if (!sig) return new Response('missing signature', { status: 400 });

  const body = await req.text();
  let event: Stripe.Event;
  try {
    event = stripe.webhooks.constructEvent(body, sig, env.STRIPE_WEBHOOK_SECRET);
  } catch (err) {
    return new Response(`invalid: ${(err as Error).message}`, { status: 400 });
  }

  return new Response('ok', { status: 200 });
}

export const dynamic = 'force-dynamic';
```

For external endpoints (webhooks, OAuth), add
`export const dynamic = 'force-dynamic'`. **Never** use Route
Handlers for internal form mutations — use Server Actions.

## 7. Env (single typed loader at project root)

```typescript
// src/env.ts (using t3-oss/env-nextjs)
import { createEnv } from '@t3-oss/env-nextjs';
import { z } from 'zod';

export const env = createEnv({
  server: {
    DATABASE_URL: z.string().url(),
    NEXTAUTH_SECRET: z.string().min(1),
    STRIPE_SECRET_KEY: z.string().min(1),
    STRIPE_WEBHOOK_SECRET: z.string().min(1),
  },
  client: {
    NEXT_PUBLIC_APP_URL: z.string().url(),
  },
  runtimeEnv: {
    DATABASE_URL: process.env.DATABASE_URL,
    NEXTAUTH_SECRET: process.env.NEXTAUTH_SECRET,
    STRIPE_SECRET_KEY: process.env.STRIPE_SECRET_KEY,
    STRIPE_WEBHOOK_SECRET: process.env.STRIPE_WEBHOOK_SECRET,
    NEXT_PUBLIC_APP_URL: process.env.NEXT_PUBLIC_APP_URL,
  },
});
```

Then `import { env } from '@/env'` everywhere. **Never** scatter
`process.env.X` reads throughout the code. Env files must stay at
project root. Load order: `process.env` → `.env.$(NODE_ENV).local` →
`.env.local` (not in test) → `.env.$(NODE_ENV)` → `.env`. First match wins.

### Browser exposure

- Without `NEXT_PUBLIC_` prefix → server only.
- With `NEXT_PUBLIC_` prefix → inlined into client bundle at build
  time; **frozen at build** — changing after `next build` has no
  effect.
- Dynamic lookups (`process.env[varName]`) are NOT inlined.

## 8. Multi-layout via route groups

```
src/app/
├── (marketing)/        # URL: /, /pricing, /blog/* — marketing chrome
│   ├── layout.tsx / page.tsx
├── (app)/              # URL: /dashboard, /settings/* — app chrome (sidebar)
│   ├── layout.tsx / dashboard/page.tsx
├── (auth)/             # URL: /login, /register — centered card
│   ├── layout.tsx / login/page.tsx
└── layout.tsx          # root: html/body, fonts, providers
```

Each route group owns its layout. **Keep multiple root layouts
rare** — navigation between them is a full reload.

## 9. Parallel routes and intercepts (modal pattern)

```
src/app/
├── @modal/
│   ├── default.tsx
│   └── (..)photos/[id]/page.tsx   # intercepted → renders as modal
├── photos/[id]/page.tsx           # real page on direct navigation
└── layout.tsx                     # renders {children} and {modal}
```

For shareable, refresh-safe, back/forward-clean modals.

## 10. `proxy.ts` (v16+) / `middleware.ts`

```typescript
// src/proxy.ts (or project root, if not using src/)
import { NextResponse, type NextRequest } from 'next/server';

export function proxy(request: NextRequest) {
  return NextResponse.next();
}

export const config = {
  matcher: '/((?!api|_next/static|_next/image|favicon.ico).*)',
};
```

- v16 renamed `middleware.ts` → `proxy.ts`. Codemod:
  `npx @next/codemod@canary middleware-to-proxy .`.
- Use cases: auth gate (redirect), geolocation, A/B headers, CORS
  preflight for `/api/*`.
- **Anti-patterns:** don't use as a general middleware hub; don't
  rely on it for Server Action security — matchers can exclude
  paths silently.

## 11. Testing — three layers, three locations

| Layer | Tool | Location | What it tests |
|---|---|---|---|
| Unit | Vitest + Testing Library | `src/**/__tests__/*.test.ts` colocated | Pure functions, components in isolation |
| Integration | Vitest + Testing Library + MSW | `src/features/<f>/__tests__/*.test.ts` or `src/app/<r>/__tests__/*.test.ts` | Full route or feature with API mocked |
| E2E | Playwright | `e2e/*.spec.ts` (project root, NOT in `src/`) | Whole-app flows in a real browser |

- MSW handlers at `src/testing/mocks/handlers.ts`.
- Render helper at `src/testing/render.tsx` wrapping `render()` with
  all needed providers.

```typescript
// src/testing/render.tsx
import { render, type RenderOptions } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

export function renderWithProviders(ui: React.ReactElement, options?: RenderOptions) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>, options);
}
```

> **Behavior over implementation.** Assert on rendered text,
> ARIA, HTTP responses (via MSW for outbound HTTP), emitted
> form payloads, and navigated URLs — not on the internal
> React tree shape or component identity.
>
> **Mocking by layer:**
> - Unit / Integration: real Next.js route handlers / Server
>   Components rendered in-process via route segment fetching
>   or `render()` from Testing Library. External systems mocked
>   at boundaries — MSW for outbound HTTP, Prisma/Drizzle
>   test doubles or SQLite for the DB, fake auth, fake time.
> - E2E: `next start` (built app) with real services via
>   `testcontainers-node` (Postgres, Redis) and Playwright
>   driving a real browser.
>
> See `references/testing-principles.md` for the full guidance.

## 12. Project conventions summary

### Folder hierarchy (unidirectional)

```
shared → features → app
```

- `components/`, `hooks/`, `lib/`, `config/`, `services/`, `types/`,
  `ui/`: cross-feature. Each may import from itself or each other.
- `features/*`: may import shared; **may not** import from `app/` or
  other features.
- `app/`: may import from shared and features.

The bulletproof-react `import/no-restricted-paths` ESLint rule
enforces this — copy it for production.

### File naming

- Component: `<name>.tsx` (multi-word: `theme-toggle.tsx`).
- Hook: `use-<name>.ts`.
- Server Action: `<verb>-<noun>.ts` with `'use server'`.
- Test: `*.test.ts(x)` inside `__tests__/`.

## 13. Hierarchy

Stack-specific MUST/NEVER:

- `page.tsx`, `layout.tsx`, `loading.tsx`, `error.tsx`,
  `not-found.tsx`, `route.ts`, `template.tsx`, `default.tsx`
  filenames — Next.js framework contract; routing breaks
  otherwise.
- `proxy.ts` (or `middleware.ts`) authentication on Server Actions
  (matchers can exclude paths — re-verify in the Server Function).
- Non-`NEXT_PUBLIC_` env accessed in client code (becomes `""`
  in client bundle).
- Mixing App Router and Pages Router.
- `loading.tsx` for layout-level uncached data (blocks navigation;
  no fallback shown).

## 14. Ecosystem versions (verify live)

Stack conventions above are stable; Next.js and library versions change. Pick libraries via live tech discovery (`npm view`; `nextjs.org/docs`) when choosing them.

## 15. Sources (URL index)

- docs: nextjs.org/docs/app/{getting-started/project-structure, api-reference/file-conventions, getting-started/server-and-client-components, getting-started/mutating-data, guides/environment-variables, api-reference/file-conventions/proxy, getting-started/caching}
- repos: github.com/{vercel/next.js/tree/canary/examples, vercel/commerce, shadcn-ui/taxonomy, alan2207/bulletproof-react/blob/master/docs/{project-structure,testing}.md, steven-tey/novel}