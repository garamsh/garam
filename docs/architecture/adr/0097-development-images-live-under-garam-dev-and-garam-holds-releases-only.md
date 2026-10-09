# ADR 0097: Development images live under `garam-dev/`, and `garam/` holds releases only

> Status: accepted
> Date: 2026-10-09

Drafted for #1241, which reserved this number. The decision is the
account owner's, and it replaces the namespace
[ADR-0095](./0095-development-images-live-under-dev-and-garam-holds-releases-only.md)
recorded.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives

## Context

ADR-0095 split the registry: `garam/` for releases and `dev/` for
development builds. It took `dev/` from the owner's answer to
`garamsh/infra` at 2026-10-08T15:32:59Z, as the later of two answers,
and rejected `garam-dev/` because `garam-dev` is already the cluster's
name and the prefix of four IAM roles.
[ADR-0093](./0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)
put the test image in that namespace, as `dev/garam-test`.

Three later facts, which #1241 records:

- **The owner's instruction.** At 2026-10-08T21:53Z, to the infra PM:
  "dev가 아니라 garam-dev" (not `dev`, but `garam-dev`). It is later
  than the answer ADR-0095 relied on, and the owner gave it twice to
  the Operator PM as well.
- **`garamsh/infra` applied it.** garamsh/infra#309, merged
  2026-10-08T22:19Z as `ba73529`, declares the seven operator-pushed
  repositories under `garam-dev/` that its #306 had declared under
  `dev/`, `garam-dev/garam-server`, `garam-dev/garam-web` and
  `garam-dev/garam-test` among them.
- **Nothing has been pushed to `dev/garam-test`**, so no pin there has
  to be carried over.

`garamsh/infra`'s `docs/architecture/aws.md` at `ba73529` records the
reason for the name, which this repository adopts: `garam/` holds what
a release workflow publishes, for anything to consume, so it belongs to
no cluster; `garam-dev/` holds what no release workflow publishes,
which an operator pushes for work in this account, so its name carries
the cluster's. The same file records the `dev/` literal as its own
error, written into the options the owner was answering, and not a
decision the owner reversed.

## Decision

- **`garam/` holds releases only**, as ADR-0095 decided:
  `garam/garam-server` and `garam/garam-web` take a bare `X.Y.Z`,
  which only `.github/workflows/release.yml` publishes, on a `vX.Y.Z`
  git tag
  ([ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)).
- **Development builds go to `garam-dev/<image>`**:
  `garam-dev/garam-server`, `garam-dev/garam-web` and the test image's
  `garam-dev/garam-test`, in the same account and region as the
  release repositories.
- **The scripts name it.** `scripts/image-build.sh` given a registry
  tags each image `<registry>/garam-dev/<image>:<tag>` and never a
  `garam/` path. `scripts/image-test-push.sh` and
  `scripts/image-test-pin.sh` hold `garam-dev/garam-test` as a
  constant and take no argument naming a repository, so no invocation
  reaches `garam/`.
- **Everything else ADR-0095 decided stands**: a person pushes a
  development build under their own identity, this repository holds
  and grants no credential for it, and ADR-0071's tag forms and
  refusals are unchanged. ADR-0095's supersession of
  [ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)'s
  repository decision carries over unchanged.
- **This supersedes ADR-0095.** ADR-0093's decision is untouched: it
  took the namespace from ADR-0095 rather than deciding it.

## What this does not decide

- **The repositories, their lifecycle and their grants.** They are
  `garamsh/infra`'s, as under ADR-0095. Whether its apply has created
  them is read from the account, not from this repository.
- **Pushing, pinning or removing any image.** Those are registry
  actions, and #1241 leaves them to the PM after this merges.

## Consequences

- **ADR-0095's costs carry over.** Each image has a release repository
  and a development one, granted separately, and the development
  namespace says where a build runs rather than what it is.
- **`garam-dev` names three things**: the cluster, the prefix of its
  IAM roles, and this namespace. ADR-0095 rejected the name for that;
  `garamsh/infra`, which raised it, now gives the namespace the
  cluster's name on purpose.
- **A reference built under ADR-0095 names a repository that is not
  declared.** `dev/garam-server`, `dev/garam-web` and `dev/garam-test`
  left `garamsh/infra`'s declarations with `ba73529`.

## Signs the decision was wrong

- An IAM pattern written for the cluster's roles, such as one
  matching `garam-dev*`, is found granting or denying access to a
  `garam-dev/` repository it was not written for.
- An image of a release is found in `garam-dev/`, or a development
  build in `garam/`.

## Rejected alternatives

- **Keeping `dev/`.** ADR-0095's namespace. The owner's later
  instruction replaces it, and `garamsh/infra`, whose options produced
  it, records it as its error.
- **Keeping ADR-0071: one repository per image, told apart by tag.**
  The owner's split stands; only the development namespace's name
  changes.
