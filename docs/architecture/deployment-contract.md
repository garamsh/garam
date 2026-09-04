# Deployment Contract

What a garam deployment must plan for that no manifest can carry: what readiness reports, what bounds termination, how the migration fails, what the binary enforces about key material, and what is not known.

## Contents
- What it is
- What readiness reports
- What the binary enforces about key material
- The key-encryption key cannot be regenerated
- The migrate job
- Termination
- The PodSecurity level is the deployment's
- Not established
- Decisions this rests on

## What it is

The chart at `deploy/charts/garam/` is the description of a garam
deployment ([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)).
It is authoritative for everything a deployment *sets* — the images, the
ports, every configuration value, the mounts, and the shape key material
arrives in — and `make chart` checks it against the source rather than
against a reader's attention.

**This document is authoritative for what the binary does with what the
chart sets, and for what is not known.** Where the two could both answer
a question, the chart answers it. Nothing here restates a value the
chart carries: the ports table, the configuration table, the image
references and the database connection each moved into
`deploy/charts/garam/values.yaml` and the templates beside it, because
two descriptions of one deployment is the defect ADR-0042 exists to
remove.

Where the chart answers, this document names what it answers for rather
than leaving a boundary that says only what is absent here
([ADR-0058](./adr/0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)).
`deploy/charts/garam/README.md` §Pinning a version is authoritative for
what `Chart.yaml`'s `version` names and when it moves (ADR-0042,
[ADR-0057](./adr/0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md)) —
a rule no responsibility document held or reached before this route.
The same file's §What it renders is authoritative for what each
template renders and for what follows from that — the update strategy
every Deployment sets and what an upgrade costs in service under it,
and the hook phase the migration Job runs in.

It records what is. Changing any behaviour named below is a decision,
not an edit here. Every claim names the path it can be checked against,
per `docs/convention/documentation.md` §Keep it current; where the
source answers a deployment question with nothing checkable, §Not
established says so rather than guessing.

The client-CA prohibition below originates in the consuming repository's
own record (`garamsh/gitops` issue #3), restated here against the source
it is checkable in. That repository had not agreed to consume the chart
when it was read on 2026-08-25, and ADR-0042 assumes nothing about
whether it will.

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
- Liveness answers for the process alone and reads nothing outside it.

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

garam takes one position on probe tolerance and no other. The chart
sets `timeoutSeconds: 3` on the readiness probe of `api`, `authz` and
`machine`, because a readiness answer is bounded at two seconds and a
probe timing out at the platform's one-second default would report its
own deadline instead of the dependency that failed. That value is
literal in those templates — no chart value moves it — and `web`'s
readiness probe carries none of it, so that one workload takes the
platform's default. Every other probe field —
`failureThreshold` and `periodSeconds` among them — is the platform's
default or the deployment's own choice, never a decision made here.

## What the binary enforces about key material

The rule is [ADR-0015](./adr/0015-operator-identity.md)'s and is not a
preference: key material reaches a process as a file readable only by
its owner, with the reading process running as that owner.

**Which owner that is, the Pod decides.** `runAsUser` sets the UID the
process runs as, and the image's `USER` does not decide it: garam's
image declares `USER garam` (uid 10001, `deploy/local/Dockerfile`),
which is a default a Pod overrides rather than a value a key file must
be owned by. #644 measured it: `garam adapter` ran as 65532, an agent
operator's user and not garam's own, against a key file owned by 65532,
and `internal/keyfile` accepted it — what the rule constrains is that
the two agree, not that either is garam's.

**It is enforced, not requested.** `internal/keyfile` refuses a key file
when any group or other permission bit is set, and separately when its
owner is not the process's own UID, and every key file a process reads
at startup goes through it: the key-encryption key, the machine
listener's own TLS key (`internal/cli/machine.go`) and the adapter's
(`internal/cli/delivery.go`) alike, so either refusal stops the start of
that process. Plain Kubernetes Secret projection satisfies neither half
— a projected file is root-owned, and `fsGroup` changes the group owner
rather than the user owner (read 2026-08-25; this repository pins no
Kubernetes version). Relaxing the check to fit plain projection is not
an option; the arrival shape the chart renders is what the rule
requires.

Four qualifications:

- **Each `*_TLS_CERT_FILE` and `*_TLS_KEY_FILE` pair is re-read on every
  handshake**, on the machine listener and in the adapter alike
  (`internal/keyfile/pair.go`), so a replaced pair is presented on the
  next connection with nothing restarted
  ([ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)).
  Two obligations follow, and a deployment that meets neither has
  rotation only on a restart:
  - **Replace the pair atomically.** A reader that opens a half-written
    file refuses the handshake. Writing beside the file and renaming
    over it leaves no such moment; writing in place does.
  - **Keep the file the process opens current.** The arrival shape
    copies the projected Secret once, at pod start, so a pair replaced
    afterwards reaches the projected volume and not the copy the process
    reads. Whatever refreshes that copy must satisfy the arrival rule
    for the key on every refresh, not only the first.
- **`GARAM_CERTIFICATE_LIFETIME` is a security parameter.** It bounds
  every certificate garam signs — an agent's and an operator's, not the
  listener's own pair, which garam does not issue
  (`internal/ca/service.go`). Expiry is the only revocation garam has —
  no CRL, no responder
  ([ADR-0014](./adr/0014-agent-authentication.md)) — so the value is the
  window a stolen certificate stays usable in. `internal/ca/service.go`
  bounds it at one hour and thirty days and refuses the start outside
  them. The default is deliberately loose while rotation is new
  ([ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md));
  lowering it is safe only where the two obligations above are met on
  the files each renewed certificate is written to, because an identity
  whose files are replaced in place or left stale stops connecting
  sooner the lower it goes.
- **An operator renews its own certificate, so a deployment must not
  build a job that rotates one.** It presents the certificate it holds
  to `POST /identity/certificate` on the machine listener and receives
  the next one
  ([ADR-0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md)).
  Three consequences a deployment acts on:
  - **Ask inside the window.** A renewal is admitted once two thirds of
    the certificate's validity has passed, and refused before that.
    Whatever asks has to run often enough to meet that window — at the
    default lifetime it is about eight hours wide — and to write what
    comes back to the files the process reads, under the two
    obligations above.
  - **Only the newest certificate renews.** garam records the
    certificate it last issued an operator and renews that one alone, so
    an operator identity is held by one process: two sharing one
    certificate will both renew and one will be refused from then on.
  - **Re-running `issue-certificate` is the recovery**, and it is the
    same act as the bootstrap. It takes the lineage from every
    certificate that already names the operator, which is what recovers
    from a renewal answer that never arrived. It revokes nothing: a
    certificate that can no longer renew keeps authenticating until it
    expires, because expiry is still the only revocation there is.
- **There is no client CA bundle, and creating one is forbidden.** The
  machine listener asks for a certificate and verifies the one it is
  given, per connection, against the certificate authority of the
  organization the certificate's own SAN names, read from the database
  at handshake time (`internal/machine/listener.go`). A pool holding
  every organization's authority would accept a certificate against a
  root that is not its own — the tenancy hole
  [ADR-0014](./adr/0014-agent-authentication.md) rests on the trust
  root to close. There is nothing to mount and nothing to rotate for
  it: what the workload mounts is its own certificate and key, the
  garam server root `GARAM_MACHINE_SERVER_ROOT_FILE` names, and the
  key-encryption key — four files, and none of them a bundle of client
  authorities.

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
- **Whatever reads it into the cluster grants to the machine
  listener's own subject.** Scope that grant to the account
  `serve machine` runs as, never to one every pod in the namespace
  presents. The chart names that account from
  `machine.serviceAccount.name` and composes nothing, so the name the
  grant is written against and the name the deployment sets are one name
  it holds in both places
  ([ADR-0043](./adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)).
  The arrival checks above read a file's owner and its mode; which
  workload opened it is not something they can tell.

**Part of that obligation has been exercised, once.** A drill on
2026-08-25 dumped one organization's `certificate_authorities` row from
`garam-dev` and restored it into a scratch database: the restored row's
`certificate` and `sealed_key` were byte-identical to the live row's,
and `issue-certificate` run against the copy issued an operator
certificate (recorded on #486). That the command read the restored row
rather than the live one was shown and not assumed — with the copy's
sealed key overwritten the same command refused, naming the key the
corrupted row then needed, while the live row was unchanged throughout.
So a sealed authority that has been through a dump and a restore opens
under the same key and still signs. That is one leg of a restore and
not a restore of a deployment; it says nothing about recovering one.
Four limits are the distance between the two:

- **The key was never restored.** It was present throughout, arriving
  as it always does. The half whose loss has no remedy is the half the
  drill left alone.
- **The live authority was never touched.** The drill ran against an
  isolated copy of one row, and the overwrite above was applied to that
  copy alone. The record names the copy as a second database and does
  not say whether it sat on separate infrastructure.
- **Nothing takes the dump.** It was taken by hand for the drill, so
  the state was shown to survive a restore rather than shown to be
  backed up, and what a real restore would draw on does not exist yet.
- **One authority, one cluster, one key.** Whether a restore holds
  across a key rotation is untested, and untestable while rotation is
  neither built nor designed.

**The machine listener's certificate and key do not carry this
property, and they are not free of obligations either.**
`GARAM_MACHINE_TLS_CERT_FILE` and `GARAM_MACHINE_TLS_KEY_FILE` — the
pair `machine.keyMaterial` names in the chart — seal nothing: they are
the listener's own pair, read for the TLS handshake and nothing else
(`internal/cli/machine.go`, `internal/machine/listener.go`). Replacing
them loses nothing, and the constraint above is the key-encryption key's
alone. What binds instead is who signs them
([ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)):

- **One certificate authority signs that pair and signs nothing else.**
  It is separate from every organization's authority, and garam never
  holds its key — the process holds the pair it signed and its
  certificate, and no more. cert-manager issuing from an issuer
  dedicated to this listener satisfies this; an issuer that also signs
  other workloads does not, because what it signs is what every agent
  trusts garam by.
- **That authority's certificate is `GARAM_MACHINE_SERVER_ROOT_FILE`,
  and garam hands it to every identity it issues a certificate to**
  (`serverRootPem` in `api/machine.yaml`). It is public, it is read once
  at startup, and it must be the root the pair above chains to — a
  mismatch reaches every agent and works for none. A deployment writing
  an adapter's files takes that same `serverRootPem` for
  `GARAM_ADAPTER_SERVER_ROOT_FILE` and not `issuerPem`, which is the
  authority that signed the agent rather than the one the agent verifies
  garam by ([adapter.md](./adapter.md)).
- **Replacing that authority invalidates what every agent already
  holds**, and garam reads it once, so it is a restart plus a reissue
  rather than a rotation. Nothing in garam schedules it; it should
  outlive the certificates it signs by years.
- **An enrolling operator needs that root before it calls, and it is
  the only file it needs.** Registering the operator answers a one-time
  token, the operator generates its own key pair in its own cluster and
  presents the token with a certificate signing request, and garam signs
  ([ADR-0064](./adr/0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)).
  The root is what the operator verifies garam by on that call, so it
  reaches the operator's cluster ahead of the token — one public file
  per deployment rather than one per operator. **An enrollment made
  without verifying garam against it hands the token to whoever
  answered.** No private key travels on this path at all: garam never
  holds one, and a deployment has nothing to write.
- **An operator bootstrapped by `issue-certificate` instead needs that
  root too, and the command does not write it**: it writes
  `certificate.pem`, `key.pem` and `issuer.pem` and no more
  (`internal/cli/issue_certificate.go`). The root is public and the
  deployment already holds it, so whoever installs that first
  certificate out of band installs the same file
  `GARAM_MACHINE_SERVER_ROOT_FILE` names beside it. That path stays
  available and is the recovery from a token nobody received.
- **Registering the operator is what the enrollment path starts with,
  and what the out-of-band path still owes separately.** An owner or an
  admin of the organization registers the operator over the browser API
  (`POST /orgs/{org}/operators`), under the identifier the certificate's
  subject GRN carries
  ([ADR-0045](./adr/0045-an-organization-registers-an-operator.md)); on
  the enrollment path that call is also what mints the token, and it
  answers the token once. A deployment that bootstraps out of band and
  skips the registration has an operator that authenticates and no
  definition can name: `POST /orgs/{org}/agent-definitions` refuses an
  operator the organization has not registered. Nothing registers one
  automatically — no listener, no migration, and no command.

## The migrate job

`migrate` applies all pending migrations and exits
(`internal/cli/migrate.go`) and listens on nothing.

- **It fails closed on a dirty database and never clears the flag
  itself.** A failed run leaves the database dirty, and every later run
  refuses with a message naming the recovery — read the failed run's
  log, fix the migration, force the version, run again
  (`internal/cli/migrate.go`). The real error exists only in the first
  run's log, so a retry that deletes the previous Job destroys the
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

A request carries a bound of its own, thirty seconds to arrive, on two
of the four HTTP listeners garam starts: the browser-facing one and the
machine one (`requestReadTimeout` in `internal/cli/cli.go`, and on the
machine listener it covers the handshake read too). The other two carry
no such bound — the machine process's health port and the authz
process's probe and metrics port — and each of those is reachable on a
Service the chart renders. It bounds reading rather than a handler — a
watch stream and a message claim both outlive it by design — so what it
disconnects is a client that has
stopped sending, which would otherwise hold a connection open until the
shutdown bound above expired.

## The PodSecurity level is the deployment's

The workloads the chart renders are written to satisfy PodSecurity
`restricted`, and the chart renders no namespace — so the level itself is
set where the namespace is created, which is the deployment's. A cluster
enforcing `baseline`, or enforcing nothing, admits them equally: the
fields the level reads narrow what a pod asks for and require nothing of
the cluster in return.

Nothing in this repository asks an apiserver whether that holds. The
render is checked here and the admission is not, so a deployment
declaring the level is what turns the sentence above into a result. The
one measurement behind it was made on a cluster this repository does not
own, against the render rather than against what runs there (#636).

A deployment writing its own manifests rather than installing the chart
needs one fact neither image carries: **`runAsNonRoot` is not implied by
the user an image declares.** Both images declare that user by name, and
the kubelet resolves the image's own user only when no numeric
`runAsUser` is set — a name it cannot read as a number is refused at
container start rather than at admission. A manifest that asserts
`runAsNonRoot` supplies the numeric UID with it.

## Not established

Answers a consumer asked for that the source does not establish. Stated
here so they are not guessed, and so the chart does not offer a value
that would assert one:

- **Read-only root filesystem.** Not established for either image. What
  is checkable: `issue-certificate` is the one command under `internal/`
  or `cmd/` that writes a file — it creates the directory `--out-dir`
  names and writes `certificate.pem`, `key.pem` and `issuer.pem` into it
  (`internal/cli/issue_certificate.go`) — so that job needs a writable
  path there and the three serve processes need none. Where the
  directory cannot be created the run fails before a certificate is
  minted; where it exists and cannot be written the run fails after, and
  the private key in that answer exists nowhere else. What the web
  runtime writes, if anything, is unverified — so the `securityContext`
  field is the deployment's to set either way.
- **Replica safety above one.** Not established for `serve api`,
  `serve authz`, or `serve machine`. No leader election exists anywhere
  in the source; whether that makes a second replica safe or unsafe is
  not answered here. This is why the chart fixes every Deployment at one
  replica and offers no value for it. (`migrate` is answered above.)
- **Worst-case time from start to `/readyz` answering ok.** Not
  established; nothing in the source bounds startup. A slow start
  reading as a failed deploy is a function of the deployment's probe
  configuration.
- **Whether the adapter answers a probe.** It answers none: it has no
  listener, and [ADR-0021](./adr/0021-liveness-and-readiness.md) decides
  the probes for the three serve processes rather than for a process
  that only dials out. Whether a sidecar beside an agent owes one, and
  what it would report, is undecided.
- **Resource requests and limits.** No figure is published for any
  process and none has been measured. The figures are the deployment's
  to pick, and to record as picked rather than measured, so a later
  reader does not take them for garam's. Setting none is not neutral: a
  pod no container of which requests either resource is `BestEffort` —
  Kubernetes' own classification, which `kubectl describe pod` reports
  as `QoS Class` — and the kubelet evicts `BestEffort` first under node
  pressure (read 2026-08-25; this repository pins no Kubernetes
  version).
- **A resource floor.** No minimum below which a process fails is known.

## Decisions this rests on

- [ADR-0003](./adr/0003-single-cli-topology.md) — one binary, the engine
  reached over plaintext gRPC inside the trust boundary
- [ADR-0009](./adr/0009-web-frontend.md) — the web app and its `/api/*`
  rewrite
- [ADR-0014](./adr/0014-agent-authentication.md) — mTLS on a listener of
  its own, per-connection trust root
- [ADR-0015](./adr/0015-operator-identity.md) — key material travels as
  files, never the environment
- [ADR-0045](./adr/0045-an-organization-registers-an-operator.md) — an
  organization registers an operator, which no command does for it
- [ADR-0016](./adr/0016-ca-key-custody.md) — the key-encryption key the
  authorities are sealed under
- [ADR-0017](./adr/0017-machine-process.md) — three serve processes from
  one image
- [ADR-0018](./adr/0018-delivery-acknowledgement.md) — the adapter
  completes on the turn-end event and holds its pairing in memory
- [ADR-0021](./adr/0021-liveness-and-readiness.md) — the two probes on a
  credential-free port
- [ADR-0023](./adr/0023-web-probes.md) — the web app answers the same
  two
- [ADR-0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md)
  — readiness reports what a replica cannot serve without
- [ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  — where the two images are published, under what names, and what
  triggers a push
- [ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
  — the listener presents one certificate signed by a garam server
  authority the deployment holds, every identity pins that root, and a
  replaced pair is served without a restart (superseding ADR-0037)
- [ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
  — the chart describes the deployment and this document keeps what a
  chart cannot carry; it supersedes ADR-0020
- [ADR-0057](./adr/0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md)
  — which change to the chart moves `Chart.yaml`'s `version`, and that a
  reviewer rather than a check is what holds it
- [ADR-0043](./adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)
  — a workload's account is named by a value the deployment sets, which
  is the subject a role is scoped to
- [ADR-0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md)
  — an operator renews its own certificate, so the out-of-band mint is a
  bootstrap and not a schedule
- [ADR-0058](./adr/0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)
  — state a working artifact carries and enforces stays there, and the
  document that owns the concern names what it hands over
- [ADR-0064](./adr/0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)
  — an operator enrols with a one-time token and keeps its key in its
  own cluster, so the first certificate is a call rather than a file a
  deployment moves
