# ADR 0043: Every workload the chart renders runs as an account of its own, named after the workload

> Status: accepted
> Date: 2026-08-25

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0042](./0042-a-chart-is-the-description-of-a-garam-deployment.md)
made `deploy/charts/garam/` the description of a garam deployment and
enumerated what it covers: the five workloads and the Services they need
to reach each other. Identity is not in that enumeration, and the chart
merged under it rendered none — no `ServiceAccount` and no
`serviceAccountName` — so every pod it renders presents the namespace's
`default` account (#503).

Three facts make that a regression rather than a missing feature, each
recorded in #503 against the cluster it was read on:

- **`serve machine` needs a role.** It reads the key-encryption key
  every organization's certificate authority is sealed under
  ([ADR-0016](./0016-ca-key-custody.md)) and refuses to start without it
  (`internal/cli/machine.go`). The account it runs under on the live
  cluster is named `garam-machine` and carries the role that reads that
  key; `default` carries none.
- **A subject every pod presents is not a subject a role can be scoped
  to.** `system:serviceaccount:<namespace>:default` is what postgres,
  the web app and the migrate Job all present, so a role trusting it is
  held by anything that lands in the namespace. #503 records three
  credentials placed for one workload each and blocked on exactly that.
- **The names cross a repository boundary.** ADR-0042 left consumption
  to `garamsh/gitops` and assumed nothing about it. That repository runs
  hand-written manifests and creates accounts of its own, so a name this
  chart renders and a name that repository binds a role to have to be
  one name; where they differ, adoption is a rename.

## Decision

### The chart renders a ServiceAccount per workload and binds each workload to it

Five workloads, five accounts. Each Deployment and the migrate Job sets
`serviceAccountName`, and `<workload>.serviceAccount.create` defaults to
`true`, so a deployment that supplies nothing gets an identity per
workload rather than one shared subject.

One account for the release is refused: it is the namespace `default`
under a better name. What makes `default` wrong is that every pod
presents it, and an account every workload presents has that property
too.

### The name is derived from the workload, and no value changes it

The account is `<release>-<component>` — the workload's own name, which
is `garam-api`, `garam-authz`, `garam-machine`, `garam-migrate` and
`garam-web` under a release named `garam`. The chart offers no value for
it.

This is a contract with another repository, not a preference. A name
each side sets separately is a name they can disagree about while both
renders succeed, and the failure surfaces as a pod that cannot reach
what its role was written for. `deploy/charts/garam/README.md`
§Workload identities carries the table so a consumer matches the names
without reading templates.

### Annotations are per account, and the chart holds none

`<workload>.serviceAccount.annotations` sets them, because binding a
cloud role to one account is what annotations on that account do. Which
key performs the binding belongs to the platform the deployment runs on,
so the chart carries no key and no default.

### `create: false` leaves the object to the deployment and keeps the binding

Turning creation off does not turn identity off: the workload still
names the same account, and the deployment supplies one under that name.
That is the path for a repository that created the account before
adopting the chart — it keeps the object it made, and adoption is not a
rename. An opt-out that dropped the binding instead would return the
pod to `default`, which is the defect this decision exists to remove.

### `chart-identity` checks the rendered identity, and every chart check states what it is complete over

`make chart` gains a third named task. It reads the workload-to-account
table in the chart's README, renders each value file under `ci/`, and
fails when a workload names no account, when a rendered pair is not the
declared one, when an account a workload names is not rendered, or when
the table itself declares two workloads under one account.

`chart-render` sweeps the source for environment names and is exact
about them; it reads nothing else a pod carries, and #503 records the
review that verified it fired correctly and did not ask what it covered.
So each chart check now names what it is complete over — in the line it
prints and in `deploy/charts/garam/README.md` §What the checks are
complete over, which also names what no check reads: volumes and mounts,
probe paths and ports, the security context, resource figures, and the
hook annotations on the migrate Job.

## Consequences

- **A workload added to the chart now carries an identity or fails the
  check.** The obligation is paid by the change that adds the workload,
  which is where the decision about its subject belongs.
- **garam now publishes names another repository depends on.** Renaming
  a workload renames its account, and that reaches whatever binds a role
  to it. A rename is a change to the table in the chart's README, and
  the check fails until the two agree — but nothing here can fail on
  behalf of the other repository, which learns of it by reading.
- **A deployment must still create the roles.** The chart renders the
  subject a role is scoped to and nothing that grants one: the role and
  its trust policy live in whatever store holds the credential, outside
  this repository and outside the cluster.
- **The check reads only what the chart renders.** A deployment setting
  `create: false` renders no account, and nothing here sees the object
  it supplies instead.
- **`make chart` renders each value file twice**, once in `chart-render`
  and once in `chart-identity`. The two checks stay one job each, which
  `docs/convention/ci.md` §One entry point per task asks for, and the
  cost is a second render of five templates.

## Rejected alternatives

- **One ServiceAccount for the release.** One object instead of five,
  and one name for a consumer to bind to. It reproduces what makes
  `default` wrong — a subject every workload presents — so the role that
  reads the key-encryption key would be held by the web app and the
  migrate Job as readily as by the machine listener.
- **An account for the machine listener alone.** It fixes the one case
  where the failure is unrepairable and leaves every other workload on
  `default`, so the next credential placed for one workload meets the
  same widening. The cost of the other four is four objects.
- **A settable account name per workload.** It is what most charts
  offer, and it would let a consumer keep names it already uses. It also
  makes the cross-repository contract a value two sides set
  independently, which is the disagreement this decision exists to
  prevent; a consumer that must keep another name has a rename, and a
  rename is visible.
- **Extending `chart-render` rather than adding a check.** One target
  and one render. It makes one name do two jobs, which
  `docs/convention/ci.md` §One entry point per task refuses, and it
  would leave the environment sweep and the identity comparison sharing
  one output line — the line whose scope #503 records as having been
  read too widely.
- **Checking identity against the templates rather than against the
  README.** It removes the declaration a reader has to keep current. It
  also removes the artefact another repository reads: a table a check
  fails on is a contract, and a table nothing reads is a description
  that drifts.
