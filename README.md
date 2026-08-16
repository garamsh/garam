# garam

Workflow management core: identity, organization, and authorization
for people and autonomous agents collaborating on shared work.

Documentation lives in [`docs/`](./docs/).

- [docs/README.md](./docs/README.md) — start here
- [docs/architecture/overview.md](./docs/architecture/overview.md) —
  what garam is and is not
- [docs/plans/roadmap.md](./docs/plans/roadmap.md) — where it is going
- [docs/convention/](./docs/convention/) — how we write code here

## Contents
- Module path
- Build and test
- Local stack
- Web frontend
- Development model

## Module path

`github.com/garamsh/garam`

## Build and test

```sh
go build ./...
go test  ./...
```

CI and local run the same commands (`make ci`). See
[docs/convention/ci.md](./docs/convention/ci.md).

## Local stack

[`deploy/local/`](./deploy/local/) runs everything — Postgres,
migrations, authz, api, and the frontend behind `--profile web` — in
containers. See [deploy/local/README.md](./deploy/local/README.md).

```sh
cp deploy/local/.env.example deploy/local/.env
docker compose -f deploy/local/docker-compose.yml up --build
```

Set `GARAM_DEV_AUTH=true` in `deploy/local/.env` for a one-click dev
sign-in on `/login` — a real session, no Google credentials needed
([ADR-0010](./docs/architecture/adr/0010-dev-auth-provider.md)). Off by
default; localhost tooling only.

The same compose file provides the dev/e2e Postgres: `make db-up`
starts only its `postgres` service. The stack and the tests get a
database each on it — the stack's `garam`, and `garam_test` for the
truncating suite behind `make e2e`.

## Web frontend

The Next.js app lives in [`web/`](./web/) and talks to the Go backend
through a same-origin `/api/*` rewrite
([ADR-0009](./docs/architecture/adr/0009-web-frontend.md)). It keeps
its own toolchain and lockfile; `make web-ci` is its gate, run in CI
alongside `make ci`.

```sh
make web-install          # pnpm install --frozen-lockfile
make web-ci               # lint, typecheck, unit tests, build, codegen check
pnpm --dir web dev        # dev server on http://localhost:3000
```

Copy `web/.env.example` to `web/.env.local` and point `GARAM_API_URL`
at your backend. Google sign-in additionally needs the backend's
`GOOGLE_REDIRECT_URL` set to `http://localhost:3000/api/auth/callback/google`,
so the OAuth round trip stays on the web origin.

### Playwright end-to-end tests

`web/e2e/` drives the built app in a real browser against the real
backend. Bring up the backend first:

```sh
make backend-up          # postgres, migrate, authz and api, in containers
make web-e2e
```

The suite builds and starts the app itself. The OAuth round trip cannot
run unattended, so the tests seed a user, an organization and a session
row directly into Postgres and hand the browser the resulting cookie;
everything after the cookie is the real backend. Override
`GARAM_E2E_DATABASE_URL` when Postgres is not on the default port.

## Development model

This repository is developed by AI agents under three roles (PM,
worker, QA). See [AGENTS.md](./AGENTS.md) for the role matrix and rules.