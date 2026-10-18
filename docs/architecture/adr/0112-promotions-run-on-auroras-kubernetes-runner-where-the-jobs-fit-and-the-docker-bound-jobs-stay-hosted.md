# ADR 0112: Promotions run on aurora's kubernetes runner where the jobs fit, and the Docker-bound jobs stay hosted

> Status: proposed
> Date: 2026-10-18

Drafted for #1512, which reserved this number. It supersedes
[ADR-0105](./0105-garams-pipeline-runs-on-a-docker-capable-aurora-runner-class.md)
and takes that record's option B. garam is read at
`d447cb60f99a34e61832bccf617e2936b784bbd3` and `garamsh/aurora` at
`4df93d567fddb9afe9e5120449053222dee2b7c7`, the commit ADR-0105 read
and still the tip of aurora's `main` on 2026-10-18. A claim below that
names no revision holds for these.

## Contents
- Context
- Decision
- Which job runs where
- What aurora's class asks of a job
- The runner label and its record
- The AGENTS.md exception
- What ci.md costs
- What the run measured
- Consequences
- Signs the decision was wrong
- Rejected alternatives

## Context

ADR-0105 chose option C, a Docker-capable aurora runner class, and
kept every job on `ubuntu-24.04` until aurora delivered it. Its first
sign that the decision was wrong reads "aurora declines a privileged
class … The pipeline then has no runner, and A or B is the option
left." That sign fired: the aurora owner closed garamsh/aurora#642 as
not planned at 2026-10-18T01:30:58Z
(`gh api repos/garamsh/aurora/issues/642 --jq .state_reason` answers
`not_planned`). No hardware is added, and no privileged `dockerd` is
allowed in the cluster beside a `garam-postgres` instance or an
OpenBao voter. The owner of both repositories then directed garam to
take the split, which is option B (#1512).

Hosted jobs on this account still start nothing: each shows
`steps=0`, with an annotation naming the account's payments
(ADR-0105 §Context). Run 39051854702 showed that a job on `aurora`
does run (ADR-0105 §What the probe measured).

ADR-0105 §What each job needs on aurora's kubernetes class measured
each job, and that measurement is what this decision rests on. One
part of it is out of date. It measured `e2e` before #1482
(`af6b783cbcd6664d0d665a944300d946afc84a68`) added OpenBao to
`make db-up`. `make integration` now needs that OpenBao's container,
and `tests/integration/transit_signing_test.go` reads the dev
server's CA and root token with `docker exec <container> cat`
(`inContainer`). The job pod has no `docker` and no daemon, so the
`services:` and `TEST_DATABASE_URL` ADR-0105 priced no longer make
`e2e` fit.

## Decision

Every job that fits aurora's kubernetes-mode class with setup changes
alone runs on `runs-on: aurora` in a job container pinned by digest:
`ci.yml`'s `ci` and `release.yml`'s `release`. Every other job stays
on `ubuntu-24.04` unchanged. Each one waits on something named in
§Which job runs where, and moves when that lands. No entry point is
added and no `services:` is declared, because neither moved job needs
one.

The job container runs as root, by aurora's decision, and garam's
`make ci` is made to pass as root rather than the container made to
run as a contributor's user. One test assumed a reader that a file's
mode refuses, which root is not. It now refuses the read in a way
root also meets (§What the run measured).

## Which job runs where

| Job | Runs on | Waiting on |
|---|---|---|
| `ci.yml` `ci` | `aurora` | — |
| `release.yml` `release` | `aurora` | — |
| `ci.yml` `e2e` | `ubuntu-24.04` | #1513: `make integration` reaches the dev OpenBao without a Docker daemon. Today it names the container (`OPENBAO_CONTAINER`) and reads its CA and root token through `docker exec` (`transit_signing_test.go` `inContainer`). The server also starts with `-dev-tls`, so a `services:` OpenBao needs its command overridden, which the container hook was not probed for. |
| `ci.yml` `web-e2e` | `ubuntu-24.04` | #1514: an entry point that brings up migrate, authz and api without compose. `make backend-up` builds the server image and runs all three through `deploy/local/docker-compose.yml`, and a service container cannot be an image the job builds from the tree (ADR-0105 §What each job needs). |
| `images.yml` `images` | `ubuntu-24.04` | A Docker-capable class. Two `docker build`s, then `docker network` and `docker run` against the web image. |
| `release.yml` `images` | `ubuntu-24.04` | A Docker-capable class. The same builds, then `docker push`, under the push-neither-until-both-built guarantee (ADR-0039 §Consequences, #945). |

The two image jobs stay where they are for a reason no follow-up here
removes. aurora's nodes refuse the user namespace rootless BuildKit
needs, `arc-runners` enforces `baseline`, which admits no privileged
container, and aurora declined the class that would provide one.
What retires their hosted label is a Docker-capable runner class,
which is aurora's to offer, or a change in the account's payment
state, which is the owner's.

`release` runs only after `images`, and `images` starts nothing on
this account. So a `v*` tag still publishes nothing, and a
self-hosted `release` does not change that until `images` runs
somewhere.

## What aurora's class asks of a job

These are aurora's constraints, read at the revision above and
measured by run 39051854702. Each moved job meets them:

- **A job container.** `ACTIONS_RUNNER_REQUIRE_JOB_CONTAINER` keeps
  the chart's `"true"`, so a job without `container:` fails. Both
  moved jobs name `docker.io/library/buildpack-deps:trixie` at the
  digest aurora's own `ci.yml` runs `make ci` in. It carries `make`,
  `git` and the shell tools the Makefile calls. Go and Node come from
  the setup actions, as on a hosted runner. No bot moves that pin, so
  it is the PM's to move under `AGENTS.md` §Roles, as its site in
  each workflow and `.github/dependabot.yml` record.
- **A trusted workspace.** checkout trusts the workspace only in a
  temporary global config it removes when it finishes. `ci` therefore
  adds `safe.directory` before any step that reads the tree with git.
  `release` reads none.
- **A 4 CPU / 8Gi job pod.** It is requested at 4 CPU and limited at
  8 CPU and 8Gi (`templates/hook-template.yaml`). Whether `make ci`
  finishes inside 8Gi is what §What the run measured reads.
- **The 60Gi work claim** each runner holds on Longhorn. Checkout,
  module and pnpm caches and `make ci`'s output are written there.
- **Root.** The hook template gives the job container no
  `securityContext` "by decision (issue #348): the container runs as
  its image's default user, root for Debian-based images", because
  every repository on the scale set shares the template
  (`templates/hook-template.yaml`). `buildpack-deps:trixie` declares
  no other user, so the job runs as uid 0, where a contributor runs
  `make ci` as an unprivileged user.
- **No Docker daemon.** `make ci` reaches none: `make test`'s image
  scripts put a recording `docker` on `PATH`
  (`scripts/image-build-test.sh`, `scripts/image-test-ecr-test.sh`).
- **Three slots for the organisation.** garam's jobs queue behind
  every other repository's on the same three runners.

aurora changes nothing under this decision.

## The runner label and its record

`aurora` is a label no ecosystem can make immutable: the scale set
behind it is whatever aurora's `main` held when Argo CD last synced.
`docs/convention/ci.md` §Verify a pinned dependency before adopting it
then owes a mover and a record on each run.

- **The mover is aurora.** Its `values.yaml` pins the runner image by
  digest, `ghcr.io/actions/actions-runner:2.338.0@sha256:4ffadc00…`.
- **The record.** `scripts/record-runner-image.sh` reads
  `RUNNER_ENVIRONMENT`. On `github-hosted` it keeps writing
  `ImageVersion`, and on `self-hosted` it writes `RUNNER_NAME` and
  `NODE_NAME`. The runner sets `RUNNER_NAME`. aurora's hook template
  sets `NODE_NAME` in the job container from the pod's
  `spec.nodeName`. Any other value of `RUNNER_ENVIRONMENT`, or an
  unset name, fails the step, as an unset `ImageVersion` already did.
  The runner's version is in the job's set-up log
  (`Current runner version`), so the run records it already.
- **What the record leaves out is the runner image reference.** No
  variable in the job container carries it. ADR-0105 §The runner
  label and its record asked aurora to set one beside the `image:` it
  names. That is not needed here: the version in the set-up log names
  the runner release, and aurora's pin names the digest at whatever
  commit synced. Should the PM want the digest in each run, the change
  is aurora's and is one `env:` entry in `hook-template.yaml`. This
  decision does not ask for it.

## The AGENTS.md exception

`AGENTS.md` §Roles admits a merge on a local run in two states.

- **"No workflow watches the base" is unchanged.** `ci.yml` and
  `images.yml` keep `branches: [main]`, so a pull request into `dev`
  starts nothing and stays on the PM's local gate.
- **"The run started nothing" no longer describes a promotion's
  `ci.yml` run as a whole.** `ci` runs on aurora with `steps>0`, while
  `e2e` and `web-e2e` still show `steps=0`, and so does every job of
  `images.yml`. The state is written as "Every job of it shows
  `steps=0`". A promotion's `images.yml` run still meets it. Its
  `ci.yml` run does not, although two of its three jobs started
  nothing. The text of `AGENTS.md` is the PM's. The pull request
  carrying this ADR raises the gap in its Convention concerns rather
  than rewording it.

## What ci.md costs

`docs/convention/ci.md` §Core principle holds that local and CI run
the same checks by the same names. The moved jobs keep it: `ci` runs
`make web-install` and `make ci`, as before, and `release` runs no
check. The cost ADR-0105 §Rejected alternatives priced for option B,
a CI-only invocation of `e2e` and `web-e2e`, is not paid here, because
neither job moves. #1513 and #1514 meet that question when they move
their job.

`ci.md` §Reference implementation still says CI runs each of the three
Docker-backed commands as a job of its own. That stays true, so the
convention needs no change for this decision.

## What the run measured

Probe pull request #1516 (`chore/probe-aurora-1512` into `main`,
closed unmerged) ran the jobs this decision moves.

- **`release`** passed on aurora at
  `6ead1cd0cbf15087b0e0a640e772cbfd74f59f13` (run 39088377064) and
  `f5e3edcf83a17d1500ad7ca55396427f2e823ef2` (run 39088643237). The
  probe dropped `needs: images`, the publishing step and the write
  permission, so what it shows is the job container, checkout and the
  runner record on aurora, and not a release.
- **`ci`** ran 16 steps at
  `f5e3edcf83a17d1500ad7ca55396427f2e823ef2` (run 39088643260, under
  `make -k ci` so every target ran) and failed in `make test` alone.
  Every other target of `make ci` passed, `web-test`'s 133 test files
  among them. The `[MSW] Error: intercepted a request without a
  matching request handler` lines in that log fail no test.
- **The one failure was root.**
  `TestRun_ReadsAgainAnEntryTheSystemCouldNotOpen`
  (`internal/delivery/forward_test.go`) made an outbox entry
  unreadable with mode `0o000` and asserted that the forwarder could
  not open it. Root's `CAP_DAC_OVERRIDE` opened it anyway, so the
  entry was offered at once. The same failure reproduces off aurora
  as uid 0 in a user namespace (`unshare -r`), and the test passes
  there as an unprivileged user.
- **The test now holds a write lease on the entry instead**
  (`fcntl` `F_SETLEASE`, `F_WRLCK`). The forwarder opens an entry
  with `O_NONBLOCK`, so an open that would break another descriptor's
  lease fails with `EWOULDBLOCK` for any user, root included. That is
  the forwarder's own open path meeting a failure that is the
  system's and not the entry's, which a mode was only standing in
  for. The test reads the lease back before it relies on it. It
  passes as an unprivileged user and as uid 0 in a user namespace.
  With the forwarder's retry replaced by setting the entry aside, it
  fails at "once it can be read it is forwarded".
- <!-- final run -->

## Consequences

- A promotion's `ci` job runs on the forge again. It is the job that
  carries `make ci`, the whole set, so `ci.md` §One entry point per
  task's record of a run against the change is a forge run for that
  set. The rest stays on the local gate.
- A promotion now carries two records: a forge run for `ci` and the
  PM's local run for `e2e`, `web-e2e` and `images`. `git.md`
  §Promotion's gate reads every job of `ci.yml` and `images.yml`, and
  the three hosted jobs among them still start nothing.
- `release` is ready on aurora, but a `v*` tag still publishes
  nothing, because `images` runs first and starts nothing.
- garam's gate depends on aurora's runners. A change that breaks
  aurora's ability to run a job breaks this gate, and garam's jobs
  wait behind every organisation repository on three slots.
- The Docker-bound jobs keep a hosted label whose runs start nothing.
  They return only through a Docker-capable class or the account's
  payment state, neither of which is garam's.

## Signs the decision was wrong

- `ci` on aurora fails where the same `make ci` passes locally, on
  memory, on the work claim or on a tool the job container lacks, and
  no change in the diff explains it.
- garam's `ci` queues on aurora for longer than the local gate takes
  to run.
- #1513 or #1514 turns out to need an aurora change after all. That
  would mean the split costs aurora something this ADR says it does
  not.
- The account's payments return. Every hosted job would then run, and
  the split would be a cost with no remaining reason.

## Rejected alternatives

- **C. A Docker-capable aurora class** (ADR-0105's decision). aurora
  declined it (garamsh/aurora#642, not planned).
- **A. Every job on `aurora`, adapted.** The image jobs need a
  builder that aurora's nodes cannot host unprivileged, and
  `tests/image/web.sh` still needs a runtime for the image it checks
  (ADR-0105 §Rejected alternatives). aurora's refusal of #642 rules
  out the privileged builder as well.
- **Moving `e2e` now on `services:`, with a CI-only route to
  OpenBao.** It would put a second way of running `make integration`
  into the workflow, which `ci.md` §Core principle forbids. #1513
  changes how the suite reaches OpenBao, after which the same names
  run in both places.
- **Running the job container as a contributor's user.** A job that
  runs as a contributor does would keep a mode-based test meaningful.
  The user is the hook template's, which aurora left without a
  `securityContext` by decision for every repository on the scale
  set, and the pinned image declares no other user. garam does not
  ask aurora to change that. Whether the kubernetes hook honours a
  `container.options` `--user` was not measured.
- **Skipping the test as root.** A skip under uid 0 leaves the job
  that runs as root blind to the path the test exists for: an entry
  the system could not open is read again rather than set aside.
- **D. Stay on the local gate.** It leaves `make ci` running nowhere
  but on a contributor's machine, when a runner that can take it
  exists.
