# Deployment Contract

The contract ADR-0020 obliges garam to publish: the ports each process listens on, the paths a probe may reach, every configuration value and whether it is secret, and how key material must arrive.

## Contents
- What it is
- Images
- Ports and probe paths
- What readiness reports
- Configuration values
- How key material must arrive
- The database connection
- The migrate job
- Termination
- Not established
- Decisions this rests on

## What it is

A repository outside this one — the repository that owns a cluster and
reconciles it — is written against this document, so a change that
falsifies it breaks someone else's deployment silently, which is the
consequence [ADR-0020](./adr/0020-published-artifacts.md) accepted. It
records what is; changing any port, path, or configuration name in it is
a decision, not an edit here.

It consolidates the contract handover the consuming repository recorded
as its issue #3 (`garamsh/gitops`): the three listeners, the machine
listener's TLS passthrough, `serve authz` plaintext and cluster-internal,
the `/api/*` same-origin rewrite, `migrate` as a PreSync-suited job, and
the client-CA prohibition all originate there and are restated here
against the source they are checkable in. That issue remains the
consumer's record of its own decisions; this is the artifact it cites
rather than copies.

Every claim below names the path it can be checked against, per
`docs/convention/documentation.md` §Keep it current. Where the source
answers a deployment question with nothing checkable, §Not established
says so rather than guessing.

## Images

Two images, because there are two runtimes ([ADR-0020](./adr/0020-published-artifacts.md)). Tags are immutable.

- **Go image** (`deploy/local/Dockerfile`) — one binary whose entry
  point takes a subcommand: `serve api`, `serve authz`, `serve machine`,
  `migrate`, `probe` (`ENTRYPOINT ["garam"]`). The runtime stage is
  `scratch`: the image carries no shell and no package manager, so the
  local stack's healthchecks run the binary's own `probe` subcommand
  (`deploy/local/docker-compose.yml`) — a kubelet's `httpGet` needs
  nothing in the image either way. The trust store the api's Google
  OIDC discovery needs is copied from the build stage
  (`COPY --from=build /etc/ssl/certs/ca-certificates.crt`); nothing in
  the module names a time zone, so no zoneinfo ships. Runs as UID 10001
  — `scratch` has no `adduser`, so the build stage creates the account
  and only its `/etc/passwd` and `/etc/group` lines are copied in for
  `USER garam` to resolve against; the ownership check
  (§How key material must arrive) reads the numeric UID, which is
  unchanged. The migrations ship inside it at `/app/migrations`
  (`COPY migrations /app/migrations`, `WORKDIR /app`), which is where
  the default `GARAM_MIGRATIONS_DIR` resolves.
- **Web image** (`deploy/local/Dockerfile.web`) — the Next.js
  application as a standalone server (`CMD ["node", "server.js"]`),
  `USER node`. The `node` base image assigns that user UID 1000; this
  is read from the Dockerfile and has not been verified against a
  running pod. Listens on port 3000 (`ENV ... PORT=3000`). The `/api/*`
  rewrite target is baked into the build (`ARG GARAM_API_URL`), so the
  runtime value must match the one the build used.

## Ports and probe paths

| Process | Listener | Set by | Default | Serves |
|---|---|---|---|---|
| `serve api` | HTTP | `GARAM_HTTP_ADDR` | `:8080` | the OpenAPI API (`api/`), plus `/metrics`, `/healthz`, `/readyz` with no credential (`internal/cli/serve.go`) |
| `serve authz` | gRPC, plaintext | `GARAM_GRPC_ADDR` | `:9090` | the engine's RPCs only (`proto/garam/authz/v1`); cluster-internal by design ([ADR-0003](./adr/0003-single-cli-topology.md)) |
| `serve authz` | HTTP | `GARAM_METRICS_ADDR` | `:9091` | `/metrics`, `/healthz`, `/readyz` — the engine speaks gRPC only, so its probes and metrics share a listener of their own ([ADR-0021](./adr/0021-liveness-and-readiness.md)) |
| `serve machine` | HTTPS, mTLS | `GARAM_MACHINE_ADDR` | `:8443` | the machine API (`api/machine.yaml`, [ADR-0019](./adr/0019-machine-openapi-spec.md)); every caller presents a client certificate, TLS 1.3 only (`internal/machine/listener.go`) |
| `serve machine` | HTTP, plaintext | `GARAM_MACHINE_HEALTH_ADDR` | `:8444` | `/healthz` and `/readyz` only — the mTLS listener admits no probe, so this process gets a port for them ([ADR-0021](./adr/0021-liveness-and-readiness.md)) |
| web | HTTP | `PORT` | `3000` | the console, plus `/healthz` and `/readyz` |
| `migrate` | none | — | — | a job: applies and exits |

- **The probe paths are `/healthz` and `/readyz`, one spelling on all
  four runtimes** (`internal/health/health.go`; `web/src/app/healthz/route.ts`,
  `web/src/app/readyz/route.ts`). Liveness answers for the process alone
  and reads nothing outside it.
- **These ports and paths are interface, not implementation**
  ([ADR-0021](./adr/0021-liveness-and-readiness.md),
  [ADR-0023](./adr/0023-web-probes.md)): changing one supersedes the
  ADR, it is not an edit to a handler.
- The mTLS listener terminates TLS itself and authenticates the caller's
  certificate (`internal/machine/listener.go`), so it cannot sit behind
  a TLS-terminating proxy — that strips the certificate and every call
  fails authentication. It needs TLS passthrough or direct exposure, and
  it is the one listener machine identities reach from outside the
  cluster ([ADR-0012](./adr/0012-agent-operator-model.md),
  [ADR-0014](./adr/0014-agent-authentication.md)). The mTLS listener
  gains no unauthenticated route.
- Browsers reach the API same-origin through the web app's `/api/*`
  rewrite (`web/next.config.ts`), so the API needs no public hostname
  for the browser path ([ADR-0009](./adr/0009-web-frontend.md)).
- `/metrics` answers on the same unauthenticated listeners as the probes
  on `serve api` and `serve authz` (`internal/cli/serve.go`,
  `internal/cli/observability.go`); `serve machine` exposes no metrics
  listener (`internal/cli/machine.go`).

## What readiness reports

Dependencies each process reports (`internal/cli/serve.go`,
`internal/cli/machine.go`):

| Process | Readiness dependencies |
|---|---|
| `serve api` | the database, and the channel to the engine |
| `serve authz` | the database alone |
| `serve machine` | the database, and the channel to the engine |
| web | none — it reads nothing, so an API outage takes no replica out of rotation ([ADR-0023](./adr/0023-web-probes.md)) |

The semantics, fixed in `internal/health/health.go`:

- A single readiness answer is bounded at two seconds inside the
  process. That is fixed in the binary; a deployment cannot change it.
- Readiness fails whole as soon as one dependency fails, naming that
  dependency and nothing else. There is no partial readiness and no
  per-route readiness.

Two sentences a consumer must plan against:

- `serve api` readiness includes database reachability, so every replica
  leaves rotation together whenever the database is unreachable. Each
  answer is bounded at two seconds inside the process; how sustained the
  unreachability must be before withdrawal follows is set by the
  deploying platform's probe configuration, not by garam. The duration
  of that window is a property of the platform's database topology and
  maintenance procedure, not of garam.
- A replica whose policy-change listener is reconnecting does **not**
  leave rotation. Readiness deliberately does not report it
  ([ADR-0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md)).

garam takes no position on probe tolerance. The local manifest sets only
`timeoutSeconds: 3` on its readiness probes — because the handler bounds
one answer at two seconds — and nothing else
(`deploy/local/minikube/garam.yaml`). Any other value a live probe
object shows, `failureThreshold` and `periodSeconds` among them, is the
platform's default or the deployment's own choice, never a decision made
here.

## Configuration values

Configuration is entirely environment variables, read once at process
start in the composition roots (`internal/cli/serve.go`,
`internal/cli/machine.go`, `internal/cli/migrate.go`) and, for the web
app, validated at boot (`web/src/env.ts`). Nothing watches them, so a
changed value takes effect on restart — a deployment that wants a
configuration change applied rolls the pods.

| Name | Read by | Default | Secret |
|---|---|---|---|
| `GARAM_DATABASE_URL` | api, authz, machine, migrate | none — required, startup refuses without it | **yes** — carries a password |
| `GARAM_GRPC_ADDR` | authz | `:9090` | no |
| `GARAM_METRICS_ADDR` | authz | `:9091` | no |
| `GARAM_HTTP_ADDR` | api | `:8080` | no |
| `GARAM_AUTHZ_ADDR` | api, machine | `localhost:9090` | no |
| `GARAM_SESSION_TTL` | api | `168h` | no |
| `GARAM_COOKIE_SECURE` | api | `true` | no |
| `GARAM_DEV_AUTH` | api | `false` — development only ([ADR-0010](./adr/0010-dev-auth-provider.md)); requires `GARAM_COOKIE_SECURE=false` | no |
| `GARAM_DEV_REDIRECT_URL` | api | `http://localhost:3000/api/auth/callback/dev` | no |
| `GARAM_MACHINE_ADDR` | machine | `:8443` | no |
| `GARAM_MACHINE_HEALTH_ADDR` | machine | `:8444` | no |
| `GARAM_MACHINE_TLS_CERT_FILE` | machine | none — required | no — it names a file holding a certificate, which is public |
| `GARAM_MACHINE_TLS_KEY_FILE` | machine | none — required | **yes**, and it names a file: the key itself must never be an environment variable's value ([ADR-0015](./adr/0015-operator-identity.md)) |
| `GARAM_CA_KEY_FILE` | machine | none — required | **yes**, and it names a file, subject to the arrival rule below |
| `GARAM_MIGRATIONS_DIR` | migrate | `migrations` (`/app/migrations` in the image) | no |
| `GOOGLE_CLIENT_ID` | api | none | no |
| `GOOGLE_CLIENT_SECRET` | api | none | **yes** |
| `GOOGLE_REDIRECT_URL` | api | none | no |
| `GARAM_API_URL` | web | none in production (`web/src/env.ts`); baked into the image at build time (`deploy/local/Dockerfile.web`), so the runtime value must match the build's | no |

Notes a deployment needs:

- `serve api` performs Google OIDC discovery at startup only when both
  `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set
  (`internal/cli/serve.go`, `internal/authn/google.go`): then the start
  depends on reaching the issuer, and an unreachable one stops the
  start. With neither set, no Google provider is registered — the start
  owes the issuer nothing, and `GET /auth/providers` omits `google`.
  One credential without the other refuses the start.
- The three `*_FILE` values are paths, not contents. Key material
  travels as a file, never through the environment — a value there is
  inherited by child processes, readable at `/proc/<pid>/environ`, and
  captured in crash dumps ([ADR-0015](./adr/0015-operator-identity.md)).

## How key material must arrive

The rule is [ADR-0015](./adr/0015-operator-identity.md)'s and is not a
preference: key material reaches a process as a file readable only by
its owner, with the reading process running as that owner.

It is enforced, not requested. At startup `internal/ca/sealer.go`
refuses the key-encryption key file when any group or other permission
bit is set, and separately when its owner is not the process's own UID;
`internal/cli/machine.go` refuses the start on either. Both halves
matter for a deployment, because plain Kubernetes secret projection
satisfies neither:

- A projected secret is owned by root, and `fsGroup` changes the group
  owner, not the user owner. `defaultMode: 0440` with `fsGroup` trips
  the permission check.
- `defaultMode: 0400` root-owned trips the ownership check — and in
  practice the process cannot even open the file.

The shape that satisfies it is already working in this repository, on
the `machine` Deployment of `deploy/local/minikube/garam.yaml`: an
initContainer running as the same UID — stated as `runAsUser: 10001`,
because the Go image carries no shell for the copy — copies the
projected secret with `install -m 0600` onto a memory-backed
`emptyDir`, with `fsGroup` set so that user can write the volume. The
process then opens a file it owns with no group or other bits, and both
checks pass. Relaxing the check to fit plain projection is not an
option; the deployment shape above is what the rule requires.

Two qualifications:

- The startup enforcement applies to `GARAM_CA_KEY_FILE`. The TLS key
  has no such check in the source; the same arrival shape covers it, and
  the local manifest copies all three files the same way.
- **There is no client CA bundle, and creating one is forbidden.** The
  machine listener requires a certificate and verifies it per connection
  against the certificate authority of the organization the
  certificate's own SAN names, read from the database at handshake time
  (`internal/machine/listener.go`). A pool holding every organization's
  authority would accept a certificate against a root that is not its
  own — the tenancy hole [ADR-0020](./adr/0020-published-artifacts.md)
  forbids. There is nothing to mount and nothing to rotate; only the
  server certificate is mounted.

## The database connection

One DSN in `GARAM_DATABASE_URL`, consumed as-is by every process that
opens the database (`internal/cli/`); nothing reassembles host, port,
user, and database. The local manifest bridges CloudNativePG's published
`uri` key directly — `secretKeyRef: {name: postgres-app, key: uri}` —
and a deployment whose database publishes the same shape can do the same
(`deploy/local/minikube/garam.yaml`).

## The migrate job

`migrate` applies all pending migrations and exits
(`internal/cli/migrate.go`); it suits a PreSync-style hook and listens
on nothing. The local cluster runs it as a Job that completes before the
serve Deployments start (`deploy/local/minikube/migrate-job.yaml`).

- **It fails closed on a dirty database and never clears the flag
  itself.** A failed run leaves the database dirty, and every later run
  refuses with a message naming the recovery — read the failed run's
  log, fix the migration, force the version, run again
  (`internal/cli/migrate.go`). The real error exists only in the first
  run's log, so a hook whose retry deletes the previous Job destroys the
  evidence.
- Every migration this project ships is applied inside a transaction and
  leaves nothing behind when it fails (`internal/cli/migrate.go`); one
  that cannot run transactionally would break that guarantee.
- **Concurrent runs are safe** by the driver's mechanism:
  golang-migrate's Postgres driver takes an advisory lock, so a second
  `migrate` waits and then finds nothing to do. That claim holds at
  `github.com/golang-migrate/migrate/v4` v4.19.1 (`go.mod`) and is
  checkable against the driver it pins.

## Termination

Every serve command installs a SIGTERM and SIGINT handler
(`internal/cli/shutdown.go`, wired into `internal/cli/serve.go` and
`internal/cli/machine.go`). When it fires, the listener closes at once,
in-flight requests and RPCs finish within a ten-second bound, and past
it they are abandoned deliberately by the process rather than by the
kubelet's SIGKILL. The process then exits 0: a requested shutdown is not
an error, so a pod a rollout or a delete terminates records `Completed`,
not `Error` (#338 settled this; the assertion is
`tests/e2e/shutdown_test.go`, which SIGTERMs the built binary as each of
the three serve processes and requires exit 0).

The bound is the binary's, not the deployment's (`shutdownGrace` in
`internal/cli/shutdown.go`): a `terminationGracePeriodSeconds` below ten
seconds truncates in-flight work, and nothing the process does at
shutdown outlasts it.

## Not established

Answers the consumer asked for that the source does not establish.
Stated here so they are not guessed:

- **Read-only root filesystem.** Not established for either image. What
  is checkable: no code under `internal/` or `cmd/` writes a file. What
  the web runtime writes, if anything, is unverified — so the
  `securityContext` field is the deployment's to set either way.
- **Replica safety above one.** Not established for `serve api`,
  `serve authz`, or `serve machine`. No leader election exists anywhere
  in the source; whether that makes a second replica safe or unsafe is
  not answered here. (`migrate` is answered above.)
- **Worst-case time from start to `/readyz` answering ok.** Not
  established; nothing in the source bounds startup. A slow start
  reading as a failed deploy is a function of the deployment's probe
  configuration.
- **A resource floor.** No minimum below which a process fails is known.

## Decisions this rests on

- [ADR-0003](./adr/0003-single-cli-topology.md) — one binary, the engine
  reached over plaintext gRPC inside the trust boundary
- [ADR-0009](./adr/0009-web-frontend.md) — the web app and its `/api/*`
  rewrite
- [ADR-0010](./adr/0010-dev-auth-provider.md) — the development-only
  provider behind `GARAM_DEV_AUTH`
- [ADR-0012](./adr/0012-agent-operator-model.md) — operators garam never
  dials, so the machine listener is reached from outside
- [ADR-0014](./adr/0014-agent-authentication.md) — mTLS on a listener of
  its own, per-connection trust root
- [ADR-0015](./adr/0015-operator-identity.md) — key material travels as
  files, never the environment
- [ADR-0016](./adr/0016-ca-key-custody.md) — the key-encryption key the
  authorities are sealed under
- [ADR-0017](./adr/0017-machine-process.md) — three serve processes from
  one image
- [ADR-0019](./adr/0019-machine-openapi-spec.md) — the machine
  listener's own specification
- [ADR-0020](./adr/0020-published-artifacts.md) — images and this
  contract, not manifests; immutable tags
- [ADR-0021](./adr/0021-liveness-and-readiness.md) — the two probes on a
  credential-free port
- [ADR-0023](./adr/0023-web-probes.md) — the web app answers the same
  two
- [ADR-0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md)
  — readiness reports what a replica cannot serve without
