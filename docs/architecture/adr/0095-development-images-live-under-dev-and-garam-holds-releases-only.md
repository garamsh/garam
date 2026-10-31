# ADR 0095: Development images live under `dev/`, and `garam/` holds releases only

> Status: superseded by
> [ADR-0097](./0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md),
> itself superseded by
> [ADR-0134](./0134-an-image-lives-at-its-publishing-repository-and-its-tag-says-what-a-build-is.md)
> Date: 2026-10-09

Drafted for #1234, which reserved this number. The decision is the
account owner's, and the costs it accepts are the ones the PM stated
against it there.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
§Decision gave each image one repository holding every build of it:
`garam/garam-server` and `garam/garam-web` took a release under a bare
`X.Y.Z` and a development build under a commit-derived or prerelease
tag, and `make image-build` given `IMAGE_REGISTRY` tagged a development
build under the release's own repository path. It rejected an
environment-specific repository per image.

The owner directed otherwise, in two answers that #1234 records:

- At 2026-10-08T15:21:04Z, to the Garam PM, that development images
  go under `garam-dev/` (comment 6063159738). The same directive asks
  every project publishing to this registry — garam,
  `garamsh/garam-agent-operator` and `garamsh/sherlock` — to define it
  that way, and development-tagged images already in a `garam/`
  repository to be moved out and then removed, each project's PM first
  checking that nothing deployed or pinned names the tag.
- At 15:32:59Z, to `garamsh/infra`, whose question named the Garam PM's
  `garam-dev/` relay: `dev/` (comment 6063488439). It is the later
  answer, given knowing the first, and it governs.

Infra's reason against `garam-dev/`, recorded in the same comment: the
cluster's `cluster_name` is `garam-dev`, and four IAM roles carry that
prefix, so a `garam-dev/` namespace would make one string mean two
things.

The PM recommended keeping ADR-0071 and stated three costs in #1234:

- two homes and two grants per image;
- a name that says where a build runs rather than what it is;
- `garam-dev` resembles the cluster prefix.

The owner chose the namespace split.

#1234 records the registry as read from ECR on 2026-10-09:
`garam/garam-server`, `garam/garam-web`, `garam/sherlock-agent`,
`garam/sherlock-workspace`, `garam/garam-agent-operator` and
`garam/garam-agent-operator-control`, all IMMUTABLE, with
`garam/garam-server` holding only `0.1.0` to `0.3.0`. No `dev/`
repository is among them.

## Decision

- **`garam/` holds releases only.** `garam/garam-server` and
  `garam/garam-web` take a bare `X.Y.Z`, which only
  `.github/workflows/release.yml` publishes, on a `vX.Y.Z` git tag.
  The trigger, the tag rule, the labels and the push role are
  [ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)'s
  and are unchanged.
- **Development builds go to `dev/<image>`**: `dev/garam-server` and
  `dev/garam-web`, in the same account and region as the release
  repositories. The repository is the release's with the `garam/`
  namespace replaced, so the image name says which project and runtime
  it is exactly as the release's does.
- **`make image-build` names them.** Given `IMAGE_REGISTRY`, it tags
  each image `<registry>/dev/<image>:<tag>` and never a `garam/` path.
  The tag forms, the refusal of a bare release version and of every
  other form, and the refusal of a tree that differs from HEAD are
  ADR-0071's and are unchanged; a release version stays refused in
  `dev/` too, so a bare `X.Y.Z` names a release wherever it is read.
- **A person pushes a development build under their own identity.**
  This repository holds and grants no credential for it, no workflow
  gains a trigger, and the release role still trusts `v*` tags alone,
  as ADR-0071 decided.
- **This supersedes ADR-0071's repository decision only**: its
  "Each image has one repository, and it holds every build of that
  image", and the release's repository paths it gave a development
  build. The rest of ADR-0071 stands.

## What this does not decide

- **Creating the `dev/` repositories, their lifecycle and their
  grants.** They are `garamsh/infra`'s to declare, and the cluster's
  pull grant follows the repositories it declares (#1234). Whether
  they are IMMUTABLE is part of that.
- **The test image.** #1234 places it, #1227's, at `dev/garam-test`,
  and #1228 is the change that moves it there and records it.
- **Moving existing development tags.** The owner's directive gives
  that to each project's PM. `garam/garam-server` held no tag but a
  release when #1234 read it.

## Consequences

The PM stated these costs, and the owner accepted them in choosing the
split:

- **Two homes and two grants per image.** Each image has a release
  repository and a development one, and whoever may push or pull one
  is granted on each separately.
- **The name says where a build runs rather than what it is.** `dev/`
  names an environment; the tag form, which ADR-0071 made the thing
  that says what a build is, still says it.

The PM's third cost was the `garam-dev` name's, and the namespace
taken is not that name; `dev/` shares no prefix with the cluster or
its roles.

And from the split itself:

- **A release repository's content is the list of releases again.**
- **A deployment running a development build sets the repository as
  well as the tag.** The chart's default `image.*.repository` names
  the release repositories, so a development tag alone points at a
  repository that does not hold it.
- **A development build has nowhere to be pushed until `dev/`
  exists.** `make image-build` produces the references either way.
- **The check's names do not move.** Without `IMAGE_REGISTRY`,
  `scripts/image-build.sh` still tags `garam-server:ci` and
  `garam-web:ci`, which is what `make web-image-e2e` reads.

## Rejected alternatives

- **Keeping ADR-0071: one repository per image, told apart by tag.**
  The PM's recommendation, for the costs under §Context. The owner
  chose the namespace split.
- **`garam-dev/<image>`.** The owner's first answer. `garam-dev` is
  already the cluster's name and the prefix of four IAM roles, so the
  string would name two things; the owner's later answer, given
  knowing this one, is `dev/`.
- **A suffixed repository in the release namespace, such as
  `garam/garam-server-dev`.** The shape ADR-0071 rejected and #1050
  found frozen; it keeps development builds under `garam/`, which the
  directive moves them out of.
