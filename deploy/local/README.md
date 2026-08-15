# Local stack

Runs the whole garam stack on one machine with Docker Compose:
Postgres, migrations, the authz engine, the API, and — behind a
profile — the web frontend.

This compose file is also the dev database: `make db-up` starts only
its `postgres` service for the e2e tests, and `make db-down` removes
that service again. Both concerns share the one compose project
`garam-local` and its `postgres-data` volume — there is no second
compose file.

## Databases

One Postgres server, two databases, because `make e2e` truncates every
table at test setup and must never reach the data you created through
the running stack:

- `garam` — the stack's own database, created by the image from
  `POSTGRES_DB`, migrated by the `migrate` service. `GARAM_DATABASE_URL`
  points here, and so does the Playwright suite (`make web-e2e`), which
  only inserts uniquely suffixed rows and seeds sessions for the same
  api it drives.
- `garam_test` — the Go store round-trips (`make e2e`). `make db-up`
  creates it if it is missing, and the suite migrates it itself. It is
  the only database anything truncates.

Postgres publishes host port 55432 by default. `GARAM_DB_PORT` overrides
it (e.g. per worktree), but it has to be an environment variable —
`GARAM_DB_PORT=55440 make db-up e2e`. The Makefile does not read
`.env`, so a value set only there moves the published port while the
Makefile's test URL stays on 55432 and `make e2e` fails to connect.

## Files

- `docker-compose.yml` — the stack; the `web` service is profile-gated.
- `Dockerfile` — multi-stage build of the `garam` binary; context is the
  repository root.
- `Dockerfile.web` — multi-stage Next.js standalone build; context is
  `web/`.
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

The API answers on `http://localhost:8080` and the frontend on
`http://localhost:3000`. Both ports, and Postgres', are overridable —
see `.env.example`.

## Startup order

`migrate` runs once against a healthy Postgres and exits; `authz` and
`api` wait for it to have exited successfully. `api` additionally waits
for `authz` to accept connections, and `web` for `api`. Neither server
registers a health endpoint, so their healthchecks are TCP connects to
their own listen port.

## Configuration

Every variable is documented in `.env.example`. Two notes:

- `GARAM_AUTHZ_ADDR` is fixed to `authz:9090` in the compose file: it is
  the internal service address and is not meant to be reconfigured.
- `GARAM_DEV_AUTH=true` adds a one-click "Dev sign-in" option to
  `/login` that creates a real session without Google credentials
  ([ADR-0010](../../docs/architecture/adr/0010-dev-auth-provider.md)).
  Off by default; localhost tooling, never enable it anywhere else. The
  login page learns the option from the api's `/auth/providers` at
  runtime, so no web rebuild is needed when toggling the flag.
- The web image bakes `GARAM_API_URL` into the build, because the
  `/api/*` rewrite is resolved at build time. Changing it means
  rebuilding the image, not just restarting the container.

Never commit a filled-in `.env`; `.gitignore` already excludes it.
