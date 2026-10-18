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

**Hosted minutes are refused today.** Promotion #1508, at
`d447cb60f99a34e61832bccf617e2936b784bbd3`, started runs 39084547388
(`images.yml`) and 39084547402 (`ci.yml`). All four jobs, `images`,
`ci`, `e2e` and `web-e2e`, show `steps=0`, no runner and the label
`ubuntu-24.04`
(`gh api repos/garamsh/garam/actions/runs/<run>/jobs`). Each job's
check run carries the annotation "The job was not started because
recent account payments have failed or your spending limit needs to be
increased"
(`gh api repos/garamsh/garam/check-runs/<job>/annotations`). Billing
is the owner's.

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

Until hosted minutes return, a promotion's runs start nothing, and
`AGENTS.md` §Roles' "A run that was never dispatched is an absent
check" applies to it. Its state "The run started nothing" is read off
each run: every job at `steps=0`, and the annotation naming the
account rather than the diff, as #1508's runs show. The PM merges a
promotion on the local run that clause describes, and owes its (iv)
on each one.

That state ends on the first promotion run whose jobs start. From that
run on, the hosted run is the promotion's verdict, and the exception
no longer reaches base `main`. Restoring the minutes is the owner's,
and no change in this repository ends the state.

"No workflow watches the base" is unchanged for pull requests into
`dev`.

## Consequences

- The pipeline keeps one shape. Every job runs the entry points it
  runs today, so `docs/convention/ci.md` §Core principle holds without
  a CI-only invocation, and no job container or runner record changes.
- Until the minutes return, no forge run verifies anything here.
  Every promotion rests on the PM's local run, and a `v*` tag
  publishes nothing, because `release.yml`'s jobs start nothing either.
- When the minutes return, the promotion's runs verify it on their
  own, with no change in this repository.
- garam's gate does not depend on aurora's runners or queue behind
  other repositories on aurora's three slots.
- Hosted minutes cost the account. That cost is the owner's to accept,
  and this decision changes no visibility or billing setting.

## Signs the decision was wrong

- Hosted minutes stay refused for long enough that every promotion
  rests on the local exception indefinitely. The forge then verifies
  nothing, and a self-hosted option is the one left.
- A hosted job fails where the same entry point passes locally, on a
  property of the runner image the job cannot control.
- The owner restores billing and the hosted runs' cost becomes the
  reason to move jobs off them.

## Rejected alternatives

- **C. A Docker-capable aurora class** (ADR-0105's decision). aurora
  declined it (garamsh/aurora#642, not planned).
- **A. Every job on `aurora`, adapted.** See §Context.
- **B. Split between `aurora` and hosted runners.** See §Context.
- **D. The local gate as the standing answer.** It holds today because
  billing refuses the hosted runs, not by choice. The decision keeps
  the workflows in place so they take over the moment the runs start.
