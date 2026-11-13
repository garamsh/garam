# What a deployment must give each garam process

For each of the six images, what any deployment must provide and what
the process does with it, read off the source, so that a deployment that
is not the Helm chart is written from this document.

How a gaon cluster declares these processes is
[deployment-gaon.md](./deployment-gaon.md).

Every claim holds at commit
`210b58da3bcdf9b4e3fd5346dca01a74f9d80e4e`, read from the files cited,
and is not checked against any later one. The exception is the
`GARAM_TRANSIT_*` entries for `serve machine`: they follow the change
of #1833 (ADR-0152), whose line numbers differ from that commit's in
`machine.go` and `platform/internal/message/transit`.

## Contents

- What it is
- Reading the entries
- The images
- garam/server: serve api
- garam/server: serve authz
- garam/server: serve machine
- garam/server: migrate
- garam/console
- garam/site
- garam/cowork: serve and migrate
- garam/cowork-web
- garam/policy: serve and migrate
- Public names
- codehub as a caller
- Databases
- Secrets and key material
- Not established

## What it is

This document owns what each process needs from its surroundings that
is not a value the chart sets. It uses no orchestrator's terms and maps
none of this onto any deployment kind.

- Every value, default and rendered mount a deployment sets stays in
  `deploy/charts/garam/`, which is authoritative for it
  ([deployment-contract.md](./deployment-contract.md) §What it is).
  Where an entry below needs a setting explained,
  `deploy/charts/garam/README.md` §Values a deployment must supply,
  §Key material and secrets, §The policy service and §Delivering the
  policy feed carry the explanation and this document routes to them.
- What the binary does with what the chart sets, and what is not known,
  stays in [deployment-contract.md](./deployment-contract.md), which
  this document does not restate: §What readiness reports,
  §What the binary enforces about key material, §Termination, §The
  migrate job, §cowork's host and §Not established.
- The chart deploys `garam/server`, `garam/console`, `garam/site` and
  `garam/policy`. It deploys neither `garam/cowork` nor
  `garam/cowork-web`, so for those two this document is the only
  description of a deployment
  ([deployment-contract.md](./deployment-contract.md) §What it is).
- Where a state of the code is unbuilt it is said so before it is
  described; nothing below describes a mechanism the source does not
  contain.

## Reading the entries

- A path without a directory in a `garam/server` entry is under
  `platform/internal/cli/`; in a `garam/cowork` entry, under
  `cowork/internal/cli/`; in a `garam/policy` entry, under
  `policy/crates/server/src/`; in a console, site or cowork-web entry,
  under that app's `src/`.
- A setting is **required** where the process refuses to start without
  it, and **optional** otherwise. A name alone says nothing about which.
- A setting listed as read is read from the process environment. The
  platform's only environment read is `envOr` (`cli.go:248`), and the
  names it can read are declared once (`cli.go:200-242`).
- "Exit 1" is any error returned from `garam`'s `main`
  (`platform/cmd/garam/main.go`).
- Counts are of what the entry lists and are repeated in the table
  below.

| Entry | Settings | Files | Listeners |
|---|---|---|---|
| `serve api` | 15 | 2 | 1 |
| `serve authz` | 3 | 0 | 2 |
| `serve machine` | 20 | 8 | 2 |
| `garam migrate` | 2 | 0 | 0 |
| `garam/console` | 3 (+3 image) | 0 | 1 |
| `garam/site` | 3 (+3 image) | 0 | 1 |
| `cowork serve` | 19 | 2 | 2 |
| `cowork migrate` | 2 | 0 | 0 |
| `garam/cowork-web` | 4 (+3 image) | 0 | 1 |
| `policy serve` | 7 | 3 | 2 |
| `policy migrate` | 1 | 0 | 0 |

## The images

Six repositories, one tag per release
(`.github/workflows/release.yml:102-222`). Each is built from one
Dockerfile under `deploy/local/`.

| Image | Built from | Runs as | Command |
|---|---|---|---|
| `garam/server` | `Dockerfile` | `garam`, uid 10001 | `ENTRYPOINT ["garam"]` |
| `garam/console` | `Dockerfile.web`, `APP=platform-web` | `node`, uid 1000 | `CMD ["node", "server.js"]` |
| `garam/site` | `Dockerfile.web`, `APP=site-web` | `node`, uid 1000 | as the console |
| `garam/cowork` | `Dockerfile.cowork` | `cowork`, uid 10001 | `ENTRYPOINT ["cowork"]` |
| `garam/cowork-web` | `Dockerfile.web`, `APP=cowork-web` | `node`, uid 1000 | as the console |
| `garam/policy` | `Dockerfile.policy` | `policy`, uid 10001 | `ENTRYPOINT ["policy"]` |

- Sources: `deploy/local/Dockerfile:46-76`,
  `deploy/local/Dockerfile.cowork:46-59`,
  `deploy/local/Dockerfile.policy:36-50`,
  `deploy/local/Dockerfile.web:42-74`. The uid of `node` is the chart
  README's (`deploy/charts/garam/README.md` §What it renders), not read
  from the base image.
- `garam/server`, `garam/cowork` and `garam/policy` are scratch images
  with the binary, a CA bundle and a passwd and group file naming the
  account, so a command that needs a shell, a file utility or `curl`
  has none to run. `garam/server` and `garam/cowork` carry their
  migrations at `/app/migrations`; `garam/policy` carries none, since
  its migrations are compiled into the binary
  (`deploy/local/Dockerfile.policy:20-21`,
  `policy/crates/server/src/lib.rs:87-92`).
- The web image sets `NODE_ENV=production`, `HOSTNAME=0.0.0.0` and
  `PORT=3000` and exposes 3000 (`deploy/local/Dockerfile.web:46,72-73`).
  Its working directory is `/app/<app>` and the server is Next.js
  `output: "standalone"` (`platform-web/next.config.ts:12`,
  `site-web/next.config.ts:12`, `cowork-web/next.config.ts:11`).
  The image sets placeholder `*.invalid` values for the address
  settings at build and the runtime values replace them
  (`deploy/local/Dockerfile.web:31-34`). The local stack probes it with
  `wget` against `/readyz`, so the node image carries a shell and
  `wget` (`deploy/local/docker-compose.yml`, the web services'
  healthchecks); the Go and Rust images carry neither.
- The local stack (`deploy/local/docker-compose.yml`) is the one place
  all six images run together, and it does not cover everything here:
  it sets `GARAM_POLICY_ADDR`, `GARAM_POLICY_CA_FILE`,
  `GARAM_POLICY_SIGNING_KEY_FILE` and `GARAM_POLICY_TOKEN_ISSUER` on the
  api and runs `garam policy-feed start` once the api's database is
  migrated (`deploy/local/docker-compose.yml`, the `policy-keys` and
  `policy-feed` services). It also runs the machine listener, turns the
  identity issuer on for the api and the listener, and gives cowork its
  four policy settings (the `machine-material`, `cowork-keys` and
  `machine` services). What it orders by
  `depends_on` is its own choice and is stated under each entry's
  Before start.
- The Go processes' only probe command is their own:
  `garam probe`, `cowork probe` and `policy probe <url>`, an HTTP GET
  that fails unless the answer is 200
  (`policy/crates/server/src/probes.rs:63-86`,
  `platform/internal/cli/cli.go:147`,
  `cowork/internal/cli/cli.go:73`).

## garam/server: serve api

`garam serve api` is the browser API: the operations of the API
specification and `GET /metrics`, `/healthz` and `/readyz`, on one
plaintext HTTP port (`serve.go:51-159`).

**Settings.**

| Setting | Required | If unset |
|---|---|---|
| `GARAM_DATABASE_URL` | yes | exit 1, "GARAM_DATABASE_URL is required" (`connect.go:17-27`) |
| `GARAM_HTTP_ADDR` | no | `:8080` (`serve.go:153`) |
| `GARAM_AUTHZ_ADDR` | no | `localhost:9090`; insecure gRPC, connected lazily (`connect.go:34-38`) |
| `GARAM_SESSION_TTL` | no | `168h`; an unparsable value is refused (`serve.go:81-83`) |
| `GARAM_COOKIE_SECURE` | no | `true`; an unparsable value is refused (`serve.go:85`) |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL` | no | no Google sign-in; the first two are set together or neither (`serve.go:215-234`) |
| `GARAM_MACHINE_TOKEN_URL` | no | no assertion exchange; an absolute https URL when set (`platform/internal/operator/assertion.go:118-137`) |
| `GARAM_POLICY_ADDR` | no | no policy delivery, and no request that changes a membership waits for one; when set, the three below are required (`policy_delivery.go:127-151`) |
| `GARAM_POLICY_CA_FILE`, `GARAM_POLICY_SIGNING_KEY_FILE`, `GARAM_POLICY_TOKEN_ISSUER` | with the address | see above; the key is loaded at start (`policy_delivery.go:175`) |
| `GARAM_IDENTITY_ISSUER` | no | issuer off (`identity.go:26-56`); the chart does not render it (`deploy/charts/garam/README.md:348`) |
| `GARAM_MACHINE_ISSUER` | no | read here only to refuse an identity issuer equal to it (`identity.go:26-56`) |

- Declarations: `cli.go:200-242`. What each Google, policy and issuer
  value means, and which are secrets, is routed in §Secrets and key
  material and §Public names.
- **The identity issuer is on only when both** `GARAM_IDENTITY_ISSUER`
  and `GARAM_POLICY_SIGNING_KEY_FILE` are set. The issuer set with no
  key is logged and leaves it off; the issuer and
  `GARAM_POLICY_TOKEN_ISSUER` both set and different, or the issuer
  equal to `GARAM_MACHINE_ISSUER`, is refused at start
  (`identity.go:26-56`;
  [deployment-contract.md](./deployment-contract.md) §The identity
  issuer is off until it is named).
- Google discovery runs at start, over HTTPS to Google, when Google
  sign-in is configured; a failure stops start (`serve.go:215-234`).

**Files.** `GARAM_POLICY_CA_FILE`, a PEM CA, read for the TLS to the
policy service, no mode enforced in what was read; and
`GARAM_POLICY_SIGNING_KEY_FILE`, an RSA PEM key of 2048 to 4096 bits
that only its owner may reach, whose owner is the process's uid
(`policy_delivery.go:175`, `platform/internal/signer/signer.go:68-76`,
`platform/internal/keyfile/keyfile.go:20-47`).

**Listeners.** One: `GARAM_HTTP_ADDR`, HTTP, plaintext, no TLS. Callers
are browsers through the site's and the console's `/api` proxy
(`site-web/src/proxy.ts:21-26`, `platform-web/src/proxy.ts:41-43`)
and cowork's server (`cowork/internal/platform/http/http.go:38`).
Read timeout 30s, a request body bounded at 1 MiB, keep-alives off
(`cli.go:263,273`, `serve.go:168-176,200`).

**Probes.** `/healthz` and `/readyz` on the same port. Readiness is
bounded at 2s and reports the database and the authz channel
(`platform/internal/health/health.go:17-72`, `serve.go:156,236-260`).
What readiness reports is
[deployment-contract.md](./deployment-contract.md) §What readiness
reports.

**Stop.** SIGINT or SIGTERM; in-flight requests get 10s, after which the
process logs "shutdown abandoned work in flight" and still exits 0
(`shutdown.go:18,24-26,34-55`).

**Before start.** `garam migrate` complete. No code reads the migration
version when `serve api` starts, so what it does against a database not
migrated is under §Not established.

**Calls.** PostgreSQL (`GARAM_DATABASE_URL`), the authz process
(`GARAM_AUTHZ_ADDR`), the policy service where configured, and Google
where configured. It holds one session connection for `LISTEN`, from
which a goroutine relays turn events
(`platform/internal/watch/postgres/postgres_relay.go:152`).

## garam/server: serve authz

`garam serve authz` answers authorization over gRPC for the other
`garam` processes (`serve.go:274-325`). Issue #1796 is named to remove
it, and the command exists at this commit, so it is described as it
stands.

| Setting | Required | If unset |
|---|---|---|
| `GARAM_DATABASE_URL` | yes | exit 1 (`connect.go:17-27`) |
| `GARAM_GRPC_ADDR` | no | `:9090` (`serve.go:299`) |
| `GARAM_METRICS_ADDR` | no | `:9091` (`serve.go:308`) |

- **Listeners.** `GARAM_GRPC_ADDR`: gRPC, plaintext, no TLS; its callers
  are `serve api` and `serve machine`. `GARAM_METRICS_ADDR`: HTTP,
  plaintext, `/healthz`, `/readyz` (database only) and `/metrics`,
  started after the gRPC port is bound; a failure of it is logged and
  does not stop the process (`serve.go:305-315`).
- **Files.** None.
- **Stop.** As `serve api` for the gRPC port (`shutdown.go:59-77`:
  graceful stop, then forced after 10s). The probe listener has no
  graceful stop (`serve.go:312`).
- **Before start.** `garam migrate` complete; the same absence as above.
- **Calls.** PostgreSQL only. It holds one session connection for
  `LISTEN` on the policy-change channel
  (`platform/internal/authz/postgres/listener.go:71`).

## garam/server: serve machine

`garam serve machine` is the machine listener: it terminates server
TLS itself and answers agents and the token exchange
(`machine.go:61-161`). The chart's README §Exposure is the cluster
owner's states the rule on what may stand in front of it.

| Setting | Required | If unset |
|---|---|---|
| `GARAM_DATABASE_URL` | yes | exit 1 (`connect.go:17-27`) |
| `GARAM_AUTHZ_ADDR` | no | `localhost:9090` (`connect.go:38`) |
| `GARAM_MACHINE_ADDR` | no | `:8443` (`machine.go:142`) |
| `GARAM_MACHINE_HEALTH_ADDR` | no | `:8444` (`machine.go:146`) |
| `GARAM_MACHINE_TLS_CERT_FILE` | yes | refused at start (`machine.go:400`) |
| `GARAM_MACHINE_TLS_KEY_FILE` | yes | refused at start (`machine.go:400`) |
| `GARAM_MACHINE_SERVER_ROOT_FILE` | yes | refused at start (`machine.go:404-414`) |
| `GARAM_CA_KEY_FILE` | yes | refused at start (`platform/internal/ca/sealer.go:80-95`) |
| `GARAM_AGENT_TOKEN_LIFETIME` | no | 1h; bounded 20m to 24h by `platform/internal/agent` |
| `GARAM_MACHINE_ISSUER` | yes | refused at start; `message.ParseIssuer` (`platform/internal/message/signing.go:53-62`) |
| `GARAM_TRANSIT_ADDR` | no | no Transit; then none of the other `GARAM_TRANSIT_*` may be set (`machine.go:422-462`) |
| `GARAM_TRANSIT_CA_FILE`, `GARAM_TRANSIT_SIGNING_KEY` | with the address | refused (`machine.go:441`) |
| `GARAM_TRANSIT_TOKEN_FILE` or `GARAM_TRANSIT_IDENTITY_FILE` | one of the two with the address | refused when neither or both is set (`machine.go:441-447`) |
| `GARAM_TRANSIT_AUTH_ROLE` | with the identity file | refused without it, and beside the token file (`machine.go:449-456`) |
| `GARAM_TRANSIT_AUTH_MOUNT` | no | `jwt`; refused beside the token file (`machine.go:449-451`) |
| `GARAM_TRANSIT_MOUNT` | no | read with the address (`machine.go:422-462`) |
| `GARAM_MACHINE_TOKEN_URL` | no | no assertion exchange; with the service-keys file set it must end in `/oauth/token` (`identity.go:92-99`) |
| `GARAM_IDENTITY_ISSUER` | no | issuer off (`identity.go:26-56`); the chart does not render it (`deploy/charts/garam/README.md:348`) |
| `GARAM_POLICY_SIGNING_KEY_FILE` | no | issuer and principal exchange off; the exchange answers 503 |
| `GARAM_POLICY_TOKEN_ISSUER` | no | read only for the equality check (`identity.go:26-56`) |
| `GARAM_IDENTITY_SERVICE_KEYS_FILE` | no | no first-party service principal (`identity.go:65-86`); the chart does not render it (`deploy/charts/garam/README.md:349`) |

- **Files**, eight, by rule
  (`platform/internal/keyfile/keyfile.go:20-47`; owner-only means no
  group or other permission bit and the owner is the process's uid, read
  from the open descriptor):
  - `GARAM_MACHINE_TLS_CERT_FILE`: public; re-read at every TLS
    handshake (`platform/internal/keyfile/pair.go`).
  - `GARAM_MACHINE_TLS_KEY_FILE`: owner-only; re-read at every
    handshake, so a replaced pair takes effect with no restart.
  - `GARAM_MACHINE_SERVER_ROOT_FILE`: public; read once at start; it
    must hold a PEM certificate (`machine.go:404-414`).
  - `GARAM_CA_KEY_FILE`: owner-only, hex
    (`platform/internal/ca/sealer.go:80-95`).
  - `GARAM_POLICY_SIGNING_KEY_FILE`: owner-only RSA PEM, 2048 to 4096
    bits (`platform/internal/signer/signer.go:68-76`).
  - `GARAM_TRANSIT_TOKEN_FILE`: owner-only
    (`platform/internal/message/transit/tokens.go:44`).
  - `GARAM_TRANSIT_IDENTITY_FILE`: owner-only, read at start
    (`platform/internal/message/transit/transit.go:124`) and at each
    login (`tokens.go:125`).
  - `GARAM_IDENTITY_SERVICE_KEYS_FILE`: public JWKS by service name,
    read at start and again on every authentication
    (`identity.go:65-86`); no mode is enforced.
  - `GARAM_TRANSIT_CA_FILE`: a CA file read for Transit's TLS
    (`machine.go:425`); its mode is not established.
- **Listeners.** `GARAM_MACHINE_ADDR`: HTTPS, which the process
  terminates with the pair above (`ListenAndServeTLS`,
  `machine.go:157`); no client certificate is asked for. Read timeout
  30s, body bound 1 MiB (`machine.go:347-352`). Callers: agents;
  cowork and codehub at `/oauth/token`, and codehub at
  `/oauth/introspect` (§codehub as a caller).
  `GARAM_MACHINE_HEALTH_ADDR`: HTTP, plaintext, `/healthz`, `/readyz`
  (database and authz channel) and `/metrics` (`machine.go:146-155`).
- **Assertion audience.** A service principal's assertion names as `aud`
  the endpoint's own URL, with `typ` `JWT`, or `GARAM_IDENTITY_ISSUER`
  as the only value, with `typ` `client-authentication+jwt` or `JWT`;
  the URL form is to be withdrawn. An `aud` naming both, a list,
  another string, or the URL form under the new `typ` is refused as a
  bad client, and the operator exchange takes the URL form only
  (`platform/internal/operator/assertion.go:28,169-170,233-256`,
  [machine-identity.md](./machine-identity.md) §cowork's principal).
  cowork sends the URL form (`cowork/internal/policy/assertion.go:84`).
  The introspection URL is derived from the token URL, so the two share
  one origin (`identity.go:88-99`). An unknown organization answers 400
  `unauthorized_client`; every other failure is `invalid_client`.
- **Stop.** The TLS listener through `serveHTTP`, 10s as `serve api`;
  the health listener has no graceful stop (`machine.go:150,157`).
- **Before start.** `garam migrate` complete; the same absence.
- **Calls.** PostgreSQL, the authz process, and Transit where
  `GARAM_TRANSIT_ADDR` is set.

## garam/server: migrate

`garam migrate` applies pending migrations and exits, listening on
nothing (`migrate.go:16-54`). What it does on failure and in concurrent
runs is [deployment-contract.md](./deployment-contract.md) §The migrate
job.

- Settings, two: `GARAM_DATABASE_URL`, required, exit 1 when unset;
  `GARAM_MIGRATIONS_DIR`, default `migrations`, which resolves to
  `/app/migrations` under the image's working directory
  (`migrate.go:27`, `deploy/local/Dockerfile:46-76`).
- Files: the directory of migrations, read; 143 files, the last version
  `000074` (`platform/migrations/`).
- It runs as the image's uid and needs a PostgreSQL role that can create
  tables, functions and triggers in its database; the DDL is in
  `platform/migrations/*.up.sql`, and the exact privileges are not
  established.

## garam/console

`garam/console` is the web app a signed-in person uses, Next.js on port
3000 (`deploy/local/Dockerfile.web:46,72-73`).

| Setting | Required | If unset |
|---|---|---|
| `GARAM_API_URL` | yes | the schema fails when the proxy module loads, which is the first request; the process stays up and every request, probes included, answers 500 (`env.ts:9-12`, [ADR-0150](./adr/0150-a-web-image-reads-the-api-address-when-it-starts-and-a-published-image-is-bound-to-no-release-name.md)) |
| `GARAM_SITE_URL` | yes | the same (`env.ts:17-20`) |
| `COWORK_WEB_URL` | no | the sidebar offers no link to cowork (`env.ts:21-24`) |

- All three are read when a request arrives and not at build
  (`env.ts:13-16`, `deploy/local/Dockerfile.web:26-34`). `GARAM_API_URL`
  is the address the api answers at from inside the deployment; the
  browser never sees it (`env.ts:6-8`).
- **Files.** None.
- **Listener.** One: `PORT`, default 3000, HTTP, plaintext. Callers:
  browsers, behind the console's public name.
- **Probes.** `/healthz` and `/readyz` on port 3000, both answering
  `ok` with no read of the api (`src/app/healthz/route.ts`,
  `src/app/readyz/route.ts`, `src/lib/probe.ts:7-11`;
  [ADR-0023](./adr/0023-web-probes.md)).
- **Calls.** The api, at `GARAM_API_URL`, for `/api/*` with the `/api`
  segment dropped (`proxy.ts:18-21`), and, in `/console`, the
  backend's `/me` to verify a session (`proxy.ts:24-30`); it sends a
  visitor without a session to the site (`proxy.ts:44-62`).
- **Before start.** Nothing is read: the probes read nothing of the api
  ([ADR-0023](./adr/0023-web-probes.md)). The local stack starts it
  after the api is healthy (`deploy/local/docker-compose.yml:207-209`),
  which is that stack's choice.
- **Stop.** Not established; see §Not established.

## garam/site

`garam/site` is the landing page and the way in (ADR-0131), the same
image shape on port 3000.

| Setting | Required | If unset |
|---|---|---|
| `GARAM_API_URL` | yes | as the console (`env.ts:9-12`) |
| `GARAM_CONSOLE_URL` | yes | as the console (`env.ts:17-20`) |
| `GARAM_COWORK_URL` | yes | as the console (`env.ts:23-26`) |

- **Files.** None. **Listener.** One, port 3000, HTTP, plaintext.
- **Probes.** As the console (`src/app/healthz/route.ts`,
  `src/app/readyz/route.ts`).
- **Calls.** The api at `GARAM_API_URL` for `/api/*`
  (`proxy.ts:21-26`); it redirects `/console/*` and `/device` to
  `GARAM_CONSOLE_URL` (`proxy.ts:27-29,34`).
- **Before start.** Nothing is read; the local stack starts it after the
  api is healthy (`deploy/local/docker-compose.yml:233-235`), which is
  that stack's choice. **Stop.** Not established.

## garam/cowork: serve and migrate

`cowork serve` is cowork's server: pages, channels, tasks, artifacts and
the MCP tools (`cowork/internal/cli/serve.go:178-261`). Image
`garam/cowork`, commands `serve`, `migrate`, `probe`,
`policy-feed start|stop` (`cowork/internal/cli/cli.go:73`).

**Settings, `serve`, 19** (declarations `cli.go:15-45`):

| Setting | Required | If unset |
|---|---|---|
| `COWORK_DATABASE_URL` | yes | invalid argument, exit 2 (`serve.go:185-188`) |
| `COWORK_PLATFORM_URL` | yes | the same; the root of the platform's browser API, an absolute URL (`platform/http/http.go:38-45`) |
| `COWORK_LISTEN_ADDR` | no | `:8090` (`serve.go:225`) |
| `COWORK_WEB_ORIGINS` | no | a comma list of host patterns; none means a browser's WebSocket and a framed artifact come from cowork's own host only (`serve.go:49-52,214`) |
| `COWORK_BLOB_STORE` | no | artifacts unavailable, a warning logged; `memory` loses them on stop; `s3` reads the six below; any other value is an error (`serve.go:140-166`) |
| `COWORK_S3_ENDPOINT` | no | no endpoint override (`blob/s3/s3.go:63-68`) |
| `COWORK_S3_REGION` | no | `us-east-1` (`blob/s3/s3.go:50-53`) |
| `COWORK_S3_BUCKET`, `COWORK_S3_ACCESS_KEY`, `COWORK_S3_SECRET_KEY` | with `s3` | refused (`blob/s3/s3.go:46-49`) |
| `COWORK_S3_PATH_STYLE` | no | false (`serve.go:149`) |
| `COWORK_S3_CREATE_BUCKET` | no | false; true creates the bucket when absent (`serve.go:157-161`) |
| `COWORK_USERCONTENT_ORIGIN` | no | no user-content listener and no HTML artifact view (`serve.go:168-176`) |
| `COWORK_USERCONTENT_ADDR` | no | `:8092`, only when the origin is set (`serve.go:236`) |
| `COWORK_USERCONTENT_SECRET` | with the origin | refused unless at least 32 bytes (`usercontent/usercontent.go:32,52-54`) |
| `COWORK_POLICY_ADDR`, `COWORK_POLICY_CA_FILE`, `COWORK_MACHINE_TOKEN_URL`, `COWORK_DEPLOYMENT_KEY_FILE` | all four or none | a partial set is refused; none means cowork asks the platform what a caller may do (`policy/policy.go:30-43`) |

- `COWORK_MIGRATIONS_DIR` is `migrate`'s and not `serve`'s.
- `COWORK_USERCONTENT_ORIGIN` must be an `http` or `https` origin with
  no path (`usercontent/usercontent.go:47-51`).
- **Files, three.** `COWORK_POLICY_CA_FILE`: PEM, read at start; the
  gRPC to the policy service trusts only it, and the token request
  trusts the system roots and it (`policy/client.go:29`,
  `policy/tokens.go:56-82`). `COWORK_DEPLOYMENT_KEY_FILE`: owner-only,
  owner the process's uid, an RSA PEM key of 2048 to 4096 bits, read at
  each token exchange and not at start
  (`policy/keyfile.go:12-34`, `policy/tokens.go:122`,
  `policy/assertion.go:23-55`). Its public half is garam's
  `GARAM_IDENTITY_SERVICE_KEYS_FILE` entry for `cowork`
  ([deployment-contract.md](./deployment-contract.md) §The identity
  issuer is off until it is named).
  `COWORK_USERCONTENT_SECRET` is not a file; it is read as bytes from
  the environment.
- **Listeners.** `COWORK_LISTEN_ADDR`: HTTP, plaintext,
  `ReadHeaderTimeout` 10s; the API with `/api` already stripped, the
  sync and live WebSockets, `/mcp` with a bearer, and `GET /healthz`
  and `GET /readyz` on the same port (`serve.go:224-229`,
  `cowork/internal/api/httpserver.go:16-17`,
  `cowork/internal/mcp/mcp.go:26`, `cowork/internal/api/sync.go:22`,
  `cowork/internal/api/live.go:13`). `COWORK_USERCONTENT_ADDR`: HTTP,
  plaintext, serving only HTML artifact versions at signed addresses
  (`serve.go:123-135,230-240`).
- **Probes.** `/healthz` answers 200; `/readyz` pings the database pool
  and answers 503 on failure, with no time bound read
  (`serve.go:120`, `cowork/internal/api/httpserver.go:16-17`).
- **Stop.** SIGINT or SIGTERM; one 10s context is shared by the servers
  in turn; a failure of the shutdown returns an error and exit 1,
  otherwise exit 0. A hijacked WebSocket outlives `Shutdown` and ends
  with the server's context (`serve.go:183,222-258`).
- **Before start.** `cowork migrate` complete; whether `serve` checks is
  not established.
- **Calls.** PostgreSQL (`COWORK_DATABASE_URL`, its own database); the
  platform's browser API (`COWORK_PLATFORM_URL`), asking `me`,
  `listOrgs` and permission checks with the caller's own credential
  (`platform/http/http.go:47-110`); S3 where configured (operations
  `PutObject`, `GetObject`, `DeleteObject`, and `HeadBucket` and bucket
  creation only under `COWORK_S3_CREATE_BUCKET`;
  `blob/s3/s3.go:73-127`); the policy service and the machine token
  endpoint where configured.
- **`cowork migrate`.** Two settings: `COWORK_DATABASE_URL`, required,
  exit 2 when unset, and `COWORK_MIGRATIONS_DIR`
  (`cowork/internal/cli/migrate.go`). It listens on nothing, refuses a
  dirty database and applies the 12 files under `cowork/migrations/`,
  the last `000006_policy_outbox`. The migrations library is
  golang-migrate v4.20.1 (`cowork/go.mod:15`).
- cowork holds no `LISTEN`: its fan-out hub is in the process
  (`serve.go:92-94`), and a case-sensitive search of `cowork/` and
  `policy/` for `LISTEN`, `pg_notify` and `NOTIFY` in Go, Rust and SQL
  found nothing.

## garam/cowork-web

`garam/cowork-web` is cowork's web app, port 3000
(`deploy/local/Dockerfile.web`).

| Setting | Required | If unset |
|---|---|---|
| `COWORK_API_URL` | yes | the schema fails at import (`env.ts:5-11`) |
| `GARAM_API_URL` | yes | the same (`env.ts:14`) |
| `GARAM_SITE_URL` | yes | the same (`env.ts:17`) |
| `COWORK_SYNC_URL` | yes | the same (`env.ts:22`) |

- `COWORK_API_URL` is cowork's server as the web app reaches it, with no
  prefix; `COWORK_SYNC_URL` is the same server as the browser reaches
  it, on this app's own host and under `/api`, a `ws` or `wss` URL
  (`env.ts:9-22`; [deployment-contract.md](./deployment-contract.md)
  §cowork's host).
- **Files.** None. **Listener.** One, port 3000, HTTP, plaintext.
- **Probes.** `/healthz` and `/readyz`, port 3000
  (`src/app/healthz/route.ts`, `src/app/readyz/route.ts`); the proxy
  leaves them and `/auth/` public (`proxy.ts:34`).
- **Calls.** cowork's server (`COWORK_API_URL`) with the caller's
  cookie, and the platform's browser API (`GARAM_API_URL`), asking which
  organizations the caller belongs to (`env.ts:9-14`). A visitor
  without a session is sent to the site (`proxy.ts:18-28`).
- **Before start.** Nothing is read: the probes read nothing of cowork.
  The local stack starts it after cowork is healthy
  (`deploy/local/docker-compose.yml:567-569`), which is that stack's
  choice. **Stop.** Not established.

## garam/policy: serve and migrate

`policy serve` serves the policy contract over gRPC and HTTP on one
TLS listener (`policy/crates/server/src/lib.rs:106-138`). Commands:
`serve`, `migrate`, `probe <url>` (`main.rs:11-17`).

**Settings, `serve`, 7**, all required, an unset one refusing start with
"`<name>` is not set" (`config.rs:36-48`):

- `POLICY_ADDR`, the listener address; no default.
- `POLICY_HEALTH_ADDR`, the probe address; no default.
- `POLICY_DATABASE_URL`, its own database (`config.rs:9-11`).
- `POLICY_TOKEN_ISSUER`, the `iss` a caller's token must name.
- `POLICY_JWKS_FILE`, the issuer's public keys.
- `POLICY_TLS_CERT_FILE` and `POLICY_TLS_KEY_FILE`, the listener's pair.

**Files, three.**

- `POLICY_JWKS_FILE`: public; read once at start; one or two public RSA
  keys, and a file that does not parse or holds none or more than the
  limit is refused (`caller.rs:150-170,289-302`).
- `POLICY_TLS_CERT_FILE`: public; read once at start
  (`tls.rs:159-176`).
- `POLICY_TLS_KEY_FILE`: owner-only and the process's uid, checked on
  the file's metadata before it is read; read once at start, so
  rotation needs a restart (`tls.rs:159-190`).

**Listeners, 2.** `POLICY_ADDR`: gRPC and the HTTP binding, one TLS
listener (rustls with ring), ALPN `h2` and `http/1.1`; there is no
plaintext path (`lib.rs:106-109,130`, `tls.rs:10-25`). Callers: garam
as `platform`, and cowork and codehub as scoped callers, each told by
the token's `sub` (`caller.rs:35-70`); any other `sub` is denied.
Two are named by a setting: garam's `serve api`
(`GARAM_POLICY_ADDR`) and cowork (`COWORK_POLICY_ADDR`); codehub is
named by none and is §codehub as a caller.
`POLICY_HEALTH_ADDR`: HTTP, plaintext,
`/healthz` and `/readyz`, a credential riding neither
(`probes.rs:13-34`).

**Probes.** `/healthz` answers 200 and reads nothing; `/readyz` answers
200 while `SELECT 1` succeeds within 2s and 503 otherwise
(`probes.rs:13-34`).

**Stop.** SIGTERM or SIGINT resolve the shutdown, which stops the
contract listener gracefully (`main.rs:44-59`, `lib.rs:133`). How long
it waits for work in flight, and the exit code on stop, are not
established.

**Before start.** `policy migrate` complete against
`POLICY_DATABASE_URL`; `policy serve` applies none (`lib.rs:75-82`).
Whether it checks that the schema is current is not established.

**Calls.** PostgreSQL (`POLICY_DATABASE_URL`). No setting names
another process or a store (`config.rs:36-48`). It holds no `LISTEN`
(§Databases).

**`policy migrate`.** One setting: `POLICY_DATABASE_URL`
(`main.rs:35-40`, `config.rs:9-11`). It applies the two migrations
under `policy/migrations/` that are compiled into the binary (four
files, an up and a down each), each in a transaction under an
advisory lock, refuses a database an earlier run left dirty, and listens
on nothing (`lib.rs:84-92`).

## Public names

The names a deployment must give, with what sits behind each and what
must agree with it. A name is a host a person or a process opens. The
chart's README §Exposure is the cluster owner's says which of them
leave the cluster.

| Name | Behind it | What must agree with it |
|---|---|---|
| The site | `garam/site`, port 3000, behind a TLS-terminating proxy; `/api/*` reaches the api | `GARAM_SITE_URL` on the console and on cowork-web; `GOOGLE_REDIRECT_URL` where Google sign-in is on |
| The console | `garam/console`, port 3000; `/api/*` reaches the api | `GARAM_CONSOLE_URL` on the site |
| cowork's host | one host split by path: `/api/` to cowork's server with the prefix stripped, `/mcp` to the same server as it stands, the rest to cowork-web (port 3000) | `GARAM_COWORK_URL` on the site, `COWORK_WEB_URL` on the console (optional), `COWORK_SYNC_URL` on cowork-web, `COWORK_WEB_ORIGINS` on cowork's server |
| The user-content origin | cowork's `COWORK_USERCONTENT_ADDR` listener, port 8092 by default | `COWORK_USERCONTENT_ORIGIN`; a name apart from cowork's host is the only reading the source supports, and is not otherwise established |
| The machine listener | `serve machine`, port 8443, TLS terminated by the process | `GARAM_MACHINE_ISSUER`, an https origin with no path and no trailing slash; the certificate named by `GARAM_MACHINE_TLS_CERT_FILE` and the root in `GARAM_MACHINE_SERVER_ROOT_FILE` |
| The identity issuer | `serve api` and `serve machine` serving the issuer's four operations | `GARAM_IDENTITY_ISSUER`, exactly `https://<host>/api`, lowercase host, no empty port, no trailing slash (`platform/internal/identity/issuer.go:76-92`); and `GARAM_POLICY_TOKEN_ISSUER`, which where both are set must equal it, as must the policy service's `POLICY_TOKEN_ISSUER` ([deployment-contract.md](./deployment-contract.md) §The identity issuer is off until it is named) |
| The token endpoint | `serve machine`'s `/oauth/token` | `GARAM_MACHINE_TOKEN_URL`, an absolute https URL that ends `/oauth/token` where the service-keys file is set (`identity.go:92-99`, `platform/internal/operator/assertion.go:118-137`); `COWORK_MACHINE_TOKEN_URL` is the one cowork posts to, and the introspection address an assertion names is this value with `/oauth/token` replaced ([deployment-contract.md](./deployment-contract.md) §The identity issuer is off until it is named) |

- The browser API's root, without a prefix, is
  `COWORK_PLATFORM_URL` on cowork and the web apps' `GARAM_API_URL`; it
  is internal and need not be a public name
  (`cowork/internal/platform/http/http.go:38-45`).
- The identity issuer's `/api` sits on the site's host or the
  console's: both web apps proxy `/api/*` to the api
  (`site-web/src/proxy.ts:21-26`, `platform-web/src/proxy.ts:41-43`),
  so the choice is the deployment's. garam recommends the site's,
  because the CLI's default origin is the site's: the CLI signs in at
  `https://garam.sh` where nothing names another
  (`garam-cli/internal/session/session.go:16-18`), and
  [ADR-0131](./adr/0131-the-session-cookie-is-host-only-and-the-site-hands-a-sign-in-to-another-app-by-a-one-time-code.md) puts sign-in at that host. The
  name stays one for the deployment's life: `GARAM_IDENTITY_ISSUER`
  is the `iss` of every token the issuer has signed, and the policy
  service's `POLICY_TOKEN_ISSUER` must equal it.
- The policy service's address is internal to garam and cowork:
  `GARAM_POLICY_ADDR` on garam and `COWORK_POLICY_ADDR` on cowork name
  the same listener, which presents a certificate the two
  `*_POLICY_CA_FILE` settings trust. codehub is a third caller, and
  whether its name for that listener is inside the deployment's network
  is the deployment's; see §codehub as a caller.

## codehub as a caller

codehub is a service garam does not deploy. It acts in an organization
as `grn:<org>/iam/service/codehub`
([machine-identity.md](./machine-identity.md) §codehub's principal). It
may run beside garam or apart from it, so this section states what
codehub must reach and leaves where it runs to the deployment. codehub
has three places to reach, each served by a process above:

| What codehub reaches | Served by | Credential codehub brings | Must the name be reachable from outside the deployment's network |
|---|---|---|---|
| The policy service's listener, `POLICY_ADDR`: gRPC and the HTTP binding's evaluations and entity feed | `policy serve` | an issuer token for the `policy` audience; any other `sub` than `platform`, `cowork` or `codehub` is denied (`policy/crates/server/src/caller.rs:35-70`) | where codehub runs apart from the deployment's network, yes; where it runs beside it, no |
| The token endpoint, `/oauth/token`, and introspection, `/oauth/introspect`, on the machine listener | `serve machine` | a `private_key_jwt` assertion signed with codehub's key | the same |
| The issuer's discovery and JWKS, on the browser-facing listener at `GARAM_IDENTITY_ISSUER` | `serve api` | none; answered without a credential | the same; the issuer's name is a public name already (§Public names) |

- **Policy service.** `POLICY_ADDR` answers codehub as it answers
  garam and cowork, on one TLS listener with no plaintext path
  (`policy/README.md` §Callers, §AuthZEN binding;
  [ADR-0149](./adr/0149-codehub-is-a-service-of-the-policy-service-whose-repositories-are-entities-and-whose-paths-are-context.md)).
  The HTTP routes are `POST /access/v1/evaluation`,
  `POST /access/v1/evaluations`,
  `GET /.well-known/authzen-configuration` and
  `POST /garam/policy/v1/entity-changes`
  (`policy/crates/server/src/http.rs:26-29`). codehub's `sub` is
  `grn:<org>/iam/service/codehub`, bound to that organization, which
  may not be `root`. The certificate the listener presents is the
  pair named by `POLICY_TLS_CERT_FILE`; codehub must trust it, and
  where it runs apart the certificate must name the host codehub
  reaches. The metadata names its endpoints under the host the client
  reached, and a request naming no host is a 400 (`policy/README.md`
  §AuthZEN binding). The policy-side support is built at this commit
  (`policy/schema/codehub.cedarschema`, `policy/README.md:68`);
  `machine-identity.md` §codehub's principal and the body of ADR-0149
  still call it unbuilt, and the source is followed here.
- **Token endpoint and introspection.** codehub asks the machine
  listener's `/oauth/token` with its principal's GRN as `client_id` and
  `audience` `policy`, and its assertion's `aud` is the endpoint's own
  URL or `GARAM_IDENTITY_ISSUER`
  (`platform/internal/operator/assertion.go:169-170,233-256`). The
  exchange answers only where `GARAM_IDENTITY_SERVICE_KEYS_FILE` holds
  a key for the name `codehub`, at most two, RS256; with none, no
  codehub principal authenticates. Introspection answers for tokens
  issued for the `codehub` audience and reads a `policy` one as
  inactive. Both are on the machine listener, whose certificate codehub
  must trust (`GARAM_MACHINE_SERVER_ROOT_FILE` is the root garam names
  for it).
- **Discovery and JWKS.** `<issuer>/.well-known/openid-configuration`
  and `<issuer>/oauth/jwks` need no credential
  ([api-surface.md](./api-surface.md) §The issuer's endpoints). They
  exist only where `GARAM_IDENTITY_ISSUER` and
  `GARAM_POLICY_SIGNING_KEY_FILE` are both set.
- **Unbuilt**, per [machine-identity.md](./machine-identity.md)
  §codehub's principal: codehub's adapter, outbox and runner, an
  authorization-code flow for codehub, and the chart rendering the
  issuer's settings (`deploy/charts/garam/README.md:348-349`). A
  deployment sets `GARAM_IDENTITY_ISSUER` and
  `GARAM_IDENTITY_SERVICE_KEYS_FILE` itself until the chart does.

## Databases

Three PostgreSQL databases, one per owner, each with a role that owns
its objects:

| Database | Setting | Read by | Migrated by |
|---|---|---|---|
| garam's | `GARAM_DATABASE_URL` | `serve api`, `serve authz`, `serve machine`, `garam migrate` | `garam migrate` |
| cowork's | `COWORK_DATABASE_URL` | `cowork serve`, `cowork migrate` | `cowork migrate` |
| the policy service's | `POLICY_DATABASE_URL` | `policy serve`, `policy migrate` | `policy migrate` |

- Version: PostgreSQL 17, and 17.11 in the local stack
  (`deploy/local/docker-compose.yml:24`). No migration of any of the
  three creates an extension: a search of `platform/migrations`,
  `cowork/migrations` and `policy/migrations` for `CREATE EXTENSION`
  found nothing.
- **Session connections.** `LISTEN` holds a connection for the life of
  the process: `serve authz`
  (`platform/internal/authz/postgres/listener.go:71`) and `serve api`
  (`platform/internal/watch/postgres/postgres_relay.go:152`), both on
  garam's database. `pg_notify` is called by `serve api`
  (`postgres_relay.go:76,101`) and by two triggers in
  `platform/migrations/000002_policies.up.sql:43` and
  `000003_policy_attachments.up.sql:44`. A pooler that does not keep a
  session for the connection breaks the listeners; whether it does is
  the pooler's. No other process in the six listens.
- The roles' privileges beyond those the migrations need are not
  established, and the connection options a URL carries are the
  deployment's: `deploy/charts/garam/README.md` §Values a deployment
  must supply names where the chart takes them.

## Secrets and key material

Named, with the process that reads each. No value is in this document
and no value is to be copied into one. The chart's README §Key material
and secrets is authoritative for how each arrives; the key-file rule is
[deployment-contract.md](./deployment-contract.md) §What the binary
enforces about key material.

| Name | Read by | Kind |
|---|---|---|
| `GARAM_DATABASE_URL` | the four `garam` commands | a URL that may carry a password |
| `COWORK_DATABASE_URL` | `cowork serve`, `cowork migrate` | the same |
| `POLICY_DATABASE_URL` | `policy serve`, `policy migrate` | the same |
| `GOOGLE_CLIENT_SECRET` | `serve api` | environment |
| `GARAM_CA_KEY_FILE` | `serve machine` | owner-only hex file |
| `GARAM_MACHINE_TLS_KEY_FILE` | `serve machine` | owner-only file |
| `GARAM_POLICY_SIGNING_KEY_FILE` | `serve api`, `serve machine` | owner-only RSA PEM |
| `GARAM_TRANSIT_TOKEN_FILE` | `serve machine` | owner-only file |
| `GARAM_TRANSIT_IDENTITY_FILE` | `serve machine` | owner-only file |
| `COWORK_S3_ACCESS_KEY`, `COWORK_S3_SECRET_KEY` | `cowork serve` | environment |
| `COWORK_USERCONTENT_SECRET` | `cowork serve` | environment, at least 32 bytes |
| `COWORK_DEPLOYMENT_KEY_FILE` | `cowork serve` | owner-only RSA PEM |
| `POLICY_TLS_KEY_FILE` | `policy serve` | owner-only file |

The public files, which need no owner-only mode, are the machine
certificate and server root, the two CA files for the policy service,
the Transit CA, `GARAM_IDENTITY_SERVICE_KEYS_FILE` and
`POLICY_JWKS_FILE`.

## Not established

Not guessed. Each is what the source read does not state; a deployment
that depends on one decides it and records it as decided.

- **Replica safety above one**, for every process here. No leader
  election exists. cowork's hub is in memory
  (`cowork/internal/cli/serve.go:92-94`); whether a second cowork
  replica is safe is not answered. For the three `serve` commands the
  answer is [deployment-contract.md](./deployment-contract.md)
  §Not established.
- **What a process does against a database not migrated.** No start-time
  version check was found in `platform/internal` outside
  `cli/migrate.go`; cowork's and the policy service's were not searched
  for beyond the files cited.
- **What a process does when its database fails over or drops a
  connection.** Not read.
- **Worst-case time from start to ready**, for every process.
- **How the Next.js standalone server stops** on SIGTERM and how long it
  needs, for the console, the site and cowork-web; and what the web
  runtime writes to disk.
- **How long `policy serve` waits for work in flight on stop**, and its
  exit code.
- **A read-only root filesystem**, **resource requests and limits** and
  **a resource floor**, for any of the six: the answers in
  [deployment-contract.md](./deployment-contract.md) §Not established
  stand and nothing was measured for cowork, cowork-web or the policy
  service.
- **The PostgreSQL privileges** each migration and each serve process
  needs beyond creating and altering its own objects.
- **Whether the user-content origin is a separate public name**
  beyond the listener and the origin check the source shows, and how it
  relates to `COWORK_WEB_ORIGINS`. The source reads as a name apart
  from cowork's host; the documents read for this entry do not say it
  is required.
- **Where codehub runs**, beside garam or apart from it, and the
  address it reaches each of the three places in §codehub as a caller
  by. codehub is a caller the documents establish; its place is the
  deployment's. Its adapter, outbox and runner are unbuilt
  ([machine-identity.md](./machine-identity.md) §codehub's principal),
  so no setting of codehub's own is read here.
- **The file mode of the CA files** (`GARAM_POLICY_CA_FILE`,
  `GARAM_TRANSIT_CA_FILE`, `COWORK_POLICY_CA_FILE`): none enforced in
  what was read.
- **Rotation of `COWORK_POLICY_CA_FILE`, `GARAM_POLICY_CA_FILE` and the
  policy service's pair** is a restart, from the reads above; whether
  anything reloads them was not searched for.
