# ADR 0039: A version tag publishes both images to the account's registry

> Status: accepted — §Decision's "Pushing that tag to GitHub is the
> only trigger that publishes" is superseded in part by
> [ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md),
> and superseded for non-release artifacts by
> [ADR-0092](./0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md),
> whose registry, commit tag, publishing from `dev` and retention are
> in turn superseded by
> [ADR-0093](./0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)
> Date: 2026-08-22

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

[ADR-0020](./0020-published-artifacts.md) decided *what* garam
publishes — two images, immutable tags, and a contract — and decided
nothing about where they go or what puts them there. Nothing since has
closed that. `.github/workflows/` holds one file, `ci.yml`, whose four
jobs (`ci`, `web`, `e2e`, `web-e2e`) run checks and push nothing;
`gh release list --repo garamsh/garam` returns nothing; the `Makefile`
names no registry target, and every occurrence of "registry" in it is a
comment saying the local stack uses none. So
`deployment-contract.md` §Images tells a consumer what is inside each
image and gives it no way to obtain one, which is what `garamsh/gitops`
needs before it can deploy garam at all.

Facts outside this repository, each read on 2026-08-22:

- The account's registry is ECR in `486152169996`, `ap-northeast-2`. It
  holds three repositories: `garam/garam` (MUTABLE, no scan-on-push,
  and empty), `garam/gagent-agent` and `garam/gagent-workspace` (both
  IMMUTABLE with scan-on-push) — `aws ecr describe-repositories
  --region ap-northeast-2`, `aws ecr list-images --repository-name
  garam/garam`.
- The cluster can already pull from that registry without a pull
  secret. Its kubelets run the ECR credential provider
  (`garamsh/infra` `modules/cluster/images.tf`), against a role whose
  policy grants `ecr:BatchGetImage` on every repository in the account
  (`garamsh/infra` `modules/aws/ecr-pull.tf`), so a repository is
  pullable as soon as it exists. ADR-0020 §Decision rested on neither a
  laptop nor the target cluster holding a registry credential that
  stays valid; the cluster half of that has since been built, and the
  laptop half is untouched.
- No `aws_ecr_repository` resource exists anywhere in `garamsh/infra`
  (`grep -rn aws_ecr_repository`), and that is a boundary rather than
  an omission: its `docs/architecture/aws.md` §Current decisions lists
  the five things the module owns — the image *pull role* among
  them — and says "Nothing else in the account is managed here". So the
  registry's repositories are owned by nobody this repository can
  address as a module, and creating one is not garam's action to take.
- The account already trusts GitHub's OIDC issuer: `garamsh/infra`
  `modules/aws/pipeline-identity.tf` reads a
  `token.actions.githubusercontent.com` provider rather than creating
  it, and gives that repository a role of its own whose trust is scoped
  by subject to that repository alone (`garamsh/infra`
  `environments/dev/aws/main.tf`). No workflow here can assume it.
- The consumer reconciles continuously with `automated.selfHeal: true`
  (`garamsh/gitops` `apps/cert-manager.yaml:18`, `apps/openebs.yaml:18`,
  `manifests/applicationset.yaml:32`, and four more), so a tag that
  could be repointed would leave "what is deployed" unanswerable from
  the manifest — the failure ADR-0020 §Decision already names. The one
  image that repository pins today it pins by tag
  (`manifests/amazon-eks-pod-identity-webhook/deployment.yaml:61`).
- The sibling decided the same shape and records it in `garamsh/gagent`
  `docs/architecture/deployment.md` §Images and tags: ECR,
  `ap-northeast-2`, two IMMUTABLE repositories with scan-on-push and a
  lifecycle policy, and a `v`-prefixed git tag whose image tag drops
  the `v`. The same document records that its release workflow and its
  IAM role do not exist yet either.

One fact about this repository bounds what may be relied on. Every
Actions run since 2026-08-18T23:03:42Z has concluded `failure`, the
last success being 2026-08-18T22:15:56Z (`gh run list --repo
garamsh/garam --limit 100 --json conclusion,createdAt`), and the most
recent of them recorded four failed jobs with zero steps executed
(`gh run view 32265642364 --repo garamsh/garam --json jobs`). A trigger
named below is therefore a decision, not a demonstrated mechanism.

## Decision

- **Both images are published to ECR in account `486152169996`, region
  `ap-northeast-2`** — the registry the cluster is already configured
  to pull from and the one the sibling publishes to. A second registry
  would put a second credential path on a cluster already wired for
  this one.
- **garam requires two repositories of whoever owns the registry:
  `garam/garam-server` and `garam/garam-web`.** Neither exists, and
  this ADR creates nothing. It fixes the names, so the work that
  creates them and the manifests written against them need agree on
  nothing else first.
- **The names carry the project and the runtime.** `garam/` is the
  account's registry namespace rather than this project's name —
  `gagent`'s two images live under it — so the segment after it has to
  say both: `garam-server` for the Go binary every serve subcommand
  and `migrate` run from (`deploy/local/Dockerfile`), `garam-web` for
  the Next.js application (`deploy/local/Dockerfile.web`). It is the
  shape `garam/gagent-agent` already takes. What ADR-0020 §Decision
  refuses is a name saying only the project, which is why `garam/garam`
  is not one of the two.
- **Both are required to be IMMUTABLE, with scan-on-push.**
  Immutability is what ADR-0020's tag rule needs from the repository
  rather than from a workflow, and the choice is free only while
  nothing has been pushed and nothing can push — which is the state
  checked above and the reason to fix it now. Retention is the
  registry owner's to set; `garam/gagent-agent` carries a lifecycle
  policy and this decision asks for none.
- **garam publishes nothing to `garam/garam`.** It is named for the
  project and is MUTABLE, and it holds no image, so leaving it unused
  costs nothing. Removing it belongs to whoever owns the account.
- **A release is a `vX.Y.Z` git tag, and the image tag is that tag
  without the `v`**: `v0.3.0` in git is `0.3.0` in both repositories.
  One commit produces both images under one version, so a manifest
  pins one version across the pair.
- **What a manifest pins is the immutable tag, not a digest.** That is
  what ADR-0020 bought by making tags immutable: the tag answers "what
  is deployed" on its own, and a consumer reconciling with self-heal
  needs nothing further. It is also the reference that can be written
  before the image exists, which matters while none does. The release
  records each image's digest alongside its tag in the GitHub release
  the tag creates, so a consumer that would rather pin a digest reads
  one instead of discovering it; under immutability the two name the
  same bytes.
- **Pushing that tag to GitHub is the only trigger that publishes.** A
  merge to `main` builds and tests and pushes nothing, so the registry
  holds released versions and no build residue.
- **The push credential is a GitHub Actions OIDC role in the same
  account, asked of `garamsh/infra`** — trusted for this repository's
  OIDC subject only, and holding `ecr:GetAuthorizationToken` with the
  push actions on the two repository ARNs and nothing else. This
  repository stores no AWS access key:
  `docs/convention/ci.md` §Security baseline prefers short-lived
  federated credentials, and the `id-token: write` permission is granted
  on the publishing job alone.
- **None of it is built.** No release workflow exists, the role does
  not exist, and neither repository exists. What is settled here is the
  registry, the names, the tag rule, the trigger and the credential
  shape, so the issues that build those have nothing left to decide and
  `garamsh/gitops` can write against names that will not change under
  it.

## Consequences

- **A cluster cannot pull garam until the workflow, the role and the
  repositories exist.** `deployment-contract.md` §Images carries the
  references and says plainly that nothing is published, so a reader
  pinning one learns it there rather than from a failed rollout.
- **This asks two things of people outside this repository**: two ECR
  repositories from whoever owns the registry, and a push role from
  `garamsh/infra`. Which of them owns the repositories is open there,
  not here — `garamsh/infra`
  `docs/architecture/aws.md` §Current decisions puts them outside its
  module today. Immutability is set when a repository is created — ECR
  can be switched afterwards, but a tag pushed while it was mutable
  stays unproven.
- **A published web image is bound to one API URL.** The `/api/*`
  rewrite target is a build argument baked into the image (`ARG
  GARAM_API_URL` in `deploy/local/Dockerfile.web`, read by
  `web/next.config.ts`), so a release either fixes one value for every
  consumer of that image or the value becomes a runtime one first. That
  choice belongs to the change that builds the release, and until it is
  made `deployment-contract.md` §Not established says the answer is not
  known.
- **An immutable tag will name bytes the commit alone does not
  reproduce.** Both Dockerfiles name their base images by tag
  (`golang:1.26.5-alpine3.24`, `node:24.18.1-alpine3.24`), and a tag
  moves, so two builds of one commit can differ while the image tag
  says they cannot.
  `docs/convention/ci.md` §Verify a pinned dependency before adopting it
  asks for a reference that cannot come to mean something else;
  satisfying it is a change to those two files, not to
  this decision.
- **The image tag is the only place a version appears.** Nothing stamps
  one into the binary (`deploy/local/Dockerfile` builds with no
  `-ldflags`), so a running process cannot report which release it is
  and a deployment reads that from the image reference alone.
- **A release that pushes one image and fails on the other leaves the
  version half-published.** Immutability means the repair is the next
  version, never a re-push of the same one.

## Rejected alternatives

- **Publishing both images into the existing `garam/garam`, separated
  by a prefix in each tag.** It asks for no new repository, and it puts
  two runtimes in one lifecycle — a retention rule counting images
  counts both — while spending the version field on saying which image
  this is. ADR-0020 asked the *name* to say which is which.
- **`garam/server` and `garam/web`, dropping the project segment.** It
  reads exactly as ADR-0020 words it, and the `garam/` namespace holds
  another project's images already, so a bare runtime word claims a
  name a sibling may need and stops saying whose server it is.
- **GitHub Container Registry.** No role to ask for and no repository
  to create, and it hands the cluster a second registry to hold a
  credential for when the one it is already wired for needs none, for
  two projects that would then publish to two places.
- **A long-lived AWS access key in a repository secret.** It removes
  the dependency on `garamsh/infra` entirely, and it puts a credential
  nobody rotates where every workflow run can read it
  (`docs/convention/ci.md` §Security baseline).
- **Publishing every commit on `main` under its SHA.** It would give a
  cluster something to deploy before anyone cuts a version, and it
  makes a registry out of a build cache: what a manifest pins is a
  release, and nothing consumes an untagged commit today.

## Errata

- 2026-08-27 — §Context's and §Consequences' "`deployment-contract.md`
  §Images" name a section that document no longer carries. It carried
  one when this ADR merged: #362 added it on 2026-08-18 and #500
  removed it on 2026-08-25, three days later, by making the chart at
  `deploy/charts/garam/` the description of a deployment and moving the
  image references into it. What the pointer was carrying is still
  recorded, at a different address: the references are in
  `deploy/charts/garam/values.yaml` and the templates beside it — which
  `deployment-contract.md` says in its own second paragraph — and the
  chart's `README.md` §Pinning a version is what now states that
  neither image exists yet. That address is what a reader following
  §Consequences' "a reader pinning one learns it there rather than from
  a failed rollout" needs, and it is what the pointer no longer
  supplies: that second paragraph is reached by reading past the
  section one came for, not by following anything the pointer carries.
  The decision is untouched — a version tag still publishes both images
  to the account's registry, under the names, the tag rule, the trigger
  and the credential shape decided here.

- 2026-08-31 — §Decision's "None of it is built. No release workflow
  exists, the role does not exist, and neither repository exists" is
  false in each of its three parts. `.github/workflows/release.yml`
  publishes both images on a `vX.Y.Z` tag push, and #791 is what
  falsified that part. The other two were built outside this repository
  and were read on 2026-08-31: `aws iam get-role --role-name
  garam-dev-ecr-push` returns a role trusting the subject
  `repo:garamsh@307152666/garam@1307729369:ref:refs/tags/v*` and holding
  `ecr:GetAuthorizationToken` with the push actions on the two
  repository ARNs and nothing else, and `aws ecr describe-repositories
  --region ap-northeast-2` returns `garam/garam-server` and
  `garam/garam-web`, both IMMUTABLE with scan-on-push. The decision is
  untouched, and so is each of the three: every one was a state this ADR
  asked to be replaced, and what replaced it is what it specified. What
  has not happened is a release — `aws ecr list-images` returned an
  empty list for both repositories the same day, and no `v*` tag exists.

- 2026-09-02 — the 2026-08-31 erratum above closes "What has not
  happened is a release — `aws ecr list-images` returned an empty list
  for both repositories the same day, and no `v*` tag exists", and
  `v0.1.0` falsified both halves. The tag was pushed on 2026-09-01, run
  33523193685 of `.github/workflows/release.yml` concluded success, and
  each repository holds `0.1.0`; the release that tag created records
  both digests. Three earlier statements go with it: §Context closes "A
  trigger named below is therefore a decision, not a demonstrated
  mechanism", and that run demonstrated it; §Consequences' "A cluster
  cannot pull garam until the workflow, the role and the repositories
  exist" names a condition every part of which is now met; and the
  2026-08-27 erratum's pointer at the chart's
  `README.md` §Pinning a version as "what now states that neither image
  exists yet" no longer describes that section. The decision is
  untouched, and `v0.1.0` is the
  first exercise of exactly what it specifies: a `vX.Y.Z` tag, the image
  tag without the `v`, both images to the account's registry under the
  names, on the trigger and the credential shape decided here. The
  chart's two image tags stay required, for a reason this release does
  not supply —
  `deploy/charts/garam/README.md` §Values a deployment must supply
  states it.

- 2026-09-02 — §Consequences' "A published web image is bound to one API
  URL" closes "That choice belongs to the change that builds the
  release, and until it is made
  `deployment-contract.md` §Not established says the answer is not
  known", and both halves are spent.
  #792 made the choice on 2026-09-01, in the same change that built the
  workflow: `.github/workflows/release.yml` bakes
  `GARAM_API_URL=http://garam-api:8080`, and that commit (593f5c3)
  removed the §Not established entry the sentence points at, so the
  document has answered nothing there since. What the choice was, and
  what it binds — the api Service a release named `garam` renders, and
  no other release name — is
  [ADR-0056](./0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md),
  and `deploy/charts/garam/README.md` §Choosing a release name is where
  a deployer meets it. The rest of the paragraph is untouched and is
  what ADR-0056 rests on: the target is a build argument baked into the
  image, so a release fixes one value for every consumer of it.
