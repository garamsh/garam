# ADR 0134: An image lives at its publishing repository, and its tag says what a build is

> Status: accepted
> Date: 2026-10-31

Drafted for #1716, which reserves this number. The direction is the
owner's of 2026-10-28, recorded on #1655 and on #1716. garam is read
at `b957e78197a96a2c6bb15ebe6308331c4979370e`, and garamsh/aurora at
`8aada7c09a959531e152d670273552d050d2d005`, the tip of its `main` on
2026-10-31; a claim below that names no revision holds for those.

## Context

- **The name repeated its prefix.**
  [ADR-0039](./0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  named `garam/garam-server` and `garam/garam-web`, so the publisher
  stood twice in each.
- **Three images had no repository.** garam builds five service images
  and a release published two. `images.yml` checked the site's,
  cowork's and cowork-web's images, each built by its own check from
  arguments no release read (#1714, #1717).
- **Development images had a namespace of their own.**
  [ADR-0095](./0095-development-images-live-under-dev-and-garam-holds-releases-only.md)
  and then
  [ADR-0097](./0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md)
  split `garam/` from a development namespace, which gave each image two
  homes and two grants. The owner's costs accepted there are the ones
  this decision removes.
- **The repositories are aurora's.** Its ADR 0033
  (`docs/architecture/adr/0033-a-published-ecr-repository-is-its-publishing-repository-and-its-image.md`,
  garamsh/aurora#710) declares the five repositories below. Per
  #1716's comment of 2026-10-28, `garam/server` and `garam/console`
  hold all 22 release tags, copied by digest from the old repositories.
  `garam/test`, and the retirement of `garam-dev/`, wait on
  garamsh/aurora#711, which is open.

## Decision

1. **An image's repository is `<publishing repository>/<image>`.** The
   image name never repeats the prefix.
2. **One image has one repository, holding every build of it.** There
   is no development namespace, so `garam-dev/` goes away. This is
   [ADR-0071](./0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)'s
   "Each image has one repository, and it holds every build of that
   image", restored under these names.
3. **The tag says what a build is.**
   - A release tag is the version without the `v`, `X.Y.Z`, which only
     `.github/workflows/release.yml` publishes, on a `vX.Y.Z` git tag
     (ADR-0039). It never moves and never expires.
   - A development tag is ADR-0071's commit prefix, or its
     `X.Y.Z-<prerelease>`.
   - The test image's moving pin stays the prefix `pinned-`
     ([ADR-0093](./0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)).
4. **The images and their names:**

   | Repository | Built from | Before |
   |---|---|---|
   | `garam/server` | `platform/`, `deploy/local/Dockerfile` | `garam/garam-server`, `garam-dev/garam-server` |
   | `garam/console` | `platform-web/`, `deploy/local/Dockerfile.web` | `garam/garam-web`, `garam-dev/garam-web` |
   | `garam/site` | `site-web/`, `deploy/local/Dockerfile.web` | none |
   | `garam/cowork` | `cowork/`, `deploy/local/Dockerfile.cowork` | none |
   | `garam/cowork-web` | `cowork-web/`, `deploy/local/Dockerfile.web` | none |
   | `garam/test` | `platform/tests/testprincipal/` | `garam-dev/garam-test` |

5. **A `vX.Y.Z` tag publishes the five service images,** building all
   five before it pushes any, and the release records each one's digest
   beside its reference. The site bakes the address the console bakes,
   so [ADR-0056](./0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)'s
   binding to the release name `garam` reaches it too. cowork-web has no
   `/api/*` rewrite and bakes no address.
6. **What a pull request builds is what a release publishes.**
   `make image-build` builds all five from `release.yml`, once each, and
   the image checks read its output rather than building their own.
7. **The test image moves once garamsh/aurora#711 creates its
   repository, `garam/test`.** Until then `tooling/image-test-push.sh`, `tooling/image-test-pin.sh`
   and `tooling/image-test-ecr-test.sh` name `garam-dev/garam-test`, and
   ADR-0093's repository stands in practice; the change that moves them
   completes #1716.
8. **This supersedes** ADR-0097, ADR-0093's repository URI and
   ADR-0039's repository names and its two images. ADR-0095 was already
   superseded by ADR-0097, so nothing of it remains. The rest of those
   ADRs stands: the trigger, the version rule, the labels, and who
   pushes.

## What this does not decide

- **The repositories, their mutability and their lifecycle.** They are
  garamsh/aurora's; how a tag rule is expressed there is its #711.
- **Deleting the old repositories,** which is the owner's approval.
  `garamsh/aurora`'s manifests pin the old names until they repoint,
  and its ADR 0033 keeps those repositories until then.
- **Deploying cowork.** The chart at `deploy/charts/garam/` deploys
  neither `garam/cowork` nor `garam/cowork-web`.

## Consequences

- **A repository's content is no longer the list of releases.** A
  reader tells a release from a development build by the tag form.
- **A deployment running a development build sets the tag alone,** and
  no longer the repository.
- **A person pushing a development build needs a credential for the
  release's repository.** Which identity holds one is aurora's, and the
  release's role still admits `v*` tags alone.
- **The check's local names move** to `garam/<image>:ci`, as do the
  GitHub Actions cache scopes `tooling/image-build.sh` writes, so the
  first run after this reads no cache for those images.
- **A release costs five builds,** and a pull request touching more
  than `docs/` builds the same five.
- **The chart's defaults change,** so a consumer's next chart version
  pulls the new names. Those repositories hold what the old ones held
  for the server and the console, under the same digests per #1716.

## Signs the decision was wrong

- A development build is found holding a tag a deployment reads as a
  release, or a lifecycle rule expires a release tag.
- An operator's grant to push a development build is found reaching a
  release tag it could overwrite or take first.
- A publishing repository needs two images of one name.

## Rejected alternatives

- **Keeping `garam-dev/`.** ADR-0095's costs, two homes and two grants
  per image, buy a separation the tag form already gives; the owner
  chose one repository.
- **Keeping `garam/garam-server` and `garam/garam-web`, adding only the
  three new images.** The rule then holds for three of five.
- **Publishing the two old images and leaving the others to a later
  change.** The three would keep a build path no release read.
- **Checking each image from a build of its own, as before.** It builds
  each image twice and checks an image no release publishes.
