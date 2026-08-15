# Local stack

Runs the whole garam stack on one machine with Docker Compose:
Postgres, migrations, the authz engine, the API, and — behind a
profile — the web frontend.

This is not the dev database compose file.
[`deploy/dev/docker-compose.yml`](../dev/docker-compose.yml) still
provides the Postgres that `make db-up` and the e2e tests use, and the
two are independent: this stack runs under the compose project
`garam-local` with its own volume, and its Postgres publishes host port
55433 so it never collides with the dev file's 55432.

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
docker compose -f deploy/local/docker-compose.yml --profile web down -v  # drop the database
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
