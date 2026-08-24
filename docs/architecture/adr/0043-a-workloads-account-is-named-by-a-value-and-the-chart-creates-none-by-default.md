# ADR 0043: A workload's account is named by a value, and the chart creates none by default

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
- **The name is a contract with another repository, not an
  implementation detail.** `garamsh/infra` conditions an IAM trust
  policy on the ServiceAccount subject, which is how `garam-machine`
  reads the key-encryption key today; the review on #504 records it.
  Whatever name this chart puts on an account has to be the name that
  trust policy already names.

The third fact is the one that decides the shape here, and it is not
visible from inside this repository. A name is not something the chart
gets to choose well: it is something the chart has to be told.

## Decision

### The account a workload runs as is a value, taken verbatim

`<workload>.serviceAccount.name` names it and the chart composes no
name — not from the release name, not from the chart name, and not as a
fallback when the value is absent.

A composed name moves. `garam.fullname` is the release name, so
`<release>-<component>` renders `garam-api` under a release named
`garam` and `prod-garam-garam-api` under one named `prod-garam`. The
subject in the trust policy does not move with it, so the credential
stops being issued — at runtime, in a pod that was starting yesterday,
with nothing in the render to show why. A first round of this change
composed exactly that name and passed its own check, because the CI
values happen to name the release `garam`.

`garamsh/gitops` holds the same rule in its own tree, from the review on
#504: `apps/openbao.yaml` pins `serviceAccount.name` and takes it
verbatim, and `charts/proxmox-csi-plugin` sets `create: false` with a
comment that a chart-generated name would move a subject the
infrastructure repository owns.

### The chart creates none by default

`<workload>.serviceAccount.name` and `.create` are both empty and false
in `values.yaml`. A deployment that supplies neither gets no
`ServiceAccount` and no `serviceAccountName`, so its workloads run as
the namespace `default` exactly as they did before this chart existed.
The chart renders zero, one or five accounts and the values decide
which.

The cluster needs one today: `serve machine`, for the key above.
`serve api` gains one when Google sign-in lands and reads a cloud-held
secret of its own. `authz`, `web` and `migrate` read none.

Creating five by default would put four subjects nothing uses in a
namespace where the next reader takes each for one that was needed. An
annotated one is worse than unused: `garamsh/gitops`'s #103 records an
annotated account with no role behind it making a consumer attempt
`AssumeRoleWithWebIdentity` against a role that does not exist, which
fails at runtime rather than showing the gap at apply. A default that
creates nothing cannot produce either.

### Identity is per workload, never one for the release

Each workload has its own `serviceAccount` block, and no value sets one
account for the whole release. A single account every workload presented
would reproduce what makes `default` wrong — a subject every pod
presents — so the role that reads the key-encryption key would be held
by the web app and the migrate Job as readily as by the machine
listener.

### `create: false` with a name is the adoption path; `create: true` without one is refused

Naming an account the chart does not create is what a deployment whose
infrastructure repository owns the object needs: the workload runs as
that account, the chart renders no object, and adoption is not a rename.
The reverse pair has no name to create under, and the render fails with
the value it wants rather than composing one — which is the composed
name this decision exists to prevent, arriving through the back door.

### Annotations are per account, and the chart holds none

`<workload>.serviceAccount.annotations` sets them, because binding a
cloud role to one account is what annotations on that account do. Which
key performs the binding belongs to the platform the deployment runs on,
so the chart carries no key and no default.

### `chart-identity` renders under two release names, and every chart check states what it is complete over

`make chart` gains a third named task. It reads the workload table in
the chart's README, renders each value file under `ci/` twice — once as
release `garam` and once as release `prod-garam` — and fails when the
two renders differ, when a rendered workload is not one the table
declares, when the chart renders an account no workload runs as, when
the value file that names no account renders one anyway, or when setting
`create` without a name renders instead of failing.

The two renders are the check the first round did not have. A single
render cannot distinguish a name taken from a value from a name composed
out of the release, because under a release named `garam` the two agree.

`chart-render` sweeps the source for environment names and is exact
about them; it reads nothing else a pod carries, and #503 records the
review that verified it fired correctly and did not ask what it covered.
So each chart check now names what it is complete over — in the line it
prints and in `deploy/charts/garam/README.md` §What the checks are
complete over, which also names what no check reads: volumes and mounts,
probe paths and ports, the security context, resource figures, the hook
annotations on the migrate Job, and the annotation keys on an account.

## Consequences

- **A deployment that wants an identity states the name.** There is no
  name to inherit, which is the point: the name lives in whatever
  repository binds the role, and this chart is told it.
- **A workload added to the chart appears in the README table or fails
  the check.** The obligation is paid by the change that adds the
  workload, which is where the decision about its subject belongs.
- **Upgrading from the first round of this change is a values change.**
  A deployment that installed it received five accounts named
  `<release>-<component>`; on this chart it receives none until it names
  them, and the names it names are its own.
- **A deployment must still create the roles.** The chart renders the
  subject a role is scoped to and nothing that grants one: the role and
  its trust policy live in whatever store holds the credential, outside
  this repository and outside the cluster.
- **Nothing here can check the agreement the contract rests on.** That
  the name a deployment sets equals the name its infrastructure
  repository binds a role to is checkable only where both are readable,
  and that is neither this repository nor this cluster.
- **`make chart` renders each value file four times**, once per release
  name in `chart-identity` and once in `chart-render`, plus one probe
  render and one refusal render per workload. The two checks stay one
  job each, which `docs/convention/ci.md` §One entry point per task asks
  for, and the whole target runs in about five seconds.

## Rejected alternatives

- **A name composed from the release and the component.** It is what
  most charts do, it needs no value, and it guarantees the account and
  the workload share a name. It also makes the name move with the
  release name, which moves a subject `garamsh/infra` conditions a trust
  policy on. The failure is a credential that stops arriving at runtime,
  and it does not appear under the release name CI uses. This is what
  the first round of this change shipped.
- **A composed name as a fallback when no value is given.** It keeps the
  value for consumers that need one and asks nothing of the rest. The
  fallback is the dangerous path, though, and it is the one a deployment
  reaches by forgetting rather than by choosing — so the shape that
  survives a forgotten value has to be no account at all, not a
  plausible one.
- **One account per workload by default.** Five objects, no value to
  set, and every workload separated from `default` on install. It also
  places four subjects nothing uses, and an annotation on one of them
  sends a consumer at a role that need not exist (gitops #103). The
  cluster needs one today and gains a second with Google sign-in; a
  default cannot know which.
- **One ServiceAccount for the release.** One object instead of five,
  and one name for a consumer to bind to. It reproduces what makes
  `default` wrong — a subject every workload presents — so the role that
  reads the key-encryption key would be held by the web app and the
  migrate Job as readily as by the machine listener.
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
- **A third value file rendering exactly the one account the cluster
  needs.** It would exercise the middle of "zero, one or five"
  directly. The templates branch per workload on one value, and the two
  existing files already render that branch both ways, so a third file
  covers no path the check does not already reach —
  `docs/convention/simplicity.md` §Rules refuses the addition on that
  ground.
