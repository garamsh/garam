# ADR 0009: Web frontend — Next.js in `web/`, Tailwind + shadcn/ui, Playwright e2e

> Status: accepted
> Date: 2026-08-02

## Context

garam needs a company web frontend over the existing API (Google OIDC,
cookie sessions, org/group/project/policy management). Requirements
from the owner: modern design, Next.js, and precise UI bug detection
via Playwright.

## Decision

- **Location**: `web/` in this repo (monorepo). The Go backend and the
  frontend share issues, PRs, and CI; no second repository.
- **Stack**: Next.js (App Router), TypeScript strict, Tailwind CSS v4,
  shadcn/ui components. Conventions: `docs/convention/stack-nextjs.md`,
  `stack-tailwind.md`.
- **API access**: Next.js `rewrites` proxy `/api/*` to the backend, so
  the session cookie stays same-origin and no CORS layer is needed.
- **Auth**: the backend's OIDC flow as-is; the UI reads session state
  from `/me`.
- **Tests**: three layers per `stack-nextjs.md` §Testing — Vitest unit,
  Vitest+MSW integration, **Playwright e2e at `web/e2e/`** driving a
  real browser against the built app.
- **CI**: a web job runs the frontend gate (lint, typecheck, unit,
  build) alongside `make ci`; Playwright runs where browsers are
  available.

## Consequences

- Two toolchains in one repo (Go + Node); each keeps its own lockfile
  and gate, CI runs both.
- The OpenAPI spec remains the contract source of truth; the frontend
  consumes it via generated types (or hand-typed clients verified
  against the spec in review).
- UI regressions are caught by Playwright before merge, not by
  inspection.
