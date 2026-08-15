# Dev database

`docker-compose.yml` here is the dev/e2e Postgres only — the container
`make db-up` / `make db-down` manage and the e2e tests connect to. It
publishes host port 55432 by default; override with `GARAM_DB_PORT`
(e.g. per worktree).

It is independent from the full local stack in
[deploy/local/](../local/), which runs its own Postgres on a different
host port. Starting one does not affect the other.

## Files

- `docker-compose.yml` — single `postgres` service with a healthcheck;
  `make db-up` waits on it.
