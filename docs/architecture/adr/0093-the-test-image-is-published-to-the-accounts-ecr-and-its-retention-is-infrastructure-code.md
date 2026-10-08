# ADR 0093: The test image is published to the account's ECR, and its retention is infrastructure code

> Status: accepted
> Date: 2026-10-08

Drafted for #1227, which reserved this number. On 2026-10-08 the owner
directed that Garam uses AWS ECR, and the PM reversed
[ADR-0092](./0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md)'s
registry choice on #1227.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

ADR-0092 publishes the test image to `ghcr.io/garamsh/garam-test` and
prunes it from a workflow step, `dataaxiom/ghcr-cleanup-action`. It
chose GHCR because no cluster pulls the image and GHCR reaches the
consumer with a package setting rather than an account change, and it
rejected an ECR repository with a new role trusting a branch, since
that widens an AWS identity past the `v*` tags
[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
gave `garamsh/infra` its reason for.

Three facts reverse that choice:

- **One registry.** Every other image lives in ECR in account
  `486152169996`. The test image was the one outside it.
- **Access and retention as code.** GHCR's cross-repository Actions
  access exists only as a web setting, with no API, so the consumer's
  read was a setting no repository records. In ECR the repository, the
  publisher role, the lifecycle policy and the consumer's pull grant
  are all `garamsh/infra` Terraform.
- **The owner's decision.** `garamsh/infra` raised a new account
  category, a CI-pushed artifact that is not a release, which was the
  owner's to decide, and the owner decided it.

## Decision

**The test image is published to an ECR repository of its own,
`486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/garam-test`,
through a publisher role of its own,
`arn:aws:iam::486152169996:role/garam-github-actions-dev`,
and its retention is that repository's lifecycle policy rather than a
workflow step.**

- **The repository and the role are `garamsh/infra`'s.** It declares,
  as one change, the repository, a publisher role assumable only by
  this repository's workflow on `refs/heads/dev`, the lifecycle policy,
  the tag mutability and the consumer's pull grant. This repository
  names the repository and the role and declares neither, and
  `scripts/image-test-workflow-test.sh` holds the names
  `.github/workflows/test-image.yml` carries to those two.
- **The credential is federated.** `.github/workflows/test-image.yml`
  assumes the publisher role through
  `aws-actions/configure-aws-credentials` and logs in through
  `aws-actions/amazon-ecr-login`, at the pins `release.yml` uses, with
  `id-token: write` on the two jobs that do. No job holds
  `packages: write`.
- **The publisher role is not the release role.**
  `garam-github-actions`, named `garamsh-garam-github-actions` before
  `garamsh/infra#303` corrected it, publishes release images alone, on
  a `v*` tag (ADR-0039). `scripts/image-test-target.sh` refuses it
  under either name and admits no role but
  `garam-github-actions-dev`, as the step before either job assumes
  one.
- **Tags: the commit's full hash, immutable; `pinned-<consumer>`,
  movable.** The repository holds that mutability. A consumer still
  pins the digest.
- **Retention is three lifecycle rules.** Rule 1 keeps `pinned-*`
  images, one per consumer; rule 2 expires an image tagged by commit
  90 days after it was pushed; rule 3 expires an untagged image after
  a day. No workflow step lists or deletes an image, so the publisher
  role needs no delete right for retention.
- **A pinned image is expired 90 days after it was pushed.**
  `garamsh/infra` measured with ECR's own lifecycle preview that rule
  priority does not protect a pinned image that also carries its
  commit tag: rule 2 selects it by that tag, and every pinned image
  carries one, because a pin copies the commit's manifest. So a pin
  holds an image for no longer than an image is held unpinned, and a
  consumer moves its pin to a newer commit before the one it holds is
  90 days old, and pins again on every move of the commit it tests
  against.
- **The consumer pulls with its own role.** The repository's policy
  grants pull to
  `arn:aws:iam::486152169996:role/garam-agent-operator-github-actions`
  (`garamsh/infra#292`).
- **A pin is the same dispatch,** from `refs/heads/dev` alone, through
  `scripts/image-test-pin.sh` with the input validation ADR-0092 gave
  it. With no prune to run against, the pin shares no concurrency
  group.
- **This supersedes ADR-0092's registry and retention mechanism
  only.** ADR-0092's other decisions stand: a non-release artifact is
  published from `dev`, which supersedes ADR-0039 for such artifacts
  only; the image contents; commit-hash tags and digest pins; and the
  `pinned-<consumer>` protocol, run by the PM. Its retention count of
  8 and its rejection of retention by age are superseded with the
  mechanism.
- **Nothing deletes the GHCR package.** It keeps what ADR-0092
  published there. Retiring it is a separate step, announced after the
  ECR repository publishes.

## What this does not decide

- **The repository's and the publisher role's declarations.** Both
  are `garamsh/infra`'s: its trust conditions, its lifecycle rules and
  its pull grant are read there and not restated here.
- **Whether moving `pinned-<consumer>` onto an image restarts rule 2's
  count.** Nothing here measured it. The procedure above assumes it
  does not: the count runs from the commit's push, which is why a pin
  moves to a newer commit rather than being refreshed on the same one.
- **When the GHCR package is retired**, and what announces it.
- **What the repository costs.** No storage is measured here.

## Consequences

- **A digest lasts 90 days from its push, pinned or not.** Under
  ADR-0092 an unpinned digest lasted 8 `dev` commits and a pinned one
  indefinitely.
- **Re-running a publish cannot move a commit's tag.** The tag is
  immutable, so a rebuilt image whose digest differs is refused at the
  push and the first image stands. ADR-0092's consequence that a
  re-run moves the tag no longer holds.
- **The GHCR package is no longer pruned.** Nothing publishes to it
  either, so it holds what it held when this merged.
- **`scripts/image-test-workflow-test.sh` holds the workflow's shape**:
  its triggers, the repository guards, the permissions, the refusal
  before the role, the role and region the credential step names, and
  the actions it may use, which leaves no cleanup action or GHCR login
  a place.

## Rejected alternatives

- **Keeping GHCR.** The consumer's access stays a web setting no
  repository records, and the image stays the one outside the
  account's registry.
- **Pruning from a workflow step against ECR.** It gives the publisher
  role a delete right and puts retention outside the Terraform that
  holds every other repository's.
- **Protecting a pin by rule priority alone.** Measured not to hold:
  the pinned image's commit tag still matches rule 2.
- **Publishing with the release role.** Its trust admits `v*` tags
  alone, and a role that also trusted a branch would carry the
  release images' push right to every `dev` commit.
- **Writing a repository name or role ARN before `garamsh/infra`
  named them.** A guessed value fails at the assume or the push with
  a message naming neither, or succeeds against something nobody
  approved.
