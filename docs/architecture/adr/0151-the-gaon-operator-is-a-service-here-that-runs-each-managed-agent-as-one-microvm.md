# ADR 0151: The gaon operator is a service here that runs each managed agent as one MicroVM

> Status: accepted; not built; superseded in part by ADR-0153 (item 3,
> the guest's user) and by
> [ADR-0158](./0158-garam-carries-no-gaon-operator-and-a-release-publishes-six-images.md)
> (item 1's image `garam/gaon-operator`, which no release publishes;
> in whole when `gaon-operator/` leaves)
> Date: 2026-11-13

Drafted for #1825, which reserves this number. The direction is the
PM's on #1813: the operator lives in this repository, it is an
operator in ADR-0012's sense, the Helm chart and
`garamsh/garam-agent-operator` stand, and this is a second operator
and not a port. garam is read at
`210b58da3bcdf9b4e3fd5346dca01a74f9d80e4e`. gaon is read at
`a7bdc57cc75f5ba5df9fcce3175fb41f5498a1f1`, and
`garamsh/garam-agent-operator` at
`ac7c9f3c40b9611f6e2bdf1eba62c14e145b4aec`; a claim below that names
no revision holds for the one of the repository it concerns.

## Context

- **garam dials no cluster, and an operator is what runs agents.**
  An operator holds a garam-issued key, exchanges its assertion at
  `exchangeClientAssertion` for a one-hour token and calls the machine
  listener (ADR-0012, ADR-0102,
  [operators.md](../operators.md)). Its half of the key is unbuilt
  (operators.md §Issued PEM identity). The operator this ADR decides
  is unbuilt, and so is every file below it.
- **Provisioning and runtime are separate facts.** The operator
  reports whether the workload exists and is ready through
  `reportProvisioningState`, one of `pending`, `provisioned`, `ready`
  and `failed`; the agent reports whether it is connected
  (ADR-0012, `platform/api/machine.yaml` `ProvisioningState`). `failed`
  is reserved there for an observation that separates a workload that
  cannot start from one still starting.
- **garam holds no agent definition.** ADR-0118 dropped
  `agent_definitions`, and `platform/api/machine.yaml` has no
  operation that lists the agents assigned to a caller. The desired
  values of a managed agent are the Operator control service's, which
  the cluster controller pulls
  ([agents.md](../agents.md) §Separated execution). In
  `garamsh/garam-agent-operator` that pull is contract C2:
  `GET /v1/operators/self/desired` answers the whole releasable set of
  the proved controller, level-triggered, under a cursor that only
  wakes a long poll, and `POST /v1/operators/self/agents/{agent}/status`
  reports the rendered revision
  (`garamsh/garam-agent-operator@ac7c9f3c40b9611f6e2bdf1eba62c14e145b4aec:docs/architecture/adr/0043-pull-managed-agents-desired-state-from-the-control-service-and-keep-each-grn-on-one-source.md`,
  whose source marker its ADR-0077 supersedes; the feed stands, and
  `…:docs/architecture/agent.md` states that nothing else builds an
  `Agent`).
  Its control service holds no cluster access, by seam, and a
  controller is served only its own agents
  (`…:docs/architecture/adr/0079-run-only-on-our-own-system-and-keep-the-seams-that-allow-a-bring-your-own-operator.md`).
  A pair is one cluster manager and at most one control service, each
  with its own key, and a control service names one manager once
  (ADR-0124, operators.md §An Operator's two identities).
- **What an agent's pod holds today is wide.** In
  `garamsh/garam-agent-operator` a managed agent is a StatefulSet of
  one: the agent container, a workspace container, `garam`'s adapter as
  a native sidecar, init containers that place credentials and make
  directories, two claims, one user for every container, a
  supplementary group, a finalizer fencing the writers on evidence
  they stopped, and a placement token minted at each release
  (`…:docs/architecture/agent.md`, `…:docs/architecture/adr/0042-fence-each-agent-pod-on-positive-evidence-its-writers-stopped-and-mint-an-adapter-only-placement-token.md`).
  garam's own standalone runner keeps the same three-process shape on
  rootless Docker as three containers in one network namespace, user
  65532, two volumes, a SIGTERM drain that never forces, and the
  adapter stopped last ([agents.md](../agents.md) §The host's runner).
  The adapter runs beside exactly one agent because the agent's
  gateway binds loopback and because it must outlive the agent while
  it shuts down ([adapter.md](../adapter.md) §What it is).
- **gaon serves a smaller unit.** The group is
  `compute.gaon.garam.sh/v1alpha1`, cluster-scoped, with no namespaces
  (D4 on `garamsh/gaon#117`). A `MicroVM` has one `image`, one
  `command`/`args`, `env`, `user`, `files` and `ports`, readiness and
  liveness probes, `terminationGracePeriodSeconds`, `volumes` and
  `serviceAccountName`
  (`garamsh/gaon@a7bdc57cc75f5ba5df9fcce3175fb41f5498a1f1:lib/api/microvm.go`).
  Its spec is immutable once created except `runStrategy`,
  `idleTimeoutSeconds`, `resources` and `volumes`
  (`…:lib/api/microvm.go`, `Spec` update rule). Its status is a phase
  (`Pending`, `Running`, `Suspended`, `Succeeded`, `Failed`,
  `Stopped`), `conditions` (`Ready` is written by the Node Agent,
  `LeaseExpired` by the node lifecycle controller), `restartCount`,
  `addresses` and `lastExit` (code and time); a status whose
  `bindingEpoch` differs from the spec's reads as `Pending` through
  `EffectivePhase` (`…:lib/api/microvm.go`).
- **gaon's answer to garam's needs is recorded.** On
  `garamsh/gaon#125` (2026-11-12) gaon's PM answered the twelve needs
  as served: a custom kind through a CustomResourceDefinition with
  `ownerReferences` and garbage collection, a Go client in
  `github.com/garamsh/gaon/lib/api` whose first tag is `lib/api/v0.1.0`
  after #118 and #119 merge, conditions and `lastExit` readable,
  Secrets the operator writes and a Node reads only when a MicroVM
  bound to it references them, and a label-selected `NetworkPolicy`
  (D6 on #117) enforced beside mvmd's per-MicroVM policy. What the
  Resource API document promises and what `main` serves are different
  things, and §Consequences keeps them apart.
- **gaon's answers to garam's later needs are recorded as D17.** The
  answer comment on `garamsh/gaon#164`, which gaon's PM records as D17
  on `garamsh/gaon#117`, says:
  1. a `NetworkPolicy` is a cluster-scoped kind with Kubernetes
     semantics and an `ipBlock` of `cidr` plus `except`, enforced by
     the network plugin in its own nftables table, tracked in #165
     and blocked by #142;
  2. a Secret- or ConfigMap-sourced file is replaced in place in a
     running MicroVM by atomic rename, tracked in #162, which
     supersedes ADR 0020's "not updated while the MicroVM runs" for
     `files` by a new ADR, leaves `env` fixed at start, and needs
     mvmd to be able to replace a file in a running guest;
  3. the CSR signer stays Node-only (ADR 0022), and a client that is
     not a Node either runs as a MicroVM and authenticates with its
     ServiceAccount token (added to #161), or holds an
     administrator-issued certificate from gaon's CA with CN
     `garam-gaon-operator` and no Organization (`deployment.md`);
  4. a Volume is held to at most one (MicroVM, epoch), so a second
     MicroVM naming an attached Volume stays `Pending` until the
     attachment is released;
  5. `runStrategy: Always` restarts the guest, `bindingEpoch` changes
     only at a new binding, and `env` and `files` are resolved again
     at each start (#158);
  6. read-only OCI image volumes are not served in this goal, and are
     an open question in D7's storage contract.

  These are what gaon's PM states and not what `main` serves: #162,
  #161, #165 and #158 are open or unverified at the revision read.
- **Until #162, the credential does not refresh in a running
  MicroVM.** A `valueFrom` of an `env` or a `files` entry is
  "resolved when the MicroVM starts and is not updated while the
  MicroVM runs" (`…:lib/api/microvm_process.go`). garam's agent tokens
  live an hour by default and are replaced by the assignee, and the
  adapter reads each token file before every call and never holds it
  ([agents.md](../agents.md) §Tokens in place of certificates,
  [adapter.md](../adapter.md) §Token mode).
- **garam's side names no Kubernetes API.** The words are prose: "pod"
  at `platform/api/machine.yaml` lines 463, 467, 474, 534, 1925, 1928,
  1946 and 1965, "replica" and "containers" at 1928-1931, and at
  `platform/internal/agent/agent.go` lines 7, 56 and 80.

## Decision

Each point answers one item of #1825. Every alternative not chosen is
listed with the reason.

### 1. A folder `gaon-operator/`, a module of its own and an image `garam/gaon-operator`

- **The folder is `gaon-operator/`**, a service's own tree under
  ADR-0132's first kind, as `cowork/` is: the module, the binary under
  `cmd/`, its tests and its documents. Its module is
  `github.com/garamsh/garam/gaon-operator`, its binary
  `gaon-operator/cmd/gaon-operator` (as `cowork/cmd/cowork`), and its
  image `garam/gaon-operator` under ADR-0134's rule that an image
  lives at its publishing repository. The name says which cluster it
  is an operator for and so cannot be mistaken for
  `garamsh/garam-agent-operator`, which stays the Kubernetes one.
- **Alternatives.** `operator/` and `agent-operator/` name no cluster,
  and the second is one word from the other repository. A
  `<service>-<surface>` name (ADR-0132's second kind) would make the
  operator a surface of `platform/`, and it is a peer of it
  (ADR-0012), authenticating with tokens `platform/` issues
  (structure.md §Services). A folder under `platform/` puts a second
  binary in the server's module and its dependencies in the server's
  `go.sum`, which ADR-0125 chose not to do for `cowork/`.
- **The folder is not created by this pull request.** A service's
  folder is created by its first pull request (structure.md
  §Services), which is the first slice's. That pull request adds the
  module to `go.work` and to `tooling/ci/modules`.
- **The dependency on gaon's client** is
  `github.com/garamsh/gaon/lib/api`, and no tag of it exists at the
  revision read. Until `lib/api/v0.1.0` exists the module takes a
  pseudo-version of a commit of gaon, recorded in the pull request
  that adds it, and takes the tag when it is cut. A tag is the gaon
  PM's to cut.

### 2. No custom gaon kind for an agent: the MicroVM is the record

- **The operator reconciles the control service's desired feed
  directly into a `MicroVM`, a `Secret` and a `Volume`.** It writes no
  `CustomResourceDefinition`. Everything it needs to find again after a
  restart is on the objects it made: a label carrying the digest of
  the agent's GRN (a label value is at most 63 characters of a set
  that holds no `:`, `…:lib/api/labels.go`), a label carrying the
  organization's digest, and annotations carrying the epoch and the
  revision rendered. The objects' names are the digest of the whole
  GRN, as `garamsh/garam-agent-operator` names its objects: `agent-`
  and sixteen hex characters of the SHA-256 of the GRN
  (`…:docs/architecture/adr/0009-construct-a-claimed-agent-from-the-operators-own-configuration.md`,
  whose construction its ADR-0077 supersedes and whose naming
  stands),
  because gaon has no namespace to carry the organization and a name
  is a DNS subdomain.
- **What a kind of its own would add.** A place for a person to write
  an agent by hand, as `Agent` is in Kubernetes; a status of its own
  that holds the evidence the operator needs and cannot recompute,
  which in Kubernetes is the epoch and the writer-fence record
  (`…:docs/architecture/adr/0016-report-what-the-operator-observed-and-stay-silent-where-it-observed-nothing.md`,
  superseded by its ADR-0077, read here as history);
  and one owner for `ownerReferences` so that a `Secret` and a
  `Volume` are collected with it. **What it costs.** A second record
  of what the control service already holds, with the source-marker
  problem `garamsh/garam-agent-operator` met in ADR-0043 and
  removed in ADR-0077 by building an `Agent` only from the feed; a
  CRD the operator registers with a role that can register kinds; and
  a status split in two. Nothing in the first
  slice writes an agent by hand: `garamsh/garam-agent-operator`
  builds no other (`…:docs/architecture/agent.md`).
- **A gaon-side kind would be gaon's or aurora's, not garam's.**
  gaon's PM names a CRD the operator registers as served (D1, D3,
  `garamsh/gaon#125` item 9), so it is available whenever a second
  case needs it.
- **The `ownerReferences` between a `MicroVM`, its `Secret` and its
  `Volume`** are the gaon-side way to collect them together
  (`resource-api.md` §Metadata); whether garbage collection is served
  at the revision read is gaon#120's and not verified.
- **Sign it is wrong.** An operator that needs a durable fact it can
  neither recompute nor put in a label or annotation, or a person who
  needs to write an agent without the control service.

### 3. One MicroVM holds one composed guest image with a supervisor; the credential arrives as a Secret-sourced file

- **The agent is one MicroVM and one image, whose entrypoint is a
  small supervisor that runs the three programs of a generation**:
  the agent runtime, the workspace and garam's adapter, as one user,
  the gateway on the guest's loopback, started in that order and
  drained in the reverse order, without force, with the adapter last.
  It is the standalone runner's contract
  ([agents.md](../agents.md) §The host's runner) moved inside the
  guest: gaon's MicroVM carries one process, and a guest is a machine
  where several processes under one parent are the ordinary shape.
- **What the feed carries for images.** Contract C2 at
  `garamsh/garam-agent-operator@ac7c9f3c40b9611f6e2bdf1eba62c14e145b4aec`
  answers the controller's whole releasable set with each agent's
  latest revision, its profile's settings and its configuration (model,
  embedding, tools, workspace size), and the flags `stopped` and
  `recovery`; it carries no image and no digest, because "the container
  image is neither a profile nor a definition field" and "stays the
  operator's own configuration"
  (`…:docs/architecture/control.md`, ADR-0007 there). The controller
  states the images it runs the other way, by `PUT
  /v1/operators/self/images` (`{name, ref, digest?}`, ADR-0074 there).
  A promise that the feed will name images is not found at that commit.
- **First slice: one composed image per release of `gaon-operator`.**
  The runtime, workspace and adapter images are pinned by digest as the
  runner pins them, and an image built here from the three digests
  carries the supervisor; the three digests are recorded as labels of
  the image and reported by `PUT /v1/operators/self/images`. Where a
  revision, now or later, names an image or digest for an agent that
  the composed image does not carry, the operator starts nothing for
  that agent and reports `failed` with that reason; it never runs an
  agent from other images than the feed names. At the commit read the
  case cannot arise, because the feed names no image. **The limit
  this sets:** in the first slice an agent's version moves with the
  operator's release, and no agent is moved to another image alone.
- **Direction, recorded as a request and no dependency: read-only
  OCI image volumes.** A MicroVM that mounts further OCI images
  read-only as volumes, beside its one root image, would let a guest
  whose root holds garam's supervisor and adapter mount the runtime's
  and the workspace's images at paths, with no build-time composition
  and no container engine in the guest, and digests a feed names would
  be honoured as named. gaon's PM does not serve it in this goal and
  holds it as an open question for an image-sourced Volume in D7's
  storage contract, to be reopened with a need when the first slice
  ships (`garamsh/gaon#164`, item 6). Nothing here waits on it, and
  the supervisor stays garam's either way.
- **User and files.** The user is 65532, as in the runner; each of the
  agent's token files, the garam server root and the control root is a
  `files` entry sourced from a `Secret` key with mode `0400` and the
  MicroVM's user as owner (`…:lib/api/microvm_process.go`, `MicroVMFile`
  and `ValueSource`). No credential is an inline `content` or `value`:
  a Node may read every MicroVM, so the spec is not where a secret
  lives (`resource-api.md` §Authorization, "Reads of MicroVMs and
  Volumes are open to a Node").
- **Storage.** The memory store and the workspace are two gaon
  `Volume`s named by the agent's digest, mounted by `volumes` where
  the runner mounts its two volumes. A `Volume` outlives a MicroVM
  and is removed only by a person, as the runner refuses to replace a
  volume that is gone.
- **The readiness probe** is the supervisor's own listener on a named
  port (`MicroVMPort`, `…:lib/api/microvm_port.go`): ready means the
  three programs run, and says nothing of whether the agent works,
  which is what `ready` means in `ProvisioningState`.
  `terminationGracePeriodSeconds` is the drain's bound, to be set from
  the same value the control service renders.
- **How the credential reaches it.** The operator mints the agent's
  tokens as its assignee (`mintAgentToken`), writes them to a `Secret`
  it labels, and creates the MicroVM naming that `Secret`. The only
  reader of the `Secret` besides the operator is the Node the MicroVM
  is bound to, which the Node authorizer admits for a `get` of one
  `Secret` a MicroVM bound to it references, and no `list` or `watch`
  (`resource-api.md` §Authorization). The operator's ClusterRole may
  `create`, `update`, `delete` and `list` `secrets` limited by label
  selector (gaon#118 and `resource-api.md` §RBAC), and reads an absent
  object by `list`, because a `get` of one is judged on no labels and
  a selector rule refuses it
  (`garamsh/gaon@a7bdc57cc75f5ba5df9fcce3175fb41f5498a1f1:docs/workloads.md`).
- **Refresh in a running MicroVM is gaon's #162.** The alternatives
  were: replace the MicroVM at every token period, which discards a
  guest for a token; lengthen the token to the one-day ceiling alone,
  which leaves a stale file for the whole day; let the guest renew its
  own token, which ADR-0100 rejects because a stolen token would renew
  itself; or have gaon replace a `Secret`-sourced `files` entry in
  place. The decision is the last, which gaon's PM serves in `#162`
  and which needs an mvmd capability that gaon has not confirmed.
- **The interim, until #162 is served.** The operator writes the new
  token to the `Secret` and restarts the MicroVM before the token
  lapses, with the deployment setting `GARAM_AGENT_TOKEN_LIFETIME` at
  its ceiling of one day, so an agent is restarted once in that
  period. A restart is `runStrategy` set to `Halted` and then back to
  `Always` (`resource-api.md`, the state transitions: any state to
  `Stopped` when `Halted`, `Stopped` to `Pending` when `Halted` is
  left), and gaon resolves `files` and `env` again at each start. The
  operator does not replace the MicroVM for this. The token restart is
  the second slice (§7). In the first slice a token that lapses stops
  the agent being served, and the operator reports what it observes of
  the MicroVM, which is not a statement about the token.
- **What a restart is to garam's fence is not established.** The
  epoch does not change at a restart, and a fence keyed on the epoch
  sees no new generation. But adapter.md says that a generation's
  first activation is new intent, that every later one replays it, and
  that an agent resumes "on the next generation or placement its
  assignee starts" (§Token mode). Whether a restarted guest reports a
  new generation, and whether the operator must mint a new placement
  token at a restart as the Kubernetes operator does at each fence
  release, is not settled by adapter.md or agents.md, and is for the
  second slice to establish before it ships.
- **Alternatives for the shape.**
  - *Three MicroVMs, one per program.* The agent's gateway binds
    loopback unless mTLS is configured, and the workspace is reached
    over loopback; separate MicroVMs have separate addresses, so
    every link would need the certificates ADR-0012 refused to
    operate.
  - *A multi-process MicroVM in gaon* (a list of processes sharing a
    network, an order and a stop order). It is a pod, which gaon
    chose not to build; and the order-and-drain rules would be gaon's
    to carry for one tenant. Rejected, and replaced by the recorded
    request for read-only image volumes above.
  - *A container engine inside the guest* (the standalone runner's
    shape moved in whole). It repeats inside the guest what mvmd does
    outside it, and it needs registry credentials in the guest.
  - *The standalone runner's supervisor reused.* It drives a container
    engine and holds its intent in a store beside the volumes
    (`platform/internal/workload`), none of which a guest has.
- **Not measured.** What the supervisor costs to build and keep.

### 4. One policy for every agent, ingress denied, egress by prefix

- **Every agent MicroVM carries the labels `garam.sh/role=agent` and
  `garam.sh/organization=<digest of the organization GRN>`.** One
  `NetworkPolicy` selects `role=agent` and allows no ingress at all, and
  egress to a short list: garam's machine listener, the control
  service, the cluster's DNS address, and any address outside the
  cluster's and the LAN's prefixes on port 443. A MicroVM any
  policy selects is default-deny in that direction (D6 on #117,
  `garamsh/gaon#125` item 12). The wide egress entry below is an
  `ipBlock` of `0.0.0.0/0` with the cluster's and the LAN's prefixes
  in `except`, which is the semantics gaon's PM states (#165).
- **No ingress is needed, so none is allowed.** Delivery is
  agent-initiated and garam dials nothing (ADR-0012); the adapter and
  the agent talk over loopback inside the guest. This is stricter than
  the recipe gaon's answer suggests, which admits the same
  organization label: an agent has no reason to be reached by another
  agent, and a rule that admits it is a path a later flaw would use.
  Tenant separation is then the absence of a route and not a rule per
  organization, so the organization label is carried for audit and for
  a later policy and no second kind of policy is written now.
- **Alternatives.** A policy per organization, as the answer on #125
  describes: one object per tenant to create and delete with it, and
  it enforces nothing the single policy does not. A policy per agent:
  the same, in a larger number. No policy and mvmd's per-MicroVM
  policy alone, which is the outer bound and is not written by the
  operator.
- **Why the last entry is wide.** [ai-gateway.md](../ai-gateway.md)
  says the gateway (ADR-0126) is unbuilt and that "an agent today holds
  its own provider key and calls the provider directly", and a
  provider is known by name and not by a prefix that stays. Until the
  gateway is built the wide entry stands; when it exists the entry
  becomes the gateway's address, and the gateway is what retires it.
- **Enforcement is gaon's.** `NetworkPolicy` and `Network` are
  interpreted by the network plugin and are not in `lib/api` at the
  revision read (`…:lib/api` holds no such type), so a client library
  for it is a need (§What gaon does not yet serve).

### 5. Conditions, deactivation, replacement, exit and restart

- **The report.** It follows `garamsh/garam-agent-operator`'s rule that
  the operator reports what it observed and nothing else, and sends
  nothing where it observed nothing (its ADR-0016, superseded by its
  ADR-0077, read here as history and not as what that repository
  serves).

  | Observed on the MicroVM | Reported |
  |---|---|
  | `Pending`, or a status whose epoch differs from the spec's | `provisioned` once the MicroVM exists |
  | `Ready` true and `Running` | `ready` |
  | `Failed`, with `lastExit` | `failed` |
  | the object was not read | nothing |

  `failed` is reachable here and was not on a StatefulSet, because
  `Failed` and `lastExit` separate a workload that cannot start or
  exited from one still starting; `machine.yaml` reserves the state for
  exactly an observation of that kind. `pending` is never sent: it is
  what garam holds before any report. Each report carries the epoch
  the operator holds the agent at, and a stale one is refused.
- **Deactivation.** The lifecycle authority deactivates, and for a
  managed agent that is the control service, whose feed carries
  `"stopped": true` for an agent a stop holds (contract C2, as above).
  The operator stops the
  workload by setting `runStrategy` to `Halted`, the field gaon lets
  change, and deletes nothing while the stop is unverified. It treats
  gaon's `Stopped` as the evidence the writers stopped, and treats a
  Node whose `LeaseExpired` is true, or a status it cannot read, as no
  evidence and leaves the MicroVM and its `Volume`s as they are, which
  is the rule of `garamsh/garam-agent-operator` ADR-0042 stated in
  gaon's terms.
- **Replacement.** A MicroVM's spec is immutable, so any change of the
  image, user, files or environment is a new MicroVM. The operator
  creates it only after the old one is observed `Stopped` or absent.
  Safety does not depend on that order: a Volume is held to at most
  one (MicroVM, epoch), and a second MicroVM naming an attached Volume
  stays `Pending` until the attachment is released (`garamsh/gaon#164`,
  item 4), so gaon holds the order too; the operator's order is what
  makes the `Pending` wait short. A new MicroVM starts under a new
  placement token, minted at each release as the Kubernetes operator
  does, which §3 lists as not established for a restart. A replacement
  that moves the agent to another operator is the lifecycle
  authority's and arrives at this operator as the agent leaving its
  feed: the operator halts the MicroVM and keeps the `Volume`, as the
  Kubernetes operator keeps the claim. Replacement for a change of
  spec is a later slice, after the second.
- **A MicroVM that exits.** For an active agent the strategy is
  `Always`, which restarts the guest with `bindingEpoch` unchanged and
  resolves `files` and `env` again at each start (`garamsh/gaon#164`,
  item 5). The adapter obtains an activation at each start through the
  lifecycle authority (adapter.md §Activation and runtime status).
  `restartCount` and `lastExit` are what the operator logs. What a
  restart is to the fence is not established (§3). `RerunOnFailure`
  and `Once` are not chosen: an agent that exits cleanly is as
  unwanted as one that fails.
- **Its own restart.** The operator holds no state a restart loses.
  It lists the MicroVMs it labelled, reads the whole feed, and renders
  again what differs: the cursor is in memory and a request without it
  answers the whole set, as in its ADR-0043. A credential Secret that
  exists is left alone and one that is missing is minted again, never
  retried in memory, because a token held only in one process is lost
  with it.

### 6. Identity: a certificate and a label-limited ClusterRole in gaon; a pair registered in garam

- **In gaon**, the operator authenticates in one of two ways, which
  gaon's PM states (`garamsh/gaon#164`, item 3). The CSR signer stays
  Node-only (ADR 0022), so neither is a signed request:
  - *(a) a ServiceAccount token, once #161 is served.* The operator
    runs as a MicroVM with a ServiceAccount, its token authenticates
    as `gaon:serviceaccount:<name>`, and a ClusterRoleBinding to a
    label-limited ClusterRole holds it. gaon's PM recommends it, and
    the long-term choice is this one.
  - *(b) an administrator-issued certificate, available today.* It
    comes from gaon's CA as `deployment.md` describes for the
    scheduler, the controllers and the CLI, with CN
    `garam-gaon-operator` and no Organization (D12 on #117), and RBAC
    binds the user by that CN.

  **The first slice uses the certificate, unless #161 has merged when
  the slice is written.** The role is the same either way: one
  ClusterRole with `microvms`, `volumes` and `secrets` limited by a
  label selector to what it labels, `list` and `watch` of `microvms`,
  and no right to register a kind or bind a role (the escalation rule,
  `resource-api.md` §Authorization).
- **In garam**, the operator is a registered pair: a cluster manager,
  which this service is, and a control service. The control service
  is the lifecycle authority of its agents (ADR-0078) and holds the
  delegation; the operator holds its own key and nothing else of
  garam's. The grant the organization writes to the control service
  names this manager as its controller and covers `iam:agent.activate`
  and the creation operation for the first slice; configuration,
  recovery and replacement are added by a new grant when the slice
  that needs them lands, since a grant is never edited (operators.md
  §Delegated operation authority). `iam:agent.activate` is grant-only.
- **The control service is the existing published image, run
  unchanged.** The pair in a gaon deployment is `gaon-operator` as the
  manager and the control service of `garamsh/garam-agent-operator`
  run from its published image `garam/garam-agent-operator-control`
  in the same registry, registered under its own record and key. A
  manager holds at most one control service and a control service
  names one manager (ADR-0124), so the pair does not share the
  Kubernetes operator's registration. The alternatives: a control
  service written in this repository, which writes a second
  desired-state store for the same values; the operator writing the
  definition itself, which breaks the seam that the control service
  holds no cluster access (ADR-0079 there); and garam holding the
  definition, which reverses ADR-0118.
- **What the image needs to run**, as that repository's `control.md`
  states it: a PostgreSQL store at `CONTROL_DATABASE_URL`, which it
  migrates before it serves; a TLS certificate and key for its API
  listener (`--api-certificate-file`, `--api-key-file`, port 8080
  by default); garam's machine URL and server root; and the key, the
  `client_id` and the token URL of its operator, placed from one key
  issuance; a health listener on 8081. Its settings were read from
  that document and not from the image or its source, and the image's
  registry path and tag were not read.
- **The condition this rests on.** Nothing merges in that repository
  under `garamsh/aurora#749`. The first slice shows the image starting
  and serving C2 outside Kubernetes to a controller that is not its
  own. If it cannot without a change there, that is an escalation on
  `garamsh/aurora#749` and not a patch from this repository.
- **The key lives in a gaon `Secret`** and arrives as a `files` entry
  of the operator's own MicroVM, owned by its user, as operators.md
  asks of the operator's half (one external writer, reread at each
  exchange); until #162 is served the file is not refreshed while the
  MicroVM runs, so a rotated key is read at the next start, which an
  operator that holds no state accepts. The same Secret is created by
  whoever deploys it, never by the operator.

### 7. The first slice

- **In scope.** The operator reads the feed as the proved controller,
  mints an agent's tokens, writes the `Secret`, creates the labelled
  MicroVM and `Volume`s, reports `provisioned` and `ready`, halts the
  MicroVM when the feed deactivates it, and reports `failed` on a
  `Failed` phase. One agent of one organization, started, reported
  ready and stopped on deactivation, under the single `NetworkPolicy`.
- **The second slice** is the token restart: the operator writes
  the new token to the `Secret` and restarts the MicroVM before the
  token lapses (§3, the interim), after establishing what a restart is
  to the fence.
- **Left for later slices**, each its own pull request:
  - replacement for a change of spec, with the writer-stopped
    evidence;
  - token refresh in a running MicroVM, once #162 is served, which
    retires the interim;
  - recovery;
  - the adapter's activation through the control service and the
    placement token;
  - draining under `terminationGracePeriodSeconds`;
  - the gateway's address in place of the wide egress entry;
  - metrics for the exchange and the feed.
- **Alternatives.** A slice that starts a MicroVM with no adapter
  would be ready and unreachable, and a slice with the whole of
  replacement would hold the fence before the first agent runs. The
  slice above is the smallest that a person can watch end to end.

### 8. What garam's side changes

- **The wording.** "pod", "replica" and "containers" in
  `platform/api/machine.yaml` (lines 463, 467, 474, 534, 1925, 1928,
  1931, 1946, 1965) and in `platform/internal/agent/agent.go` (lines
  7, 56, 80) become the neutral "workload", and "ready replica"
  becomes "ready workload": the contract says what the report means,
  and not which scheduler observed it. It is a description change
  with no operation or schema change, and a separate pull request
  from this ADR, which is Markdown only. `machine.yaml` is
  authoritative and `agent.go` follows it.
- **`failed` is described for a second kind of observer.** The
  reserved wording at line 1931 names a ready replica count as what
  fails to separate the cases; it stays true, and gains the case of an
  observer that has a terminal phase.
- **Other assumptions found.** `adapter.md` §What it is and
  §Foreign facts say the adapter runs "in that agent's pod" and that
  the sidecar is native; in a guest it is a process beside the agent
  and the reason (outlive the agent while it shuts down) is carried by
  the supervisor's order. `agents.md` §What an agent is built from
  names `garamsh/garam-agent-operator`'s flags as the only operator's
  construction. `overview.md` lines 47, 52, 248 and 280 repeat "pod".
  ADR-0012 and ADR-0053 are frozen and keep their wording.
  `deployment-contract.md` is held by another worker this round and is
  not touched here.
- **Nothing in garam's operations changes.** The operator calls
  `exchangeClientAssertion`, `mintAgentToken`,
  `reportProvisioningState` and the control service's C2; it names no
  new route.

## What gaon does not yet serve that the first slice needs

Read from the Resource API document, the `lib/api` sources, and
`garamsh/gaon#117`, `#125` and `#164` at the revision above; "not
verified" says gaon states it and `main` was not read. Where a number
is shown it is one those sources name.

- A tag of `github.com/garamsh/gaon/lib/api`: `lib/api/v0.1.0`
  follows #118 and #119 (`garamsh/gaon#125`). None is cut, and the
  module takes a pseudo-version meanwhile. Issue: #118, #119.
- Labels, selectors and `ownerReferences` serving (#118 contract,
  #119 serving, #120 garbage collection). The types are in `lib/api`
  (`labels.go`); serving is not verified. Issue: #118, #119, #120.
- `NetworkPolicy` as a kind with an `ipBlock` and `except`, which the
  one policy and its wide egress entry need. Issue: #165, blocked by
  #142 (`garamsh/gaon#164`, item 1).
- Replacing a `Secret`-sourced file in place in a running MicroVM,
  which retires the token interim and needs an mvmd capability gaon
  has not confirmed. Issue: #162 (`garamsh/gaon#164`, item 2).
- A ServiceAccount token as the operator's authentication, which
  retires the administrator-issued certificate. Issue: #161
  (`garamsh/gaon#164`, item 3).
- RBAC rules limited by label selector, which codehub's PM made a
  condition. gaon's answer gives the operator create and update on
  `secrets`, "which can be limited by a label selector (#118)"
  (`garamsh/gaon#125`, item 11). Served is not verified. Issue: #118.
- The Node authorizer's read of a Secret a MicroVM references and the
  MicroVM ADR with named ports. Documented, not verified. Issue: #124.
- `lastExit`'s end time. The code and time are in `lib/api` already.
  Issue: #73.
- The condition a second MicroVM shows while a Volume is attached,
  which D7's storage contract is to state (`garamsh/gaon#164`,
  item 4). Answered as `Pending`; the exact condition is not served.
- That a restart keeps the epoch and resolves `files` and `env` again,
  which #158 states in `node-agent.md` (`garamsh/gaon#164`, item 5).
  Answered, and not verified against `main`.

Not a dependency: read-only OCI image volumes, which gaon does not
serve in this goal (`garamsh/gaon#164`, item 6).

## Consequences

- **Easier.** The operator is small: it holds no definition, no
  second kind and no store, and a restart loses nothing. Tenant
  separation is one policy that allows no ingress.
- **Harder.** The agent runtime, workspace and adapter are composed
  into one image, so an agent's version moves with the operator's
  release, and the supervisor is a program to keep. A token cannot be
  renewed in a running MicroVM until #162 is served, so an agent is
  restarted once a day meanwhile. The control service must start and
  serve outside Kubernetes unchanged, or the work escalates. Every
  gaon item above is a dependency of the first slice.
- **Ruled out.** Garam dialing the cluster, a custom kind in the first
  slice, a MicroVM per program, an ingress rule for agents.

## Signs the decision was wrong

- A second tenant or process needs to reach an agent, and the ingress
  rule becomes a list.
- The supervisor in the composed image grows into the standalone
  runner's intent, adoption and nonce store, or the composed image's
  rebuild at each release becomes a cost that no longer fits.
- The operator needs a durable fact that no label or annotation holds,
  or a person needs to write an agent by hand: the sign to add the
  kind.
- #162 is not served, and a restart is not a safe step for the fence,
  so agents cannot live a token's lifetime.
- The wide egress entry outlives the gateway, or the control service
  cannot run unchanged outside Kubernetes.
- A second operator for another cluster repeats most of the folder's
  code, which is the sign that the shared part is a library.
