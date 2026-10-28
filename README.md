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
- Repository layout
- Module path
- Build and test
- Searching this repository
- Local stack
- The garam CLI
- Web frontend
- Development images
- Test image
- Development model

## Repository layout

garam is becoming an agent-based collaboration platform, and this
repository a monorepo of its services
([#1655](https://github.com/garamsh/garam/issues/1655)).
[ADR-0125](./docs/architecture/adr/0125-the-repository-is-a-monorepo-of-services-laid-out-by-responsibility.md)
decides the layout: `platform/`, `platform-web/`, `site-web/`,
`lib/design/`, `tooling/` and `deploy/`, with a folder for each new
service. None of it is built yet. Until #1659 moves the tree, every path
below is today's.

## Module path

`github.com/garamsh/garam/platform`

## Build and test

Every module has a `Makefile` answering the same verbs, so
`make -C <module> ci` is that module's gate and `make ci` runs every
module's. `make affected BASE=origin/dev` prints the modules a change
can affect, each with its reason, which is what CI runs. See
[docs/convention/ci.md](./docs/convention/ci.md).

```sh
make affected BASE=origin/dev   # the modules this branch can affect
make -C platform ci             # one module's gate
make ci                         # every module's — run it before you push
```

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
database each on it — the stack's `garam`, and `garam_test` for `make
integration` and `make e2e`, the first of which truncates.

## The garam CLI

`platform-cli/` builds the `garam` a person or a program administers
garam with ([docs/architecture/cli.md](./docs/architecture/cli.md)),
a client binary apart from the server's (ADR-0132).

```sh
garam login --endpoint https://garam.example   # device grant (ADR-0120)
garam orgs use acme                            # the org later commands act in
garam operators list -o json                   # the API's own answer
garam operators keys issue hosted --rotation planned --out hosted-key.json
```

Every browser API operation becomes a command derived from
`platform/api/openapi.yaml` by `go run ./internal/cli/cligen` in
`platform-cli/`, which `make gen` runs (ADR-0122). An answer carrying a
secret, such as an issued key, is written to the file `--out` names at
mode 0600 and never printed.

## Web frontend

Two Next.js apps talk to the Go backend through a same-origin `/api/*`
rewrite ([ADR-0009](./docs/architecture/adr/0009-web-frontend.md)):
[`site-web/`](./site-web/), the landing page and the way in, and
[`platform-web/`](./platform-web/), the signed-in application, each on
its own origin ([ADR-0131](./docs/architecture/adr/0131-the-session-cookie-is-host-only-and-the-site-hands-a-sign-in-to-another-app-by-a-one-time-code.md)).
They share one lockfile at the root; `make -C site-web ci` and
`make -C platform-web ci` are their gates, and `make ci` runs both — so
`make ci` needs pnpm and Node as well as Go.

```sh
make -C platform-web install   # pnpm install --frozen-lockfile
make -C platform-web ci        # install, format, lint, typecheck, unit tests, build, codegen check
pnpm --dir platform-web dev   # console on http://console.garam.localhost:3000
pnpm --dir site-web dev       # site on http://garam.localhost:3001
```

Copy each app's `.env.example` to `.env.local` and point `GARAM_API_URL`
at your backend. Open each app on the host shown: a person signs in on
the site, and the console is handed a session of its own on its host
(ADR-0131). Google sign-in registers only when the backend holds
`GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, and additionally needs
`GOOGLE_REDIRECT_URL` set to
`http://garam.localhost:3001/api/auth/callback/google`, so the OAuth
round trip stays on the site's origin. Without the credentials the
backend starts and serves everything else, and `GET /auth/providers`
does not list `google`.

### Playwright end-to-end tests

`platform-web/e2e/` and `site-web/e2e/` drive the built apps in a real
browser against the real backend. Bring up the backend first:

```sh
make backend-up          # postgres, migrate, authz and api, in containers
make web-e2e
```

Each suite builds and starts both apps itself. The OAuth round trip
cannot run unattended, so the tests seed a user, an organization and a
session row directly into Postgres and hand the browser the resulting
cookie;
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
stays outside `make -C platform-web ci`.

## Development images

A development image goes into `garam-dev/garam-server` or
`garam-dev/garam-web`
([ADR-0097](./docs/architecture/adr/0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md)),
and its tag is what says it is not a release
([ADR-0071](./docs/architecture/adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)).
`garam/garam-server` and `garam/garam-web` hold releases only: a bare
`X.Y.Z` belongs to `.github/workflows/release.yml`, which publishes
one on a `vX.Y.Z` git tag and on nothing else. Build one
from a clean checkout of the commit you mean to name:

```sh
make image-build IMAGE_REGISTRY=<registry>                     # tag: HEAD's first 12 hex digits
make image-build IMAGE_REGISTRY=<registry> IMAGE_TAG=0.4.0-rc.1  # a prerelease tag
```

`<registry>` is the host in the chart's `image.*.repository`
(`deploy/charts/garam/values.yaml`); the path under it is
`garam-dev/<image>`, and the contexts, the Dockerfiles and the build
arguments are the ones the release reads. Each image is labelled with
HEAD and the tag.
`tooling/image-build.sh` refuses, before building anything, a bare
release version, a tag of any other form, a commit prefix that is not
HEAD's, and a working tree that differs from HEAD; its last line names
the commit built.

Nothing here pushes the result. Pushing takes a credential of your
own for the `garam-dev/` repositories, which this repository neither
holds nor grants, and the release's push role admits `v*` tags only.
Those repositories are `garamsh/aurora`'s to declare, and its
`infrastructure/aws/main.tf` declares them at
`4df93d567fddb9afe9e5120449053222dee2b7c7`.
`garamsh/infra`'s `ba7352930033f0eb1632ddb4340bb8fd2ba8b9c3`
(garamsh/infra#309) declared them before aurora absorbed it.
`GARAM_WEB_IMAGE=<registry>/garam-dev/garam-web:<tag> make web-image-e2e`
checks the web image you built before you push it.
A deployment running it sets `image.*.repository` to the `garam-dev/`
repository as well as the tag.

Without `IMAGE_REGISTRY`, `make image-build` builds both images as
`garam-server:ci` and `garam-web:ci`, which is what
`.github/workflows/images.yml` runs through `make web-image-e2e`.

## Test image

Another repository's CI copies `garam`, `testprincipal` and the
migrations of one commit out of the test image
([ADR-0092](./docs/architecture/adr/0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md)),
which an operator pushes to an ECR repository of its own under their
own AWS credentials; no workflow publishes it
([ADR-0093](./docs/architecture/adr/0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)).
From a clean checkout:

```sh
make test-image       # builds garam-test:<HEAD>
make test-image-e2e   # builds it, then checks the paths, linking and label
make test-image-push  # builds and checks it, then pushes it to ECR
make test-image-pin COMMIT=<hash> CONSUMER=<name>  # moves pinned-<name>
```

`platform/tests/testprincipal/README.md` §The test image is the
consumer's contract: what it holds, how to pin it, and how long a
version stays.

## Development model

This repository is developed by AI agents under the roles and rules in
[AGENTS.md](./AGENTS.md).