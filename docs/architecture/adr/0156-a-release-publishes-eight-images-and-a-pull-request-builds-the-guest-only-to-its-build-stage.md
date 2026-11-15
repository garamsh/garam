# ADR 0156: A release publishes eight images, and a pull request builds the guest only to its build stage

> Status: superseded by
> [ADR-0158](./0158-garam-carries-no-gaon-operator-and-a-release-publishes-six-images.md)
> (a release publishes six images again, and ADR-0150 and ADR-0134
> §Decision 6 stand as they did before this one)
> Date: 2026-11-13

Drafted for #1857, which reserves this number. It supersedes in part
[ADR-0150](./0150-a-web-image-reads-the-api-address-when-it-starts-and-a-published-image-is-bound-to-no-release-name.md)
(a release publishes six images) and
[ADR-0134](./0134-an-image-lives-at-its-publishing-repository-and-its-tag-says-what-a-build-is.md)
(§Decision 6, what a pull request builds is what a release publishes),
the second for the guest alone.

## Context

[ADR-0151](./0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)
builds the gaon operator here and
[ADR-0153](./0153-the-gaon-guest-runs-as-root-and-its-supervisor-drops-each-program-to-the-account-it-needs.md)
composes the guest image its microvm boots. Neither has a published
image: a deployment of
[deployment-gaon.md](../deployment-gaon.md) cannot pull what no release
pushes.

The guest's Dockerfile reads three images from the private registry,
each by digest: `sherlock/agent`, `sherlock/workspace` and the adapter.
The release's credential can pull them. The pull request's run holds
none, and its role trusts `v*` tag refs only, so a pull request cannot
build the final stage without a trust change in garamsh/aurora that
nobody has asked for.

## Decision

1. **A release publishes eight images**: the six of ADR-0150 and
   `garam/gaon-operator` and `garam/gaon-guest`, all or none, with
   eight digests in the release body. The names are
   `<publishing repository>/<image>` as ADR-0134 gives them.
2. **The operator is built as the other Go services are**:
   `deploy/local/Dockerfile.gaon-operator`, a scratch image running as
   a numeric account.
3. **The guest reads three private sources by digest**: the agent
   image, the workspace image and the adapter. The adapter is
   `garam/server` of an earlier release, and a pull request moves its
   pin after each release.
4. **A pull request builds the guest only to its `build` stage**, with
   no registry credential and no change of trust. The result is named
   `garam/gaon-guest-build`, which cannot be read as the guest, and
   `tooling/image-build.sh --public-only` makes the reduction for any
   image whose Dockerfile pulls from the private registry. The release
   and `make -C gaon-operator guest-run` build the final stage.

## Consequences

- A pull request that breaks the guest's final stage, a `COPY --from`
  of one of the three sources or the supervisor's start, is not seen
  until a release builds it or a person runs `guest-run`. A release
  whose guest fails to build pushes nothing (ADR-0039), so the cost is
  the run and the next version.
- The adapter pin is behind by at least one release and moves by a
  pull request, so the guest carries the adapter of the release before.
- The digests of the three sources are as stale as their pins; nothing
  here checks that a pin still resolves before a release does.
- `images.yml` checks the operator's image, not the guest's.

## Signs the decision was wrong

- A release fails on the guest's final stage for a cause a pull request
  could have shown, twice.
- aurora gives the pull request's role a pull credential, which would
  make the reduction avoidable.
- The adapter's pin is moved so rarely that a guest ships an adapter
  several releases old.
