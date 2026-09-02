# ADR 0057: A template edit moves the chart version, and a reviewer is what checks it

> Status: accepted
> Date: 2026-09-03

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[ADR-0042](./0042-a-chart-is-the-description-of-a-garam-deployment.md)
§Decision states the rule in one sentence: `Chart.yaml`'s `version`
"moves when the chart changes and names no garam version".
`deploy/charts/garam/README.md` §Pinning a version carries the same
sentence, and no responsibility document in `docs/architecture/` states
it at all.

Two readings of "the chart changes" answer one case differently: a
template edit that changes what an install renders without changing any
value a deployment supplies. The chart's own history took both. Read on
2026-09-03 over `deploy/charts/garam/`:

| Commit | What it changed | `Chart.yaml`'s `version` |
|---|---|---|
| c8b6123 (#500) | the chart's publication | set to `0.1.0` |
| 583076e (#504) | the account each workload runs as — a value a deployment supplies | `0.1.0` to `0.2.0` |
| ff185d8 (#763) | PodSecurity fields on five templates and `_helpers.tpl` | unchanged |
| 0a0bfb9 (#732) | object names and a pod field, across six templates | unchanged |
| cedb376 (#635) | `fsGroup` on `templates/machine.yaml` | unchanged |

The last three change what an install renders and left the version where
it stood. So the practice reads the rule as *moves when what a consumer
supplies changes*, and the sentence reads as *moves when the chart
changes*; the two agree everywhere except on the case above, which is
the ordinary one.

Nothing here detects the difference. `make chart` runs eight sub-checks
— `chart-lint`, `chart-render`, `chart-identity`, `chart-names`,
`chart-rewrite`, `chart-guards`, `chart-security` and `chart-package` —
and no target compares `Chart.yaml`'s `version` against anything:
`grep -rn 'Chart\.yaml' Makefile scripts/ *.sh` returns nothing, while
the same search for `values.yaml` returns the targets that render them.
`chart-package` runs `helm package`, which reads the file to name the
artifact, and that reading cannot fail on a version that did not move.
So a contributor meets this rule nowhere, which is how three changes
took the other reading without anyone noticing.

Raised on #929 as a Convention concern and measured in #931.

## Decision

- **The rule as written binds, and this ADR confirms ADR-0042 rather
  than superseding it.** ADR-0042 keeps its `accepted` status and its
  §Decision sentence stands unchanged and unqualified. A reader who
  finds this ADR later is reading the same decision stated against the
  case that divided, not an overruling of it.
- **A template edit that changes the render moves the version, even
  when no value a deployment supplies changes.** What a deployment
  installs is the rendered documents, not the values it supplied, so a
  change to a template is a change to the chart.
  `deploy/charts/garam/README.md` §Pinning a version tells a deployment
  to pin a git ref, and the version is the only thing that tells it the
  render underneath that pin has moved. A pin whose output changes
  silently is not a pin.
- **The three commits are not revisited.** ff185d8, 0a0bfb9 and cedb376
  keep the version they merged with. A version is not retroactively
  bumped: it names what the chart was at the ref a consumer pinned, and
  rewriting it now would make two different charts claim one number.
  This clause exists so the question is closed rather than reopened
  against each of them in turn.
- **Nothing checks this, and nothing is added to.** It is held by
  review, under `docs/convention/review.md` §What each check verifies,
  whose Architecture row already fails a diff where "a decision or a
  domain changed without the documents that describe it" — a rule about
  what a diff must carry alongside another change, which is what this
  is. Recording that here is what stops the next measurement filing the
  absence as an unnoticed gap.

## What this does not decide

- **How far the version moves.** Major, minor or patch is the change
  author's, and no rule here derives it from what the diff touched.
- **Whether `Chart.yaml` ever gains an `appVersion`.** ADR-0042 decided
  it carries none and its 2026-09-02 erratum records that `v0.1.0`
  landed and set none; this ADR reads only `version`.
- **Whether a check could be built later.** The rejected alternative
  below states what makes one hard, not that it may never exist. What
  would change the answer is a way to run it that satisfies
  `docs/convention/ci.md` §Core principle.

## Consequences

- **A change touching `deploy/charts/garam/templates/` carries a
  `Chart.yaml` edit.** That is a second file in a diff whose author may
  see no need for it, paid on every template change rather than by
  whoever deploys next.
- **The version moves more often than a release does.** It names no
  garam version (ADR-0042), so a chart at `0.5.0` beside images at
  `0.1.0` is the expected reading and not a mismatch to reconcile.
- **A reviewer who misses it leaves no trace.** No run reports the
  omission, so the failure mode is exactly the one #931 found: a
  correct-looking green on a change that skipped the bump. What this
  buys is that the reviewer now has a decision to cite instead of two
  defensible readings.
- **The three commits stay as evidence.** A reader comparing the
  history against this rule finds three counter-examples, which is why
  §Decision names them rather than leaving them to be discovered.

## Rejected alternatives

- **An erratum on ADR-0042.** Rule 2 in [README.md](../README.md)
  allows one where "a decision stands but a fact supporting it was
  wrong". No fact under that sentence was wrong: the sentence said what
  it says, and three changes read it the other way. An erratum would
  record a correction that is not one, and rule 2's own text sends a
  changed or restated decision to a new ADR.
- **Superseding ADR-0042.** Supersession records a decision that
  changed, and this one did not. It would also mark the chart's
  founding ADR as overruled over a clause about a version field, which
  costs a later reader the whole of what ADR-0042 still decides.
- **Taking the practice's reading — the version is the contract's
  version.** It is coherent, and it is what a `values.yaml`-shaped API
  would want: what a deployment supplies is unchanged, so nothing it
  wrote needs revisiting. This chart also ships templates, and the 23
  documents `chart-render` reported rendering from them on 2026-09-03
  are what a deployment gets. Under
  that reading a consumer pinning a ref cannot tell a render that moved
  from one that did not, which is the reader ADR-0042 wrote
  §Pinning a version for.
- **Adding a check that reads the diff.** It is the only shape that
  could catch this: the rule is about what a diff must contain, so a
  check for it needs a base to compare against. A working tree has
  none, so such a check runs in a pipeline and not on a contributor's
  machine, against `docs/convention/ci.md` §Core principle — local and
  CI checks are the same checks, invoked by the same names — and §One
  entry point per task, which refuses an entry point that becomes a
  pipeline component. It would also spend runner minutes on every pull
  request to catch a case that arises on the few that touch the chart;
  #899 measured a superseded run at four job-runs and
  `docs/architecture/README.md` §Operation records the accounting a
  merge is held to.
- **Leaving it unstated and correcting it case by case.** That is the
  state #931 measured: two readings, both defensible, three commits on
  one of them, and nothing to cite in a review. The cost of stating it
  is this file; the cost of not stating it was paid three times without
  being noticed.
