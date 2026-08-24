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
garam's to assume. Nothing here is written into `garamsh/gitops`.

## What it renders

Five workloads, a `ClusterIP` Service in front of each of the four that
listen, and a `ServiceAccount` each workload runs as
(§Workload identities). Each workload's file renders all of its own
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
  and orders it itself.

Every Deployment is `replicas: 1` and the chart offers no value for it.
Replica safety above one is not established
(`docs/architecture/deployment-contract.md` §Not established), and a
chart value would assert what garam does not know. Establishing it is an
ADR, then a value.

## Workload identities

**Every workload runs as an account of its own**, and the chart creates
it. The names, under a release named `garam` — another release name
prefixes each of them, as it does every other object here:

| Workload | Kind | ServiceAccount |
|---|---|---|
| `garam-api` | Deployment | `garam-api` |
| `garam-authz` | Deployment | `garam-authz` |
| `garam-machine` | Deployment | `garam-machine` |
| `garam-migrate` | Job | `garam-migrate` |
| `garam-web` | Deployment | `garam-web` |

A workload and its account carry one name on purpose, and the chart
offers no value that changes either: a repository that binds a role to
an account looks it up by the workload's name, and a name two
repositories set separately is a name they can disagree about silently.
`make chart` reads this table and fails when the render stops matching
it (§What keeps it true), so it is the contract another repository
matches its own manifests against rather than a description of them.

**One account per workload, never one for the release.** A single
account every workload presented would be the namespace `default` under
a better name: a role scoped to it is held by every pod that presents
it, and the role the machine listener needs is one no other workload may
hold.

**Annotations are how a cloud role binds to an account**, and each
account takes its own through `<workload>.serviceAccount.annotations`.
The chart carries none: which key binds a role is the deployment's
platform's.

**The migrate Job's account is created in the same hook phase as the
Job**, at a lower weight. A `pre-install` hook runs before the chart's
ordinary resources exist, and the apiserver refuses a pod naming an
account it cannot find; a deployment supplying that one itself has it in
place before the release's first install rather than with it.

**`<workload>.serviceAccount.create: false` does not turn identity off.**
The workload still names the account above; what changes is that the
chart renders no object and the deployment supplies one under that name.
That is the path for a repository that created the account before
adopting this chart — it keeps the object it made, and adoption is not a
rename.

### Why the machine listener cannot run as `default`

`serve machine` reads the key-encryption key every organization's
certificate authority is sealed under
([ADR-0016](../../../docs/architecture/adr/0016-ca-key-custody.md)) and
refuses to start without it (`internal/cli/machine.go`). That key
arrives in the Secret `machine.keyMaterial.secretName` names, which this
chart does not create; whatever populates it grants to a subject, and
the subject to grant to is `garam-machine` rather than one every pod in
the namespace presents.

Getting it wrong fails in the right direction — the pod does not start,
and restoring the identity fixes it. What is unrepairable is the fix
that looks obvious from that failure: minting a replacement key. A
different key opens no authority already sealed and there is no path
from one key to another, so every organization loses the authority it
already has (`docs/architecture/deployment-contract.md` §The
key-encryption key cannot be regenerated, which carries what the only
remedy costs).

## Values a deployment must supply

`values.yaml` documents each. These five have no default and the render
fails without them:

- `image.server.tag`, `image.web.tag` — no release exists, so no default
  names an image that can be pulled.
- `database.secretName` — the Secret holding `GARAM_DATABASE_URL`.
- `machine.keyMaterial.secretName` — the Secret holding the machine
  listener's certificate and key, the garam server root that signed
  them, and the key-encryption key.
- `web.apiUrl` — `web/src/env.ts` refuses to boot in production without
  it.

Setting `api.google.clientId` makes `api.google.redirectUrl` and
`api.google.clientSecret` required too. One Google credential without
the other refuses the start, so the chart refuses the render instead.

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
| `GARAM_DEV_AUTH` | Development only, and it requires `GARAM_COOKIE_SECURE=false` (ADR-0010). `deploy/local/` is where it is turned on. |
| `GARAM_DEV_REDIRECT_URL` | Read only by the provider above. |

Two things sit outside that comparison:

- `PORT` is not in the table because no garam source reads it. The web
  image sets it (`deploy/local/Dockerfile.web`) and Next.js reads it, so
  the chart sets no value and names 3000 on the container port and both
  probes to match.
- `issue-certificate` is a command rather than a workload — the one
  out-of-band bootstrap (ADR-0015), run by hand against the same
  database. The three variables it reads are already rendered above for
  the workloads that need them, and whoever runs it supplies the same
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
`emptyDir`, with `fsGroup` set so it can write there.

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
been cut, and both ECR repositories are empty, so the references
`values.yaml` carries pull nothing today. `Chart.yaml` has no
`appVersion` for the same reason; the change that publishes the first
release sets it and makes a default for the two tags honest.

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
  §Workload identities. Every rendered Deployment and Job names the
  account that table pairs it with, every account a workload names is
  rendered, and a workload naming none fails rather than reaching a
  cluster as `default`.

The comparisons are what an executable artefact buys over prose: a
variable added to the source and not to the chart, or a workload added
without an identity, fails the check instead of being discovered by a
deployment.

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
  Deployment and Job runs as, and whether the accounts they name are
  rendered.

Nothing checks the rest of what a pod carries — volumes and mounts,
probe paths and ports, the security context, resource figures, the hook
annotations on the migrate Job. Each is reviewed rather than checked.

Two things sit outside any of them:

- A deployment setting `serviceAccount.create: false` renders no
  account, and nothing here can see the object it supplies instead.
- `web.apiUrl` must equal the value the web image was built with, and
  nothing can check it: the rewrite target is baked in at build time.

## What it is not

`deploy/local/` is untouched by this chart and is not derived from it.
The compose stack and the minikube manifests there are development
artefacts with no ingress, no issued certificate, and images that exist
only on the machine that built them. Nothing in this chart is a starting
point for that, and nothing there is a starting point for this.
