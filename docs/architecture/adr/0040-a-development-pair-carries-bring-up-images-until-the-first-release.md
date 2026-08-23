# ADR 0040: A development pair carries bring-up images until the first release

> Status: accepted
> Date: 2026-08-24

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
settled the release path — two repositories, an immutable version tag,
a `vX.Y.Z` git tag as the only trigger, a GitHub Actions OIDC role as
the credential — and none of it can run. A development cluster is being
stood up now and needs an image before any release can exist, which is
a case ADR-0039 does not cover: it settles what a release does, and
there is no release to do it.

Facts, each read on 2026-08-24:

- **The release path cannot fire.** The last `ci` run to succeed is
  2026-08-18T21:24:01Z, and every `ci` run since concluded `failure`
  (`gh run list --repo garamsh/garam --limit 100 --json
  conclusion,createdAt,name,event`). Two later runs conclude `success`
  and neither is a workflow doing work: both are dependency-graph
  events (`event: dynamic`) that execute no step. `AGENTS.md` §This
  project records the cause the account owner gives — the `garamsh`
  Actions quota is exhausted — and that this repository cannot check
  it.
- **Both release repositories now exist and both are empty.**
  `aws ecr describe-repositories --region ap-northeast-2` lists
  `garam/garam-server` and `garam/garam-web`, both `IMMUTABLE` with
  scan-on-push, created 2026-08-23, and `aws ecr list-images` returns
  no image for either. It no longer lists `garam/garam`, which ADR-0039
  §Decision left unused for the account's owner to remove.
  `garamsh/infra` #96 is the request the pair came from.
- **The release push credential exists and reaches exactly those two.**
  `aws iam get-role-policy --role-name garam-dev-ecr-push --policy-name
  ecr-push` allows the five push actions on the `garam/garam-server`
  and `garam/garam-web` ARNs and on no other resource.
- **A repository is pullable the moment it exists.** The cluster's pull
  role grants `ecr:BatchGetImage` and its two companions on
  `repository/*` in the account (`garamsh/infra`
  `modules/aws/ecr-pull.tf`), assumed by the kubelet's credential
  provider, so a new repository needs no pull secret and no change
  there.
- **Both development repositories now exist and both are empty.**
  `aws ecr describe-repositories --region ap-northeast-2` lists
  `garam/garam-server-dev` and `garam/garam-web-dev`, both `IMMUTABLE`
  with scan-on-push, created 2026-08-24, and `aws ecr list-images`
  returns no image for either. `garamsh/infra` #128 (still open, read
  2026-08-24) is the request they came from, and asks for them held in
  an input of their own precisely so the push role above does not come
  to reach them.
- **Local development is a different thing and is untouched.** `make
  minikube-images` builds `garam:local` and `garam-web:local` into the
  local cluster's own daemon and uses no registry (`Makefile`), which
  is what [ADR-0020](./0020-published-artifacts.md) §Decision fixed. A
  cluster whose nodes are not the machine that built the image cannot
  be fed that way.

One sentence of ADR-0039 §Decision decides whether any of this is open
at all: "Pushing that tag to GitHub is the only trigger that publishes.
A merge to `main` builds and tests and pushes nothing, so the registry
holds released versions and no build residue." It is read here against
the bullets around it, which define publishing as both images under one
version taken from a `vX.Y.Z` git tag, into the two named repositories,
with each digest recorded in the GitHub release that tag creates. Its
subject is the release path and the repositories that path writes to,
and what it refuses is an automated push per merge accumulating where a
manifest pins.

## Decision

- **Bring-up images go to two development repositories of their own:
  `garam/garam-server-dev` and `garam/garam-web-dev`** — the same
  registry, account and region as the release pair, both `IMMUTABLE`
  with scan-on-push. This ADR creates nothing: the registry's owner
  does, and `garamsh/infra` #128 is the request.
- **Immutability is not a formality on a development repository.**
  `imagePullPolicy: IfNotPresent` is the default for a tagged image, so
  a tag that moves is not re-pulled by a node already holding it: the
  cluster runs the older bytes while the registry says otherwise, and
  the fault presents as the node's. Immutability makes that impossible
  rather than discouraged, and it is free only before the first push.
- **A bring-up tag names the commit, and no two pushes share one.** The
  tag is the first twelve characters of the commit SHA; where one
  commit is built twice, the second push appends `-2` to that and the
  third `-3`. The commit is on GitHub before the push, so the tag names
  source that someone who did not build it can fetch. No bring-up tag
  takes the `X.Y.Z` form: that shape means a release, and only the
  release path mints one.
- **The push is a person's, under their own AWS identity.** No
  credential is created, stored, or widened for it — the operator
  authenticates an SSO session they already hold, which is short-lived
  and never enters this repository. That is what
  `docs/convention/ci.md` §Security baseline prefers over a long-lived
  key; it governs a pipeline's credential and no pipeline holds this
  one. ADR-0039's OIDC role is not extended to the development pair,
  which is what `garamsh/infra` #128 keeps outside the input that
  role's policy is built from.
- **No bring-up push touches `garam/garam-server` or
  `garam/garam-web`.** An administrator's identity can reach them, so
  what holds the two paths apart is this rule and two distinct names,
  not a permission boundary. Both are `IMMUTABLE` and empty, and a
  version number pushed there by mistake is spent for good.
- **Nothing here is published and nothing here is a release.** No
  version, no git tag, no GitHub release, no recorded digest, and no
  reference a consumer may read as part of the contract.
  [deployment-contract.md](../deployment-contract.md) §Images carries
  the two published references and marks these two as bring-up only.
- **This complements ADR-0039 and amends nothing in it.** ADR-0039
  governs the path that publishes a release; this governs the interval
  before that path can run once. Every sentence of ADR-0039 §Decision
  holds unchanged after this one: the release pair still takes an image
  from a `vX.Y.Z` tag alone, the OIDC role still reaches nothing else,
  and the two repositories a manifest pins still hold released versions
  and no build residue. That guarantee binds this decision rather than
  merely permitting it, and the termination below is how it is kept:
  bring-up churn stays out of the release pair without being parked
  next door for good.
- **What ends this ADR is the release path publishing its first
  image** — `aws ecr list-images --repository-name garam/garam-server`
  returning an image. The two development repositories are emptied and
  removed then, not deprecated and not kept for convenience, and the
  change that removes them carries both the ADR that supersedes this
  one and the edit dropping the bring-up references from
  `deployment-contract.md` §Images (`docs/architecture/README.md` rules
  2 and 3).

## Consequences

- **The development repositories accumulate without bound while they
  live.** Every push is a new immutable tag and no lifecycle rule is
  asked for — `garamsh/infra` #128 leaves retention open deliberately.
  What bounds them is the removal above rather than expiry, which makes
  that event one to act on rather than one to notice.
- **A manifest pinning a bring-up tag is edited on every push.** A tag
  that cannot move means the repository owning the cluster changes an
  image reference each time it wants new bytes. That cost is paid by
  the consumer, and it is another reason the interval this ADR covers
  is meant to be short.
- **Success on the development cluster does not prove the release path
  works.** The trigger, the workflow and the role are unbuilt, and
  nothing here exercises any of them. ADR-0020 §Consequences accepted
  the same divergence for local development; this narrows it to the
  credential and the trigger without closing it.
- **A bring-up tag identifies the commit, not everything that went into
  the build.** The web image bakes its `/api/*` rewrite target at build
  time (`ARG GARAM_API_URL`, `deploy/local/Dockerfile.web`), so two
  images built from one commit can differ in it while their tags differ
  only by the ordinal. Whoever pushed the second build is the only
  record of what changed.
- **The bytes come from an operator's machine.** No pipeline builds
  them, so nothing but that operator's own clean checkout ties a tag to
  the commit it names, and the scan on push is the only check the image
  meets. Replacing that is what the release path is for, and until it
  runs this is the standard a bring-up image is held to.

## Rejected alternatives

- **Hand-pushing bring-up images into `garam/garam-server` and
  `garam/garam-web`.** It asks for no new repository and gives a
  manifest its final names at once, and it spends version numbers on
  experiments where a tag can never be reused — push `0.1.0` wrongly
  once and that number is gone. It also makes ADR-0039's "released
  versions and no build residue" false in the two repositories that
  sentence is about.
- **Making the release pair `MUTABLE` for the bring-up and switching it
  back afterwards.** It needs nothing new either, and it gives away the
  one thing ADR-0020 §Decision calls cheap only before the first push:
  ECR can be switched later, but a tag pushed while the repository was
  mutable stays unproven (ADR-0039 §Consequences).
- **One moving tag on the development pair — `dev`, or `latest`.** The
  manifest would be written once and never edited again, and under the
  default `IfNotPresent` a node already holding that tag never pulls
  the new bytes, so what runs and what the manifest says diverge with
  nothing to read that admits it.
- **Loading images onto the cluster's nodes out of band, the way local
  development loads one daemon.** It asks nothing of the registry's
  owner, and it needs a path onto every node for every image, on a
  cluster reconciled from git by a repository that expects to pull
  one.
- **Waiting for the Actions quota.** It costs nothing to build and
  leaves one path to the registry rather than two, and it blocks the
  bring-up on an account payment this repository can neither make nor
  check (`AGENTS.md` §This project). That wait has no stated end; this
  decision does.
