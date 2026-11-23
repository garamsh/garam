# ADR 0181: A release publishes the chart as an OCI artifact under the release version, and a consumer pins it by digest

> Status: accepted; built in `.github/workflows/release.yml`,
> `tooling/chart-push.sh` and `deploy/charts/Makefile`; supersedes in
> part
> [ADR-0042](./0042-a-chart-is-the-description-of-a-garam-deployment.md)
> §Consequences' "Nothing packages or publishes the chart" and its
> pinning of the chart by a git ref alone
> Date: 2026-11-23

## Context

ADR-0042 makes the chart the description of a deployment and says how a
consumer references it: "a consumer pins the chart by a git ref of
`garamsh/garam` and the images by tag", because "nothing packages or
publishes the chart" and `Chart.yaml`'s `version` "names no garam
version".

Goal #2012 deploys garam on a cluster whose platform repository,
aurora, takes charts as OCI artifacts in ECR, pinned by digest
(aurora#814 criteria 5 and 6). The repositories for it are
`486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam/charts/garam`
and `.../codehub/charts/codehub` (aurora#829). They are created by
aurora's apply, which has not run when this is decided, and they are
pushed by the push role the images already use
(`.github/workflows/release.yml`).

Two facts constrain the version. The ECR repositories are IMMUTABLE
(ADR-0039), so one chart version cannot be pushed twice, and
`Chart.yaml`'s `version` stays the same across releases whenever the
chart has not changed. And a release is `v0.1.0-dev.N`, `v0.1.0-rc.N` or
`v0.1.0` (ADR-0157), the version the images carry.

## Decision

A release publishes the chart as an OCI artifact to the repository
`garam/charts/garam` of the registry the images go to, under the
release's version, and a consumer pins it by the digest the registry
returns.

1. **The published version is the release's.** The chart is packaged
   with `--version` and `--app-version` set to the release version, so
   the chart and the images of a release carry one version, as
   ADR-0039 asks of the images. `Chart.yaml`'s own `version` is the
   source's, still moved by a change to the chart (ADR-0057), and is not
   what is published. ADR-0042's sentence that it "names no garam
   version" holds for the source file and not for the artifact.
2. **A job of its own pushes it.** The `chart` job of `release.yml` runs
   after the images and `release` does not wait for it. A repository
   that does not exist yet fails that job alone, with a message saying
   so, and the job is re-run once the repository exists; the release,
   its images and their digests are not held back.
3. **The digest is what a consumer pins.** The push's digest is recorded
   in the job's step summary and as the job's `chart-digest` output. It
   is not in the GitHub release's notes, which the `release` job writes
   without waiting for the chart.
4. **What this replaces in ADR-0042.** Its sentences "A consumer pins
   the chart by a git ref of `garamsh/garam` and the images by tag" and
   "Nothing packages or publishes the chart, so a tag or a commit SHA of
   this repository is the reference" no longer hold: a release
   publishes the chart, and the artifact is pinned by digest. A git ref
   of `garamsh/garam` remains a way to read the source. The rest of
   ADR-0042, including that the chart is the description of a
   deployment, stands.

## Consequences

- A release whose chart is not yet published can exist, and the
  published chart for a version appears when the job succeeds. A
  consumer reading the release finds the images' digests there and the
  chart's in the job's summary.
- One chart version, pushed once. A re-run of the job after a success
  fails on the immutable repository; the failure names the tag.
- Two version numbers describe the chart, the source's and the
  artifact's. README §Pinning a version states both, and a reader
  comparing `Chart.yaml` with a published artifact's `version` finds
  them different by design.
- The first run of the job is the first check of the digest format:
  `tooling/chart-push.sh` reads the `Digest:` line of `helm push` and
  fails on an answer without a `sha256:` digest, which its test covers
  with a stand-in for helm and not with helm's own output.

## Signs the decision was wrong

- A consumer needs to tell two published charts of one release apart,
  because the chart changed after the tag. A release is immutable, so
  this would show as a request to republish under a suffix.
- The `chart` job fails for a reason other than a missing repository on
  releases made after the repository exists, which would mean it does
  not belong beside the release.
- Consumers keep reading `Chart.yaml`'s version off the artifact and
  mistake it for the source's, which would mean the two numbers should
  not differ.
