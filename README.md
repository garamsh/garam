# garam

Workflow management core: identity, organization, and authorization
for people and autonomous agents collaborating on shared work.

Documentation lives in [`docs/`](./docs/).

- [docs/README.md](./docs/README.md) — start here
- [docs/architecture/overview.md](./docs/architecture/overview.md) —
  what garam is and is not
- [docs/plans/roadmap.md](./docs/plans/roadmap.md) — where it is going
- [docs/convention/](./docs/convention/) — how we write code here

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
backend. Bring up the backend first, in three terminals or with `&`:

```sh
make db-up
export GARAM_DATABASE_URL='postgres://garam:garam@localhost:55432/garam?sslmode=disable'
go run ./cmd/garam migrate
go run ./cmd/garam serve authz
GARAM_COOKIE_SECURE=false GOOGLE_REDIRECT_URL=http://localhost:3000/api/auth/callback/google \
  go run ./cmd/garam serve api
```

Then, once per machine `pnpm --dir web exec playwright install chromium`,
and to run the suite:

```sh
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