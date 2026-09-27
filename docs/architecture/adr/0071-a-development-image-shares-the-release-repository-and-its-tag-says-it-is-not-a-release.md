# ADR 0071: A development image shares the release repository, and its tag says it is not a release

> Status: accepted
> Date: 2026-09-27

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
§Decision fixed one repository per image, `garam/garam-server` and
`garam/garam-web`, required both to be IMMUTABLE, and closed with:

> **Pushing that tag to GitHub is the only trigger that publishes.** A
> merge to `main` builds and tests and pushes nothing, so the registry
> holds released versions and no build residue.

The owner set the account's policy on 2026-09-27 (#1120): each image
has one repository, a development build goes into the same repository
as the release, and what tells the two apart is an immutable tag — a
commit-derived or prerelease tag for development, the bare version for
a release. Under ADR-0039's closing sentence such a build has no place
in either repository, and this repository gives a person no way to
produce one: `scripts/image-build.sh` tags each image `<name>:ci`, a
name no registry holds, and nothing else here builds either image.

The environment-specific repositories that stood in for this were never
garam's decision. #1050 measured `garam/garam-server-dev` and
`garam/garam-web-dev` on 2026-09-05, found no workflow, manifest,
script or document in this repository naming either, and found a
frozen `dev` tag in each that nothing produced or consumed.

The two neighbouring publishers already take this shape, each read on
2026-09-27 on its `dev` branch:

- `garamsh/sherlock` at `8218189`, `docs/architecture/deployment.md`
  §Images and tags: `garam/sherlock-agent` and
  `garam/sherlock-workspace` hold both a release's semver and a
  development build tagged by its commit's short SHA.
- `garamsh/garam-agent-operator` at `52567d6`,
  `docs/architecture/delivery.md`: `garam/garam-agent-operator` is
  "the single home for every build of this operator", tagged by the
  commit's 12-digit abbreviated hash.

## Decision

- **Each image has one repository, and it holds every build of that
  image.** `garam/garam-server` and `garam/garam-web` are the ones
  ADR-0039 named; garam uses and asks for no environment-specific one.
- **The tag says what a build is.** A bare `X.Y.Z` is a release, and
  only `.github/workflows/release.yml` publishes one, on a `vX.Y.Z` git
  tag — the trigger, the tag rule and the OCI revision and version
  labels are ADR-0039's and #1020's, unchanged. A development build
  takes one of two forms and nothing else:
  - **commit-derived** — 7 to 40 lowercase hex digits that are a prefix
    of the commit built, twelve by default;
  - **prerelease** — `X.Y.Z-<identifiers>` as Semantic Versioning 2.0.0
    writes one, without build metadata, which a registry tag cannot
    carry.
- **`make image-build` is the development path.** Given
  `IMAGE_REGISTRY`, and optionally `IMAGE_TAG`, it builds both images
  from the contexts, Dockerfiles and build arguments the release reads,
  under the release's repository paths in that registry, and labels
  each with the commit built and the tag given. It refuses a bare
  release version, any other tag, a commit prefix that is not HEAD's,
  and a working tree that differs from HEAD, before anything is built.
  Without `IMAGE_REGISTRY` it is the check it was.
- **This repository pushes a development build nowhere.** A person
  pushes what that target built, with a credential this repository
  neither holds nor grants. The release's role still trusts `v*` tags
  alone, and no workflow gains a trigger.
- **This supersedes ADR-0039 in part**: the sentence quoted above,
  which now holds for a release and for no build a person pushes. The
  rest of ADR-0039 stands, immutability included.

## What this does not decide

- **The removal of `garam/garam-server-dev` and `garam/garam-web-dev`.**
  They are the account owner's, as ADR-0039 §Decision left
  `garam/garam` to that owner; #1050 tracks them.
- **Retention.** ADR-0039 asked for no lifecycle policy, and development
  tags change nothing about whose that setting is.
- **What a deployment pins.** The chart takes any tag the repository
  holds; running a development build is the deployment's choice.

## Consequences

- **A repository's content is no longer the list of releases.** The tag
  form is what separates the two, and a reader holding a reference
  reads it there: a bare version was released, anything else was built
  by hand.
- **A development tag is spent on its first push.** Both repositories
  are IMMUTABLE, so a commit-derived tag names one commit for good and
  a prerelease tag cannot be pushed twice. That is why the default is
  derived from HEAD and a tree differing from HEAD is refused — the
  label and the tag would otherwise name a commit whose tree was not
  what was built.
- **A release version cannot be taken early through this repository.**
  A hand-pushed `X.Y.Z` would make the release of that version fail for
  good; `scripts/image-build.sh` refuses to tag one. Nothing refuses a
  `docker tag` typed by hand — the guard is on this repository's entry
  point, not on the registry.
- **The check's names do not move.** `scripts/image-build.sh --names`
  and `make web-image-e2e` still read `garam-server:ci` and
  `garam-web:ci` when no registry is given, so a pull request's image
  build and the name the web check discovers are what they were.

## Rejected alternatives

- **An environment-specific repository per image,
  `garam/garam-server-dev` and `garam/garam-web-dev`.** It keeps the
  release repositories free of development tags, and it gives each
  image two homes, two grants to keep equal and a name that says where
  a build was meant to run rather than what it is. The owner's policy
  rejects it.
- **A moving `dev` tag.** It names whatever was pushed last, which
  IMMUTABLE refuses, and #1050 is what one left behind.
- **A workflow that publishes development builds.** It needs a trigger
  other than `v*` and a role trusting it, and adding a workflow run on
  `dev` is outside what #1120 asks. ADR-0039's rejection of publishing
  every commit on `main` under its SHA is untouched.
- **A prefixed commit tag, such as `sha-<hash>` or `dev-<hash>`.** A
  bare hex prefix already cannot be read as `X.Y.Z`, and it is the form
  both neighbouring publishers push.
