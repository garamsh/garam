# ADR 0042: A chart is the description of a garam deployment, and the contract keeps what a chart cannot carry

> Status: accepted
> Date: 2026-08-25

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0020](./0020-published-artifacts.md) decided that garam publishes
two images and a contract and no manifests, and its rejected
alternatives give the reason: manifests here put cluster state in two
places, and **the failure is not that both write — it is that neither
can tell which write is authoritative.**

That reason is still true. What has changed is the price of the answer
it chose. A contract kept true by prose was falsified three times in one
night:

- `#480` — the pull-secret claim, contradicted by a live
  `ImagePullBackOff`.
- `#485` — two claims contradicted by the code they describe.
- `deployment-contract.md` §The key-encryption key cannot be
  regenerated, which told a consumer the machine listener's pair was
  "safe to have issued by cert-manager or any other issuer". It
  contradicted
  [ADR-0037](./0037-the-machine-listener-presents-a-per-organization-certificate.md),
  which decided that the listener presents its organization's own
  authority, and it stays false under
  [ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md),
  which superseded it: the pair is what every agent trusts garam by, so
  an issuer that also signs other workloads is the one thing it must not
  come from. The sentence was approved without being checked against the
  ADR it contradicted and another repository built on it; #495 removed
  it.

Each was a sentence nothing could run. `docs/convention/ci.md` §One
entry point per task already says what separates a check that did the
work from one that read like it; the contract had no entry point at all,
so every claim in it rested on a reader remembering to check it.

A chart is manifests, so the two-places problem does not disappear by
naming the artefact differently. What removes it is that the chart is
the source and the cluster-owning repository holds a values file and a
version rather than a second set of manifests.

Two facts bound what may be relied on here, each read on 2026-08-25:

- **No release exists and neither image repository holds a tag.**
  `gh release list --repo garamsh/garam` returns nothing;
  `aws ecr list-images --region ap-northeast-2 --repository-name` returns
  an empty list for `garam/garam-server` and for `garam/garam-web`, both
  of which exist and are `IMMUTABLE`
  ([ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)).
  So no image reference this chart could carry pulls anything today.
- **`garamsh/gitops` has not agreed to consume this.** Nothing in that
  repository was written or read as part of this decision.

## Decision

- **`deploy/charts/garam/` is the authoritative description of a garam
  deployment, and this supersedes ADR-0020.** ADR-0020's status becomes
  superseded and its body is unchanged. Two images, immutable tags and a
  local-development loop that uses no registry stand exactly as it
  states them; what changes is that garam publishes a chart beside them,
  and that the chart rather than a document is what a consumer is
  written against.
- **The chart covers the five workloads garam runs and the Services they
  need to reach each other**: `serve api`, `serve authz`,
  `serve machine`, the web console, and the `migrate` Job. Every
  environment variable the source reads is either rendered by the chart
  or listed in the chart's README with the reason it is not — the
  adapter's seven, which belong to a pod garam does not deploy
  ([adapter.md](../adapter.md)); `GARAM_MIGRATIONS_DIR`, whose default
  already resolves inside the image; and the two behind the
  development-only sign-in provider
  ([ADR-0010](./0010-dev-auth-provider.md)).
- **The chart refuses everything about exposure, and ADR-0020's
  boundary survives intact.** Which address each process answers on and
  which of them leaves the cluster stays the cluster-owning
  repository's. A chart expresses that without deciding it by rendering
  only what one workload needs to reach another — four `ClusterIP`
  Services — and no Ingress, no Gateway, no `LoadBalancer`, and no
  `service.type` value. Exposure is added beside the chart, not
  configured inside it.
- **The chart states what a consumer must not put in front of the
  machine listener.** Nothing that terminates TLS may stand in front of
  port 8443: the listener terminates TLS itself and authenticates the
  client certificate per connection against the organization's own
  authority ([ADR-0014](./0014-agent-authentication.md)), and a
  terminating proxy strips that certificate — the failure ADR-0014 calls
  silent and total. A chart that offered an ingress value for that
  listener would be a chart that causes it, which is the second reason
  the previous point renders none. The client-CA prohibition follows the
  same shape: there is nothing to mount, so there is no value for it.
- **The chart refuses to say a replica count above one is safe.** Every
  Deployment renders `replicas: 1` and no value moves it. No leader
  election exists in the source, so a value would assert what garam has
  not established. Establishing it is an ADR, and then a value.
- **Nothing in the chart carries a secret value, and no value may become
  one.** Every credential is named as a Secret and a key the cluster
  already holds, and the chart creates no Secret. Key material reaches
  a process as a file readable only by its owner
  ([ADR-0015](./0015-operator-identity.md)); the chart renders the
  arrival shape that satisfies it — an initContainer copying the
  projected Secret as the reading UID onto a memory-backed volume — and
  generates nothing, because a regenerated key-encryption key opens no
  authority already sealed ([ADR-0016](./0016-ca-key-custody.md)).
- **The chart renders how key material arrives and not how it is
  replaced.** The machine listener re-reads its certificate and key on
  every handshake, so a replaced pair is served without a restart
  ([ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)),
  and the copy the arrival shape makes is made once at pod start. A
  refresh that keeps that copy current has to rename over the file the
  process opens — a reader that opens a half-written one refuses the
  handshake — and has to satisfy the arrival rule on every refresh
  rather than only the first. The chart renders no such refresh: what
  would perform it belongs to the cluster the deployment owns, and
  nothing here could be run against a cluster to show it works. Both
  obligations are stated in `deploy/charts/garam/README.md` §Key
  material and secrets, where whoever adds the refresh reads them.
- **`make chart` is what keeps the chart true, and `make ci` runs it.**
  It lints and renders every value file under `deploy/charts/garam/ci/`,
  and it compares the environment names the render carries against the
  names the source reads — derived from `envOr` in `internal/cli/`,
  which wraps the module's only `os.Getenv` call, and from the schema in
  `web/src/env.ts`. It fails on a name the source reads that the chart
  neither sets nor exempts, on a name the chart sets that no source
  reads, and on a name declared exempt and rendered at once. That is the
  property this whole decision is bought for: prose could not be run,
  and this can.
- **`deployment-contract.md` shrinks and changes role.** It stops being
  the artefact a consumer is written against and keeps only what no
  manifest can carry: what readiness reports and what bounds it, what
  the binary enforces about key material and the obligations a
  deployment carries for rotating it, the shutdown bound the binary
  fixes, how `migrate` fails, why the key-encryption key cannot be
  replaced, and the questions §Not established answers with "not known".
  Its ports table, its configuration table and its images table are
  deleted, because the chart renders all three and two descriptions of
  one deployment is the defect this decision exists to remove. Where the
  two could both answer, the chart is authoritative and the document
  says so. The cert-manager sentence named in §Context is not among the
  deletions: #495 removed it first, and what goes here is the
  duplication that let it stand unchecked.
- **A consumer pins the chart by a git ref of `garamsh/garam` and the
  images by tag.** Nothing packages or publishes the chart, so a tag or
  a commit SHA of this repository is the reference, and `Chart.yaml`'s
  `version` moves when the chart changes and names no garam version.
  The images keep ADR-0039's rule — a `vX.Y.Z` git tag, the image tag
  without the `v`, one version across the pair — supplied through
  `image.server.tag` and `image.web.tag`, which are required rather
  than defaulted because no default could name an image that exists.
  `Chart.yaml` carries no `appVersion` for the same reason. The change
  that publishes the first release sets it and makes those defaults
  honest at once.
- **Consumption is `garamsh/gitops`' to accept, and this decision does
  not assume it.** Nothing is written into that repository here. Until
  it adopts the chart, the chart is a readable description rather than
  an applied one, and a consumer writing its own manifests reads
  `values.yaml` and the templates instead of a document that might have
  drifted.
- **`deploy/local/` is untouched.** The compose stack and the minikube
  manifests remain development artefacts, and ADR-0020's decision for
  local development — build into the local cluster's own daemon and use
  no registry — stands unchanged.

## Consequences

- **Cluster state is in one place only while the consumer holds a
  values file rather than manifests.** If `garamsh/gitops` adopts the
  chart and then patches the rendered output, ADR-0020's failure
  returns in a form that is harder to see, because both sides can point
  at the chart. Nothing here can prevent that; what it can do is leave
  no reason to.
- **garam now owes a second artefact's accuracy to a repository it does
  not control.** The check makes drift in the configuration surface
  loud, and it makes nothing else loud: a probe path, a port, a mount
  path or a hook annotation can still go wrong silently, and no render
  catches a manifest an apiserver would reject.
- **A contributor who changes an environment variable now has a check to
  satisfy.** Adding one to the source fails `make ci` until the chart
  either sets it or the README says why not. That is the cost the check
  is bought with, and it is paid by the change that creates the
  obligation rather than by whoever deploys next.
- **`make ci` now depends on a Helm binary.** It comes from a module pin
  in the `Makefile` — the same `go run tool@version` shape the five
  generators already use — so nothing is installed by hand, and the
  first run downloads and builds it.
- **A deployment cannot take this chart and run it today.** Both image
  tags are required and neither repository holds one, so the first thing
  a consumer meets is a failed render rather than a failed pull. That is
  the intended order.
- **The contract's shrinking loses a reader's single place to look.**
  What was one document is now a chart and a smaller document, and a
  reader who wants the configuration surface reads `values.yaml`. The
  compensation is that `values.yaml` cannot be wrong about what the
  chart renders, and the paragraph it replaced could.
- **Rotating the machine listener's pair costs a restart under this
  chart alone.** garam's half of the mechanism is built and the chart's
  half is not: the pair is re-read per handshake, and what the chart
  renders is a copy made once. A deployment that wants rotation without
  a restart supplies the refresh itself, and a deployment that does not
  is where it was before ADR-0040 — nothing regresses, and what it pays
  is a restart each time the listener's certificate is replaced.
- **The five workloads are described twice in this repository** — here
  and in `deploy/local/minikube/`. They are not the same description:
  the local manifests carry a dev sign-in provider, throwaway key
  material and images that exist on one machine. Keeping them apart is
  deliberate, and a change to a port or a probe path touches both.

## Rejected alternatives

- **Keeping ADR-0020 and hardening the contract instead** — a check that
  reads the document and compares its tables against the source. It
  needs no new artefact and it leaves the consumer writing its own
  manifests, which is the duplication that put a false TLS sentence into
  another repository. A check that parses prose is also a check that
  goes stale against a reworded paragraph, and the paragraph is the
  thing being kept honest.
- **Publishing the chart to a registry.** It gives a consumer a version
  to pin that is not a git ref, and it needs a release pipeline, a
  repository and a credential, none of which exist —
  [ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  records that even the image half is unbuilt. A git ref is pinnable
  today and can be replaced by a published chart later without changing
  what the chart says.
- **Retiring `deployment-contract.md` entirely.** One artefact rather
  than two, and it would put the readiness semantics, the shutdown
  bound and §Not established either into the chart's README — where
  they are not about the chart — or nowhere. What a process does is not
  what a manifest says, and the document that survives is the one for
  facts a manifest has no field for.
- **A `service.type` value, or an ingress the machine listener opts out
  of.** It is what most charts do and it would make the chart usable
  without anything beside it. It also puts the one exposure that must
  never terminate TLS behind a flag, and a flag is a thing a consumer
  sets without reading why it exists. Rendering no exposure at all is
  the version of this decision that cannot be misconfigured.
- **Generating the key-encryption key in the chart on first install.**
  It removes the one bootstrap a consumer must perform out of band, and
  a second install, a re-render, or a restored backup would mint a
  different key and leave every certificate authority in the database
  unopenable ([ADR-0016](./0016-ca-key-custody.md)).
- **Extending `deploy/local/minikube/` into the deployable manifests.**
  The shapes are already close and the divergence is the point: the
  local stack turns on a sign-in provider that must never run elsewhere,
  and it is built to prove processes schedule rather than to reconcile a
  cluster.
