# ADR 0157: Until launch a release is `v0.1.0-dev.N` or `v0.1.0-rc.N`, and no development build takes those tags

> Status: accepted; built in `.github/workflows/release.yml`,
> `tooling/release-version.sh` and `tooling/image-build.sh`
> Date: 2026-11-13

Drafted for #1868, which reserves this number. The direction is the
owner's decision 14 on garamsh/aurora#749
(issuecomment-6846684552). It supersedes in part
[ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
(§Decision, "A release is a `vX.Y.Z` git tag"),
[ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
(§Decision, "A bare `X.Y.Z` is a release") and
[ADR-0134](./0134-an-image-lives-at-its-publishing-repository-and-its-tag-says-what-a-build-is.md)
(§Decision 3, the release tag's form). garam is read at
`3b0500bad93025033a62ec893ddc633d037ac3e3`.

## Context

- **No release before launch may look like one after it.** The owner
  decided that no new `0.x.0` is cut before launch: a release is
  `0.1.0-dev.N`, a candidate for the rehearsals `0.1.0-rc.N`, and
  launch `0.1.0`. N counts from 1 per project and never repeats, and
  what exists is renamed under the new form, not deleted.
- **garam's fifteen versions map onto it.** garamsh/aurora#749's
  comment issuecomment-6846780701 puts them in version order,
  `0.1.0` to `0.8.0`, which become `0.1.0-dev.1` to `0.1.0-dev.15`.
  The next release is `v0.1.0-dev.16`.
- **Two rules about a tag meet the new form.** ADR-0039 makes a release
  a `vX.Y.Z` git tag, and `release.yml` refused every other tag.
  ADR-0071 and ADR-0134 tell a release from a development build by the
  image tag's form: a bare `X.Y.Z` is a release, and a development
  build takes a commit prefix or an `X.Y.Z-<prerelease>`.
  `0.1.0-dev.16` is a valid `X.Y.Z-<prerelease>`, so under those rules
  `tooling/image-build.sh` accepts it for a development build. The
  repositories are IMMUTABLE, so such a push would take the release's
  version for good, and a reader of a repository could not tell the two
  apart.

## Decision

1. **Until launch a release is a git tag `v0.1.0-dev.N`,
   `v0.1.0-rc.N` or `v0.1.0`,** N a positive integer without a leading
   zero. The image tag is the tag without the `v`, as ADR-0039 says.
2. **`tooling/release-version.sh` is the guard.** `release.yml` runs it
   first and takes the image version from what it prints. It accepts
   exactly the three forms and, for any other tag, exits non-zero and
   says nothing was published. `tooling/release-version-test.sh` drives
   it, and `make -C tooling ci` runs that test.
3. **The two lines are reserved to releases, whole.** A development
   build takes a commit prefix or an `X.Y.Z-<prerelease>` outside
   `0.1.0-dev.*` and `0.1.0-rc.*`. `tooling/image-build.sh` refuses
   every tag in those lines, one the release guard would refuse
   included, so a tag of either line is never a development build's.
   A bare `X.Y.Z` stays refused as before. A commit prefix is hex
   digits alone and a release tag holds a `.`, so the two cannot meet.
4. **From `v0.1.0` the form is `vX.Y.Z` again,** as ADR-0039 gives it.
   The guard accepts exactly the three forms until then, and the change
   that follows launch widens it. The names `0.1.0-dev.N` and
   `0.1.0-rc.N` stay reserved, because they will have been released.
5. **The push role is unchanged.** Its subject ends in
   `ref:refs/tags/v*`, which `v0.1.0-dev.16` matches, and a run on any
   other ref still gets no credential.

## Consequences

- **A reader still tells a release by its tag form,** now one of the
  three. A repository holding `0.1.0-dev.5` holds a release, never a
  build a person made.
- **A tag outside the form publishes nothing,** `v0.9.0` among them.
  The workflow at an older commit has no guard and takes the old
  pattern, so a `v*` tag pushed at such a commit runs that commit's
  workflow. Renaming the existing tags and releases is the PM's and is
  not done by this change.
- **A development build loses `0.1.0-dev.*` and `0.1.0-rc.*`.** Its
  prerelease takes another identifier, such as `0.1.0-wip.1`.
- **The release body and the chart read the new form unchanged.** The
  body names `<repository>:<version>` and the chart's image tags take
  any tag the repository holds.
- **Whether a repository's lifecycle policy keeps these tags is
  garamsh/aurora's.** Decision 14 has aurora change its ADR 0033 and
  that policy before an image tag moves. Nothing here reads either, so
  the first release under this form is where a policy that does not
  know it would show.

## Signs the decision was wrong

- An image tagged `0.1.0-dev.N` or `0.1.0-rc.N` is found in a
  repository that no `release.yml` run pushed it to.
- A lifecycle rule expires an image under one of the forms.
- A release is needed under a tag the guard refuses, and the guard is
  edited around rather than the form decided again.
- A development build needs a tag in a reserved line.

## Rejected alternatives

- **Leaving development builds free to use any prerelease.** ADR-0134
  allows it, and it lets a hand push take `0.1.0-dev.16` before the
  release does and read as the release.
- **Reserving only the tags the guard accepts.** `0.1.0-dev.0` would
  then be a development build that reads as a release the guard
  refuses.
- **A guard accepting any `vX.Y.Z-<prerelease>` and `vX.Y.Z`.** The
  owner's decision names three forms, and `v0.9.0` is the tag it exists
  to refuse.
- **A prefix on development builds, such as `sha-` or `dev-`.**
  ADR-0071 rejected it, and the commit prefix already cannot be read as
  a version.
