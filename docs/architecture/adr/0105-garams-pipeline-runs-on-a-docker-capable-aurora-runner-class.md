# ADR 0105: garam's pipeline runs on a Docker-capable aurora runner class

> Status: accepted
> Date: 2026-10-17

Accepted by the Garam PM on 2026-10-18 under the owner's delegation,
recorded in the review on #1480.

Drafted for #1477, which reserved this number. garam is read at
`57fcdd227b19d57b0f613c89a4bf5b7511475f4d`, `garamsh/aurora` at
`4df93d567fddb9afe9e5120449053222dee2b7c7`, and the Actions Runner
Controller chart at its tag `gha-runner-scale-set-0.15.0`, commit
`1945b42820760eb50893c8da77fdfc42cf8dad45`. A claim below that names no
revision holds for these.

## Contents
- Context
- What the probe measured
- What each job needs on aurora's kubernetes class
- The four options
- Decision
- What aurora would have to change
- The AGENTS.md exception
- The runner label and its record
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives

## Context

No forge run of this repository executes a step. Issue #1477 records
every job of every run at `steps=0`, with an annotation that names the
account rather than the diff. Run 38885187625, on promotion #1471,
shows `ci`, `e2e` and `web-e2e` each at `steps=0`, no runner, label
`ubuntu-24.04`
(`gh api repos/garamsh/garam/actions/runs/38885187625/jobs`), and its
`ci` and `e2e` check runs carry "The job was not started because
recent account payments have failed or your spending limit needs to be
increased" (`gh api repos/garamsh/garam/check-runs/<id>/annotations`).
The issue records every merge since as resting on the PM's local run
under `AGENTS.md` §Roles, "A run that was never dispatched is an
absent check". A `v*` tag cannot publish either: `release.yml` names
`ubuntu-24.04` too.

`garamsh/aurora` runs a runner scale set for the organisation
(`kubernetes/components/gha-runner-scale-set/values.yaml`):

- `githubConfigUrl: https://github.com/garamsh` and
  `runnerScaleSetName: aurora`, so a workflow names it as
  `runs-on: aurora`.
- `minRunners: 3` and `maxRunners: 3`, one per worker. aurora's
  `docs/architecture/runners.md` §Capacity holds that count to what
  memory admits: a second slot fits no worker.
- `containerMode.type: kubernetes`, with a 60Gi Longhorn work claim per
  runner. The container hook builds each job as a job pod of 4 CPU and
  8Gi requested and 8 CPU and 8Gi limited
  (`templates/hook-template.yaml`).
- `ACTIONS_RUNNER_REQUIRE_JOB_CONTAINER` keeps the chart's `"true"`, so
  every job declares `container:`. aurora's `secrets.md` rests on it:
  the runner container's token can read the `github-app` Secret, and
  workflow steps run in the job pod under a ServiceAccount no role
  binds.
- `arc-runners` enforces Pod Security `baseline`
  (`kubernetes/applications/gha-runner-scale-set.yaml`), which admits
  no privileged container.

Four of this repository's six jobs reach a Docker daemon.
`make db-up` and `make backend-up` go through
`deploy/local/docker-compose.yml`, and `make web-image-e2e` through
`docker build`, `docker network` and `docker run` in
`scripts/image-build.sh` and `tests/image/web.sh`; `release.yml`'s
`images` builds and pushes through `docker/setup-buildx-action`,
`docker/build-push-action` and `scripts/image-push.sh`.

What #1477 could not read was whether the organisation's runner group
admits `garamsh/garam`: the runner-group API answered 403 to the PM's
token.

## What the probe measured

A branch `probe/aurora-runner-1477` held one workflow,
`.github/workflows/probe-aurora-runner-1477.yml`, at commit
`05d39b84c775ea57da0f89285be5c06cb3f24a89`: `on: push` for that branch
alone, `permissions: contents: read`, no secret, two jobs on
`runs-on: aurora` in the job container
`docker.io/library/buildpack-deps:trixie@sha256:ffbfa3ee92287a5fa41c03118bfe9a75c095838bccbe1173322854b8f2387116`,
the image and digest aurora's own `ci.yml` names. The branch was
deleted after the run was read; `git ls-remote origin
refs/heads/probe/aurora-runner-1477` returns no line where the same
call for `refs/heads/dev` returns one.

Run 39051854702, created 2026-10-17T17:00:25Z, concluded `success`
(`gh api repos/garamsh/garam/actions/runs/39051854702/jobs`; logs from
`gh api repos/garamsh/garam/actions/jobs/<job>/logs`):

| Job | Id | Runner | Group | Labels | Steps | Started, completed |
|---|---|---|---|---|---|---|
| `probe` | 117451888310 | `aurora-runner-jjjsh` | `default` | `["aurora"]` | 10 | 17:00:28Z, 17:01:06Z |
| `services` | 117452038970 | `aurora-runner-7lfvr` | `default` | `["aurora"]` | 5 | 17:01:08Z, 17:02:40Z |

- **The runner group admits this repository, and the account's
  payments do not stop a self-hosted job.** A runner took `probe` 3 s
  after the run was created.
- **The job ran as the hook template says.** `NODE_NAME=worker-01`,
  `RUNNER_ENVIRONMENT=self-hosted`, uid 0, `nproc` 18,
  `cpu.max 800000 100000`, `memory.max 8589934592`. The runner logged
  `Current runner version: '2.338.0'`. `ImageVersion` was unset.
- **No daemon reaches the job.** No `docker` on `PATH`, no
  `/var/run/docker.sock`, `DOCKER_HOST` unset.
- **No daemonless builder that needs a user namespace can run there.**
  `/proc/sys/user/max_user_namespaces` read `0`, and
  `unshare --user --map-root-user true` failed with `Operation not
  permitted`. `CapEff` read `00000000a80425fb`, which holds no
  `CAP_SYS_ADMIN`.
- **A run step's shell is `sh -e`,** not bash, in this image.
- **`actions/checkout` works in the job container,** with the image's
  git 2.47.3; `git rev-parse HEAD` read the probe commit once
  `safe.directory` named the workspace.
- **A service container is reached on `localhost` and not by its
  name.** `services:` started `postgres:17.11-alpine3.24` at the digest
  `deploy/local/docker-compose.yml` pins; a TCP connect succeeded on
  `localhost:5432` and failed on `postgres:5432` within 60 s, which is
  what a service sharing the job pod's network namespace gives.

## What each job needs on aurora's kubernetes class

| Job | Fits | What it would need |
|---|---|---|
| `ci.yml` `ci` | yes | The job container above by digest; the setup actions as now; a `safe.directory` step before `make ci`. `make test`'s image scripts put a recording `docker` on `PATH` and reach no daemon. Whether `make ci` finishes inside 8Gi was not run. |
| `ci.yml` `e2e` | with a changed invocation | `services: postgres` at compose's pinned digest with `POSTGRES_DB: garam_test`, `TEST_DATABASE_URL` set to that service on `localhost:5432`, and `make integration e2e` without `make db-up`, which the Makefile's `?=` admits. `make db-up` is then no longer run by any check. |
| `ci.yml` `web-e2e` | no, not without a new entry point | `make backend-up` builds the server image and runs `migrate`, `authz` and `api` through compose. A service is started before the first step, so it cannot be an image this job builds from the tree. It needs an entry point that runs the three from `go build` output against a Postgres service, and a Chromium with its system libraries: the Playwright image for `@playwright/test` 1.62.1 by digest, or `playwright install --with-deps` as the uid 0 the probe read. |
| `images.yml` `images` | no | Two `docker build`s, then `docker network` and `docker run` against the web image. The job pod has no daemon; rootless BuildKit needs the user namespace the node refuses; rootful BuildKit and Docker-in-Docker need a privileged container `baseline` refuses. A remote builder elsewhere in aurora could build, and `tests/image/web.sh` would still have nothing to run the image on. |
| `release.yml` `images` | no | The same builds, then `docker push` of both. A remote builder could push from the builder, and the push-neither-until-both-built guarantee (ADR-0039 §Consequences, #945) would move into that builder's output handling. `id-token: write` on a self-hosted runner was not probed: the probe took no OIDC role. |
| `release.yml` `release` | yes, once the runner record is replaced | `actions/checkout`, `scripts/record-runner-image.sh` and `softprops/action-gh-release`, in a job container. |

Two jobs fit with setup changes alone, one with a changed invocation,
and three need what aurora's kubernetes class excludes by design.

## The four options

| Option | What runs on the forge | What garam changes | What aurora changes | Local and CI run the same names |
|---|---|---|---|---|
| A. All jobs on `aurora`, adapted | Every job, once each is adapted | Job containers, `services:`, a backend entry point without compose, and a builder and image runtime for both image jobs | A builder service, privileged or user-namespaced, and a place to run the built web image | No: `db-up`, `backend-up` and the image targets stop being what CI runs |
| B. Split | `ci`, `e2e`, `release`; `web-e2e` after a new entry point | As A for those jobs; `images` and `release.yml` `images` stay on `ubuntu-24.04` | Nothing | No, for `e2e` and `web-e2e` |
| C. A second runner class with a Docker-in-Docker sidecar | Every job, as written | `runs-on:` labels and `scripts/record-runner-image.sh` | A second scale set, its namespace, its slot and its runner image (§What aurora would have to change) | Yes |
| D. Stay on the local gate | Nothing | Nothing | Nothing | Yes, by running nothing on the forge |

## Decision

garam's six jobs run on one runner class that provides a Docker
daemon (option C). Every job keeps the entry points it invokes today,
so the pipeline stays one shape and `docs/convention/ci.md` §Core
principle keeps holding: CI re-runs what a contributor already ran, by
the same names.

What garam asks of aurora is **a Docker-capable runner class isolated
from the nodes that hold cluster secrets**. How the class is realised
is aurora's, and garam ranks the ways in this order:

1. **Isolated.** The class runs on a node that holds no cluster
   Secret and no workload that does, such as a dedicated worker or VM.
   A job that reaches that node through `dockerd` then reaches nothing
   but the node.
2. **Fallback: `dind` on a shared worker.** A second aurora scale set
   in `containerMode.type: dind` on a worker that also holds cluster
   secrets. aurora may choose it where the first is unavailable. It
   carries the node-scope exposure §What aurora would have to change
   names.

The exposure is garam's problem as well as aurora's. Every worker
holds a `garam-postgres` instance and an OpenBao voter, and ADR-0104
(#1479) lets an OpenBao Transit provider hold garam's message signing
keys there.

No job moves to `runs-on: aurora` ahead of that class. Until aurora
delivers it the local gate stands as it does (option D), and the
workflow-label change is the pull request that follows the class, not
this one.

## What aurora would have to change

Each of these is aurora's to decide; this records what the option asks
for, not how aurora answers. The items assume a `dind` scale set,
whether on a dedicated worker or a shared one. The node-scope exposure
below is what the isolated realisation in §Decision removes and the
fallback keeps, and a node outside the three workers also changes the
slot item.

- **A scale set in `dind` mode, in a namespace of its own.** The chart
  at 0.15.0 builds that mode with a `dind` sidecar running `dockerd`
  under `securityContext.privileged: true`, sharing the socket with the
  runner container. `arc-runners` enforces `baseline`, so the class
  needs a namespace enforcing `privileged`.
- **A decision on what a privileged node exposes.** Workflow code on
  this class reaches `dockerd`, which can start a privileged container,
  which holds the node. aurora's `secrets.md` isolation is at pod
  scope, and this class removes it at node scope: every Secret and
  token mounted on that node is reachable from a job there, including
  a kubernetes-mode runner's token if one stands beside it. aurora
  places one runner per worker, and its §Capacity table puts a
  `garam-postgres` instance and an OpenBao voter on every worker.
- **Restricting the class to the repositories that need it.** A runner
  group that selects `garamsh/garam` keeps the exposure off every other
  repository's workflow code. That is an organisation setting and the
  owner's.
- **A slot from the existing memory.** aurora's runners.md §Capacity
  holds each worker to one slot and records the owner's decision of
  2026-10-17 that no RAM is added. A `dind` runner standing idle on a
  worker beside a kubernetes-mode runner leaves that runner's job pod
  failing admission when its job arrives (runners.md §Open
  questions, "Nothing reserves a job pod's 4 CPU and 8Gi"). So the two
  classes need placement that keeps them on different workers, and the
  class pair holds three slots between them. `minRunners: 0` on the
  `dind` class holds none while garam runs nothing; garam's workflows
  run on a promotion pull request and a `v*` tag only (§Operation of
  [README.md](../README.md)).
- **A work volume.** The chart's `dind` mode mounts the work directory
  as an `emptyDir`. aurora's `values.yaml` records 24.89 GiB of
  ephemeral storage per node and chose a work claim for that reason.
  What garam's jobs write was not measured.
- **A runner image carrying what the jobs call.** In `dind` mode the
  steps run in the runner container. `make`, `git` and `python3`
  (`tests/image/web.sh`) are what garam's jobs call beyond the setup
  actions, and what `ghcr.io/actions/actions-runner:2.338.0` carries
  was not read here.
- **`NODE_NAME` and the runner image reference in the runner's
  environment** (§The runner label and its record).

## The AGENTS.md exception

`AGENTS.md` §Roles admits a merge on a local run in two states. This
decision changes the first and leaves the second.

- **"The run started nothing" stops being reachable for a job on an
  aurora class.** Run 39051854702 executed on a self-hosted runner
  while every hosted job on this repository stayed at `steps=0`, so
  the account's payments are not what dispatches such a job. A job
  waiting for a runner is queued, not undispatched, and "aurora is
  down" is the judgement §Roles already says reaches neither state.
- **The exception ends for base `main` on the first promotion pull
  request whose head carries the label change.** `pull_request` reads
  the workflow files of the merge, so that pull request's own run is
  the first one dispatched on the class. From it on, (iv) is owed by no
  promotion.
- **"No workflow watches the base" is unchanged.** `ci.yml` and
  `images.yml` keep `branches: [main]`, so a pull request into `dev`
  still starts nothing, and this decision does not reopen that filter.

The text of `AGENTS.md` is the PM's. The pull request carrying this ADR
proposes the wording in its Convention concerns rather than making it.

## The runner label and its record

A runner class label is a reference no ecosystem can make immutable:
the scale set behind it is whatever aurora's `main` holds when Argo CD
syncs. `docs/convention/ci.md` §Verify a pinned dependency before
adopting it then owes a mover and a record on each run.

- **The mover is aurora.** Its `values.yaml` pins the runner image by
  digest, and the `dind` image beside it would be pinned the same way.
- **The record replaces `scripts/record-runner-image.sh`'s
  `ImageVersion`,** which a self-hosted runner does not set (the probe
  read it unset). What a run resolved to on this class is the runner
  image, the runner and the node. The runner version is already in the
  job's set-up log. `RUNNER_NAME` is set by the runner. `NODE_NAME` and
  the runner image reference are not, and aurora's runner template sets
  them: the first through the downward API, as its hook template does,
  and the second as a literal beside the `image:` it names. The script
  writes all three into the step summary and fails when one is unset,
  as it fails today.
- **The label change and its §Operation pairing land together, after
  this ADR is accepted.** That pull request moves every `runs-on:` in
  `ci.yml`, `images.yml` and `release.yml` to the class aurora names,
  replaces the script, and updates [README.md](../README.md)
  §Operation, which records where this repository's checks run.

## What this does not decide

- The account's payment state or the repository's visibility. Either
  would return the hosted runners, and both are the owner's.
- The name of the second class, its slot size, its placement and its
  namespace. Those are aurora's.
- Whether the kubernetes-mode `aurora` class later takes the jobs that
  fit it. This decision keeps the pipeline on one class.

## Consequences

- garam's checks run on the forge again for every promotion and every
  release, on runners another repository owns. A change that breaks
  aurora's ability to run a job breaks this repository's gate with it,
  as aurora's runners.md §This repository's own checks run only on
  `aurora` records for aurora itself.
- No workflow job changes shape: `make db-up`, `make backend-up` and
  `make web-image-e2e` stay what CI runs.
- garam's jobs wait behind every organisation repository's jobs on the
  same three slots.
- Until aurora delivers the class nothing changes here: the local gate
  stands, and a `v*` tag still publishes nothing.
- A job on that class can reach the node it runs on. On the isolated
  realisation that node holds nothing else; on the fallback it holds
  cluster secrets garam's own deployment depends on, which is why
  garam ranks the two rather than leaving the choice unweighted.

## Signs the decision was wrong

- aurora declines a privileged class, or delivers one no worker has
  memory for. The pipeline then has no runner, and A or B is the
  option left.
- garam's jobs on the class queue for longer than the local gate takes
  to run, because three slots are shared across the organisation.
- The class is realised on a worker that holds cluster secrets, and a
  job escapes to the node.
- A build on the `dind` class fails on the `emptyDir` work directory's
  space where it passes locally.
- The jobs that fit aurora's kubernetes class turn out to be the ones
  that change, while the Docker-bound ones stay put. That would make
  the split cheaper than this ADR priced it.

## Rejected alternatives

- **A. All jobs on `aurora`, adapted.** Three jobs cannot run there as
  aurora stands. The image jobs need a builder aurora would have to
  host privileged, since the nodes refuse user namespaces, and
  `tests/image/web.sh` still needs a runtime to run the image it
  checks. The jobs that can be adapted stop running `db-up` and
  `backend-up`, so CI would test a second way of running the checks.
  That is the drift `ci.md` §Core principle exists to prevent.
- **B. Split.** Every promotion would carry two records, a forge run
  for some jobs and a local run for the rest, and `e2e` and `web-e2e`
  would run under CI-only invocations. It is the per-job shape the
  owner asked this decision not to take. It also leaves
  `release.yml`'s `images` on a hosted runner, so a `v*` tag still
  publishes nothing.
- **D. Stay on the local gate.** It costs aurora nothing and keeps the
  current record. It leaves every first-run check owed, #1466's
  included, and `release.yml` unable to publish a version. As a
  standing answer it accepts that this repository has no pipeline.
  It stands only until aurora answers C.
