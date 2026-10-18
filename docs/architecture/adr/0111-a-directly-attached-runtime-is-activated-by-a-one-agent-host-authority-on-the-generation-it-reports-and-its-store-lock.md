# ADR 0111: A directly attached runtime is activated by a one-agent host authority on the generation it reports and its store lock

> Status: accepted
> Date: 2026-10-18

Accepted by the Garam PM on 2026-10-18 under the owner's delegation,
recorded in the review on #1498. The review added four points from the
Sherlock PM's answers: restart ordering, a pinned loopback literal, the
status read, and outbox entries written under an earlier ID. They stand
under §Decision.

Drafted for #1355, which reserves this number. It compares the models
#1355 names and recommends one, for the PM to decide under the owner's
delegation. Garam is read at
`bc0dfca5c42fa102ced311f10de790155f6d1ba3`. Sherlock is read at
`d63ac48957cea7214cdd4cfb06b45b1b859e3d7d`, the tip of its `dev`
branch on 2026-10-18, through
`gh api 'repos/garamsh/sherlock/contents/<path>?ref=<sha>'`. The
Sherlock facts the PM recorded on #1355 from
`e3d3461b0214f01304a38d00e2a240b304cee0c0` were re-read there and hold.
No source, schema or migration changes with this record.

## Context

ADR-0102 §Common agent enrollment gives managed and direct placement
one admission. Its §Direct attachment proof remains an implementation
gate leaves the direct runtime's writer proof unselected.
`agent-enrollment.v1` is built for managed placement alone:
`EnrollmentBinding.admissible` in `internal/agent/admission.go` answers
`ErrDirectPlacementGated` (`direct_placement_gated`) for
`PlacementDirect` before any authority is read. That refusal is what an
accepted decision here would lift.

### What Garam holds

- **One admission write.** `admit` in
  `internal/agent/postgres/postgres_admission.go` writes a new agent
  fenced from birth, with its first assignment, or moves an existing
  one from exactly the assignment it expects into a new lineage. Either
  way no generation is active.
- **The standalone supervisor is a one-agent lifecycle authority**
  (ADR-0082). Its activation forward, `activate` in
  `internal/workload/control.go`, checks four things before it calls
  Garam: the generation's placement token, the epoch and revision the
  generation started with, `provenance`, and `claim`. `provenance`
  confirms that the runtime is the recorded container, running from the
  recorded start with no restart. `claim` confirms that the generation
  was never asked for before on this host. Of the four, only
  `provenance` depends on Docker.
- **Writer exit is proven only for what the supervisor created.** That
  proof is the recorded container ID and nonce, and the exit the engine
  reports (ADR-0085). Nothing in `internal/workload` observes a process
  the supervisor did not create.
- **The fences are Garam's.** Activating a newer generation ends the
  older one's admission per request, and a replaced lineage is refused
  (ADR-0078). Protected writes and replacements share the execution row
  (ADR-0080).
- **A message's audience is the recipient GRN.** `openKey.Sign` in
  `internal/message/signing.go` sets `aud` to `m.recipient.String()`.

### What Sherlock holds

- **The store lock is an OS lock that lasts the process's store
  lifetime.** `lockWriter` in
  `internal/memory/sqlite/writer_lock_unix.go` takes
  `flock(LOCK_EX|LOCK_NB)` on `<real path of the store>.lock` before
  the store opens. A held lock refuses a second opener with
  `ErrStoreLocked`. The descriptor is close-on-exec. The kernel releases
  the lock when the process exits, however it exits. Sherlock's ADR 0002
  bounds the lock to one host and one file. A copy on another host, or a
  store on a shared volume, is not fenced by it.
- **The generation is minted only after the lock is held.**
  `startQueue` in `cmd/sherlock/agent.go` opens the queue, and with it
  the store and its lock, and only then mints 128 random bits. They are
  never persisted, and a launcher cannot pass them. A start the lock
  refuses mints no generation (ADR 0016).
- **A drain closes the listener, then memory, and releases the lock
  last.** That is Sherlock's ADR 0022, and the `Serve` comment in
  `internal/gateway/httpserver.go`. The status route reads no store, so
  it answers through the drain for as long as the listener is open.
- **`GET /agents/{agentID}/status` reports and enforces nothing.** It
  reports `contract_version` 1, `agent_id`, `assignment_epoch`,
  `generation`, `config_revision`, `state` and `started_at`
  (`statusResponse` in `internal/gateway/handler.go`). A path naming
  another ID is answered 404. No field says the lock is held. A serving
  process implies the lock, because it exists only after taking it.
- **The gateway serves loopback unless `tls:` is set.** `serveAgent` in
  `cmd/sherlock/agent.go` refuses a non-loopback address without TLS.
  Over loopback no caller is authenticated (ADR 0012).
- **The audience is the startup identity, compared byte for byte.**
  `serveAgent` builds the verifier from `--agent-id`. In
  `internal/gateway/signature.go`, `verify` refuses `aud != v.audience`
  as `audience`. `--agent-id` is required, opaque and verbatim. The
  store is not keyed by it, so restarting under another ID keeps
  memory. Entries already in the outbox keep the ID they were written
  under (ADR 0016 §Consequences).
- **The lock is taken before the listener binds.** `serveAgent` calls
  `startQueue` and only then `gateway.Serve`. It logs "agent listening"
  between the two, so that line is written before the bind.

### What the Sherlock PM answered

The Sherlock PM answered on 2026-10-18, from the same revision, and the
Garam PM relayed the answers in the review on #1498.

- **Outbox entries are provenance only.** The serving agent never reads
  an entry back. It compares none of the entry's `agent_id`,
  `assignment_epoch` and `generation`, so nothing in Sherlock refuses or
  misroutes an entry written under an earlier ID.
- **The drain order is a contract.** The listener closes, then memory,
  and the writer lock is released last. It is ADR 0022's Decision.
  `Serve` and `Store.Close` in `internal/memory/sqlite/sqlite.go`
  implement it, and
  `TestServe_WriterLockIsReleasedOnlyAfterMemoryCloses` tests it.
  Sherlock notifies Garam before any change to it merges.
- **The gateway never sets `SO_REUSEPORT`.** A second bind of the same
  address fails with `EADDRINUSE`. `127.0.0.1` and `[::1]` are distinct
  addresses, so one process can hold each.

### What was measured on this host

On Linux 6.12.111, on 2026-10-18, a Python process took
`flock(LOCK_EX|LOCK_NB)` on a scratch `memory.db.lock`. Then:

- `/proc/locks` listed it as `FLOCK ADVISORY WRITE <pid>` with the
  file's device and inode;
- a second `flock` on the same path answered `EWOULDBLOCK`;
- after `unlink` and re-creation of the path, a new `flock` succeeded
  while the first holder still held the old inode.

The lock is therefore positive evidence on one host for as long as
nobody replaces the lock file, and nothing more. Sherlock's ADR 0002
and the PM's record on #1355 say the same.

## Identity and audience

- **Agent identity** is the GRN that Garam's `admit` allocates or
  keeps. It is the one name every Garam fence, token and audience binds.
- **Runtime ID** is the `--agent-id` a Sherlock process was started
  with. It is fixed for the process's lifetime, and Sherlock never
  derives or changes it.
- **The audience binding** is the equality of the two. Garam signs `aud`
  as the GRN, and Sherlock refuses any other value byte for byte. The
  adapter delivers to `/agents/<GRN>/messages`, which a runtime under
  another ID answers 404. So a runtime is bound to an agent exactly when
  it was started with `--agent-id` equal to that GRN.

There are two cases, and each settles as follows.

- **Existing GRN.** The runtime was started with the GRN of an agent
  Garam already holds, because it ran managed, standalone or attached
  before. A replacement under `admit` keeps that GRN, so the binding
  holds with no restart.
- **Arbitrary ID.** This is every new direct agent. Admission allocates
  the GRN, so no running process can have been started with it, and the
  same holds for a runtime started under any name of its own. The
  binding is made by explicit reconfiguration: the runtime is drained
  and started again with `--agent-id=<GRN>` on the same store. Memory
  survives that restart because the store is not keyed by the ID. The
  restart also mints a new generation, which is the one activated.

The alternative is an alias, where Garam signs `aud` as the runtime's
own ID or Sherlock accepts more than one audience. Either way, a name a
runtime chose would receive the agent's messages. It changes the JWS
audience or mutates runtime identity, and #1355 assumes neither. It
would need an ADR in each project and the owner's approval, so it is
not this record's to select.

The authority refuses to activate a runtime unless the `agent_id` it
reads equals the GRN byte for byte. That is the binding check, and it
is the comparison Sherlock already makes.

## The models compared

Both models keep these unchanged:

- ADR-0102's one-agent authority, admitted under `agent:enroll` through
  `admit`;
- the execution token on every protected call;
- the generation, lineage and assignment fences;
- no organization-wide operator powers, and no provisioning.

They differ in who holds the activation authority, and in what the
writer evidence is.

### Model A: a minimal same-host attachment binding

Garam records an attachment for the agent: the host, the runtime's
loopback address and the generation attached. The connector holds a
one-agent attachment credential and asks Garam itself to activate the
generation that `/status` reports. The connector is the adapter, or a
process beside it.

- **Evidence.** The generation the runtime reports while `serving`,
  read over loopback, rests on Sherlock's lock. There is no separate
  authority process.
- **New mechanism.** An attachment record and its credential kind, and
  an activation path from the connector to Garam.
- **What it breaks.** It merges the connector and the lifecycle
  authority into one process, which ADR-0102 §Runtime and messaging
  trust keeps apart. It is close to ADR-0082's rejected "runtime's own
  certificate as lifecycle authority": the adapter shares the runtime's
  host, and in practice often its account.

### Model B: a process-owning scoped host authority

The authority is an ADR-0082 supervisor registration held by a host
process apart from the adapter. It serves the adapter
`agent-execution.v1`/`.v2` on a socket of its own (ADR-0084, ADR-0085),
and it owns the writer by starting it. The Docker backend exists. A
plain child-process backend would start Sherlock on the host's existing
store without a container.

- **Evidence.** The exit of what the authority started: the engine's
  report for a container, or `wait` for a child. For the first start, it
  is the lock its own child obtained.
- **New mechanism.** For a host process, a second backend behind
  `Engine`. Nothing on Garam's side changes.
- **What it cannot do.** It cannot observe a process it did not start,
  so an already-running Sherlock is first drained by a person and then
  started by the authority. The arbitrary-ID case needs that restart
  anyway, but an existing-GRN runtime would restart only for the
  authority's sake.

### Cases

The third column is this record's recommendation, under §Decision:
Model B's authority, with Sherlock's lock and the generation it reports
standing in for `provenance` when the authority attaches a process it
did not start.

| Case | Model A | Model B | Recommended |
|---|---|---|---|
| First attachment, existing GRN | Reads `/status`, records the attachment, activates the generation reported | A person drains the runtime; the authority starts it and activates its child's generation | The authority reads `/status` itself, checks `agent_id`, epoch and revision, claims the generation and forwards; no restart |
| First attachment, arbitrary ID | Restart under the GRN, then as above | Restart under the GRN by the authority, then as above | Restart under the GRN by a person, then as above; the new generation is the only one claimable |
| Retry of admission | `agent-enrollment.v1` answers the recorded binding | Same | Same; nothing is re-issued |
| Retry of activation | A new path whose request identifier rules would have to be written | ADR-0084's identifier and `claim`: the same request, a newer token | Same as B |
| Runtime restart, same assignment | The generation changes; the connector activates the new one, and Garam fences the old | The authority observes the exit, starts again and activates | The authority reads a new generation `serving` at the same address. The old listener is closed, so the old process is past its drain, and on the same store it no longer holds the lock. It claims and forwards, and Garam fences the old generation |
| Disconnect, connector to Garam | Undecided; no protected call until it answers | Same | Same; the runtime keeps running, and the same generation resumes under its activation |
| Disconnect, authority or adapter to runtime | The connector cannot tell a hang from an exit; protected calls stop | The authority still sees its child or container | Undecided: nothing is forwarded, and the adapter makes no protected call while its own read fails. The same generation read again resumes, and another is a restart |
| Stale PID or nonce | A PID-keyed attachment can name a reused PID; a generation key cannot, since it is never reused | A child's PID is not reused before it is reaped; a container is recorded ID plus nonce | No PID is recorded. A recorded generation that no longer answers is treated as a restart, and a generation already claimed is refused by `claim` |
| Foreign host | Not reachable over loopback, so never attached | The authority starts only local processes | Not reachable over loopback; `tls:` placements are outside this record |
| Foreign store at the same address | Activatable; the lock does not fence across stores | Excluded, since the authority names the store it starts | Activatable, as in A. The old process is fenced at Garam, and the cost is diverging local memory, not two writers on one store. Shared volumes, hard links and a removed lock file are declared out of placement |
| Replacement to another authority | A new attachment credential; rules unwritten | ADR-0082's replacement approval: next epoch, new lineage, old registration and activation ended | ADR-0082's replacement through `admit`. The new authority forwards only a generation reporting the new epoch, which takes a restart with the new `--assignment-epoch` |
| Uncertain ownership | Refuse | Refuse | Refuse, for each of: `/status` unreadable or malformed, `contract_version` not 1, `agent_id` not the GRN, `state` not `serving`, a generation other than the one the request names, or a record the authority lost. No activation is forwarded and no protected call is made |

## Decision

Direct placement is admitted under `agent-enrollment.v1`. Its assignee
is an ADR-0082 supervisor registration for that one agent. The
supervisor runs in an attach mode, which forwards an activation only
for a generation it has itself read `serving` under the agent's GRN at
the runtime's loopback address. The writer evidence is the runtime's own
store lock, which Sherlock's generation stands for.

- **Admission.** An administrator's `agent:enroll` approval names the
  placement, the host and a supervisor registration, and `admit` writes
  it. This is ADR-0102 §Common agent enrollment step 1 as written.
  `direct_placement_gated` is lifted by the implementation pull request
  once this record is accepted, and not before.
- **Credentials.** The supervisor obtains its own credential through the
  existing supervisor exchange. The assignee mints the agent's
  short-lived token, as ADR-0102 step 3 states. No operator key reaches
  the host.
- **The attach mode is `activate` with `provenance` replaced.** The
  placement token, epoch, revision, `claim`, the forward and `settle`
  stay as they are. In place of the container check, the supervisor
  reads `/agents/<GRN>/status` itself and requires all of the following:
  - `contract_version` is 1;
  - `agent_id` is the GRN;
  - `state` is `serving`;
  - `generation`, `assignment_epoch` and `config_revision` equal the
    request's.

  An unreadable answer is `undecided`, and any other mismatch is
  `placement_not_current`. It holds no Docker socket.
- **A replacement activates only a new generation.** The new assignment
  raises the epoch, and the attach mode forwards only a generation
  reporting it, so the lineage change and the writer change coincide.
- **Placement is one host, one local filesystem, and loopback.** The
  supervisor, the adapter and the runtime share the runtime's network
  namespace, and the store is not on a shared or network volume. A
  person who removes the lock file, hard-links the store or shares the
  volume is misusing the placement. The deployment documents say so,
  and this record does not claim to detect it.
- **The runtime address is a pinned loopback literal.** It is
  `127.0.0.1:<port>` or `[::1]:<port>`, never `localhost`. The
  supervisor and the adapter are configured with the same literal that
  the runtime binds. Sherlock accepts `localhost` as loopback, and that
  name can resolve to either family, so two processes could each hold
  one. A pinned literal leaves one address for one process.
- **A restart starts the new process only after the old one has
  exited.** This binds whoever restarts the runtime, since the attach
  mode starts and signals nothing. A free port is not a free lock: the
  old process frees its port before its lock, and the new one takes the
  lock before it binds. A start refused with "another process owns the
  memory store" is retried rather than repaired. The supervisor reads
  readiness only from a `/status` answer, never from the "agent
  listening" log line, which is written before the bind.
- **The status read carries no credential in this placement.** Without
  `tls:` the gateway serves loopback and authenticates no caller
  (Sherlock ADR 0012), so the read rests on same-host trust below. A
  `tls:` placement would present a client certificate the gateway
  verifies against its `client-ca`. That is C2, and outside this record.
  Sherlock has no caller-only setting: a certificate that authenticates
  a caller is also the transport origin of any unsigned message that
  caller posts. The supervisor posts no message, so it is never an
  origin.
- **Outbox entries written under an earlier ID are not forwarded, and
  no drain is required before the restart.** This is Garam's call. The
  adapter sets aside every entry whose `agent_id` is not the agent's
  GRN ("the entry is another agent's", `outboundOf` in
  `internal/delivery/forward.go`), before any call to Garam. It leaves
  the file in the outbox and logs the entry. Before attachment no Garam
  message reached a runtime under another ID, so none of those entries
  answers one. An existing-GRN runtime changes no ID, and its entries
  are forwarded as before.
- **Same-host trust is the stated assumption.** This is #1355's option
  C1. A process that can bind the runtime's loopback port, or run as the
  runtime's account, is inside the trust boundary. The adapter's message
  delivery already rests on the same trust, so requiring more of
  activation would move the gap rather than close it.

### Writer evidence and its owner

- **The runtime owns the evidence.** Sherlock takes the lock and mints
  the generation after it, so only the process holding the store can
  report a fresh generation. Garam neither holds this evidence nor can
  read it.
- **The supervisor owns reading it, and the decision to forward.** It
  reads `/status` itself rather than trusting the adapter's request
  body. It records each generation it forwards (`claim`), and it is the
  only party that asks Garam to activate.
- **Garam owns the fence.** An activated newer generation ends the older
  one's admission, a replaced lineage is refused, and `admit` alone
  writes identity and assignment.
- **The adapter owns none of it.** It reports what it reads, and it
  makes no protected call before activation, after a definite refusal,
  or while its own status read fails.
- **The person running the host owns the placement preconditions.**
  These are the ones §Decision lists, and a separate OS account for the
  supervisor, which ADR-0082 already requires: "a runtime or a tool must
  not reach it by mount or account".

### Loopback ingress and the existing adapter

Sherlock without `tls:` listens on loopback only, so the adapter runs
in the runtime's network namespace. On a host that runs Sherlock
directly, that is the host's own namespace. The adapter dials Sherlock
there, as a managed sidecar and a standalone adapter container do. It
reaches the supervisor through `GARAM_ADAPTER_CONTROL_SOCKET`, exactly
as ADR-0085 states. In the token mode it presents the supervisor's
control token and the generation's placement token. No adapter setting
or route is new. What changes for the adapter is where it runs: as a
host process under its own account rather than as a container the
supervisor created. The placement token is then a file in the adapter's
directory, written owner-only by the supervisor.

### What it does not cover

- A runtime serving with `tls:`, reached off loopback. Its activation
  would need the supervisor to be a client Sherlock's mTLS admits. That
  is option C2 on #1355, and it is left for a placement that needs it.
- Multi-node and shared-volume placements, which stay on the managed
  path.
- Message-channel or command authorization in Sherlock, which stays
  above it (sherlock#1067, Option A).

## Alternatives

- **Model A alone.** Rejected. It adds a credential kind, a record and
  an activation path, and it merges the connector with the lifecycle
  authority. The evidence it would rest on is the same lock the
  recommendation uses.
- **Model B alone, the process-owning authority.** This is the
  alternative to take if the PM wants proof by observed exit rather than
  by lock. Its cost is that an existing-GRN runtime restarts once for
  the authority's sake, and a child-process backend has to be written
  behind `Engine`. Under it, "attach an already-running Sherlock" is
  dropped, as the PM recorded on #1355.
- **A host-owned lease** (ADR-0102's earlier recommendation). Rejected
  on ADR-0102's own ground: a lease's expiry proves that the holder
  stopped renewing, not that it exited.
- **Requiring mTLS for activation (C2).** It authenticates the
  `/status` answer, at the price of a certificate and its rotation on
  every direct host. That is the configuration ADR-0100 removed
  elsewhere.
- **Reading `/proc/locks` as well.** It is world-readable and names the
  lock holder's PID and inode, as measured above. It would tell the
  supervisor that the configured store's lock is held. It would not tell
  it that the holder is the process answering `/status`: the supervisor
  runs under another account, so it cannot read that process's
  descriptors without privilege. It is a new mechanism, so it is left
  out. It is the first addition to consider if foreign-store attachment
  is observed.
- **An audience alias.** Rejected under §Identity and audience.
- **A lock-held field or live re-check on `/status`.** This would be a
  Sherlock contract change, and the recommendation does not need it.

## Consequences

- No Sherlock change is needed. On Garam's side, the implementation is
  three things: lifting `direct_placement_gated`, the supervisor's
  attach mode replacing `provenance`, and the deployment documents for
  the placement. `admit`, `claim`, the fences and the adapter are
  reused unchanged.
- Every new direct agent restarts its runtime once under its GRN. An
  existing-GRN runtime attaches without a restart.
- The entries the adapter sets aside under an earlier ID stay in
  `outbox/` on the agent's volume indefinitely, and Garam leaves them
  there. Sherlock never removes an outbox entry, as the Sherlock PM
  answered on 2026-10-18 and the Garam PM relayed. Its copy, verify and
  restore carry those entries byte for byte (Sherlock
  `docs/architecture/deployment.md` §Snapshot and restore, at
  `d63ac48957cea7214cdd4cfb06b45b1b859e3d7d`). Only entries written
  before attachment are affected, so the set is bounded and does not
  grow after the restart. Removing a runtime's files is not the
  adapter's to do.
- A runtime on another store at the same address can be attached. Its
  predecessor is fenced at Garam but may go on writing its own memory.
  The record accepts that divergence rather than proving it absent.
- Garam's assurance for a direct agent is one host's process isolation
  and the lock's advisory scope. A managed or standalone agent gets more
  than that, and the difference is stated where the placement is
  documented.

## Signs the decision was wrong

- A generation reported `serving` at the attached address while another
  process held the same store's lock. That would mean the lock or the
  drain order does not hold as Sherlock's ADR 0002 and ADR 0022 state.
- Two activated generations of one direct agent both committing
  protected writes at Garam.
- Attached agents regularly found on a shared or network volume, or
  with their lock file replaced. That would mean the placement
  precondition cannot be left to the person running the host.
- A non-Sherlock listener on the loopback port answering a `/status`
  that the supervisor accepted. That would mean C1's trust boundary is
  wrong for direct hosts, and C2 or `/proc/locks` is owed.

## Errata

- 2026-10-18 — §Decision says "Direct placement is admitted under
  `agent-enrollment.v1`", and §Consequences counts lifting
  `direct_placement_gated` among the implementation's three things.
  Both are wrong about which wire carries the admission. At
  `85da7bb11bc0b8613eab2b6e5a87659f0a40528d`, where this record merged,
  `agent-enrollment.v1` serves an operator's token alone and requires
  an operator as its assignee (`operates` and `authorizeCreation` in
  `internal/agent`). A supervisor registration cannot exist apart from
  the standalone approval that spends it: `agent_supervisors.enrollment_id`
  is `NOT NULL` and references `agent_enrollments`, and that spending
  is what calls `admit` (`migrations/000031`,
  `internal/agent/postgres/postgres_standalone.go`). The administrator's
  `agent:enroll` approval naming the host and a supervisor
  registration, which §Decision describes, is therefore the standalone
  approval spent under `standalone-enrollment.v2`. It now records
  `placement: direct` (`migrations/000053`), and `agent-enrollment.v1`
  refuses direct placement with the permanent reason
  `direct_placement_requires_administrator_approval` instead of the
  gate. The decision stands: the assignee is an ADR-0082 supervisor
  registration for the one agent, admitted through `admit`.
- 2026-10-18 — §Cases' "Replacement to another authority" row rests on
  the new authority's first activation succeeding once the runtime
  reports the new epoch. At `85da7bb11bc0b8613eab2b6e5a87659f0a40528d`
  it could not, for an agent that had ever been active, under either
  placement. `activateAgent` requires `replacesActivationId` to equal
  the agent's most recent activation, and nothing told a supervisor
  with a fresh record what that was. The spending of a replacement now
  records that activation under the execution lock and answers it as
  `replacesActivationId`, and the host seeds its supervisor's record
  with it. The row's recommendation stands.
