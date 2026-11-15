# ADR 0155: garam ships a second Helm chart that renders the gaon kinds

> Status: accepted; built in `deploy/charts/garam-gaon/`
> Date: 2026-11-13

Drafted for #1850, which reserves this number. It supersedes nothing:
[deploy/charts/garam/](../../../deploy/charts/garam/) stays as it is.

## Context

[deployment-gaon.md](../deployment-gaon.md) states every object a gaon
deployment of garam holds, so that aurora can write them from one
document. Aurora would then own a template of garam's, and the
document and the template would drift.

gaon now answers how a chart reaches a cluster: `helm template RELEASE
CHART | gaon apply -f -` applies a stream of gaon kinds, with Helm as
the template engine only (gaon ADR 0035, `cli.md` §Applying a
Helm-rendered stream, at `cfb148dea568674525207bd329a94f1d9ccc912f`).
The stream keeps Helm's order and has no hook, prune or history.

The existing chart renders Kubernetes kinds and is checked as such, so
it cannot also render these.

## Decision

garam ships a second chart, `garam-gaon`, in
`deploy/charts/garam-gaon/`, rendered with the pinned Helm and no other
tool. It emits the kinds of deployment-gaon.md and nothing else: eight
`Deployment`s with `replicas: 1` and `Recreate`, three migration `Job`s,
the `Service`s, and behind `gateway.enabled` the `Gateway`, `HTTPRoute`
and `TLSRoute` of gaon's contract. It creates no `Secret` or
`ConfigMap`: a secret is a reference to one the deployment creates.

Migrations come before services by a required value, `stage`, which
selects one of two renders. A single render would start the services
first, because Helm emits a `Deployment` before a `Job`.

`make -C deploy/charts ci` checks it on the terms of the first chart:
it renders, every setting rendered is one the source reads, every
missing required value refuses the render by its name, no secret value
is in a rendered object or the repository, and every rendered field is
one a gaon file names.

## Consequences

- aurora supplies values and writes no template of garam's.
- A deployer applies two renders in turn and waits between them. That
  script is theirs and is not in the repository.
- The field check is a list of field paths compared by hand with gaon's
  files, not a parse with gaon's types. Parsing needs a Go module here
  (a `go.work` and a dependency-bot entry outside the chart's paths).
  It would reach `MicroVMSpec` and `Service`, which are in gaon's
  importable `lib/api`, and not the wrapper fields of a `Deployment`
  and a `Job` (`replicas`, `selector`, `strategy`, `completions`,
  `backoffLimit`, `template`), which are in `workloads/internal/job`,
  and not the gateway kinds, which no unit serves. A field gaon adds is
  not seen until the list is changed, and a field's value or type is
  not checked.
- Two charts are two things to keep in step with the settings the source
  reads; `gaon-render` sweeps the second's environment names, per
  object for cowork, the policy service and each web app, and over one
  union for the three garam subcommands and the Jobs.
- Every object states `resources` from a value with no default: gaon
  fails a MicroVM that names no cpu or no memory. The figure is the
  deployment's, because the repository holds none.
- The chart renders the identity issuer and the service keys, which the
  cowork policy half needs for its token exchange, and refuses an
  issuer the platform would refuse at start.
- Nothing applies the chart to a gaon cluster. That it works there is
  not shown.

## Signs the decision was wrong

- aurora writes a garam template of its own beside the chart, or
  patches its output.
- A render the check passes is refused by `gaon apply`, which means the
  field list has missed a type's rule.
- A deployer needs an order between kinds that two renders cannot give,
  and asks gaon for hooks.
- The workload wrapper types become importable, or a Go module in
  `deploy/charts` is judged worth its cost, and the field list is
  replaced by a parse, which would show the hand comparison had been
  too weak.
