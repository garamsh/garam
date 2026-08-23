# Deployment Contract

The contract ADR-0020 obliges garam to publish: the ports each process listens on, the paths a probe may reach, every configuration value and whether it is secret, and how key material must arrive.

## Contents
- What it is
- Images
- Ports and probe paths
- What readiness reports
- Configuration values
- How key material must arrive
- The key-encryption key cannot be regenerated
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

Two images, because there are two runtimes ([ADR-0020](./adr/0020-published-artifacts.md)). Their tags are immutable, and [ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md) fixes where they are published and under what names:

| Image | Published as |
|---|---|
| Go | `486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/garam-server:<version>` |
| Web | `486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/garam-web:<version>` |

`<version>` is a released git tag with its `v` dropped — `v0.3.0` in git
is `0.3.0` here — and one commit publishes both images under the same
one, so a manifest pins one version across the pair. What a manifest
pins is that tag rather than a digest: the tags are immutable, so the
tag answers what is deployed on its own. Each image's digest is
recorded beside its tag in the GitHub release for that version, for a
consumer that would rather pin one.

**Both repositories exist and neither holds an image.** Read on
2026-08-24: `aws ecr describe-repositories --region ap-northeast-2`
returns nine repositories, `garam/garam-server` and `garam/garam-web`
among them, both `IMMUTABLE`, beside `gagent`'s five and the bring-up
pair named below; `aws ecr list-images --repository-name` returns an
empty list for each of the two names above; no workflow pushes to a
registry (`.github/workflows/ci.yml`); `gh release list --repo
garamsh/garam` returns nothing. So a manifest may name either
reference above and still pull nothing today: what is missing is the
tag, not the repository, and the first release is what supplies it.

No pull secret is needed for either repository: the cluster pulls with
a role granted `ecr:BatchGetImage` on every repository in the account,
assumed by the kubelet's credential provider (`garamsh/infra`
`modules/aws/ecr-pull.tf`, `modules/cluster/images.tf`, read
2026-08-24).

- **Go image** (`deploy/local/Dockerfile`) — one binary whose entry
  point takes a subcommand: `serve api`, `serve authz`, `serve machine`,
  `migrate`, `probe`, `adapter` (`ENTRYPOINT ["garam"]`). The last of
  those is the only one a cluster this repository does not describe
  runs: `adapter` is the delivery loop that goes beside one agent, in
  that agent's pod ([adapter.md](./adapter.md)), so this image is pulled
  there as well as here. The runtime stage is
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

**The two references in the table are the whole of what this contract
publishes.** The bring-up pair named next is not part of it: a manifest
this document owes accuracy to is written against the table and never
against those two names. Until a release exists, images reach a
development cluster by hand through `garam/garam-server-dev` and
`garam/garam-web-dev`
([ADR-0041](./adr/0041-a-moving-tag-carries-the-bring-up-images-and-every-consumer-pulls-with-always.md)).
Each push moves the tag `dev` onto the bytes just built and pushes a
second tag beside it on the same bytes: the first twelve characters of
the commit the build came from. That commit tag records what was built
and promises nothing about fetching the commit it names, and ADR-0041
makes both repositories mutable, so no tag in either is evidence of the
bytes behind it.

**A manifest that pins either bring-up reference sets
`imagePullPolicy: Always` on every container that names it.** The `dev`
tag moves, and a container spec that leaves the field unset takes
`IfNotPresent` for any tag but `:latest`: a node already holding
`garam/garam-web-dev:dev` then keeps running the bytes it has while the
registry holds newer ones, no manifest changes, and the divergence
surfaces as one node behaving unlike another. `Always` is what makes a
moving tag safe to pin, and it is owed on both bring-up references —
the commit tag included, because a mutable repository can move that one
too. The two published references above do not carry the obligation:
their tags are immutable, so the default policy is safe there.

Nothing else is promised about the bring-up pair: no version means
anything there, no digest is recorded anywhere, and both repositories
are emptied and removed once the release path publishes its first
image, which stops every reference into them resolving. A manifest that
pins one is deploying a bring-up build knowingly and for that long.
Both exist and neither is empty: read on 2026-08-24, `aws ecr
describe-repositories --region ap-northeast-2` returns both with
scan-on-push, and each has held an image since that date. Which tags
either holds is read from `aws ecr list-images` and not from here: a
bring-up tag is recorded nowhere, so a sentence naming one would be
stale at the next push. Both read `IMMUTABLE` on that date;
`garamsh/infra` owns `image_tag_mutability` and makes them mutable
against ADR-0041, and `garamsh/infra` #128 is the request they came
from.

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
| `issue-certificate` | none | — | — | the one bootstrap [ADR-0015](./adr/0015-operator-identity.md) leaves out of band: mints an operator's own first certificate, for operator subjects only, and exits. No serve path reaches it, so no serving process can mint an identity |
| `adapter` | none | — | — | nothing: it dials out and listens on no port. It runs beside one agent in that agent's pod, claims that agent's messages from the machine listener above, and posts them into the agent platform's gateway over loopback ([adapter.md](./adapter.md), [ADR-0013](./adr/0013-agent-messaging.md)). It answers no probe — see §Not established |

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
`internal/cli/machine.go`, `internal/cli/migrate.go`,
`internal/cli/issue_certificate.go`) and, for the web
app, validated at boot (`web/src/env.ts`). Nothing watches them, so a
changed value takes effect on restart — a deployment that wants a
configuration change applied rolls the pods.

| Name | Read by | Default | Secret |
|---|---|---|---|
| `GARAM_DATABASE_URL` | api, authz, machine, migrate, issue-certificate | none — required, startup refuses without it | **yes** — carries a password |
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
| `GARAM_MACHINE_TLS_KEY_FILE` | machine | none — required | **yes**, and it names a file: the key itself must never be an environment variable's value ([ADR-0015](./adr/0015-operator-identity.md)), and the file is subject to the arrival rule below |
| `GARAM_CA_KEY_FILE` | machine, issue-certificate | none — required | **yes**, and it names a file, subject to the arrival rule below — and the key it names cannot be regenerated (§The key-encryption key cannot be regenerated) |
| `GARAM_MIGRATIONS_DIR` | migrate | `migrations` (`/app/migrations` in the image) | no |
| `GOOGLE_CLIENT_ID` | api | none | no |
| `GOOGLE_CLIENT_SECRET` | api | none | **yes** |
| `GOOGLE_REDIRECT_URL` | api | none | no |
| `GARAM_ADAPTER_AGENT` | adapter | none — required | no |
| `GARAM_ADAPTER_MACHINE_URL` | adapter | none — required | no |
| `GARAM_ADAPTER_GATEWAY_URL` | adapter | `http://127.0.0.1:8080` — the loopback address the gateway binds unless mTLS is configured for it ([ADR-0012](./adr/0012-agent-operator-model.md)) | no |
| `GARAM_ADAPTER_GATEWAY_AGENT` | adapter | none — required | no |
| `GARAM_ADAPTER_TLS_CERT_FILE` | adapter | none — required | no — it names a file holding a certificate, which is public |
| `GARAM_ADAPTER_TLS_KEY_FILE` | adapter | none — required | **yes**, and it names a file, subject to the arrival rule below |
| `GARAM_ADAPTER_ISSUER_FILE` | adapter | none — required | no — it names a file holding the issuing authority's certificate |
| `GARAM_API_URL` | web | none in production (`web/src/env.ts`); baked into the image at build time (`deploy/local/Dockerfile.web`), so the runtime value must match the build's | no |

Notes a deployment needs:

- `serve api` performs Google OIDC discovery at startup only when both
  `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set
  (`internal/cli/serve.go`, `internal/authn/google.go`): then the start
  depends on reaching the issuer, and an unreachable one stops the
  start. With neither set, no Google provider is registered — the start
  owes the issuer nothing, and `GET /auth/providers` omits `google`.
  One credential without the other refuses the start.
- The adapter is given the agent's own identity: the certificate
  `issueAgentCertificate` returned, its private key, and the authority
  that signed it. That authority is what garam's own listener is
  verified against, because the listener presents a certificate of the
  same per-organization authority
  ([ADR-0037](./adr/0037-the-machine-listener-presents-a-per-organization-certificate.md)),
  so the three files arrive together and rotate together — reissue and
  restart, with no reload ([ADR-0014](./adr/0014-agent-authentication.md)).
  `GARAM_ADAPTER_AGENT` is garam's GRN for the agent and
  `GARAM_ADAPTER_GATEWAY_AGENT` is what the agent platform serves it
  under; they are two names for one agent and neither can be derived
  from the other.
- The `*_FILE` values are paths, not contents. Key material
  travels as a file, never through the environment — a value there is
  inherited by child processes, readable at `/proc/<pid>/environ`, and
  captured in crash dumps ([ADR-0015](./adr/0015-operator-identity.md)).

## How key material must arrive

The rule is [ADR-0015](./adr/0015-operator-identity.md)'s and is not a
preference: key material reaches a process as a file readable only by
its owner, with the reading process running as that owner.

It is enforced, not requested. `internal/keyfile` refuses a key file
when any group or other permission bit is set, and separately when its
owner is not the process's own UID, and every key file a process reads
at startup goes through it: the key-encryption key and the machine
listener's own TLS key alike, so either refusal stops the start. Both
halves matter for a deployment, because plain Kubernetes secret
projection satisfies neither:

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

- The startup enforcement applies to `GARAM_CA_KEY_FILE` and to
  `GARAM_ADAPTER_TLS_KEY_FILE`. The machine listener's own TLS key has
  no such check in the source; the same arrival shape covers it, and the
  local manifest copies all three files the same way.
- **There is no client CA bundle, and creating one is forbidden.** The
  machine listener requires a certificate and verifies it per connection
  against the certificate authority of the organization the
  certificate's own SAN names, read from the database at handshake time
  (`internal/machine/listener.go`). A pool holding every organization's
  authority would accept a certificate against a root that is not its
  own — the tenancy hole [ADR-0020](./adr/0020-published-artifacts.md)
  forbids. There is nothing to mount and nothing to rotate; only the
  server certificate is mounted.

## The key-encryption key cannot be regenerated

**The key-encryption key — the bytes in the file `GARAM_CA_KEY_FILE`
names, not the path — cannot be regenerated.** Every organization's
certificate authority is sealed under the key present when that
authority was created, and the sealed bytes open only under that same
key: a different one is refused by the identifier of the key it needs
rather than read as corruption (`internal/ca/service.go`,
`internal/ca/sealer.go`). Rotation is not built and not designed — the
identifier is what would make it possible later, and nothing performs
it ([ADR-0016](./adr/0016-ca-key-custody.md)) — so there is no path
from one key to another. Under a different key every authority sealed
under the old one is unopenable and no certificate can be signed or
renewed for its organization (`internal/ca/service.go`); the only
remedy is a new authority per organization and a reissue of every
certificate under it.

Nothing detects the substitution at start: the arrival checks above
pass on any well-formed key, and nothing compares it against what the
database already holds (`internal/cli/machine.go`). The wrong key
starts cleanly and fails at the first certificate asked for.

The obligation that follows, in the form a deployment can act on:

- **Created once, never replaced.** A re-run of whatever supplies the
  key must be a no-op that keeps what already exists, never a
  regeneration.
- **It must survive the cluster.** A key that exists only as something
  the cluster generated is lost with the cluster, and every
  organization's certificate authority is lost with it.
- **Where it lives is the cluster owner's decision.** garam requires
  the two properties above and names no store; choosing one is not
  this contract's to do.

**The machine listener's certificate and key do not carry this
property.** `GARAM_MACHINE_TLS_CERT_FILE` and
`GARAM_MACHINE_TLS_KEY_FILE` — `tls.crt` and `tls.key` where the local
manifest names them (`deploy/local/minikube/garam.yaml`) — seal
nothing: they are the listener's own pair, read for the TLS handshake
and nothing else (`internal/cli/machine.go`,
`internal/machine/listener.go`). The pair is safe to regenerate at any
time and safe to have issued by cert-manager or any other issuer. The
constraint above is the key-encryption key's alone.

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
`internal/cli/machine.go`), and so does `adapter`
(`internal/cli/delivery.go`), which stops claiming and exits 0. What it
was carrying goes with it: the pairings it held reach no disk, their
leases lapse, and garam hands those messages out again
([ADR-0018](./adr/0018-delivery-acknowledgement.md)). That is why it has
to outlive the agent beside it — a native sidecar rather than an
ordinary container — and why its own termination grace period is budget
on top of the agent's drain rather than shared with it. When it fires, the listener closes at once,
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
- **What `/api/*` points at in a published web image.** Not established.
  The rewrite target is a build argument baked into the image (`ARG
  GARAM_API_URL` in `deploy/local/Dockerfile.web`, read by
  `web/next.config.ts`), and no release exists to have chosen a value,
  so a consumer cannot assume a published image carries the local
  default.
- **Whether the adapter answers a probe.** It answers none: it has no
  listener, and [ADR-0021](./adr/0021-liveness-and-readiness.md) decides
  the probes for the three serve processes rather than for a process
  that only dials out. Whether a sidecar beside an agent owes one, and
  what it would report, is undecided.
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
- [ADR-0013](./adr/0013-agent-messaging.md) — the adapter garam writes
  and an orchestrator deploys, beside the agent it serves
- [ADR-0014](./adr/0014-agent-authentication.md) — mTLS on a listener of
  its own, per-connection trust root
- [ADR-0015](./adr/0015-operator-identity.md) — key material travels as
  files, never the environment
- [ADR-0016](./adr/0016-ca-key-custody.md) — the key-encryption key the
  authorities are sealed under
- [ADR-0017](./adr/0017-machine-process.md) — three serve processes from
  one image
- [ADR-0018](./adr/0018-delivery-acknowledgement.md) — the adapter
  completes on the turn-end event and holds its pairing in memory
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
- [ADR-0037](./adr/0037-the-machine-listener-presents-a-per-organization-certificate.md)
  — the listener presents its organization's own certificate, which is
  what an agent verifies it against
- [ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  — where the two images are published, under what names, and what
  triggers a push
- [ADR-0041](./adr/0041-a-moving-tag-carries-the-bring-up-images-and-every-consumer-pulls-with-always.md)
  — the mutable bring-up pair that carries images until the first
  release, the moving tag it carries them under, and the pull policy a
  consumer of one owes
