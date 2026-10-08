# ADR 0093: An operator pushes the test image to the account's ECR, and no workflow holds a credential for it

> Status: accepted
> Date: 2026-10-08

Drafted for #1227, which reserved this number. On 2026-10-08 the owner
directed that Garam uses AWS ECR, and then, on `garamsh/infra`'s
question why `dev` needs Actions at all when a development push to ECR
can be a direct one, that the test image is pushed by an operator. The
PM recorded both on #1227. The owner then decided that development
images live under the `dev/` namespace, answering `garamsh/infra` on
2026-10-08 (#1234, which reserves ADR-0095 for that decision), and the
test image is one.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[ADR-0092](./0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md)
publishes the test image to `ghcr.io/garamsh/garam-test` from
`.github/workflows/test-image.yml` on every push to `dev`, and prunes
it from a workflow step. It chose GHCR because no cluster pulls the
image and GHCR reaches the consumer with a package setting rather than
an account change, and it rejected an ECR repository with a new role
trusting a branch, since that widens an AWS identity past the `v*` tags
[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
gave `garamsh/infra` its reason for.

Three facts reverse that choice:

- **One registry.** Every other image lives in ECR in account
  `486152169996`. The test image was the one outside it.
- **Access and retention as code.** GHCR's cross-repository Actions
  access exists only as a web setting, with no API, so the consumer's
  read was a setting no repository records. In ECR the repository, its
  lifecycle policy and the consumer's pull grant are all
  `garamsh/infra` Terraform.
- **The owner's decision.** Publishing from CI into ECR needed a role
  trusting `refs/heads/dev`, a new category of CI credential that is
  not a release's. The owner decided against it: the test image is
  operator-pushed, the category `garam/garam-agent-operator` is
  already in.

## Decision

**The test image is pushed to an ECR repository of its own,
`486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/dev/garam-test`,
by an operator under their own AWS identity, and its retention is that
repository's lifecycle policy. No workflow publishes it, and no
workflow holds a credential for it.**

- **The namespace is `dev/`.** The test image is a development
  image, and #1234 puts development images there, out of the release
  namespace `garam/`; ADR-0095 records that decision, not this one.
- **The repository is `garamsh/infra`'s.** It declares the repository,
  its tag mutability, its lifecycle policy and the consumer's pull
  grant, and who may push to it. This repository names the repository
  in `Makefile` and declares none of it.
- **No CI credential.** There is no role trusting `refs/heads/dev` and
  no publishing workflow. `.github/workflows/test-image.yml` is
  removed; no workflow here assumes an AWS identity but `release.yml`'s
  on a `v*` tag (ADR-0039).
- **An operator runs two `make` targets.** `make test-image-push`,
  from a clean checkout of the commit, builds the image and checks it
  as `make test-image-e2e` does, pushes it under the commit's tag, and
  reads its digest back from the registry
  (`scripts/image-test-push.sh`). `make test-image-pin COMMIT=<hash>
  CONSUMER=<name>` moves `pinned-<consumer>` to that commit's image by
  putting its manifest again under the digest it was read with, and
  reads the tag back (`scripts/image-test-pin.sh`). Both run under the
  caller's AWS credentials, and neither is in `make ci`.
- **What each refuses before it reaches AWS** is held by
  `scripts/image-test-ecr-test.sh` in `make test`: a repository that is
  not an ECR repository URI, a working tree that differs from HEAD, a
  commit that is not 12 to 40 lowercase hex digits, and a consumer name
  that cannot follow `pinned-` in a tag.
- **Tags: the commit's first twelve hex digits, immutable;
  `pinned-<consumer>`, movable.** The commit tag is the commit-derived
  form
  [ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
  gives a development build, at its default length; a pin names the
  commit in full or by those twelve. The repository holds that
  mutability. A commit already in
  the repository is reported and not built again, since a rebuild would
  differ in its bytes and be refused at the push. A consumer still pins
  the digest.
- **Retention is three lifecycle rules.** Rule 1 keeps `pinned-*`
  images, one per consumer; rule 2 expires an image tagged by commit
  90 days after it was pushed; rule 3 expires an untagged image after
  a day. Nothing here lists or deletes an image.
- **A pinned image is expired 90 days after it was pushed.**
  `garamsh/infra` measured with ECR's own lifecycle preview that rule
  priority does not protect a pinned image that also carries its
  commit tag: rule 2 selects it by that tag, and every pinned image
  carries one, because a pin puts the commit's manifest under a second
  tag. So a pin holds an image for no longer than an image is held
  unpinned, and a consumer moves its pin to a newer commit before the
  one it holds is 90 days old.
- **The consumer pulls with its own role.** The repository's policy
  grants pull to
  `arn:aws:iam::486152169996:role/garam-agent-operator-github-actions`
  (`garamsh/infra#292`).
- **This supersedes ADR-0092's registry, its publishing from `dev`,
  and its retention mechanism.** ADR-0092's other decisions stand: the
  image contents, digest pins, and the `pinned-<consumer>` protocol,
  run on the PM's request. Its commit tag, the full hash, gives way to
  ADR-0071's form above. Its retention
  count of 8 and its rejection of retention by age are superseded with
  the mechanism.
- **Nothing deletes the GHCR package.** It keeps what ADR-0092
  published there until ECR holds a pinned image. Retiring it is a
  separate step, announced first.

## What this does not decide

- **The repository's declarations.** Its mutability, its lifecycle
  rules, its pull grant and which identities may push are
  `garamsh/infra`'s, read there and not restated here. The rules and
  the measurement above were declared for `garam/garam-test`;
  `dev/garam-test` is one of the development repositories #1234 lists
  for `garamsh/infra` to declare, with the same ones.
- **The `dev/` namespace itself**, which is ADR-0095's (#1234).
- **Whether moving `pinned-<consumer>` onto an image restarts rule 2's
  count.** Nothing here measured it. The procedure above assumes it
  does not: the count runs from the commit's push, which is why a pin
  moves to a newer commit rather than being refreshed on the same one.
- **When the GHCR package is retired**, and what announces it.
- **What the repository costs.** No storage is measured here.

## Consequences

- **No image appears per `dev` commit.** One is pushed when a pin
  moves, and that pin is what the consumer reads. A consumer that wants
  a commit no pin has named asks for one.
- **The image is built on the operator's machine,** from a checkout
  `scripts/image-test-push.sh` refuses unless it is clean, and is
  checked there by `tests/image/test-image.sh` before the push. No
  runner records the build.
- **A digest lasts 90 days from its push, pinned or not.** Under
  ADR-0092 an unpinned digest lasted 8 `dev` commits and a pinned one
  indefinitely.
- **A commit's tag cannot be moved.** It is immutable, so the first
  image pushed for a commit stands. ADR-0092's consequence that a
  re-run moves the tag no longer holds.
- **The GHCR package is no longer published to or pruned.** It holds
  what it held when this merged.

## Rejected alternatives

- **Keeping GHCR.** The consumer's access stays a web setting no
  repository records, and the image stays the one outside the
  account's registry.
- **Tagging with the full hash, as ADR-0092 did.** A development
  build takes ADR-0071's commit-derived form, twelve digits by default,
  and the test image is one.
- **`garam/garam-test`, this ADR's first repository.** The release
  namespace holds releases only under #1234.
- **Publishing from a workflow on `dev` through a role of its own.**
  This ADR's first draft did, through
  `garam-github-actions-dev` trusting this repository's workflow on
  `refs/heads/dev`. It gave every push to `dev` an AWS push right for
  an image only a pin is read from, and the owner decided against
  that credential.
- **Publishing with the release role.** Its trust admits `v*` tags
  alone, and a role that also trusted a branch would carry the release
  images' push right to every `dev` commit.
- **Pruning from a workflow step or a script against ECR.** It needs a
  delete right and puts retention outside the Terraform that holds
  every other repository's.
- **Protecting a pin by rule priority alone.** Measured not to hold:
  the pinned image's commit tag still matches rule 2.
- **Copying the pin with `docker buildx imagetools create`.** It
  needs a Docker login, and nothing in it ties the bytes it pushes to
  the digest it read. ECR's `PutImage` takes the manifest with that
  digest and refuses one that does not match it.
