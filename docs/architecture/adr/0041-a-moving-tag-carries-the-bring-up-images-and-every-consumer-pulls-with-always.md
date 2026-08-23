# ADR 0041: A moving tag carries the bring-up images, and every consumer pulls with Always

> Status: accepted
> Date: 2026-08-24

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0040](./0040-a-development-pair-carries-bring-up-images-until-the-first-release.md)
settled the bring-up path: two development repositories of their own,
both `IMMUTABLE` with scan-on-push, one tag per commit, pushed by a
person because no pipeline can. The account owner, who owns the
registry, has since decided that the two are `MUTABLE` and carry a
development tag pushed directly. That decision is recorded here rather
than reopened. What is settled here is the shape that tag takes and
what has to hold now that immutability does not.

Facts, each read on 2026-08-24:

- **Immutability was load-bearing in ADR-0040 rather than
  decorative.** ADR-0040 §Decision: "`imagePullPolicy: IfNotPresent` is
  the default for a tagged image, so a tag that moves is not re-pulled
  by a node already holding it: the cluster runs the older bytes while
  the registry says otherwise, and the fault presents as the node's."
  The default it rests on is Kubernetes': a container spec that leaves
  `imagePullPolicy` unset takes `IfNotPresent` for any tag but
  `:latest`, and `Always` for `:latest` or for no tag at all. Removing
  immutability without replacing that guard opens the hole the sentence
  describes.
- **The per-commit tag has already failed once, and #458 is the
  record.** `garam/garam-web-dev:24bf2201549f` was pushed from
  `24bf2201549f618e99b569bd1ab0c88ef4fa9c1a`, which `git branch -r
  --contains` finds on `origin/sauce-git/i456` and on no other ref.
  `docs/convention/git.md` §PRs squash-merges every PR and the branch
  is deleted, so that commit becomes unreachable while the tag naming
  it sits in an `IMMUTABLE` repository and can be neither moved nor
  removed. The repair taken was to keep the branch alive, which leaves
  a published tag's meaning resting on nobody tidying up.
- **Every bring-up image is a hand push and will stay one.** No
  workflow pushes to a registry (`.github/workflows/ci.yml`), and
  `AGENTS.md` §This project records why none can run. So a scheme that
  mints a new tag per push spends an edit in the consuming repository
  on every image — ADR-0040 §Consequences accepted that cost, and it is
  the cost the owner's decision removes.
- **The registry holds images in both development repositories
  already.** `aws ecr describe-repositories --region ap-northeast-2`
  returns nine repositories, every one `IMMUTABLE` with scan-on-push;
  `aws ecr list-images` returns nothing for `garam/garam-server` and
  `garam/garam-web`, one tag (`3b9341d934c5`) for
  `garam/garam-server-dev`, and two (`3b9341d934c5`, `24bf2201549f`)
  for `garam/garam-web-dev`. Mutability is therefore a change to two
  repositories that have been pushed to, not a setting chosen while it
  is still free.
- **Nothing pins either bring-up reference yet.** `garamsh/gitops`
  holds no manifest for either image: `apps/` names platform components
  alone, `charts/` holds no chart for garam, and
  `clusters/garam-dev/values/` is empty. An obligation put on a
  consumer now lands before the first manifest is written rather than
  after.
- **Scan-on-push works and covers less than its name suggests.**
  `garamsh/infra` #131, with readings dated 2026-08-24, records what
  ECR BASIC scanning reaches on a buildx push: coverage is decided per
  artifact, the web image's platform child scanned `COMPLETE` with no
  findings, the server image's child is refused for carrying no OS
  package database, and both attestation children are refused by media
  type.

## Decision

- **ADR-0040 is superseded whole, not in part.** Every rule that still
  holds is restated below, so applying the bring-up path means reading
  this ADR and nothing else.
- **The two development repositories become `MUTABLE` and keep
  scan-on-push.** `garamsh/infra` owns `image_tag_mutability` and makes
  the change against this ADR; nothing here flips it, and no image
  already pushed is re-pushed, retagged, or removed for it.
- **The development tag is `dev`, one per repository, and it moves.**
  `garam/garam-server-dev:dev` and `garam/garam-web-dev:dev`. Each push
  repoints it at the bytes just built, so a manifest names it once and
  is never edited to take newer ones. It is not `latest`: that is
  Docker's implicit tag, so an unqualified reference to the repository
  would resolve to a bring-up build.
- **A commit-derived tag is pushed beside it, on the same bytes, every
  time.** The first twelve characters of the commit the build came
  from — ADR-0040's shape kept. It costs one `docker tag` and one push,
  and it is the only thing that says which source produced the bytes
  now serving; a moving tag alone destroys that record each time it
  moves. ADR-0040's `-2` and `-3` ordinals for a rebuilt commit are
  dropped: a second build of one commit repoints that tag too, so a
  commit tag names the last build of its commit rather than every
  build.
- **A bring-up commit tag records what was built and promises nothing
  about fetching it.** It names the commit the bytes came from at push
  time; nothing holds that commit reachable, and `git.md` §PRs squashes
  branch commits away. This is what resolves #458 — the guarantee that
  issue is about is not made here, so it cannot be broken — and it is
  why `sauce-git/i456` stops being held open for
  `garam/garam-web-dev:24bf2201549f` and is deleted like any other
  merged branch. A tag that names the wrong thing is now movable, so a
  mistake is repairable rather than spent.
- **A consumer that pulls either bring-up image sets
  `imagePullPolicy: Always`, and
  [deployment-contract.md](../deployment-contract.md) §Images carries
  that as an obligation on whoever writes the manifest.** It is written
  there rather than only here because the repository that writes
  manifests is written against the contract
  ([ADR-0020](./0020-published-artifacts.md) §Decision), and this is
  exactly what that contract exists to publish: a property of garam's
  artifacts a cluster owner cannot infer. The obligation binds the
  commit tag as much as `dev` — both sit in a mutable repository, so
  neither is evidence of the bytes behind it.
- **Nothing bring-up is published and nothing is a release.** No
  version, no git tag, no GitHub release, no recorded digest, and no
  reference a consumer may read as part of the contract. No bring-up
  tag takes the `X.Y.Z` form: that shape means a release and only the
  release path mints one. The two references in
  deployment-contract.md §Images remain the whole of what this project
  publishes.
- **No bring-up push touches `garam/garam-server` or
  `garam/garam-web`, and both stay `IMMUTABLE`.** An administrator's
  identity can reach them, so what holds the two paths apart is this
  rule and two distinct names rather than a permission boundary. A
  version number pushed there by mistake is spent for good.
  [ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  is unamended by this: a release still takes a `vX.Y.Z` git tag alone,
  the OIDC role still reaches nothing else, and the two repositories a
  manifest pins still hold released versions and no build residue.
- **The push is a person's, under their own AWS identity.** No
  credential is created, stored, or widened for it — the operator
  authenticates an SSO session they already hold, which is short-lived
  and never enters this repository. That is what
  `docs/convention/ci.md` §Security baseline prefers over a long-lived
  key; it governs a pipeline's credential and no pipeline holds this
  one. ADR-0039's OIDC role is not extended to the development pair,
  which is what `garamsh/infra` #128 and #129 keep outside the input
  that role's policy is built from.
- **What ends this arrangement is the release path publishing its first
  image** — `aws ecr list-images --repository-name garam/garam-server`
  returning one. The two development repositories are emptied and
  removed then, not deprecated and not kept for convenience, and the
  change that removes them carries the ADR superseding this one
  together with the edit dropping the bring-up paragraphs and their
  pull-policy obligation from deployment-contract.md §Images
  (`docs/architecture/README.md` rules 2 and 3).

## Consequences

- **A pod pulling a bring-up image cannot start while the registry is
  unreachable, even on a node that already holds the bytes.** That is
  what `Always` asks of the kubelet, and it is paid on every container
  start rather than every deploy. It is the price of the moving tag,
  and it is one more way a development cluster's failures do not
  resemble a released one's: the release pair's tags are immutable, so
  a manifest pinning a version stays correct under the default policy.
- **"What is deployed" stops being answerable from the manifest.**
  `:dev` names whatever was pushed last, so the answer moves into the
  registry, where the commit tag sharing a digest with `dev` carries
  it. ADR-0020 §Decision refuses that for a release; it is accepted
  here for the interval this ADR covers and ends with it.
- **A manifest edit becomes the way to roll back, and the only thing it
  is for.** The previous bytes stay in the repository under their own
  commit tag, so going back means pinning that tag in place of `dev`.
  The per-push edit ADR-0040 forced is paid only when someone
  deliberately pins something other than the latest build.
- **Every bring-up tag from here is unproven, the commit tags
  included.** ECR can be switched back to `IMMUTABLE`, but a tag pushed
  while the repository was mutable stays unproven (ADR-0039
  §Consequences). Nothing in these two repositories is evidence of its
  own bytes afterwards, which is un-spendable in one direction only and
  is why the release pair is not touched.
- **The three tags already pushed gain no protection and lose their
  guarantee.** `3b9341d934c5` in both repositories and `24bf2201549f`
  in the web one were pushed under ADR-0040. They are left as they are,
  and once the repositories are mutable they can be moved like any
  other tag.
- **The repositories still accumulate without bound.** Every push adds
  a commit tag and leaves the digests before it in place; no lifecycle
  rule is asked for — `garamsh/infra` #128 leaves retention open
  deliberately — so what bounds them is the removal above rather than
  expiry, which makes that event one to act on rather than one to
  notice.
- **Scan-on-push behaves as it did.** A scan fires on each push, so
  bytes arriving under a moved tag are scanned as the bytes before them
  were, and coverage is unchanged: per artifact, refusing the server
  image's child for carrying no OS package database and both
  attestation children by media type (`garamsh/infra` #131, read
  2026-08-24). Mutability changes none of it.
- **Success on the development cluster does not prove the release path
  works.** The trigger, the workflow and the role are unbuilt, and
  nothing here exercises any of them. ADR-0020 §Consequences accepted
  the same divergence for local development; this narrows it to the
  credential and the trigger without closing it.
- **A bring-up tag identifies the commit, not everything that went into
  the build.** The web image bakes its `/api/*` rewrite target at build
  time (`ARG GARAM_API_URL`, `deploy/local/Dockerfile.web`), so two
  images built from one commit can differ in it — and under a moving
  tag they can now carry the same tag as well. Whoever pushed the
  second build is the only record of what changed.
- **The bytes come from an operator's machine.** No pipeline builds
  them, so nothing but that operator's own clean checkout ties a tag to
  the commit it names, and the scan on push is the only check the image
  meets. Replacing that is what the release path is for, and until it
  runs this is the standard a bring-up image is held to.

## Rejected alternatives

- **Keeping ADR-0040 as it stands.** It answers "what is deployed" from
  the manifest alone, asks nothing of a consumer's pull policy, and
  makes every new image a two-repository coordination step — build,
  push, edit the manifest pinning the old tag, merge that — on a path
  where the build is already a person's own. It also carries a
  guarantee that has failed once (#458) and whose repair is an
  undeleted branch per image.
- **`latest` as the moving tag.** Kubernetes defaults
  `imagePullPolicy` to `Always` for `:latest`, so the hole would close
  with nothing asked of a consumer — and the safety would then be a
  property of a tag's spelling rather than a stated rule: a manifest
  that sets `IfNotPresent` explicitly loses it silently, and so does
  any puller that is not a kubelet. `latest` is also Docker's implicit
  tag, so a reference to the repository with no tag at all would
  resolve to a bring-up build.
- **The moving tag alone, with no commit tag beside it.** One `docker
  tag` and one push cheaper per image, and nothing then maps the bytes
  now serving to a commit: the operator who pushed becomes the only
  record of what was built, and the bytes before them are reachable by
  digest alone.
- **Mutable repositories keeping one tag per commit and no moving
  tag.** It keeps the record and needs no pull-policy obligation, since
  a tag nothing has pulled before is pulled under any policy — and it
  leaves the per-push manifest edit exactly where ADR-0040 left it,
  which is the cost this decision exists to remove. Mutability would
  then buy only the ability to repair a wrong tag.
- **Pinning a digest in the consuming manifest instead.** A digest is
  the bytes, so nothing can move underneath it and no pull policy has
  to be asked for — and it is the per-push manifest edit again, in the
  form ADR-0020 §Rejected alternatives already declined for the
  published pair, with a reference nobody can read as a commit.
- **Stating the pull policy in this ADR alone.** One file fewer to
  change, and the repository that writes the manifest is written
  against deployment-contract.md rather than against these ADRs
  (ADR-0020 §Decision), so the rule would sit where its audience does
  not read: a note rather than an obligation, and a contract that omits
  it breaks someone else's deployment silently, which is the failure
  ADR-0020 §Consequences names.
