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
- Searching this repository
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

`make ci` runs these checks over the packages it lists — run it before
you push. See [docs/convention/ci.md](./docs/convention/ci.md).

## Searching this repository

This repository hard-wraps its prose and its Go comments, so a phrase
longer than a few words often straddles a line break and sits on no
single line. `grep`, `git grep` and `grep -z` all report zero hits for
such a phrase, and that zero reads exactly like absence (#624).

```sh
make search Q="the phrase to look for"
```

`make search` joins each tracked text file into one line before
matching, and names how many files it read. Every run first finds a
phrase that is there — one spanning a line break that a line-based
`git grep` cannot find — and fails rather than answering when it
cannot, so a zero it prints is an answer and not a search that could
never have matched. It reads tracked files only.

## Local stack

[`deploy/local/`](./deploy/local/) runs everything — Postgres,
migrations, authz, api, and the frontend behind `--profile web` — in
containers. See [deploy/local/README.md](./deploy/local/README.md).

```sh
cp deploy/local/.env.example deploy/local/.env
docker compose -f deploy/local/docker-compose.yml up --build
```

Signing in needs real Google credentials in `deploy/local/.env`, or a
password account, which needs an invitation from an org admin
([ADR-0041](./docs/architecture/adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
A stack whose database is empty has neither: see
[deploy/local/README.md](./deploy/local/README.md) §Configuration for
the first account.

The same compose file provides the dev/e2e Postgres: `make db-up`
starts only its `postgres` service. The stack and the tests get a
database each on it — the stack's `garam`, and `garam_test` for the
truncating suite behind `make e2e`.

## Web frontend

The Next.js app lives in [`web/`](./web/) and talks to the Go backend
through a same-origin `/api/*` rewrite
([ADR-0009](./docs/architecture/adr/0009-web-frontend.md)). It keeps
its own toolchain and lockfile; `make web-ci` is its gate, and `make
ci` runs it — so `make ci` needs pnpm and Node as well as Go.

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
Playwright suite above runs the built app from the source tree, outside
any container, so nothing else here exercises the image.

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