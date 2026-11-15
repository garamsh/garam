# ADR 0158: garam carries no gaon operator and a release publishes six images

> Status: accepted; built in `.github/workflows/release.yml` and
> `tooling/image-build.sh` for the six images; the removal of
> `gaon-operator/` is not built
> Date: 2026-11-13

Drafted for #1875, which reserves this number. The direction is the
owner's decision 15 on garamsh/aurora#749
(issuecomment-6848311693). It supersedes
[ADR-0156](./0156-a-release-publishes-eight-images-and-a-pull-request-builds-the-guest-only-to-its-build-stage.md)
in full and
[ADR-0151](./0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)
in part now, and in whole when `gaon-operator/` leaves. garam is read
at `568c6e61e57e41ee9ebf66ac9742b5ee3fa6fd21`.

## Context

- **The owner decided there is one agent operator and it is for
  gaon.** Asked why the image `garam/gaon-operator` exists, the owner
  said garam should not know gaon, and that the operator built for
  Kubernetes is replaced by one for gaon. garamsh/aurora's PM reads
  that as: garamsh/garam-agent-operator is the operator that runs
  garam's agents as MicroVMs on gaon, and garam carries no gaon
  operator, no guest image, and no place for either in its release.
  garam keeps what an operator of any kind calls: its machine API and
  the key it issues.
- **This reverses the PM's recorded decision.** ADR-0151 put the
  operator here, and ADR-0153, ADR-0154 and ADR-0156 followed it:
  ADR-0156 made the release publish `garam/gaon-operator` and
  `garam/gaon-guest`.
- **The order is set by garamsh/aurora's PM under that issue's
  decision 10 and agreed by garam's PM.** garam's release goes back to
  six images first and alone, so `v0.1.0-dev.16` (ADR-0157) can be
  cut. garamsh/garam-agent-operator's PM then takes `gaon-operator/`
  from a commit garam names. garam deletes the directory only after
  that repository holds it at a commit passing its own checks.

## Decision

1. **garam carries no gaon operator and publishes none.** The gaon
   operator is garamsh/garam-agent-operator's. A release publishes
   six images from the change that records this: `garam/server`,
   `garam/console`, `garam/site`, `garam/cowork`, `garam/cowork-web`
   and `garam/policy`, all or none, with six digests in the release
   body. `release.yml` names neither `garam/gaon-operator` nor
   `garam/gaon-guest`.
2. **What a pull request builds is what a release publishes, again.**
   `images.yml` builds the six from what `release.yml` says each is
   built from, as ADR-0134 §Decision 6 states and ADR-0150 counts. What
   ADR-0156 added only for the two images is removed: the
   `--public-only` and `--only` options of `tooling/image-build.sh`,
   `IMAGE_PUBLIC_ONLY`, `gaon-operator-image-e2e`,
   `deploy/local/Dockerfile.gaon-operator` and the two toolchain pairs.
3. **`gaon-operator/` stays in the tree until a later pull request
   removes it.** That pull request removes the directory, its `go.work`
   line, its row in `tooling/ci/modules`, its dependabot entry and the
   documents describing it, and is opened only after
   garamsh/garam-agent-operator holds the directory at a commit passing
   its own checks, which garam names. Until then `ci.yml` builds and
   tests the module. `make -C gaon-operator guest-run` builds the guest
   image with `docker build`, as it did before ADR-0156, and no
   workflow names the guest.
4. **`deploy/charts/garam-gaon` stays.** It declares garam's own
   services in gaon's kinds (ADR-0155), which the operator does not
   reach.
5. **What this supersedes.** ADR-0156 in full. ADR-0151 in part now:
   item 1's image `garam/gaon-operator`, which no release publishes.
   ADR-0151 in whole when the directory leaves, and ADR-0153 and
   ADR-0154, which follow it, are named as leaving with it; this
   change alters neither of their statuses.

## Consequences

- A release is six images again, and a pull request that breaks one
  fails `images.yml`, as before ADR-0156. The guest's final stage is
  built by no workflow: `make -C gaon-operator guest-run` is the only
  run of it, as it was before ADR-0156.
- Until the directory leaves, the responsibility documents of the
  operator and the guest describe code this repository still holds and
  no longer publishes. The pull request that removes the directory
  removes them, and rewrites the passages that name it.
- A deployment of [deployment-gaon.md](../deployment-gaon.md) cannot
  pull an operator image from garam's release, and none is owed one.
  Which image the operator of garamsh/garam-agent-operator is
  published as is that repository's.
- The window between this change and the directory's removal is open
  on a condition outside this repository: garamsh/garam-agent-operator
  has to hold the directory at a commit passing its own checks.

## Signs the decision was wrong

- garamsh/garam-agent-operator cannot hold `gaon-operator/` at a
  commit passing its own checks, so the directory stays and garam
  carries a module that nothing publishes.
- A deployment needs the operator or guest image from a garam release,
  which would mean the operator did not move.
- The pull request that removes the directory is not opened after that
  repository holds it, and the documents keep describing a directory
  that no release builds.
