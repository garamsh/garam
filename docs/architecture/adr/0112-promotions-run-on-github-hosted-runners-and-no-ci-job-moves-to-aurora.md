# ADR 0112: Promotions run on GitHub-hosted runners and no CI job moves to aurora

> Status: proposed
> Date: 2026-10-18

Drafted for #1512, which reserved this number. It supersedes
[ADR-0105](./0105-garams-pipeline-runs-on-a-docker-capable-aurora-runner-class.md).
garam is read at `d447cb60f99a34e61832bccf617e2936b784bbd3`. A claim
below that names no revision holds for it.

## Context

ADR-0105 chose option C: every job on a Docker-capable aurora runner
class, with the local gate standing until aurora built it. Its first
sign that the decision was wrong reads "aurora declines a privileged
class … The pipeline then has no runner, and A or B is the option
left." That sign fired. The aurora owner closed garamsh/aurora#642,
the request for that class, as not planned at 2026-10-18T01:30:58Z
(`gh api repos/garamsh/aurora/issues/642 --jq .state_reason` answers
`not_planned`).

The garam owner then gave the direction this ADR records, on
2026-10-18 (#1512): a promotion is verified by GitHub's own runners,
and no CI job moves to aurora.

ADR-0105's other options were measured there and are not taken:

- **A. Every job on `aurora`, adapted.** The image jobs need a
  builder aurora's nodes cannot host unprivileged, and
  `tests/image/web.sh` needs a runtime for the image it checks
  (ADR-0105 §What each job needs on aurora's kubernetes class).
  aurora's refusal of #642 rules out the privileged builder too.
- **B. Split: the jobs that fit on `aurora`, the rest hosted.** It was
  drafted and probed for #1512 before the owner's direction was read
  correctly. The branch `sauce-git/ci-split-1512`, kept unmerged as a
  record at `ba600464a9ff92f737dbe801007bfa91839bd62a`, moved `ci.yml`'s
  `ci` and `release.yml`'s `release` to `runs-on: aurora`. Probe pull
  request #1516, into `main` and closed unmerged, measured it:
  - `release` passed on aurora in runs 39088377064 and 39088643237,
    with its publishing step, `needs: images` and its write permission
    removed for the probe.
  - `ci` ran 16 steps in run 39088643260 and failed in `make test`
    alone. Every other target of `make ci` passed.
  - The one failing test,
    `TestRun_ReadsAgainAnEntryTheSystemCouldNotOpen`, refuses a read
    with mode `0o000`. The job container runs as uid 0, which that
    mode does not refuse. aurora's
    `kubernetes/components/gha-runner-scale-set/templates/hook-template.yaml`
    at `4df93d567fddb9afe9e5120449053222dee2b7c7` sets no
    `securityContext` "by decision (issue #348)", so a job runs as its
    image's default user, root for the Debian-based image the branch
    pinned. A contributor runs `make ci` unprivileged.
  - The branch changed the test to refuse the read in a way root also
    meets (`e52214e9e96d3f52c0be5054c5afe4d2bcda6f84`). The run that
    would have shown it on aurora, 39103272063, was cancelled during
    `make ci` when the direction was corrected. That change is not
    carried here.

  So B was workable at the price of a container whose user differs
  from a contributor's, and a promotion carrying two records, a forge
  run for two jobs and a local run for four. It is not what the owner
  asked for.

**Hosted minutes were refused, and that ended on 2026-10-18.**

- **The refusal.** Promotion #1508, at
  `d447cb60f99a34e61832bccf617e2936b784bbd3`, started runs 39084547388
  (`images.yml`) and 39084547402 (`ci.yml`) at 2026-10-18T00:55:14Z.
  In their first attempt all four jobs, `images`, `ci`, `e2e` and
  `web-e2e`, show `steps=0`, no runner and the label `ubuntu-24.04`
  (`gh api repos/garamsh/garam/actions/runs/<run>/attempts/1/jobs`).
  Each job's check run carries the annotation "The job was not started
  because recent account payments have failed or your spending limit
  needs to be increased"
  (`gh api repos/garamsh/garam/check-runs/<job>/annotations`).
- **What ended it.** The repository went public on 2026-10-18
  (`gh api repos/garamsh/garam --jq .visibility` answers `public`).
  Attempt 2 of run 39084547402, at the same head and started at
  2026-10-18T07:00:37Z, ran each of its three jobs on a GitHub-hosted
  runner (`gh api repos/garamsh/garam/actions/runs/39084547402/attempts/2/jobs`):
  `ci` took 13 steps and passed, `e2e` took 8 and `web-e2e` took 12.
  `e2e` failed in `make db-up integration e2e` and `web-e2e` in
  `make backend-up`, each on what its own steps ran. Attempt 2 of run
  39084547388 ran `images` in 6 steps and passed. Those failures are
  verdicts on the head, not undispatched jobs.

## Decision

- **A pull request into `dev` is verified by the PM's local run.** No
  workflow watches `dev`: `ci.yml` and `images.yml` carry
  `branches: [main]`.
- **A promotion, `dev` into `main`, is verified by the GitHub
  workflows on GitHub-hosted runners.** `ci.yml` and `images.yml` run
  on `ubuntu-24.04`, as they already declare, and `release.yml` runs
  there on a `v*` tag.
- **No job runs on aurora's runners,** now or as a planned target.
  aurora stays the cluster garam is deployed to.

No workflow, `Makefile` or code changes under this decision.

## The AGENTS.md exception

`AGENTS.md` §Roles' "A run that was never dispatched is an absent
check" admits a merge on a local run in two states. This decision
leaves one reaching a promotion and the other reaching `dev`.

- **"The run started nothing" no longer applies to a promotion.** It
  held for #1508's first attempts, every job at `steps=0` with the
  annotation naming the account. It ended with attempt 2 of run
  39084547402, whose jobs started. A promotion's hosted run is now its
  verdict, and a failed job there is a failure on the diff.
- **"No workflow watches the base" is unchanged.** `ci.yml` and
  `images.yml` keep `branches: [main]`, so a pull request into `dev`
  starts nothing and rests on the PM's local run.

Should hosted runs stop starting again, the first state returns. It is
read off the run, `steps=0` and the annotation, as before, and not
assumed from this record.

## Consequences

- The pipeline keeps one shape. Every job runs the entry points it
  runs today, so `docs/convention/ci.md` §Core principle holds without
  a CI-only invocation, and no job container or runner record changes.
- A promotion is verified on the forge again, with no change in this
  repository: the workflows already named `ubuntu-24.04`. Its jobs'
  failures, such as attempt 2's `e2e` and `web-e2e`, are fixed on
  `dev` under `docs/convention/git.md` §Promotion rather than merged
  around.
- A `v*` tag starts `release.yml` on hosted runners, whose jobs can
  now start as well.
- garam's gate does not depend on aurora's runners or queue behind
  other repositories on aurora's three slots.
- The hosted runs depend on the account's visibility and billing,
  which are the owner's. This decision changes neither.

## Signs the decision was wrong

- Hosted runs stop starting again, through a visibility or billing
  change, for long enough that promotions rest on the local exception
  indefinitely. The forge then verifies nothing, and a self-hosted
  option is the one left.
- A hosted job fails where the same entry point passes locally, on a
  property of the runner image the job cannot control.
- What the hosted runs cost the account becomes the reason to move
  jobs off them.

## Rejected alternatives

- **C. A Docker-capable aurora class** (ADR-0105's decision). aurora
  declined it (garamsh/aurora#642, not planned).
- **A. Every job on `aurora`, adapted.** See §Context.
- **B. Split between `aurora` and hosted runners.** See §Context.
- **D. The local gate as the standing answer.** It held for
  promotions only while billing refused the hosted runs. Once the runs
  started, it would mean ignoring a forge run that exists.
