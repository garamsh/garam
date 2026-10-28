# garam Helm Chart

The description of how garam is deployed: the five workloads it runs,
the ports they answer on, and every configuration value a deployment
supplies.

## Contents
- What this is
- What it renders
- Choosing a release name
- Workload identities
- Values a deployment must supply
- Not set by this chart
- Exposure is the cluster owner's
- Key material and secrets
- Message signing keys in OpenBao Transit
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

Six workloads, a `ClusterIP` Service in front of each of the five that
listen, and a `ServiceAccount` for each workload a deployment names one
for (§Workload identities). Each workload's file renders all of its own
objects:

- `templates/api.yaml` — `serve api`, port 8080.
- `templates/authz.yaml` — `serve authz`, gRPC on 9090, probes and
  `/metrics` on 9091.
- `templates/machine.yaml` — `serve machine`, mTLS on 8443, probes on
  8444.
- `templates/web.yaml` — the Next.js console, port 3000.
- `templates/site.yaml` — the Next.js site, the landing page and the way
  in (ADR-0131), port 3000.
- `templates/migrate.yaml` — the migration Job, a `pre-install` and
  `pre-upgrade` hook. It carries no `hook-failed` delete policy: a
  failed run's log is the only place its error exists. A consumer that
  renders with `helm template` receives the Job as an ordinary manifest
  and orders it itself (helm v4.2.4, the version `deploy/charts/Makefile` pins).

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

## Choosing a release name

Every object here is named after the release, and the name is a
deployment's to choose — with one exception no render shows. **A
published web image reaches the api Service a release named `garam`
renders.** The browser's `/api/*` calls are rewritten to an address
baked into the image when it is built, and every image
`.github/workflows/release.yml` publishes is built with
`http://garam-api:8080`
([ADR-0056](../../../docs/architecture/adr/0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)).
Under any other release name this chart renders `<release>-garam-api`,
the rewrite reaches a name that resolves to nothing, and nothing reports
it: every object is `Ready`, the web app's probes read nothing outside
the process (ADR-0023), and the first report is a console that shows
nothing.

So a deployment pinning `image.web.tag` to a published version installs
under the release name `garam`, or makes `garam-api` resolve to its own
api Service — a Service of that name over the same selector labels is
one shape, and rendering it and checking it are that deployment's. A
deployment that builds its own web image bakes its own address and is
bound to that one instead.

`make -C deploy/charts ci` holds the half of this that lives here: what
`.github/workflows/release.yml` bakes is the api Service this chart
renders under the release name `garam`, read against a second release
name that must render something else (§What keeps it true). The other
half — that the image a deployment pins was built with the address that
deployment's release renders — is outside every check here
(§What the checks are complete over).

## Workload identities

Every workload can run as an account of its own, and **the chart creates
none by default**. `<workload>.serviceAccount.name` is the account it
runs as and `<workload>.serviceAccount.create` renders the object under
that name; both are off until a deployment sets them, so the chart
renders none, one, or six accounts and the values decide which.

| Workload | Kind | Account name |
|---|---|---|
| `api` | Deployment | `api.serviceAccount.name` |
| `authz` | Deployment | `authz.serviceAccount.name` |
| `machine` | Deployment | `machine.serviceAccount.name` |
| `migrate` | Job | `migrate.serviceAccount.name` |
| `site` | Deployment | `site.serviceAccount.name` |
| `web` | Deployment | `web.serviceAccount.name` |

`make -C deploy/charts ci` reads this table and fails when the render
stops matching it (§What keeps it true), so a workload added without an
identity decision fails the check rather than reaching a cluster
unnoticed.

**The name is taken verbatim, and the chart composes none.** A name is
the subject an IAM trust policy in the infrastructure repository is
conditioned on — `garamsh/infra`, as the review on #504 read it on
2026-08-24 — so the two repositories have to hold one name, and a
name this chart composed from the release name would move whenever a
deployment renamed its release. That failure arrives at runtime, in a
credential that stops being issued, and a render under one release name
cannot show it — so `make -C deploy/charts ci` renders under two
([ADR-0043](../../../docs/architecture/adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)).

**Set a name only where a role is bound to it.** An account nothing uses
is a subject the next reader takes for one that was needed; an annotated
one is worse, because the annotation makes a consumer assume a role that
may not exist and fail at runtime rather than at apply.
`make -C deploy/charts ci` fails on an account no workload runs as.

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
the key-encryption key the message signing keys are sealed under, which
an earlier release also sealed each organization's certificate authority
under
([ADR-0016](../../../docs/architecture/adr/0016-ca-key-custody.md),
ADR-0104), and refuses to start without it
(`platform/internal/cli/machine.go`). That key arrives in the Secret
`machine.keyMaterial.secretName` names, which this chart does not
create; whatever populates it grants to a subject, and that subject is
the machine listener's own rather than one every pod in the namespace
presents.

`serve api` needs one when it reads a cloud-held secret of its own —
Google sign-in is the pending case. `authz`, `web`, `site` and `migrate`
read none, so a role bound to an account for any of them would grant
what nothing asks for.

Getting the machine listener's identity wrong fails in the right
direction — the pod does not start, and restoring the identity fixes it.
What is unrepairable is the fix that looks obvious from that failure:
minting a replacement key. A different key opens no authority already
sealed and there is no path from one key to another, so every
organization loses the authority it already has
(`deployment-contract.md` §The key-encryption key cannot be regenerated,
which carries what the only remedy costs).

## Values a deployment must supply

`values.yaml` documents each. These have no default and the render
fails without them:

- `image.server.tag`, `image.web.tag`, `image.site.tag` — the version a
  deployment runs is
  its own to name. A default would go on naming the release it was
  written against once the next one lands, with nothing in the chart
  changed to say so.
- `database.secretName` — the Secret holding `GARAM_DATABASE_URL`.
- `machine.keyMaterial.secretName` — the Secret each of the machine
  listener's four key-material files is read from, less whichever of
  them names a Secret of its own (§Key material and secrets).
- `machine.issuer` — the issuer every message signature names
  (ADR-0101). It is an identity, compared byte for byte, and need not
  resolve or be reachable: prefer a stable name the deployment owns
  over the cluster's DNS, so that moving or exposing the listener never
  changes it, and keep it for the deployment's life. The dev cluster's
  is `https://machine.dev.garam.sh`. Its spelling is an https URL with a
  lowercase host, no empty port, no path and no trailing slash, and the
  binary refuses any other at startup. A verifier fetches garam's key
  set from its own `keys-url`, which is where it reaches the machine
  listener and not this value, so nothing here asks for the listener
  to be published.
- `web.apiUrl` — the address the web process reaches `serve api` on,
  baked into the image at build time as the `/api/*` rewrite target and
  read again at runtime by every server-side render
  (`platform-web/src/lib/garam.ts`, binding `lib/platform-sdk-ts`), and
  for a published image it is the address §Choosing a release name
  states.
  Absent at runtime the process still starts and answers both probes,
  `/` and the rewrite keep serving, and every server-rendered route
  answers 500:
  `platform-web/src/env.ts` parses at import, and only the routes
  reaching the backend import it. `site.apiUrl` is the same address for
  the site, on the same terms (`site-web/src/lib/garam.ts`).
- `web.siteUrl` and `site.consoleUrl` — where each app sends a visitor
  to the other (ADR-0131): the console sends a visitor without a
  session to the site's `/handoff`, and the site sends the code it
  hands back, and every address under `/console`, to the console. Both
  are read when a request arrives, so the published images name no
  deployment's hosts.

**Each app holds a host-only session.** A person signs in on the site,
and the console is handed a session of its own on its host by a
one-time code (ADR-0131), so the chart sets no cookie domain and the
two hosts need not share a parent.

**Each image carries the account it declares**, as `runAsUser` and
`runAsGroup` under `image.server`, `image.web` and `image.site`. The
defaults are what the two Dockerfiles build — 10001 for the server
image, 1000 for the web images' `node` — and a deployment running either
under another account moves the pair there rather than one pair per
workload, because four of the five workloads run the server image. The
machine pod takes the server pair for its `fsGroup` as well: the copy
its init container makes has to be owned by the UID the container
beside it reads under (§Key material and secrets).

Setting any one `api.google` value makes the rest required — the guard
is on the set, not on `clientId`, so omitting any one of them refuses
the render and names the one missing. garam refuses to start holding one
half of the credential, and a release installed without the other half
is one whose sign-in method is silently absent.

`machine.agentTokenLifetime` defaults to the binary's own `1h` and
bounds every agent token garam mints (ADR-0100): the binary refuses to
start outside twenty minutes and twenty-four hours. The agent's
operator mints the next token once two thirds to three quarters of the
lifetime has passed, so a quarter of it is the margin its propagation
into the agent's Pod has to fit in; a deployment whose propagation
does not fit raises the lifetime rather than refreshing later.

`machine.tokenUrl` turns on the operator key exchange
([ADR-0102](../../../docs/architecture/adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md))
and is empty by default. It renders `GARAM_MACHINE_TOKEN_URL` on both
`serve api` and `serve machine` from one value, through one helper, so
the key `serve api` issues and the audience `serve machine` compares
cannot disagree. Empty renders the variable on neither: `serve api`
then refuses key issuance with 409 `key_issuance_unconfigured` and
`serve machine` answers the exchange 503. The value is the one URL of
the machine listener's `/oauth/token` that every operator and control
service reaches it by, directly or by TCP passthrough as §Exposure is
the cluster owner's requires. It is kept for the deployment's life,
because every assertion names it as its audience byte for byte:
changing it refuses every operator until each is re-keyed with the new
value. It is not `machine.issuer`, the message issuer. Which name it is
follows from where the callers run. A deployment whose operators and
control services all run in its cluster may use the machine Service's
cluster name; one that expects a caller outside the cluster needs a
name reachable from there, such as a TLS-passthrough route, which every
caller then uses. Set, the render refuses what the binary refuses at
startup — anything but an https URL with a host and no user
information, query or fragment — and names the value. No private key
passes through it: an operator's key stays the owner-only file
`deployment-contract.md` §Operator key and enrollment rollout names.

`resources` is empty on every workload. No figure has been measured for
any garam process and setting none is not neutral, both of which
`docs/architecture/deployment-contract.md` §Not established states — so
the chart publishes no figure and the deployment picks its own.

The machine pod is the one with two containers — the `key-material`
init container that performs the copy §Key material and secrets
describes, and the `machine` container beside it — and
`machine.resources` reaches both. One value rather than two, because
what reads a figure reads the pod: a namespace with a quota admits a pod
only when every container carries the resource that quota bounds, and a
QoS class is decided across every container, init containers included. A
second value would leave both of those to a deployment that set only the
one named after the workload. The copy reserves nothing extra either —
a pod's effective request is the max of its init containers and the sum
of the rest, so the same figure on both sides is the figure once.

## Not set by this chart

Every environment variable the source reads is either rendered above or
listed here with the reason. `make -C deploy/charts ci` compares the two
sets against the source and fails on either a name it finds in neither
or a name here that the source no longer reads.

| Variable | Why not |
|---|---|
| `GARAM_ADAPTER_AGENT` | The adapter is not one of garam's workloads: it runs beside one agent, in that agent's pod, deployed by whoever runs the agent (`docs/architecture/adapter.md`). |
| `GARAM_ADAPTER_MACHINE_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_GATEWAY_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_TOKEN_FILE` | Same: the adapter's own deployment, whose assignee keeps the token current (ADR-0100). |
| `GARAM_ADAPTER_CONTROL_TOKEN_FILE` | Same: the adapter's own deployment, beside its control settings (ADR-0100). |
| `GARAM_ADAPTER_SERVER_ROOT_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_URL` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_ROOT_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_PLACEMENT_TOKEN_FILE` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_OUTBOX_DIR` | Same: the adapter's own deployment. |
| `GARAM_ADAPTER_CONTROL_SOCKET` | Same: the adapter's own deployment, which on a standalone host is its supervisor (ADR-0085). |
| `GARAM_ADAPTER_SIGN_MESSAGES` | Same: the adapter's own deployment, which alone knows whether the runtime beside it accepts a signature (ADR-0101). |
| `GARAM_ENDPOINT` | Read by the user commands (`garam login` and those after it), which run on a person's machine and never in a workload (ADR-0120). |
| `XDG_CONFIG_HOME` | Same: where a user command keeps its credentials file, on a person's machine. |
| `GARAM_MIGRATIONS_DIR` | The migrations ship inside the image and the default resolves to them there. A value here could only point the Job away from them. |

Two things sit outside that comparison:

- `PORT` is not in the table because no garam source reads it. The web
  image sets it (`deploy/local/Dockerfile.web`) and Next.js reads it, so
  the chart sets no value and names 3000 on the container port and both
  probes to match.

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
  listener terminates TLS itself, under the pair its callers pin
  garam's server root against (ADR-0040). A terminating proxy presents
  a certificate of its own, which no caller pinning that root accepts,
  so every machine call fails. Expose it by TCP passthrough or
  directly, never through an HTTP router that re-originates the
  connection.
- **There is no client CA bundle to mount, and creating one is
  forbidden**, per
  `deployment-contract.md` §What the binary enforces about key material.
  The chart offers no value for one.

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
(`deployment-contract.md` §What the binary enforces about key material).
`templates/machine.yaml` renders the shape that
does satisfy it: an initContainer running as `image.server.runAsUser`
copies the projected volume with `install -m 0600` onto a memory-backed
`emptyDir`. That volume is projected `0440`, so the pod's `fsGroup` —
`image.server.runAsGroup`, the same account — is what gives the copy a
group it can read the source under. The `emptyDir` it writes to is
world-writable with or without `fsGroup`, so the write is not what the
setting buys (measured 2026-08-26 on Kubernetes v1.36.3; this
repository pins no Kubernetes version).

Four files reach the process, and `machine.keyMaterial` names the Secret
each is read from and the key it is under: the listener's certificate
and key, the garam server root that signed them and that every caller
garam issues a credential to pins garam by, and the key-encryption key.
One authority signs that pair and nothing else, so an issuer that also
signs other workloads must not mint it (ADR-0040).

**The four need not sit in one Secret.** A file takes
`machine.keyMaterial.secretName` unless it names its own —
`certSecretName`, `keySecretName`, `serverRootSecretName`,
`kekSecretName` — and the pod projects each named key, from the Secret
holding it, into one directory: that key alone, so nothing else the
Secret carries is projected. Two Secrets is the layout of the one
deployment this chart has been rendered against (#1093): what issues
the listener's pair and what holds the key-encryption key are
different systems there. Moving the key-encryption key into the issued
Secret to fit one name would change how every message signing key is
sealed, which is not a packaging decision to take, per
`deployment-contract.md` §The key-encryption key cannot be regenerated.

**The copy is made once, and rotation is the deployment's other half.**
The listener re-reads its certificate and key on every handshake, so a
replaced pair is served on the next connection with nothing restarted
(ADR-0040). This chart renders the copy at pod start and nothing that
refreshes it, so a pair replaced in the Secret reaches the projected
volume and not the file the process opens: under this chart alone,
rotation is still a pod restart. Whatever a deployment adds to refresh
that copy carries both obligations
(`deployment-contract.md` §What the binary enforces about key material):

- It replaces the pair by renaming over the file, never by writing in
  place. A reader that opens a half-written file refuses the handshake.
- It satisfies the arrival rule on every refresh, not only the first:
  the file the process opens stays owned by that process with no group
  or other bit set.

The chart generates no key material, and the key-encryption key is why —
it cannot be regenerated, per `deployment-contract.md` §The
key-encryption key cannot be regenerated: a chart that minted one would
leave every key sealed under the first unopenable on its second install.
Supplying it, keeping it beyond the cluster's life, and choosing where
it lives are the deployment's.

## Message signing keys in OpenBao Transit

`machine.transit` holds the keys garam signs messages with in an
OpenBao or Vault Transit key instead of sealing them under the
key-encryption key
([ADR-0104](../../../docs/architecture/adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md)).
It is off while `machine.transit.address` is empty, and the render then
carries none of its variables. Set, `caSecretName`, `tokenFile` and
`signingKey` are required and the render refuses without each, naming
it: garam refuses to start holding part of the setting. What the
deployment owes OpenBao — the key, the policy and the auth role — is
stated in
`deployment-contract.md` §A Transit provider for message signing keys.

- **The policy** grants `update` on `<mount>/keys/<key>/rotate` and
  `<mount>/sign/<key>` and nothing else: those are the two calls garam
  makes.
- **The CA** is read from the Secret `caSecretName` names, under
  `caKey`, and projected as it is: it is public, so it is not copied to
  an owner-only file.
- **The token is not in this chart.** `tokenFile` names where in the
  machine container the binary reads it, on every call, and whatever
  writes and renews it is the deployment's. The file is held to the
  arrival rule above — owned by `image.server.runAsUser`, no group or
  other bit — and replaced by renaming over it. `machine.podAnnotations`
  is how a writer that injects itself into the pod, such as OpenBao
  Agent's injector, is asked for; the chart renders no writer and no
  volume for one.

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
on its own. The release repositories, `garam/garam-server` and
`garam/garam-web`, hold releases only. A development build
is in `garam-dev/garam-server` or `garam-dev/garam-web` (ADR-0097),
tagged by a commit prefix or `X.Y.Z-<prerelease>` rather than a bare
version (ADR-0071), so running one sets `image.*.repository` as well
as the tag, and pins a build nobody released.

**A digest is optional and names the bytes.** `image.server.digest` and
`image.web.digest` render after the tag rather than in place of it —
`repository:tag@digest`, the form a kubelet resolves by digest — so the
tag goes on naming the version a deployment chose and the two are one
value a reader holds in one place. Against the two release repositories
it restates what an immutable tag already fixes; what it is for is an
image mirrored into a registry that is not, and a deployment whose
policy is that a reference names bytes.

A deployment must be able to pull both references, and how it does that
is not garam's to decide: the registry admits no anonymous pull and the
credential it issues expires. `image.pullSecrets` is one way to supply
one and the chart does not require it; a node credential minted per pull
is another.

**`0.1.0` is the first released version.** `v0.1.0` ran
`.github/workflows/release.yml` on 2026-09-01, and both repositories
hold `0.1.0` under the digests the release that tag created records
(ADR-0039). The two tags stay required all the same: the version a
deployment runs is its own to name, and a default here would go on
naming `0.1.0` once the next release lands, with nothing in the chart
changed to say so. `Chart.yaml` still carries no `appVersion` — ADR-0042
records that the change publishing the first release sets it, and no
change since has.

## What keeps it true

`make -C deploy/charts ci` is the entry point, and `make ci` runs it.
helm comes from the pin in `deploy/charts/Makefile`, so nothing is
installed by hand and every run takes one version. It reports what it
covered and fails when a count is zero:

- `chart-lint` — `helm lint --strict` against each value file in `ci/`.
- `chart-render` — `helm template` against each, so a template that
  cannot render fails, and the environment comparison above against the
  names the source reads: `envOr` in `platform/internal/cli/`, which
  wraps the module's only `os.Getenv` call, and the schema in
  `platform-web/src/env.ts`.
- `chart-identity` — the identity comparison against
  §Workload identities. Every rendered Deployment and Job is a workload
  that table declares, every account the chart renders is one a workload
  runs as, the value files that name none render none, and every render
  is repeated under a second release name and must come out identical.
- `chart-names` — `helm template` under the longest release name Helm
  accepts, and every object name the renders carry held to 63
  characters, which an apply enforces and a render does not. A render
  that fails fails the target: each lands in a file before its names are
  read, so a failure cannot pass as a value file that named nothing
  while the other kept the count above zero.
- `chart-rewrite` — the `/api/*` target `.github/workflows/release.yml`
  bakes into the web image against the api Service this chart renders
  under the release name `garam` (§Choosing a release name). It renders
  again under `chart-names`' release name as the control: that render
  must name something else, so the equality is read rather than assumed.
- `chart-guards` — each value file under `ci/refused/` rendered on top
  of `minimal-values.yaml`, and refused by the guard it is named after:
  the failure must be an `execution error` from a template, opening with
  the value the file is named for. A refusal for any other reason — a
  values file helm cannot parse, a template that stopped somewhere else
  — is not that guard firing. `minimal-values.yaml` must still render,
  so a chart that refused everything fails here instead of passing every
  case above.
- `chart-security` — the PodSecurity `restricted` comparison against
  every workload §Workload identities declares. The pod spec sets
  `runAsNonRoot: true` and a `seccompProfile` type, every container
  refuses privilege escalation and drops `ALL`, and every container's
  effective `runAsUser` is an integer — its own, or the pod's where it
  sets none, which is the rule the kubelet applies and the one most
  containers here rely on. The line it prints counts how many took the
  pod's.
- `chart-package` — `helm package`, then a search of the unpacked
  artifact for `cpu` and `memory` as keys. Every file not excluded
  ships, `ci/` included, so a value file reaches a consumer without
  passing the seven above, each of which reads the source tree instead.
  This file ships in the artifact too, so it names the two as words
  rather than as keys.

The comparisons are what an executable artefact buys over prose: a
variable added to the source and not to the chart, or an account name
that moves with the release name, fails the check instead of being
discovered by a deployment.

## What the checks are complete over

A check that fires correctly says nothing about what it never reads, so
what each is complete over is recorded here rather than inferred.
`chart-render`, `chart-identity`, `chart-names`, `chart-rewrite`,
`chart-security` and `chart-package` each point here in the line they
print.

- `chart-lint` and `chart-render` are complete over **templates that
  produce YAML**: one that cannot render fails. Neither asks an
  apiserver whether the YAML it produced would be accepted.
- `chart-render` is complete over **environment names**, exactly: every
  name the source reads is rendered or declared unset, and every name
  rendered is read. It reads no value — a name rendered with the wrong
  value passes. What the Go source reads is the `envName` block in
  `platform/internal/cli`, not a pattern over the calls that read it, so
  a wrapper around `envOr` cannot quietly shrink what this covers: the
  target refuses a second read of the environment under `internal|cmd`
  and refuses a name written at a call site instead of declared there.
  The web half is `platform-web/src/env.ts` parsed for its own shape,
  and it carries no such guard — a module reading `process.env` directly
  would be a name this check never sees.
- `chart-identity` is complete over **pod identity**: which account each
  Deployment and Job runs as, whether every account rendered is one a
  workload runs as, and whether either moves when the release name
  changes. It reads no annotation — an account carrying a key no
  platform binds a role through passes.
- `chart-names` is complete over **rendered object names against the
  63-character limit**, under the longest release name Helm accepts. It
  reads the name and nothing else: a name inside the limit but wrong, or
  any other field an apiserver would refuse, passes. It asks no
  apiserver either — it holds one rule that an apply enforces, which is
  why the rule is written here rather than discovered there. It reads no
  image either, and the release name it renders under is one no
  published web image serves: the object names survive it and the
  `/api/*` rewrite does not, which §Choosing a release name states and
  `chart-rewrite` reads.
- `chart-rewrite` is complete over **one URL against one rendered
  Service**: the `GARAM_API_URL` build argument in
  `.github/workflows/release.yml` against the api Service and port a
  render under the release name `garam` names. It reads no other build
  argument, no other workload's address and no image — what a build
  actually baked is the image's fact, and `make web-image-e2e` is what
  reads that one.
- `chart-guards` is complete over **the values named by the files in
  `ci/refused/`**: each must refuse the render and name the value its
  filename names, and `minimal-values.yaml` must still render. It reads
  no other value — a value documented as guarded and given no file
  there passes, which is why a new guard lands with its file.
- `chart-security` is complete over **the PodSecurity `restricted`
  fields of a rendered pod spec**: `runAsNonRoot: true` and a
  `seccompProfile` type of `RuntimeDefault` or `Localhost` on the pod,
  and on every container, init containers included, an integer effective
  `runAsUser`, `allowPrivilegeEscalation: false`, and `ALL` among the
  dropped capabilities. It reads the first two on the pod spec only:
  `restricted` admits them on a container instead, so a workload that
  moved them there fails this check though an apiserver would admit it.
  It
  reads no other field of a security context and asks no apiserver. What
  no pod spec carries stays outside it — which user an image actually
  runs as is the image's fact, not this render's, and nothing here reads
  it.
- `chart-package` is complete over **two resource names in the published
  artifact**: no file `helm package` ships sets `cpu` or `memory` as a
  key. It reads no third name, so a figure under `ephemeral-storage` or
  an extended resource passes, and it reads no render — a figure a
  deployment sets in its own values is that deployment's and reaches no
  file this opens.

`chart-package` reaches one thing §Not established in
`docs/architecture/deployment-contract.md` says garam does not publish:
a resource figure. Nothing stops the chart from publishing an answer to
the rest — a read-only root filesystem, replica safety above one, a
bound on startup — and each of those stays reviewed rather than checked.

Nothing checks the rest of what a pod carries — volumes and mounts,
probe paths and ports, the security-context fields `chart-security` does
not read, the hook annotations on the migrate Job. Each is reviewed
rather than checked.

Two things sit outside any of them:

- A deployment setting `serviceAccount.create: false` renders no
  account, and nothing here can see the object it supplies instead — nor
  whether the name it sets is the one its infrastructure repository
  binds a role to. That agreement is the contract §Workload identities
  states and no check in this repository can reach.
- A digest a deployment sets is a value no check here reads. Nothing
  pairs it with the tag beside it either: which digest a tag resolves
  to is the registry's fact and no target opens a registry, so a pair
  naming two different images renders, pulls the digest, and leaves the
  tag naming a version that is not running.
- `web.apiUrl` must equal the value the web image was built with, and
  nothing here can check it: the rewrite target is baked in at build
  time, so the value is the image's and no render holds it.
  `chart-rewrite` reaches the published image's half of that — what a
  release bakes — and a deployment's own image and its own value are
  outside it.

## What it is not

**It renders nothing that refreshes the key-material copy.**
§Key material and secrets states what a deployment adds and what that
addition owes. Rendering it here would move an obligation
`deployment-contract.md` §What the binary enforces about key material
places on the deployment, and what a refresher
reads, how often, and what it does with a renewed pair that does not
chain to the root already loaded are decisions rather than packaging.
#1093 carries the case for taking them; until they are taken, rotation
under this chart alone is a pod restart and no value here changes that.

**It offers no Secret reference for `api.google.clientId`.** The client
identifier is not a credential — `client_id` is a parameter of the
authorization request the browser is redirected to (RFC 6749 §4.1.1) —
so the chart renders it as a literal beside `redirectUrl`, and only
the client secret is a Secret reference. A deployment holding the
identifier in a Secret today supplies it here as a value instead,
where it lands in that deployment's values file beside every Service
and Secret name the chart already asks for.

`deploy/local/` is untouched by this chart and is not derived from it.
The compose stack and the minikube manifests there are development
artefacts with no ingress, no issued certificate, and images that exist
only on the machine that built them. Nothing in this chart is a starting
point for that, and nothing there is a starting point for this.
