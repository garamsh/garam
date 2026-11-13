# Local stack

Runs the whole garam stack on one machine with Docker Compose:
Postgres, migrations, the authz engine, the policy service with the
feed that fills it, the API, the machine listener, and — behind
profiles — the web frontend and cowork.

This compose file is also what the test gates run against. `make db-up`
starts only its `postgres`, `openbao` and `rustfs` services for the Go
suites `make integration` and `make e2e`, and `make db-down` removes
them again; `rustfs` is the S3 API cowork's blob store is tested
against, under keys that are this stack's alone; `make
backend-up` starts everything but the profile-gated `web` service, the
policy service and its feed included, which is the backend `make
web-e2e` drives. Every concern shares the one
compose project `garam-local` and its `postgres-data` volume — there is
no second compose file.

## Contents
- Databases
- Files
- Up and down
- Startup order
- cowork
- policy
- machine
- codehub-e2e
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

- `docker-compose.yml` — the stack; the `web` and `cowork` services are
  profile-gated.
- `Dockerfile` — multi-stage build of the `garam` binary; context is the
  repository root.
- `Dockerfile.cowork` — multi-stage build of the `cowork` binary;
  context is the repository root, kept to cowork's module by
  `Dockerfile.cowork.dockerignore`.
- `Dockerfile.policy` — multi-stage build of the `policy` binary;
  context is the repository root, kept to `policy/` by
  `Dockerfile.policy.dockerignore`.
- `policy-tls.sh` — the throwaway certificate the policy service
  serves under, made once.
- `policy-keys.sh` — the signing key the api mints policy tokens with
  and the JWKS the policy service verifies them against, made once.
- `machine-material.sh` — the machine listener's certificate and
  private key and its key-encryption key, made once.
- `cowork-keys.sh` — cowork's deployment key, the service keys file
  that holds its public half and the CA file cowork trusts, made once.
- `docker-compose.codehub-e2e.yml`, `codehub-e2e-keys.sh` — what `make
  codehub-e2e` adds to the stack; see codehub-e2e below.
- `cowork-gateway.Caddyfile` — the path split on cowork's host.
- `Dockerfile.web` — multi-stage Next.js standalone build of the app
  `APP` names: `platform-web`, `site-web` or `cowork-web`; context is
  the repository root, kept to the web packages by
  `Dockerfile.web.dockerignore`.
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

## cowork

```sh
docker compose -f deploy/local/docker-compose.yml --profile web --profile cowork up --build
docker compose -f deploy/local/docker-compose.yml --profile web --profile cowork down
```

The `cowork` profile adds five services: `cowork-db`, a one-shot that
creates the `cowork` database on the one Postgres where absent;
`cowork-migrate`, which applies `cowork/migrations/` to it; `cowork`,
the server; `cowork-web`, its web app; and `cowork-gateway`, which
serves both on one host. cowork keeps artifacts in the `rustfs`
service's `cowork` bucket. It asks the policy service what a caller
may do, through the machine listener's token endpoint; see below.

Open cowork at `http://cowork.garam.localhost:3100`. It has no sign-in
of its own: a visitor is sent to sign in on the site, which needs the
`web` profile, and is handed a session of that host's own (ADR-0131,
ADR-0135).

The gateway splits the host by path as a deployment's does
(`cowork-gateway.Caddyfile`, and
[deployment-contract.md](../../docs/architecture/deployment-contract.md)
§cowork's host), so the session cookie rides the sync and live
WebSockets:

- `/api/`, with the prefix stripped — cowork's API, sync, live,
  `/healthz` and `/readyz`.
- `/mcp` — cowork's MCP endpoint.
- Everything else — cowork-web.

`garam` refuses plain HTTP to a host that is not loopback, so point it
at this gateway with `COWORK_ENDPOINT=http://localhost:3100`.
`localhost:8092` is the user-content origin HTML artifacts are viewed
from. `COWORK_LOCAL_WEB_PORT` and `COWORK_LOCAL_USERCONTENT_PORT` move
them, and a moved gateway port moves `GARAM_COWORK_URL` with it. `make
cowork-image-e2e cowork-web-image-e2e` checks both images outside the
stack, as `images.yml` does.

## policy

The default stack runs the policy service, with no profile. It adds six
services: `policy-db`, a one-shot that creates the `policy` database on
the one Postgres where absent; `policy-migrate`, which applies
`policy/migrations/` to it; `policy-tls`, a one-shot that writes a
throwaway certificate into a volume; `policy-keys`, a one-shot that
writes the signing key and the JWKS into another; `policy`, the server;
and `policy-feed`, a one-shot that runs `garam policy-feed start`. No
port is published.

The api reaches the service at `policy:8443` over TLS, trusting the
certificate `policy-tls` made as its CA, and signs its tokens with the
key `policy-keys` made, under the issuer `https://garam.localhost/api`
(`GARAM_POLICY_ADDR`, `GARAM_POLICY_CA_FILE`,
`GARAM_POLICY_SIGNING_KEY_FILE`, `GARAM_POLICY_TOKEN_ISSUER`; ADR-0146).
The same name is `GARAM_IDENTITY_ISSUER` on the api and on the machine
listener, so the identity issuer is on in both, signing with the same
key.
`policy-feed` turns the feed on once `migrate` has applied the api's
schema and the service is healthy, and backfills the outbox, so what the
api wrote before is delivered as well. Running it again changes nothing
but each row's generation.

The key is made here and nowhere else: `policy-keys.sh` runs on first
start and generates an RSA 2048 key and the JWKS holding its public
half, whose `kid` is the key's RFC 7638 thumbprint. Both live in the
`policy-keys` volume, mounted at `/policy-keys` in the api, read-only.
`signing.pem` belongs to uid 10001, the api's user, at mode 600, and the
policy service never mounts it: it is given `jwks.json` alone, at mode
644. Nothing prints the key and nothing commits one. The files stay
across `up`, and a fresh pair comes only with `down -v`, which drops the
volume, the certificate with it, and the databases. The JWKS that was
committed here is gone: nothing held its private half.

## machine

The default stack runs the machine listener, `garam serve machine`,
with no profile, published to no port: callers reach it at
`machine:8443` over TLS, and its health port is 8444. It adds three
services: `machine-material`, a one-shot that writes its certificate,
private key and key-encryption key into the `machine-material` volume;
`cowork-keys`, a one-shot that makes cowork's keys into the
`cowork-keys` volume; and `machine`. The certificate names `machine`
and is its own root. The listener has the identity issuer on, signs
with the key `policy-keys` made, and checks a service's assertion
against the service keys file `cowork-keys` wrote, whose one entry is
cowork's public key (ADR-0129).

Both scripts run on first start and generate what they hold:
`machine-material.sh` an EC P-256 certificate and key and 32 random
bytes in hex, `cowork-keys.sh` an RSA 2048 key and the JWKS holding its
public half, whose `kid` is the key's RFC 7638 thumbprint. Each private
file belongs to uid 10001, which is the account of the `garam` and
`cowork` images, at mode 600 (ADR-0017). The machine listener mounts its
volume read-only and the service keys file alone of cowork's, by volume
subpath. cowork mounts its volume read-only and never the machine
listener's. Nothing prints a key and nothing commits one. The files
stay across `up`, and a fresh set comes only with `down -v`.

cowork is given its four policy settings: `COWORK_POLICY_ADDR`,
`COWORK_POLICY_CA_FILE`, `COWORK_MACHINE_TOKEN_URL` and
`COWORK_DEPLOYMENT_KEY_FILE`. It exchanges a `private_key_jwt`
assertion signed with its deployment key at
`https://machine:8443/oauth/token` and presents the issuer's token to
the policy service. Its CA file holds the policy service's certificate
and the machine listener's: the first is what the gRPC trusts, the
second what the token request verifies. cowork waits for a healthy
`machine` and `policy`. So every decision it asks for is answered by the
policy service and not by the platform's `checkPermissions`, and a
cowork that the service refuses fails the call.

The service serves no plaintext, here or anywhere (`policy/README.md`
§Transport). The stack's connection to Postgres is the one thing here
that is plaintext, because this Postgres serves no TLS. `make
policy-image-e2e` checks the image outside the stack, against a Postgres
that refuses anything else.

## codehub-e2e

```sh
make codehub-e2e
```

Runs garam's side of codehub taking garam as its issuer and decision
point, against a stack of its own, and exits non-zero when any answer
differs from the one `tooling/codehub-e2e.sh` names. A stand-in plays
codehub's token client, adapter and outbox. The run prints each
request and the answer's status and body, with a token cut to its
header, its claims and the first 12 characters of its signature.

`docker-compose.codehub-e2e.yml` is the override, never used by the
default stack: it points the api and the `machine` service at keys the
script made for the run and the policy service at the issuer's own keys,
and publishes the api, the machine listener and the policy service each
on a free host port. The keys are made by the script under a temporary
directory and carried into the stack by `codehub-e2e-keys.sh`; the
project, its volumes and the keys are removed at every exit.
`CODEHUB_E2E_PROJECT` names the compose project (default
`garam-codehub-e2e`); the script refuses `garam-local`, the dev
stack's.

The images are built from the checkout. Nothing is pulled from ECR.

What the platform's feed writes. `garam policy-feed start` delivers
entities (the organization, its members, its teams, projects, users,
agents and operators) and, from the memberships, each member's role
link on the organization (ADR-0146 item 4). It delivers no other
policy. The run writes no link itself: the owner's and the member's
are the platform's, written when the organization was created and the
invitation accepted, and the run reads the policy service's database
to say so. Every allow in the output rests on them. A stranger who is
no member is denied, though tenant isolation would deny them with or
without a link; the line that shows a link gone is the read of the
active policies after the run removes the member by SQL (no route
removes one). The run also writes the owner's user and session row by
SQL, as `platform-web/e2e/seed.ts` does, because the stack has no
sign-in that needs no browser; the member registers through an
invitation. The run says each of these where it does it.

A person's token for the audience `codehub` comes from the device
grant the garam CLI uses, approved with the owner's session.

## Startup order

`migrate` runs once against a healthy Postgres and exits; `authz` and
`api` wait for it to have exited successfully. `api` additionally waits
for `authz`, `policy-tls` and `policy-keys`, and `web` and `site` for
`api`. `machine` waits for `migrate`, a healthy `authz`, and the three
one-shots that write its files, `machine-material`, `policy-keys` and
`cowork-keys`. `cowork-migrate` waits for `cowork-db`, `cowork` for it,
`rustfs`, `api`, `cowork-keys`, a healthy `machine` and a healthy
`policy`, `cowork-web` for `cowork`, and `cowork-gateway` for
`cowork-web`. `policy-migrate` waits
for `policy-db`, `policy` for it, `policy-tls` and `policy-keys`, and
`policy-feed` for `migrate` and a healthy `policy`. `api` does not wait
for `policy`: a policy service that is down delays a write by the gate's
five seconds and never keeps the api from starting. Every server's
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

`garam serve machine` answers both on `GARAM_MACHINE_HEALTH_ADDR`, which
compose fixes at port 8444 and publishes to nothing, because its own
listener serves no probe route. `policy` does the same on
`POLICY_HEALTH_ADDR`, which compose fixes at port 8091 and publishes to
nothing.

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
- The web image holds no `GARAM_API_URL`: the process reads it when it
  starts, so changing it means restarting the container.

Never commit a filled-in `.env`; `.gitignore` already excludes it.
