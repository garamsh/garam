# garam Helm Chart

The description of how garam is deployed: the five workloads it runs, the ports they answer on, and every configuration value a deployment supplies.

## Contents
- What this is
- What it renders
- Workload identities
- Values a deployment must supply
- Not set by this chart
- Exposure is the cluster owner's
- Key material and secrets
- Pinning a version
- What keeps it true
- What the checks are complete over
- What it is not

## What this is

[ADR-0042](../../../docs/architecture/adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
makes this chart the authoritative description of a garam deployment,
superseding ADR-0020's "images and a contract, not manifests".
`docs/architecture/deployment-contract.md` keeps what no manifest can
carry — what readiness reports, what bounds termination, what is not
established — and no longer restates what is here.

Consuming it is the cluster-owning repository's decision to make, not
garam's to assume. Nothing here is written into `garamsh/gitops`, which
held nothing of this chart when it was read on 2026-08-25.

## What it renders

Five workloads, a `ClusterIP` Service in front of each of the four that
listen, and a `ServiceAccount` for each workload a deployment names one
for (§Workload identities). Each workload's file renders all of its own
objects:

- `templates/api.yaml` — `serve api`, port 8080.
- `templates/authz.yaml` — `serve authz`, gRPC on 9090, probes and
  `/metrics` on 9091.
- `templates/machine.yaml` — `serve machine`, mTLS on 8443, probes on
  8444.
- `templates/web.yaml` — the Next.js console, port 3000.
- `templates/migrate.yaml` — the migration Job, a `pre-install` and
  `pre-upgrade` hook. It carries no `hook-failed` delete policy: a
  failed run's log is the only place its error exists. A consumer that
  renders with `helm template` receives the Job as an ordinary manifest
  and orders it itself (helm v4.2.4, the version the `Makefile` pins).

Every Deployment is `replicas: 1` and the chart offers no value for it.
Replica safety above one is not established
(`docs/architecture/deployment-contract.md` §Not established), and a
chart value would assert what garam does not know. Establishing it is an
ADR, then a value.

Each also sets `strategy: type: Recreate`, so that count holds across an
upgrade and not only at rest. The default `RollingUpdate` brings the new
pod up before taking the old one down, which at one replica runs two of
the workload for the length of the rollout — the state `replicas: 1`
exists to avoid. The cost is that an upgrade is a gap in service rather
than a handover, which is the same trade `replicas: 1` already makes for
a node failure.

## Workload identities

Every workload can run as an account of its own, and **the chart creates
none by default**. `<workload>.serviceAccount.name` is the account it
runs as and `<workload>.serviceAccount.create` renders the object under
that name; both are off until a deployment sets them, so the chart
renders none, one, or five accounts and the values decide which.

| Workload | Kind | Account name |
|---|---|---|
| `api` | Deployment | `api.serviceAccount.name` |
| `authz` | Deployment | `authz.serviceAccount.name` |
| `machine` | Deployment | `machine.serviceAccount.name` |
| `migrate` | Job | `migrate.serviceAccount.name` |
| `web` | Deployment | `web.serviceAccount.name` |

`make chart` reads this table and fails when the render stops matching
it (§What keeps it true), so a workload added without an identity
decision fails the check rather than reaching a cluster unnoticed.

**The name is taken verbatim, and the chart composes none.** A name is
the subject an IAM trust policy in the infrastructure repository is
conditioned on — `garamsh/infra`, as the review on #504 read it on
2026-08-24 — so the two repositories have to hold one name, and a
name this chart composed from the release name would move whenever a
deployment renamed its release. That failure arrives at runtime, in a
credential that stops being issued, and a render under one release name
cannot show it — so `make chart` renders under two
([ADR-0043](../../../docs/architecture/adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)).

**Set a name only where a role is bound to it.** An account nothing uses
is a subject the next reader takes for one that was needed; an annotated
one is worse, because the annotation makes a consumer assume a role that
may not exist and fail at runtime rather than at apply. `make chart`
fails on an account no workload runs as.

**Annotations are how a cloud role binds to an account**, and each
account takes its own through `<workload>.serviceAccount.annotations`.
The chart carries none: which key binds a role is the deployment's
platform's.

They reach an account this chart creates. Setting them with
`create: false` is refused rather than dropped: the chart renders no
ServiceAccount to carry them, and an annotation silently discarded is a
role that never arrives — a failure that surfaces at runtime, in a
credential that is not issued. Annotate that account where it is
created.

**`create: false` with a name set is the supported pair.** The workload
runs as that account and the chart renders no object, which is what a
deployment whose infrastructure repository owns the account needs — it
keeps the object it made, and adoption is not a rename. The reverse pair
is refused: `create: true` with no name has no name to create under, and
the render fails rather than composing one.

**The migrate Job's account is created in the same hook phase as the
Job**, at a lower weight. A `pre-install` hook runs before the chart's
ordinary resources exist, and the apiserver refuses a pod naming an
account it cannot find; a deployment supplying that one itself has it in
place before the release's first install rather than with it.

### Which workloads need one

`serve machine` is the only one that needs an account today. It reads
the key-encryption key every organization's certificate authority is
sealed under
([ADR-0016](../../../docs/architecture/adr/0016-ca-key-custody.md)) and
refuses to start without it (`internal/cli/machine.go`). That key
arrives in the Secret `machine.keyMaterial.secretName` names, which this
chart does not create; whatever populates it grants to a subject, and
that subject is the machine listener's own rather than one every pod in
the namespace presents.

`serve api` needs one when it reads a cloud-held secret of its own —
Google sign-in is the pending case. `authz`, `web` and `migrate` read
none, so a role bound to an account for any of them would grant what
nothing asks for.

Getting the machine listener's identity wrong fails in the right
direction — the pod does not start, and restoring the identity fixes it.
What is unrepairable is the fix that looks obvious from that failure:
minting a replacement key. A different key opens no authority already
sealed and there is no path from one key to another, so every
organization loses the authority it already has
(`docs/architecture/deployment-contract.md` §The key-encryption key
cannot be regenerated, which carries what the only remedy costs).

## Values a deployment must supply

`values.yaml` documents each. These five have no default and the render
fails without them:

- `image.server.tag`, `image.web.tag` — no release exists, so no default
  names an image that can be pulled.
- `database.secretName` — the Secret holding `GARAM_DATABASE_URL`.
- `machine.keyMaterial.secretName` — the Secret holding the machine
  listener's certificate and key, the garam server root that signed
  them, and the key-encryption key.
- `web.apiUrl` — the address the web process reaches `serve api` on,
  baked into the image at build time as the `/api/*` rewrite target and
  read again at runtime by every server-side render
  (`web/src/lib/garam.ts`). Absent at runtime the process still starts
  and answers both probes, `/` and the rewrite keep serving, and every
  server-rendered route answers 500: `web/src/env.ts` parses at import,
  and only the routes reaching the backend import it.

Setting any one `api.google` value makes the rest required — the guard
is on the set, not on `clientId`, so omitting any one of them refuses
the render and names the one missing. garam refuses to start holding one
half of the credential, and a release installed without the other half
is one whose sign-in method is silently absent.

`machine.certificateLifetime` defaults to the binary's own `24h` and is
a security parameter: it bounds every certificate garam signs for an
agent or an operator, expiry is the only revocation garam has, and the
binary refuses to start outside one hour and thirty days
(`docs/architecture/deployment-contract.md` §What the binary enforces
about key material). Lowering it is safe only where each of those
identities renews and rewrites its own files without a restart, which
this chart does not deploy and cannot check.

`resources` is empty on every workload. No figure has been measured for
any garam process and setting none is not neutral, both of which
`docs/architecture/deployment-contract.md` §Not established states — so
the chart publishes no figure and the deployment picks its own.

## Not set by this chart

Every environment variable the source reads is either rendered above or
listed here with the reason. `make chart` compares the two sets against
the source and fails on either a name it finds in neither or a name here
that the source no longer reads.

| Variable | Why not |
|---|---|
| `GARAM_ADAPTER_AGENT` | The adapter is not one of garam's workloads: it runs beside one agent, in that agent's pod, deployed by whoever runs the agent (`docs/architecture/adapter.md`). |
| `GARAM_ADAPTER_MACHINE_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_GATEWAY_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_GATEWAY_AGENT` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_TLS_CERT_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_TLS_KEY_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_SERVER_ROOT_FILE` | Same: the adapter's own deployment. |
| `GARAM_MIGRATIONS_DIR` | The migrations ship inside the image and the default resolves to them there. A value here could only point the Job away from them. |

Two things sit outside that comparison:

- `PORT` is not in the table because no garam source reads it. The web
  image sets it (`deploy/local/Dockerfile.web`) and Next.js reads it, so
  the chart sets no value and names 3000 on the container port and both
  probes to match.
- `issue-certificate` is a command rather than a workload, run by hand
  against the same database: the bootstrap ADR-0015 leaves out of band.
  It is not the only step a deployment performs out of band — the key
  material §Key material and secrets names is the other. The three
  variables `issue-certificate` reads are already rendered above for the
  workloads that need them, and whoever runs it supplies the same
  values.

## Exposure is the cluster owner's

The chart renders **no Ingress, no Gateway, no LoadBalancer and no
`service.type` value**. Which address each process answers on, and which
of them leaves the cluster, stays the cluster-owning repository's
decision — ADR-0020 settled that and ADR-0042 keeps it. A chart
expresses it by rendering the Services a workload needs to reach another
workload, and nothing that reaches outside.

One rule binds whatever a consumer puts in front of the machine
listener:

- **Nothing that terminates TLS may stand in front of port 8443.** The
  listener terminates TLS itself and authenticates the client
  certificate per connection against the certificate authority of the
  organization the certificate's SAN names (ADR-0014). A terminating
  proxy strips that certificate and every machine call fails
  authentication — a failure ADR-0014 calls silent and total. Expose it
  by TCP passthrough or directly, never through an HTTP router that
  re-originates the connection.
- **There is no client CA bundle to mount, and creating one is
  forbidden** (`docs/architecture/deployment-contract.md` §What the
  binary enforces about key material). The chart offers no value for
  one.

The other listeners carry no such constraint: `serve api` and the web
app sit behind an ordinary TLS-terminating proxy — the session cookie's
`Secure` flag assumes one — and `serve authz` is cluster-internal
(ADR-0003).

## Key material and secrets

**No value in this chart is a secret, and none may become one.** Every
credential is named — a Secret and a key — and the chart creates no
Secret of its own.

Key material reaches a process as a file readable only by its owner
(ADR-0015), the binary refuses to start otherwise, and plain Secret
projection satisfies neither half of that rule
(`docs/architecture/deployment-contract.md` §What the binary enforces
about key material). `templates/machine.yaml` renders the shape that
does satisfy it: an initContainer running as UID 10001 copies the
projected Secret with `install -m 0600` onto a memory-backed
`emptyDir`. The Secret is projected `0440`, so `fsGroup: 10001` is what
gives that copy a group it can read the source under. The `emptyDir` it
writes to is world-writable with or without `fsGroup`, so the write is
not what the setting buys (measured 2026-08-26 on Kubernetes v1.36.3;
this repository pins no Kubernetes version).

The Secret holds four files, and `machine.keyMaterial` names the key
each is under: the listener's certificate and key, the garam server root
that signed them and that every identity garam issues to pins garam by,
and the key-encryption key. One authority signs that pair and nothing
else, so an issuer that also signs other workloads must not mint it
(ADR-0040).

**The copy is made once, and rotation is the deployment's other half.**
The listener re-reads its certificate and key on every handshake, so a
replaced pair is served on the next connection with nothing restarted
(ADR-0040). This chart renders the copy at pod start and nothing that
refreshes it, so a pair replaced in the Secret reaches the projected
volume and not the file the process opens: under this chart alone,
rotation is still a pod restart. Whatever a deployment adds to refresh
that copy carries both obligations
(`docs/architecture/deployment-contract.md` §What the binary enforces
about key material):

- It replaces the pair by renaming over the file, never by writing in
  place. A reader that opens a half-written file refuses the handshake.
- It satisfies the arrival rule on every refresh, not only the first:
  the file the process opens stays owned by that process with no group
  or other bit set.

The chart generates no key material, and the key-encryption key is why
— it cannot be regenerated
(`docs/architecture/deployment-contract.md` §The key-encryption key
cannot be regenerated), so a chart that minted one would leave every
certificate authority in the database unopenable on its second
install. Supplying it, keeping it beyond the cluster's life, and
choosing where it lives are the deployment's.

## Pinning a version

Nothing packages or publishes this chart, so it is referenced by a git
ref of `garamsh/garam` — a tag, or a commit SHA, which cannot come to
mean something else. `Chart.yaml`'s `version` moves when the chart
changes and names no garam version.

The images are pinned separately, through `image.server.tag` and
`image.web.tag`. A release is a `vX.Y.Z` git tag and the image tag is
that tag with the `v` dropped; one commit publishes both images under
one version, so a deployment pins one version across the pair
(ADR-0039). The tags are immutable, so the tag answers what is deployed
on its own.

A deployment must be able to pull both references, and how it does that
is not garam's to decide: the registry admits no anonymous pull and the
credential it issues expires. `image.pullSecrets` is one way to supply
one and the chart does not require it; a node credential minted per pull
is another.

**Neither image exists yet.** No release pipeline exists, no release has
been cut, and both ECR repositories were empty when ADR-0042 read them
on 2026-08-25, so the references `values.yaml` carries pull nothing.
`Chart.yaml` has no `appVersion` for the same reason; the change that
publishes the first release sets it and makes a default for the two tags
honest.

## What keeps it true

`make chart` is the entry point, and `make ci` runs it. helm comes from
the module pin in the `Makefile`, so nothing is installed by hand and
every run takes one version. It reports what it covered and fails when a
count is zero:

- `chart-lint` — `helm lint --strict` against each value file in `ci/`.
- `chart-render` — `helm template` against each, so a template that
  cannot render fails, and the environment comparison above against the
  names the source reads: `envOr` in `internal/cli/`, which wraps the
  module's only `os.Getenv` call, and the schema in `web/src/env.ts`.
- `chart-identity` — the identity comparison against
  §Workload identities. Every rendered Deployment and Job is a workload
  that table declares, every account the chart renders is one a workload
  runs as, the value files that name none render none, and every render
  is repeated under a second release name and must come out identical.
- `chart-package` — `helm package`, then a search of the unpacked
  artifact for `cpu` and `memory` as keys. Every file not excluded ships,
  `ci/` included, so a value file reaches a consumer without passing the
  three above, each of which reads the source tree instead. This file
  ships in the artifact too, so it names the two as words rather than as
  keys.

The comparisons are what an executable artefact buys over prose: a
variable added to the source and not to the chart, or an account name
that moves with the release name, fails the check instead of being
discovered by a deployment.

## What the checks are complete over

A check that fires correctly says nothing about what it never reads, so
what each is complete over is recorded here rather than inferred.
`chart-render` and `chart-identity` each point here in the line they
print.

- `chart-lint` and `chart-render` are complete over **templates that
  produce YAML**: one that cannot render fails. Neither asks an
  apiserver whether the YAML it produced would be accepted.
- `chart-render` is complete over **environment names**, exactly: every
  name the source reads is rendered or declared unset, and every name
  rendered is read. It reads no value — a name rendered with the wrong
  value passes.
- `chart-identity` is complete over **pod identity**: which account each
  Deployment and Job runs as, whether every account rendered is one a
  workload runs as, and whether either moves when the release name
  changes. It reads no annotation — an account carrying a key no
  platform binds a role through passes.
- `chart-names` is complete over **rendered object names against the
  63-character limit**, under the longest release name Helm accepts. It
  reads the name and nothing else: a name inside the limit but wrong,
  or any other field an apiserver would refuse, passes. It asks no
  apiserver either — it holds one rule that an apply enforces, which is
  why the rule is written here rather than discovered there.
- `chart-guards` is complete over **the values named by the files in
  `ci/refused/`**: each must refuse the render and name the value its
  filename names, and `minimal-values.yaml` must still render. It reads
  no other value — a value documented as guarded and given no file
  there passes, which is why a new guard lands with its file.
- `chart-package` is complete over **two resource names in the published
  artifact**: no file `helm package` ships sets `cpu` or `memory` as a
  key. It reads no third name, so a figure under `ephemeral-storage` or
  an extended resource passes, and it reads no render — a figure a
  deployment sets in its own values is that deployment's and reaches no
  file this opens.

`chart-package` reaches one thing §Not established in
`docs/architecture/deployment-contract.md` says garam does not publish: a
resource figure. Nothing stops the chart from publishing an answer to the
rest — a read-only root filesystem, replica safety above one, a bound on
startup — and each of those stays reviewed rather than checked.

Nothing checks the rest of what a pod carries — volumes and mounts,
probe paths and ports, the security context, the hook annotations on the
migrate Job. Each is reviewed rather than checked.

Two things sit outside any of them:

- A deployment setting `serviceAccount.create: false` renders no
  account, and nothing here can see the object it supplies instead — nor
  whether the name it sets is the one its infrastructure repository
  binds a role to. That agreement is the contract §Workload identities
  states and no check in this repository can reach.
- `web.apiUrl` must equal the value the web image was built with, and
  nothing can check it: the rewrite target is baked in at build time.

## What it is not

`deploy/local/` is untouched by this chart and is not derived from it.
The compose stack and the minikube manifests there are development
artefacts with no ingress, no issued certificate, and images that exist
only on the machine that built them. Nothing in this chart is a starting
point for that, and nothing there is a starting point for this.
