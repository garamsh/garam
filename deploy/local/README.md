# Local stack

Runs the whole garam stack on one machine with Docker Compose:
Postgres, migrations, the authz engine, the API, and — behind a
profile — the web frontend.

`garam serve machine` — the listener a machine identity authenticates
on — is not part of this stack
([ADR-0017](../../docs/architecture/adr/0017-machine-process.md)). Its
key material is per host, readable by its owner alone, and a container
running as another user is refused it; run that listener outside
compose, configured as the `platform/internal/cli` package documents, or
on the local Kubernetes stack in `minikube/`, which generates that key
material and hands the process a file it owns.

This compose file is also what the test gates run against. `make db-up`
starts only its `postgres`, `openbao` and `rustfs` services for the Go
suites `make integration` and `make e2e`, and `make db-down` removes
them again; `rustfs` is the S3 API cowork's blob store is tested
against, under keys that are this stack's alone; `make
backend-up` starts everything but the profile-gated `web` service, which
is the backend `make web-e2e` drives. Every concern shares the one
compose project `garam-local` and its `postgres-data` volume — there is
no second compose file.

## Contents
- Databases
- Files
- Up and down
- Startup order
- Probes
- Metrics
- Configuration

## Databases

One Postgres server. `make integration` truncates every table at test
setup, so neither Go suite shares a database with the running stack,
whose data you created and keep:

- `garam` — the stack's own database, created by the image from
  `POSTGRES_DB`, migrated by the `migrate` service. `GARAM_DATABASE_URL`
  points here, and so does the Playwright suite (`make web-e2e`), which
  only inserts uniquely suffixed rows and seeds sessions for the same
  api it drives.
- `garam_test` — both Go suites (`make integration`, which holds the
  store round-trips, and `make e2e`), and one suffixed database per
  linked worktree, so concurrent runs neither truncate nor migrate each
  other's. `make db-up` creates the one it is told to, and each suite
  migrates it itself. These are the only databases anything truncates.
  Neither suite creates a database of its own: both connect to the one
  `GARAM_TEST_DATABASE_URL` names and truncate it.

A worktree's database outlives the worktree, and `make db-prune` is what
collects it. It keeps `garam_test` and one database per worktree still
on disk, drops the rest, and names each one. Nothing else removes them
short of `docker compose down -v`, and that the destructive escape hatch
worked is what kept the accumulation out of sight: a stack left up
gathers a database per worktree indefinitely, which reached 89 of them
and 777 MB on one machine before anything counted (#934). It is not part
of `make ci` — a runner starts empty and its databases go with it, so
this is a local cost only.

The `openbao` service is a dev-mode OpenBao, pinned by digest, which
the Transit custodian's integration tests sign against (ADR-0104). It
holds nothing between starts: each start generates a new CA and root
token inside the container, where `make integration` has the suite read
them, and each test makes a Transit mount of its own and removes it. It
publishes host port 28200 by default, and `GARAM_OPENBAO_PORT` moves it
as `GARAM_DB_PORT` moves Postgres.

Postgres publishes host port 25432 by default. `GARAM_DB_PORT` overrides
it, in `.env` or in the environment
(`GARAM_DB_PORT=55440 make db-up e2e`). `make integration`, `make e2e`
and `make web-e2e` ask compose which port it published, so either place
moves every suite with the server.

There is one dev Postgres per machine, not one per worktree. This file
pins the compose project with `name: garam-local`, which takes
precedence over the directory Compose would otherwise name the project
after, so every worktree addresses the same container — `GARAM_DB_PORT`
moves that one server rather than standing up a second.

## Files

- `docker-compose.yml` — the stack; the `web` service is profile-gated.
- `Dockerfile` — multi-stage build of the `garam` binary; context is the
  repository root.
- `Dockerfile.web` — multi-stage Next.js standalone build; context is
  `platform-web/`.
- `.env.example` — every variable the stack reads.

## Up and down

```sh
cp deploy/local/.env.example deploy/local/.env   # then fill in Google OAuth
docker compose -f deploy/local/docker-compose.yml up --build
docker compose -f deploy/local/docker-compose.yml --profile web up --build
docker compose -f deploy/local/docker-compose.yml --profile web down
docker compose -f deploy/local/docker-compose.yml --profile web down -v  # drop both databases
```

Without `--profile web`, the `web` service is neither built nor started.
Pass the profile to `down` as well, otherwise the web container is left
running.

Every `up` above passes `--build`, and `make backend-up` passes it too.
Compose starts an image that already exists without asking whether the
sources moved, so an `up` without it runs the previous binary — and the
previous `platform/migrations/`, which ship in the same image — and
reports the stack healthy. The build is cached, so an unchanged tree
costs seconds; the containers are recreated either way, because the
image id moves on every export whether or not a layer did.

That movement is the provenance attestation BuildKit attaches to each
export, and `build.provenance: false` in the compose file does not stop
it. Compose v5.3.1 validates the key and keeps it in the output of
`docker compose config`, but never passes it to the builder, so the same
tree built twice still lands on two image ids.

The API answers on `http://localhost:8080`; with the `web` profile the
console answers on `http://console.garam.localhost:3000` and the site on
`http://garam.localhost:3001`. A browser resolves every `.localhost`
name to this machine. Each app holds a host-only session cookie, and the
console is handed its session by the site after a sign-in there
(ADR-0131); a cookie names a host and not a port, so the two run on two
hosts rather than on two ports of `localhost`. Every port, Postgres'
included, is overridable — see `.env.example`.

## Startup order

`migrate` runs once against a healthy Postgres and exits; `authz` and
`api` wait for it to have exited successfully. `api` additionally waits
for `authz`, and `web` and `site` for `api`. Every server's
healthcheck is its own readiness probe, so a service is healthy here
when it can serve a request and not merely when it has bound its port.

## Probes

Every garam process answers two probes on a port that asks for no
credential: `/healthz`, which reports the process alone, and `/readyz`,
which reports the dependencies a request needs — the database for each
of them, and the engine channel for the api. `/healthz` reads nothing
outside its process, so a database outage does not restart the stack.

- `http://localhost:8080/healthz`, `/readyz` — the api, on the port it
  already serves the HTTP API on.
- `http://localhost:9091/healthz`, `/readyz` — the authz engine, on the
  listener its metrics use.
- `http://localhost:3000/healthz`, `/readyz` and
  `http://localhost:3001/healthz`, `/readyz` — the console and the
  site, each on the port it serves on. Neither reads the api, so both
  keep answering through an api outage and the app stays in rotation.

`garam serve machine` is not part of this stack; it answers both on
`GARAM_MACHINE_HEALTH_ADDR`, a port of its own, because its own
listener serves no probe route.

## Metrics

Both servers export Prometheus metrics, and the stack publishes both
endpoints on the host:

- `http://localhost:8080/metrics` — the api, on the port it already
  serves the HTTP API on (`GARAM_LOCAL_API_PORT`).
- `http://localhost:9091/metrics` — the authz engine. It speaks gRPC
  only, so its metrics and its probes share a listener of their own:
  `GARAM_METRICS_ADDR`, fixed at `:9091` in the compose file, published
  on `GARAM_LOCAL_METRICS_PORT`.

Nothing in this stack scrapes them. Point a Prometheus at both URLs, or
`curl` one directly. Spans are not exported: the tracer mints the
`trace_id` the logs carry and stops there, because there is nowhere to
ship spans to yet.

## Configuration

Every variable is documented in `.env.example`. Three notes:

- `GARAM_AUTHZ_ADDR` is fixed to `authz:9090` in the compose file: it is
  the internal service address and is not meant to be reconfigured.
- The first account is not something this stack mints. `/login` takes
  Google, which needs real credentials in `.env`, or a username and a
  password, which `/register` mints only from an invitation an org
  admin issued
  ([ADR-0041](../../docs/architecture/adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
  An empty database holds no admin, so the way in is Google credentials
  or user and session rows written against the database directly, as
  `platform-web/e2e/seed.ts` does for the browser suite.
- The web image bakes `GARAM_API_URL` into the build, because the
  `/api/*` rewrite is resolved at build time. Changing it means
  rebuilding the image, not just restarting the container.

Never commit a filled-in `.env`; `.gitignore` already excludes it.
