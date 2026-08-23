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

`make ci` runs the same commands, and the pipeline that is configured
to run it does not run here ([AGENTS.md](./AGENTS.md) §This project) —
so run it before you push. See
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
its own toolchain and lockfile; `make web-ci` is its gate, run
alongside `make ci`.

```sh
make web-install          # pnpm install --frozen-lockfile
make web-ci               # format, lint, typecheck, unit tests, build, codegen check
pnpm --dir web dev        # dev server on http://localhost:3000
```

Copy `web/.env.example` to `web/.env.local` and point `GARAM_API_URL`
at your backend. Google sign-in registers only when the backend holds
`GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, and additionally needs
`GOOGLE_REDIRECT_URL` set to `http://localhost:3000/api/auth/callback/google`,
so the OAuth round trip stays on the web origin. Without the
credentials the backend starts and serves everything else, and
`GET /auth/providers` does not list `google`.

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
everything after the cookie is the real backend. `make web-e2e` asks
compose which port Postgres was published on and hands the suite that
URL, so `GARAM_DB_PORT` moves the suite with the server.

### The image a cluster runs

`make web-image-e2e` builds the web image and checks it rather than the
app: that the standalone server starts, that `/healthz` and `/readyz`
answer, and that `/api/*` reaches the URL baked into the build. The
Playwright suite above drives a dev server, so nothing else here
exercises the image.

```sh
make web-image-e2e                        # builds the image, then checks it
GARAM_WEB_IMAGE=<ref> make web-image-e2e  # checks that image instead
```

The override is how an image pulled from a registry by digest is
verified. Like `make e2e` and `make web-e2e` it needs Docker, so it
stays outside `make web-ci`.

## Development model

This repository is developed by AI agents under the roles and rules in
[AGENTS.md](./AGENTS.md).