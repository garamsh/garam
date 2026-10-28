# Deployment Contract

What a garam deployment must plan for that no manifest can carry: what
readiness reports, what bounds termination, how the migration fails,
what the binary enforces about key material, and what is not known.

## Contents
- What it is
- What readiness reports
- Operator key and enrollment rollout
- What the binary enforces about key material
- The key-encryption key cannot be regenerated
- A Transit provider for message signing keys
- The message issuer is kept for the deployment's life
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
arrives in — and `make -C deploy/charts ci` checks it against the source
rather than against a reader's attention.

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
`deploy/charts/garam/README.md` §What it renders is authoritative for
what each template renders and for what follows from that — the update
strategy
every Deployment sets and what an upgrade costs in service under it,
and the hook phase the migration Job runs in.

The repositories the chart's `image.*.repository` names,
`garam/garam-server` and `garam/garam-web`, hold releases only: a bare
`X.Y.Z`, which only a `vX.Y.Z` git tag publishes
([ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)).
A development build a person made and pushed is in
`garam-dev/garam-server` or `garam-dev/garam-web`
([ADR-0097](./adr/0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md)),
under a commit-derived or prerelease tag
([ADR-0071](./adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)),
so a deployment running one sets the repository as well as the tag.
`tooling/image-build.sh` is authoritative for which development
references this repository produces, and refuses every other tag
before it builds; `README.md` §Development images is the procedure.
The test image is none of these: an operator pushes it for another
repository's CI to `garam-dev/garam-test`, an ECR repository of its
own that no release writes to; no deployment runs it, and the chart
names it nowhere
([ADR-0092](./adr/0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md),
[ADR-0093](./adr/0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)).

It records what is. Changing any behaviour named below is a decision,
not an edit here. Every claim names the path it can be checked against,
per `docs/convention/documentation.md` §Keep it current; where the
source answers a deployment question with nothing checkable,
§Not established says so rather than guessing.

The client-CA prohibition below originates in the consuming repository's
own record (`garamsh/gitops` issue #3), restated here against the source
it is checkable in. That repository had not agreed to consume the chart
when it was read on 2026-08-25, and ADR-0042 assumes nothing about
whether it will.

## What readiness reports

Dependencies each process reports (`platform/internal/cli/serve.go`,
`platform/internal/cli/machine.go`):

| Process | Readiness dependencies |
|---|---|
| `serve api` | the database, and the channel to the engine |
| `serve authz` | the database alone |
| `serve machine` | the database, and the channel to the engine |
| web | none — it reads nothing, so an API outage takes no replica out of rotation ([ADR-0023](./adr/0023-web-probes.md)) |

The semantics, fixed in `platform/internal/health/health.go`:

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

## Operator key and enrollment rollout

**Off until a deployment sets it.** garam serves key issuance and the
private_key_jwt exchange
([ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md))
once `GARAM_MACHINE_TOKEN_URL` is set. The chart renders it from
`machine.tokenUrl` on both `serve api` and `serve machine`, and renders
it on neither while that value is empty, its default
(`deploy/charts/garam/README.md` §Values a deployment must supply).
`garamsh/aurora` adopted the workflow below for its deployment:
aurora#663, merged 2026-10-18T18:14:09Z as
0a14d255f158291c132cbfed63437580d85de5dd, sets
`GARAM_MACHINE_TOKEN_URL` on both processes to the machine Service's
in-cluster name. Its operator authenticates by key: aurora#664's
post-merge comment of 2026-10-19
(https://github.com/garamsh/aurora/pull/664#issuecomment-6257270142)
records version 0.2.0 running with its key and no exchange failure,
and the agent ready at 00:44:37Z.

- Set `machine.tokenUrl` to the one absolute https URL of the machine
  listener's `/oauth/token` that every operator and control service
  reaches it by, directly or by TCP passthrough, so the TLS each meets
  is the listener's own. The URL carries no user information, query or
  fragment; the render refuses any other shape, as the binary does at
  startup. Keep it for the deployment's life. Every assertion names it
  as its audience byte for byte, so changing it refuses every Operator
  until each is re-keyed with the new value. Unset, `serve api` issues
  no key, refusing with 409 and the reason
  `key_issuance_unconfigured`, and `serve machine` answers the
  exchange 503. It is not `machine.issuer` (`GARAM_MACHINE_ISSUER`),
  the message issuer.
  - Which name that is follows from where the callers run. A
    deployment whose operators and control services all run in its
    cluster may use the machine Service's cluster name. One that
    expects a caller outside the cluster needs a name reachable from
    there, such as a TLS-passthrough route, and every caller then uses
    that name, the in-cluster ones included.

- Place each controller/control-service private key in its own
  owner-only file via the deployment's secret mechanism, never in
  source, an environment, image or agent/adapter/workspace mount.
  Garam keeps public verification material only; generation/download
  expose the key once as ADR-0102 §Operator identity and custody states.
- Declare one external/admin writer for the stable PEM, never a
  competing writer. Atomic replacement
  and reload per exchange use `kid` from that same key snapshot.
  Operator owns the exact owner-UID/0600 copy/mount mechanism; raw
  projection or `fsGroup` alone meets neither proof. Token refresh
  stays separate from supplying the private key.
- Install the key and pinned server root before startup. Ordinary
  access refresh uses fresh short assertions and never rotates the
  private key. Planned key rotation installs and proves a successor
  before withdrawing the old one; lost/compromised issuance replaces
  immediately with no overlap. garam enforces the 24-hour maximum
  planned overlap and next-request revocation
  ([operators.md](./operators.md) §Issued PEM identity).
- Retain `server-root.pem` for adapter TLS and Sherlock's issuer-fetch
  `keys-ca-file`, moving consumer and mount atomically if their paths
  change. No caller presents a client certificate, and server
  hostname/root verification and fetch trust stay. The message issuer
  remains stable and is not the OAuth token endpoint audience.
- A direct runtime can enroll without provisioning a Kubernetes Pod
  or Docker generation. Its one-agent connector still supplies current
  scoped API access and writer/lifecycle fencing; this is a new path,
  not a claim the current standalone runner already implements it.
- Garam accepts an Operator by its key alone; the certificate, the
  shared-secret cohort and the enrollment token are gone
  ([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md),
  [ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)),
  so a deployment issues each Operator identity a key before it runs.
  No Operator compatibility switch is authorized. gitops#14 and
  infra#331 remain owner gates.
- Message signing still seals private keys under the key-encryption
  key, unless the deployment holds them in Transit
  (§A Transit provider for message signing keys). The retired
  per-organization certificate authorities never authorize removing
  that key, message trust roots or signing rows.

The existing plan,
`docs/plans/2026-10-09-machine-identity-migration.md`, names sequence
and evidence; this decision authorizes no deployment or destruction.

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
and `platform/internal/keyfile` accepted it — what the rule constrains
is that the two agree, not that either is garam's.

**It is enforced, not requested.** `platform/internal/keyfile` refuses a
key file when any group or other permission bit is set, and separately
when its owner is not the process's own UID, and every key file a
process reads at startup goes through it: the key-encryption key and the
machine listener's own TLS key (`platform/internal/cli/machine.go`), so
either refusal stops the start of that process. Plain Kubernetes Secret
projection satisfies neither half — a projected file is root-owned, and
`fsGroup` changes the group owner rather than the user owner (read
2026-08-25; this repository pins no Kubernetes version). Relaxing the
check to fit plain projection is not an option; the arrival shape the
chart renders is what the rule requires.

Two qualifications:

- **The `GARAM_MACHINE_TLS_CERT_FILE` and `GARAM_MACHINE_TLS_KEY_FILE`
  pair is re-read on every handshake**
  (`platform/internal/keyfile/pair.go`), so a replaced pair is presented
  on the next connection with nothing restarted
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
- **There is no client CA bundle, and creating one is forbidden.** The
  machine listener asks for no client certificate and authenticates
  every caller by a token, read on every request
  (`platform/internal/machine/listener.go`,
  [ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md),
  [ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)),
  so a bundle would verify nothing. There is nothing to mount and
  nothing to rotate for it: what the workload mounts is its own
  certificate and key, the
  garam server root `GARAM_MACHINE_SERVER_ROOT_FILE` names, and the
  key-encryption key — four files, and none of them a bundle of client
  authorities.

## The key-encryption key cannot be regenerated

**The key-encryption key — the bytes in the file `GARAM_CA_KEY_FILE`
names, not the path — cannot be regenerated.** It seals the keys garam
signs messages with
([ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)),
each under the key present when it was created, and the sealed bytes
open only under that same key: a different one is refused by the
identifier of the key it needs rather than read as corruption
(`platform/internal/ca/sealer.go`). Rotation is not built and not
designed — the identifier is what would make it possible later, and
nothing performs it ([ADR-0016](./adr/0016-ca-key-custody.md)) — so
there is no path from one key to another. Under a different
key-encryption key the signing key does not open, and every claim
answers 500 naming that key in the listener's log. A signing key is
replaceable: `garam retire-message-signing-key <kid>` withdraws it, and
the next hand-out signs under a new key sealed by the key present. A
verifier holding the published set as a file refuses the new key's
signatures until the file is replaced. A deployment that configures the
Transit provider below stops sealing new message keys under it; the keys
already sealed stay sealed under it.

It also holds sealed the private key of every organization's
certificate authority an earlier release created. garam signs nothing
with them and reads their rows only for whether one exists, which the
organization deletion preview reports; the rows stay in place until
their owner decides to drop them, and they open under no other key
([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)).

Nothing detects the substitution at start: the arrival checks above
pass on any well-formed key, and nothing compares it against what the
database already holds (`platform/internal/cli/machine.go`). The wrong
key starts cleanly and fails at the first claim it signs.

The obligation that follows, in the form a deployment can act on:

- **Created once, never replaced.** A re-run of whatever supplies the
  key must be a no-op that keeps what already exists, never a
  regeneration.
- **It must survive the cluster.** A key that exists only as something
  the cluster generated is lost with the cluster, and every key sealed
  under it is lost with it.
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
So a sealed authority that had been through a dump and a restore opened
under the same key and signed. That is one leg of a restore and
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
(`platform/internal/cli/machine.go`,
`platform/internal/machine/listener.go`). Replacing them loses nothing,
and the constraint above is the key-encryption key's alone. What binds
instead is who signs them
([ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)):

- **One certificate authority signs that pair and signs nothing else.**
  garam never holds its key — the process holds the pair it signed and
  its certificate, and no more. cert-manager issuing from an issuer
  dedicated to this listener satisfies this; an issuer that also signs
  other workloads does not, because what it signs is what every caller
  trusts garam by.
- **That authority's certificate is `GARAM_MACHINE_SERVER_ROOT_FILE`,
  and garam hands it beside every credential it issues**
  (`serverRootPem` in the agent-token mint answer and the standalone
  enrollment answer, `platform/api/machine.yaml`). It is public, it is
  read once at startup, and it must be the root the pair above chains to
  — a mismatch reaches every caller and works for none. A deployment
  writing an adapter's files takes that same `serverRootPem` for
  `GARAM_ADAPTER_SERVER_ROOT_FILE` ([adapter.md](./adapter.md)).
- **Replacing that authority invalidates the root every caller already
  holds**, and garam reads it once, so it is a restart, with every
  holder's copy replaced, rather than a rotation. Nothing in garam
  schedules it; it should outlive the certificates it signs by years.
- **An operator needs that root before it calls.** The root is what the
  operator verifies garam by on its first exchange, so it reaches the
  operator's cluster ahead of the operator's key — one public file per
  deployment rather than one per operator. **An exchange made without
  verifying garam against it hands the operator's assertion to whoever
  answered.**
- **Registering the operator comes first.** An owner or an admin of the
  organization registers the operator over the browser API
  (`POST /orgs/{org}/operators`), under the identifier its GRN carries
  ([ADR-0045](./adr/0045-an-organization-registers-an-operator.md)),
  and its key is issued on that record
  ([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)).
  Nothing registers one automatically — no listener, no migration, and
  no command.
- **An operator with no registration does not authenticate.** The
  machine listener reads the record on every request, so the
  registration is a precondition for the operator's token rather than a
  separate tidiness
  ([ADR-0070](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)).

## A Transit provider for message signing keys

A deployment that sets `GARAM_TRANSIT_ADDR` holds every message
signing key `garam serve machine` creates from then on in an OpenBao or
Vault Transit key
([ADR-0104](./adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md),
`platform/internal/message/transit`); one that sets none of
`GARAM_TRANSIT_*` seals them under the key-encryption key as before, and
one that sets part of it does not start. The chart's `machine.transit`
values render the variables, and `deploy/charts/garam/README.md`
§Message signing keys in OpenBao Transit is authoritative for them. The
deployment supplies:

- **OpenBao over `https`**, under a certificate the root in
  `GARAM_TRANSIT_CA_FILE` verifies. No other scheme is accepted, the
  system roots are not consulted, and a redirect is not followed.
- **One `ecdsa-p256` Transit key** the deployment creates, which garam
  never creates, configures or deletes; garam refuses a key of another
  type. It stays non-exportable, is not deleted while a key row names
  it, and its `min_encryption_version` is never raised past a version
  the published set holds as signing or next — that version would stop
  signing.
- **An ACL policy of two paths on that key**: `update` on
  `<mount>/keys/<key>/rotate` and `<mount>/sign/<key>`, the two garam
  calls; it reads the public half from `rotate`'s answer and never
  reads the key, as ADR-0104's erratum of 2026-10-18 records.
  `platform/tests/integration/transit_signing_test.go` runs it under a
  token holding those two and nothing else.
- **A token file**, `GARAM_TRANSIT_TOKEN_FILE`, owner-only and owned by
  the process's UID: it is read through `platform/internal/keyfile` like
  every key file above, but on every call rather than at start, so a
  renewed token takes effect on the next call and a file not yet written
  fails a claim rather than the start. garam logs in with nothing, so
  the auth role binding the machine workload's account to the policy,
  and whatever writes and renews the file, are the deployment's.

**What the binary does with it:** the published set is read from
Postgres and keeps being served while OpenBao is unreachable; a claim
garam cannot sign is refused as a server error with no attempt spent;
a key due that OpenBao does not make — unreachable, or refusing — waits
for the next read while the current key signs, and a refusal is logged
as an error; readiness does not report OpenBao, and the start
does not reach it. Turning the provider on announces a Transit key with
the full two-hour lead while the current sealed key goes on signing,
so the key-encryption key must still be present then — it is, because
`serve machine` does not start without it. Turning it off leaves the
current
Transit key unable to sign there: claims are refused until the sealed
key announced in its place begins two hours later, or at once after
`garam retire-message-signing-key` retires it.

## The message issuer is kept for the deployment's life

**`GARAM_MACHINE_ISSUER` names the deployment to every verifier, and a
changed value is a different issuer.** `garam serve machine` refuses to
start without one, and refuses anything but an https URL in one
spelling — a lowercase host, no empty port, no path and no trailing
slash (`platform/internal/message/signing.go`, `ParseIssuer`), because a
verifier compares it byte for byte
([ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)).
**It is an identity, not an address.** It need not resolve or be
reachable, and a verifier fetches the key set from a `keys-url` of its
own, which is where it reaches the machine listener; so it asks for
nothing to be published. What the binary cannot check is that the
value stays the same: a verifier configured with the old one refuses
every message signed under the new one. So a deployment picks a
stable name it owns once, rather than the cluster's DNS, so that
moving or exposing the listener never changes it, and keeps it; the
dev cluster's is `https://machine.dev.garam.sh`. The chart renders it
from `machine.issuer`
(`deploy/charts/garam/README.md` §Values a deployment must supply).

A standalone host trusts that same value only where it names it:
`garam standalone run --message-issuer` renders it into the runtime's
configuration, exactly as `GARAM_MACHINE_ISSUER` spells it, beside the
address the runtime fetches garam's key set at
(`agents.md` §Standalone hosts), and turns the adapter's signing on
with it.
Nothing stands in for it: a host that names no issuer renders no entry,
signs nothing and trusts no garam signature, and one that names another
spelling refuses every message garam signs. A changed
issuer reaches a running generation only when it is replaced, since
the runtime reads its issuers at startup.

**An Operator's runtime trusts the same string.** On a cluster the
agent's runtime names garam in its configuration's `issuers` entry by
an exact `iss` (Sherlock v0.4.0, `docs/architecture/gateway.md`
§Signed messages), which garamsh/garam-agent-operator renders from its
`--garam-issuer`, and that value must equal this chart's
`machine.issuer` byte for byte, as a standalone host's must above.
Nothing checks the two against each other: a mismatch on either reads
as 403 `signature rejected: issuer` on every message once the adapter's
`GARAM_ADAPTER_SIGN_MESSAGES` is on, and not as an error at start
(`adapter.md` §Stable identifiers and the outbox).

## The migrate job

`migrate` applies all pending migrations and exits
(`platform/internal/cli/migrate.go`) and listens on nothing.

- **It fails closed on a dirty database and never clears the flag
  itself.** A failed run leaves the database dirty, and every later run
  refuses with a message naming the recovery — read the failed run's
  log, fix the migration, force the version, run again
  (`platform/internal/cli/migrate.go`). The real error exists only in
  the first run's log, so a retry that deletes the previous Job destroys
  the evidence.
- Every migration this project ships is applied inside a transaction and
  leaves nothing behind when it fails
  (`platform/internal/cli/migrate.go`); one that cannot run
  transactionally would break that guarantee.
- **Concurrent runs are safe** by the driver's mechanism:
  golang-migrate's Postgres driver takes an advisory lock, so a second
  `migrate` waits and then finds nothing to do. That claim holds at
  `github.com/golang-migrate/migrate/v4` v4.20.1 (`go.mod`) and is
  checkable against the driver it pins.

## Termination

Every serve command installs a SIGTERM and SIGINT handler
(`platform/internal/cli/shutdown.go`, wired into
`platform/internal/cli/serve.go` and
`platform/internal/cli/machine.go`), and so does `adapter`
(`platform/internal/cli/delivery.go`), which stops claiming and exits 0.
What it was carrying goes with it: the pairings it held reach no disk,
their leases lapse, and garam hands those messages out again
([ADR-0018](./adr/0018-delivery-acknowledgement.md)). That is why it has
to outlive the agent beside it — a native sidecar rather than an
ordinary container — and why its own termination grace period is budget
on top of the agent's drain rather than shared with it. When it fires,
the listener closes at once, in-flight requests and RPCs finish within a
ten-second bound, and past it they are abandoned deliberately by the
process rather than by the kubelet's SIGKILL. The process then exits 0:
a requested shutdown is not an error, so a pod a rollout or a delete
terminates records `Completed`, not `Error` (#338 settled this; the
assertion is `platform/tests/e2e/shutdown_test.go`, which SIGTERMs the
built binary as each of the three serve processes and requires exit 0).

Both terminations exit 0, so which of the two happened is in the log and
nowhere else: one that reached the bound and dropped what was still in
flight writes a line at warn naming the bound, and one that finished
inside it writes none (#1064).

The bound is the binary's, not the deployment's (`shutdownGrace` in
`platform/internal/cli/shutdown.go`): a `terminationGracePeriodSeconds`
below ten seconds truncates in-flight work, and nothing the process does
at shutdown outlasts it.

A request carries a bound of its own, thirty seconds to arrive, on two
of the four HTTP listeners garam starts: the browser-facing one and the
machine one (`requestReadTimeout` in `platform/internal/cli/cli.go`, and
on the machine listener it covers the handshake read too). The other two
carry no such bound — the machine process's probe and metrics port and
the authz process's — and each of those is reachable on a Service the
chart renders. It bounds reading rather than a handler — a watch stream
and a message claim both outlive it by design — so what it disconnects
is a client that has stopped sending, which would otherwise hold a
connection open until the shutdown bound above expired.

The browser-facing listener keeps no connection alive: it answers each
request with `Connection: close` and closes the connection after the
response (`apiServer` in `platform/internal/cli/serve.go`). The
console's `/api` rewrite reaches it through Next.js's proxy, whose
pooled sockets never time out and which ignores a server's `Keep-Alive`
hint. A request that reused a socket at the moment the listener closed
it for idleness was reset and answered 500 (#1428). A longer idle
timeout would only make that rarer, so the cost is one connection per
request on a cluster-local hop instead. The machine listener keeps its
connections alive.

## The PodSecurity level is the deployment's

The workloads the chart renders are written to satisfy PodSecurity
`restricted`, and the chart renders no namespace — so the level itself
is set where the namespace is created, which is the deployment's. A
cluster enforcing `baseline`, or enforcing nothing, admits them equally:
the fields the level reads narrow what a pod asks for and require
nothing of the cluster in return.

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
  is checkable: the code under `platform/internal/` that writes a file
  is the standalone host's commands
  (`platform/internal/cli/standalone_host.go`,
  `platform/internal/cli/standalone_run.go`), the supervisor's workload
  (`platform/internal/workload/`) and the adapter's outbox
  (`platform/internal/delivery/outbox.go`), none of which a serve
  process runs, so the three serve processes need no writable path. What
  the web runtime writes, if anything, is unverified — so the
  `securityContext` field is the deployment's to set either way.
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
- [ADR-0014](./adr/0014-agent-authentication.md) — a listener of its
  own for machine callers
- [ADR-0015](./adr/0015-operator-identity.md) — key material travels as
  files, never the environment
- [ADR-0045](./adr/0045-an-organization-registers-an-operator.md) — an
  organization registers an operator, which no command does for it
- [ADR-0070](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)
  — the record is what an operator's token authenticates on, so
  deleting it stops the operator and an unregistered one never starts
- [ADR-0016](./adr/0016-ca-key-custody.md) — the key-encryption key,
  which cannot be regenerated
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
  triggers a release
- [ADR-0071](./adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
  — a development build is told apart by its tag, which this
  repository refuses to make a bare release version
- [ADR-0097](./adr/0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md)
  — a development build lives under `garam-dev/`, and the release
  repositories hold releases only
- [ADR-0092](./adr/0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md)
  — the test image is published outside the release repositories,
  and no deployment runs it; its registry, its commit tag and its
  publishing from `dev` are superseded by ADR-0093
- [ADR-0093](./adr/0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)
  — an operator pushes the test image to its ECR repository in the
  account, no workflow holds a credential for it, and its retention is
  that repository's lifecycle policy, which `garamsh/aurora`'s
  `infrastructure/modules/aws/ecr-repositories.tf` declares at
  `4df93d567fddb9afe9e5120449053222dee2b7c7`
- [ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
  — the listener presents one certificate signed by a garam server
  authority the deployment holds, every caller pins that root, and a
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
- [ADR-0058](./adr/0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)
  — state a working artifact carries and enforces stays there, and the
  document that owns the concern names what it hands over
- [ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)
  — garam signs every message it hands out under keys the
  key-encryption key seals, and names itself by an issuer the
  deployment keeps for its life
- [ADR-0104](./adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md)
  — a deployment may hold its message signing keys in an OpenBao
  Transit key it supplies, and the authorities stay on the
  key-encryption key
- [ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
  — every machine caller authenticates with a garam-issued credential
  and short-lived tokens over server TLS
- [ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)
  — an Operator enters by a key alone, so a deployment issues it one
  before it runs
- [ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)
  — the certificate path goes, and the sealed authority rows and the
  key-encryption key stay
