# ADR 0092: A test image is published to GHCR from `dev`, and no release carries it

> Status: accepted — its registry and retention mechanism are
> superseded by [ADR-0093](./0093-the-test-image-is-published-to-the-accounts-ecr-and-its-retention-is-infrastructure-code.md),
> which publishes the test image to the account's ECR
> Date: 2026-10-07

Drafted for #1213, which reserved this number. The image, the
registry, the trigger and the retention are the PM's decisions there,
in comments 6027160590, 6027190552 and 6027198628, taken under the
owner's delegation after `garamsh/infra`'s review.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives
- Errata

## Context

`garamsh/garam-agent-operator` #271 runs real-Garam cases in its
promotion CI and needs a fixed Garam to run them against. That CI cannot
read this repository's source: deploy keys are disabled across the
organization. `tests/testprincipal`
([ADR-0081](./0081-a-test-only-command-signs-in-the-first-principal-of-a-database-it-creates.md))
is built today only inside this repository's own test stacks, from a
checkout.

[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
§Decision makes a `vX.Y.Z` tag the only trigger that publishes, and
its rejected alternatives refuse GitHub Container Registry and
publishing every commit under its SHA. Both were weighed for the
release images a cluster pulls: GHCR because the cluster is already
wired to ECR, per-commit publishing because it would make a deployable
registry out of a build cache.
[ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
narrowed the first sentence to a release and still pushes no
development build from a workflow. `garamsh/infra` cited ADR-0039 as
the reason its trust conditions admit `v*` tags alone, and pointed out
that moving a build output between two repositories of one
organization needs no AWS change.

## Decision

**This repository publishes a test image on every push to `dev`, to a
private GHCR package of its own, `ghcr.io/garamsh/garam-test`, and no
release image carries what it holds.**

- **The image is a file source and is never run.** A consumer copies
  three paths out with `docker create` and `docker cp`: `/garam` and
  `/testprincipal`, statically linked linux/amd64 executables of
  `./cmd/garam` and `./tests/testprincipal` built with
  `CGO_ENABLED=0`, and `/migrations/`, the repository's `migrations/`
  verbatim. It has one linux/amd64 manifest, and its
  `org.opencontainers.image.revision` label names the full commit.
  `tests/testprincipal/README.md` §The test image is the consumer's
  contract.
- **It is built and checked by name.** `make test-image` builds it
  from `tests/testprincipal/Dockerfile`, and `make test-image-e2e`
  checks the three paths, the static linking and the label;
  `.github/workflows/test-image.yml` runs that check before it pushes.
- **The tag is the commit's full hash, and a consumer pins the
  digest.** GHCR offers no immutable tags, so the digest is the
  reference that cannot come to name other bytes.
- **The credential is the workflow's own `GITHUB_TOKEN`**, with
  `packages: write` on the jobs that push or delete. No AWS identity
  or ECR repository is involved, and the release role,
  `garamsh-garam-github-actions`, is never assumed for it. Publishing
  it cuts no release.
- **GHCR, because no cluster pulls this image.** ADR-0039 chose ECR as
  the registry the cluster is wired to; a consumer's CI is the only
  reader here, and GHCR reaches it with a package setting rather than
  an account change.
- **The consumer reads it through the package's Actions access**,
  granted to `garamsh/garam-agent-operator` with the read role. That
  is a package setting an organization admin applies once.
- **Retention keeps the 8 newest unpinned versions and every pinned
  one.** After each publish, `dataaxiom/ghcr-cleanup-action` runs with
  `keep-n-tagged: 8` and `exclude-tags: pinned-*`; an excluded version
  is outside the count, so a pinned version among the newest does not
  push an unpinned one out. `delete-untagged: true` removes every
  version left without a tag. A pinned version is never one: it
  carries its `pinned-` tag and is excluded before either rule runs,
  and it is a single manifest that names no other version.
- **A pin is a dispatch of the same workflow.** Given a commit and a
  consumer name, it moves the tag `pinned-<consumer>` to that commit's
  version, so each consumer holds one pinned version at a time and the
  one it held before becomes prunable. The PM runs it when a consumer
  asks and records the pin on #1213 or its successor. A `dry-run`
  input prints what a prune would delete and deletes nothing. The
  dispatch runs only from `dev`, so it is always that branch's copy of
  the workflow.
- **The bound on pinned versions is a procedure, not a mechanism.**
  Anyone who can dispatch a workflow here can pin under any consumer
  name, and each new name keeps one more version; what holds the count
  to one per consumer is that the PM is the one who runs it.
- **This supersedes ADR-0039 for non-release artifacts only.** A
  version tag stays the only trigger that publishes a release image,
  to the repositories ADR-0039 named; the test image is published from
  `dev` to GHCR. ADR-0039's refusals of GHCR and of per-commit
  publishing stand for release images.
- **This supersedes ADR-0081's "no binary or image Garam ships carries
  it" in part.** No release image and no binary built from
  `./cmd/garam` carries `testprincipal`, and the deployed sign-in is
  unchanged; the test image carries it as a file. The rest of ADR-0081
  stands.

## What this does not decide

- **The image's size against the free plan's private-package storage.**
  The PM's count of 8 was set against that plan; nothing here measures
  a bill.
- **A second consumer.** Another repository needs its own Actions
  access on the package and pins under its own name.

## Consequences

- **A consumer's unpinned digest lasts 8 `dev` commits.** A pin older
  than that is refreshed, or held through the dispatch.
- **Every commit on `dev` costs a build and a push.** The publish job
  declares no concurrency group, since a group cancels a pending run
  when a newer one queues and that commit would get no image.
- **The prune and the pin share a concurrency group**, so a pin cannot
  land on a version a running prune has already listed. A pending pin
  that a later queued prune replaces shows as cancelled and is
  dispatched again.
- **Re-running a publish moves the commit's tag.** The rebuilt image
  can differ in digest, and the version it replaces becomes untagged;
  the next prune deletes it. A consumer that pinned that version's
  digest without a `pinned-` tag loses it then.
- **The prune deletes with `GITHUB_TOKEN`**, which needs this
  repository to hold the admin role on the package.

## Rejected alternatives

- **An ECR repository with a new role trusting a branch.** It asks
  `garamsh/infra` for an AWS identity whose trust widens past `v*`
  tags, the narrowness ADR-0039 gave infra its reason for, to move a
  build output no cluster pulls.
- **Actions artifacts read across repositories.** The consumer would
  need a token reaching into this repository, and an artifact expires
  on a schedule this repository does not tie to a pin.
- **Retention by count alone.** A busy day on `dev` would prune a
  consumer's pinned digest.
- **Retention by age.** At the rate `dev` moves, the window a pin
  needs holds more versions than the private-package storage the PM
  set the count against.
- **A stage in `deploy/local/Dockerfile`.** It saves a file, and it
  puts `tests/` into the release image's build context, which the
  root `.dockerignore` keeps out so that no test command can reach a
  release image (ADR-0081).

## Errata

- 2026-10-08 — §Decision names the release role
  `garamsh-garam-github-actions`. It was wrong when written: the name
  `garamsh/infra` approved in its #293 carries no `garamsh-` prefix,
  and the prefixed form came from a relayed refinement nobody
  approved. The release role is `garam-github-actions`, which
  `.github/workflows/release.yml` assumes since #1229. The decision
  stands: no AWS identity, under either name, is assumed for the test
  image.
