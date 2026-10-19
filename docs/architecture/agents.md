# Agents

Autonomous identity, provisioned outside garam (ADR-0012). Implemented
by `internal/agent`.

Pending removal
([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)):
the certificate path goes in G4 (#1494) — an agent's and a
supervisor's certificates, a managed agent's initial certificate and a
recovery's v1 form — and what this document says of it holds until
then. Legacy execution mode, the
staged cut-over, `garam cutover-inventory` and every agent definition
are gone (ADR-0118), as are garam's own templates and the browser's
legacy `operatorGrn`
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)).

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Assignment` | what holds an agent — an operator, or a standalone host's supervisor (ADR-0082) — and the epoch that binding holds |
| `Service` | records the state each party reports (ADR-0053), and mints an agent's tokens for the operator or supervisor holding it (ADR-0100) |
| `ExecutionService` | what a lifecycle authority does to an agent's execution — creates a managed agent and issues its first certificate, activates a generation, deactivates it, inspects a certificate and a generation, and recovers the credential lineage (ADR-0077, ADR-0078) |
| `ExecutionStore` | the execution row every agent has, its activations and its credential lineage, written under the row's lock (ADR-0080), and a managed agent's creation and the initial issuance that starts its lineage |
| `ManagedCreation` | an agent the hosted control service created: its GRN, the controller and epoch of its first assignment, the `agent:create` reference and request identifier that authorized it, and the lineage it was born into |
| `Enrollment` | an administrator's approval of a standalone host: the hash of the host's bootstrap secret, the fingerprints of its two keys, its request identifier, the agent and supervisor GRNs it binds, the assignment a replacement expects, and the public result once spent (ADR-0082) |
| `Supervisor` | a standalone host's current supervisor registration: the one agent it holds and the certificate garam last issued it |
| `StandaloneService` | how an administrator approves a host, withdraws an unspent approval and revokes a supervisor, each decided from current authority; it signs nothing |
| `SupervisorService` | how a host spends its approval for both first certificates, and how its supervisor renews its own |
| `StandaloneStore` | approvals, registrations and supervisor renewals, written after the execution row's lock wherever an assignment or an execution changes |
| `MemberAuthorizer` | decides a user's authority from current state, as approving, withdrawing, revoking and spending need |

garam mints an agent's GRN and never changes it, so which operator runs
the agent is a relation a reassignment moves rather than part of the
identity. That GRN must name an agent of the organization that owns it:
ADR-0014 rests tenancy on its organization segment, so the domain
refuses a pairing that disagrees rather than trusting its callers, and
the schema refuses it again where nothing routes around it. An
assignment stays inside that organization and names an operator
(ADR-0015) or the agent's own supervisor (ADR-0082), which the schema
alone decides; an operator route compares the operator on the row, so
none reaches a supervisor-held agent. This domain reads no operator
record of its own and addresses an operator by GRN. Every assignment
raises the
epoch, and a state report is accepted only at the current one — the
operator a reassignment replaced reports nothing about the agent it
lost. An agent is never an organization member (ADR-0014), so nothing
here references `users`.

`Service` is what an operator or a supervisor does to the agents it
holds. What an operator reports about the agent's pod is accepted only
for an agent assigned to that operator at the current epoch (ADR-0053):
comparing the operator on the row is the epoch fence, and the tenancy
check too, since a GRN carries its organization. An agent's token is
minted only for the operator or supervisor holding it at the current
epoch, and only inside a lineage its lifecycle authority opened
(ADR-0100). A caller that is not an operator is refused, except where
an agent reports its own runtime state: that is bounded by the
identity the request proved rather than by an assignment, and runs
the execution fence first, so the call carries its activation's token
([machine-identity.md](./machine-identity.md) §Generation and lineage).
A lost credential is replaced by the control service's recovery
(§Separated execution).

garam holds no definition of an agent and no name for one
([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)):
what an agent runs is its control service's or its standalone host's,
and the console tells two agents apart by their identifiers.

`Store` answers `Service` and, org by org, the read the browser-facing
listener serves people: an organization's agents and the assignments
holding them, each a batch read rather than one per agent. An agent no
operator holds contributes no assignment, so the two lists are not one
length. Both aggregates are rehydrated inside this package — their
fields are unexported and no exported constructor restores a stored
row's state or timestamps — which is why the store that reads them back
lives here rather than below, beside the others until #1083 moves this
domain's store out of the package.

Signing happens through the `RequestSigner` seam the composition root
satisfies from `internal/ca`, so the two domains still cooperate
through `grn.GRN` alone and this one never holds key material. The
HTTP surfaces are the machine listener — where the operator reports the
pod and the agent reports itself, each on a route of its own and
neither reaching the other's fact (ADR-0053) — and the browser-facing
one for the organization's members, which reads the agents and
writes none: an agent is created through an operator's control
service or by a standalone host's enrollment. The queue a message
reaches an agent through is the messaging domain.

What an agent is built from is the operator's, and garam holds no
value of it. The images an agent's Pod is
built from and the size of the volume it keeps state on are named by
flags that Garam Agent Operator's `cmd/main.go` declares
([`garamsh/garam-agent-operator`](https://github.com/garamsh/garam-agent-operator)),
which is authoritative for what every agent that operator constructs
is built from. Read at
[`2d795f5f562c8d3509a443288c6531c01a92458f`](https://github.com/garamsh/garam-agent-operator/blob/2d795f5f562c8d3509a443288c6531c01a92458f/cmd/main.go),
when the repository was named `gagent-operator`, they were
`--agent-image`, `--agent-copy-image`, `--agent-tools-image` and
`--agent-storage-size`; that list is the reading at that commit and not
the operator's current set.

The boundary is drawn by what each side can check rather than by what
either owns. An image reference is a fact about a registry some cluster
can reach — a mirror, an air-gapped copy, the pull secret that admits
it, the network policy in front of it — and garam observes none of
that: ADR-0012 keeps it from dialing into a cluster, so a digest
entered here would be a claim garam has no way to test and whose
failure, at deploy time, it would never see. The operator does not test
one either; it takes the reference as given and leaves it to the API
server to refuse on the object, which is the first party in the chain
that can. garam is on no part of that chain. Sherlock
([`garamsh/sherlock`](https://github.com/garamsh/sherlock)) stated the
same boundary from the agent's own side, for the tool set it loads, at
`internal/config/config.go:79-82` in
[`eeea6a9cafef6742593a37d3a6baac1dd66cc98b`](https://github.com/garamsh/sherlock/blob/eeea6a9cafef6742593a37d3a6baac1dd66cc98b/internal/config/config.go#L79-L82),
read when the repository was named `gagent`.

Nothing in garam requires a version or a capability of an agent today.
Such a requirement is the one thing on this boundary garam could
properly make, because what its own machine contract needs is a fact
garam holds rather than one about somebody's registry. ADR-0079 accepts
one: each interservice contract works with the adjacent supported
release of its peer in both upgrade orders, and unsupported semantics
are refused by name. None of it is built, so garam still publishes no
release of that contract and no compatibility window (ADR-0049).

## Common managed and direct enrollment

**Built for both placements, over two wires.**
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
sets one admission contract, `agent-enrollment.v1`, in place of
separate managed/standalone identity models. That wire admits managed
placement. Direct placement is admitted by an administrator's
standalone approval that names it, spent under
`standalone-enrollment.v2`
([ADR-0111](./adr/0111-a-directly-attached-runtime-is-activated-by-a-one-agent-host-authority-on-the-generation-it-reports-and-its-store-lock.md),
whose Errata record that the wire is this one). §Separated execution
and §Standalone hosts describe the older cohorts, which stay served.

- **What is built.** The hosted control service prepares an approval
  under a member's `agent:create` reference, then admits the agent it
  binds; `api/machine.yaml` §prepareAgentEnrollment and
  §admitAgentEnrollment are authoritative for the wire and the
  refusal reasons. Credential delivery is `mintAgentToken` and
  activation `activateAgent`, unchanged. `AdmissionService`
  (`internal/agent/admission_service.go`) holds both stages. It admits
  managed placement alone, and refuses direct placement with the
  reason `direct_placement_gated`, permanently: a direct agent's
  authority is an administrator and a one-agent supervisor, never an
  operator.
- **A managed replacement rests on `agent:replace`.** An owner's or
  administrator's reference names the agent, the assignment it is on
  and the controller it moves to, and the hosted service's grant
  covers both controllers
  ([ADR-0107](./adr/0107-a-managed-replacement-is-authorized-by-an-agent-replace-reference-naming-the-agent-its-assignment-and-the-controller-it-moves-to.md)).
  The approval records the expected assignment and, spent, the epoch
  and lineage the move wrote; the agent keeps the managed creation
  that made it, if any. The reference stays current at the
  assignment the replacement writes, so a retry is answered and an
  agent replaced before it ever ran starts on it.
- **One admission write.** Admitting an identity is one store
  primitive, `admit` (`internal/agent/postgres/postgres_admission.go`),
  which `agent-enrollment.v1`, `managed-enrollment.v1`/`.v2` creation
  and standalone spending each call: a new agent fenced from birth
  with its first assignment, or a move from exactly the expected
  assignment into a new lineage, which a managed replacement and a
  standalone one share. An `agent-enrollment.v1` admission
  also records the managed creation row, so one reference creates one
  agent under either managed contract and its first activation
  carries that reference as a `managed-enrollment` creation's does.

- Provisioning and enrollment are separate. A managed runtime is
  provisioned by the Operator; a direct runtime may already be running
  independently. Connecting it requires neither an Operator record
  nor a Kubernetes workload nor the Docker three-container runner.
- Both use the same approval, identity admission, token delivery and
  activation stages defined in ADR-0102 §Common agent enrollment. The
  differences are the approval's authority and placement: the hosted
  service's delegation/controller proof/member reference, or an
  administrator-approved one-agent connector using the supervisor
  registration/authorization shape. A direct connector has no
  organization-wide operator powers and never enrolls peers.
- New admission mints one stable GRN, starts fenced with an assignment
  epoch and lineage and activates no writer. Replacement preserves
  that GRN and compares the expected assignment before raising the
  epoch and replacing the lineage. Retry returns the same public
  binding; changed intent conflicts, superseded placement refuses.
- The assignee delivers/refreshes short-lived agent API access; the
  lifecycle authority establishes writer ownership and activates the
  reported generation. A directly connected runtime's authority must
  establish that ownership too; unverifiable ownership stops protected
  Garam work without asserting a second writer is safe. Existing
  execution locks and recovery permissions remain.
- The runtime owns memory, replay state, identity/generation reports
  and trusted message issuers. Its connector owns narrow Garam API
  access and delivery; no Operator PEM or Garam message-signing key
  enters the runtime or workspace. Admission grants no Sherlock
  command/channel permission ([messaging.md](./messaging.md)).

**Direct attachment is built; its joint acceptance with a real
Sherlock is not.** The tests stand in a runtime that keeps Sherlock's
lock and drain order (`tests/integration/direct_attachment_test.go`).

- **The authority is an ADR-0082 supervisor in an attach mode.** An
  administrator approves a host with `placement: direct`, which binds
  no key. The host spends it under `standalone-enrollment.v2`, whose
  spending registers the supervisor for the one agent and calls
  `admit`, as a standalone host's does. Garam places, registers and
  fences both placements alike; the placement tells the host which
  runner to start (§The host's runner).
- **The supervisor reads the runtime's status itself.** Its activation
  forward is the standalone one with the container check replaced: it
  reads `/agents/<GRN>/status` at a pinned loopback literal,
  `127.0.0.1:<port>` or `[::1]:<port>`, and forwards only a generation
  reporting `serving` under the GRN, at the request's epoch and
  revision, that it has never claimed before.
- **The writer evidence is the runtime's own.** Sherlock's store lock is
  taken before its generation is minted, so a fresh generation
  `serving` holds the store. Same-host trust is the stated assumption,
  and the status read carries no credential on loopback.
- **A new direct agent restarts once under its GRN.** Garam signs the
  audience as the GRN, and admission allocates it, so no running
  runtime can already carry it. Whoever restarts the runtime starts the
  new process only after the old one exits, and retries a start the
  store's lock refuses. The adapter sets aside every outbox entry
  written under the earlier ID and leaves it in place.
- **The placement is one host, one local filesystem and loopback.** A
  `tls:` placement, shared volumes and multi-node placement are
  outside it. A direct agent's assurance is one host's process
  isolation and the lock's advisory scope, less than a managed or
  standalone agent's.

Legacy v1/v2 approval bindings and supervisor credential retry behavior
remain unchanged until explicitly migrated; an `agent-enrollment.v1`
retry answers the recorded binding and re-issues nothing.

## Tokens in place of certificates

**Built for an agent's operator, a standalone host's supervisor, the
control service's contracts and the adapter.**
[ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
replaces an agent's certificate with a short-lived token, and the
order is the plan's,
`docs/plans/2026-10-09-machine-identity-migration.md`: this is steps
G1b to G1d, with the adapter reading its tokens as G2
([adapter.md](./adapter.md) §Token mode), and a supervisor's own
credential is §Standalone hosts'. A certificate still authenticates as
it did until G3.

- **The assignee mints, at the current epoch.** `mintAgentToken`
  answers an operator, or a standalone host's supervisor presenting its
  own token and naming no audience, for an agent assigned to it now and
  no other,
  which is ADR-0015's rule. The token names that assignee and epoch
  and the lineage the agent's execution holds, read under the
  execution row; it is admitted only for an agent whose lifecycle
  authority opened a lineage, inside it.
- **Every request reads it against both as they stand.** The token's
  row is joined to the agent's assignment and execution, so a
  reassignment or a recovery refuses it from the next request, on a
  connection already open too, and the execution fence admits a
  protected write on the token's serial only in the same terms
  (`migrations/000039_agent_tokens.up.sql`). A fenced agent's calls
  carry their activation's token in a `Garam-Execution` header of its
  own, since the agent token takes `Authorization`.
- **It lives an hour by default**, configurable from twenty minutes to
  a day, and its start is backdated by the minute a certificate's is,
  so a database whose clock trails the minting process finds it
  current at once. A mint voids nothing; an agent keeps its newest
  five tokens per audience and its newest ten in all, and an audience
  is an operator the organization registered, so naming more
  audiences widens nothing. A token renews no certificate: an agent
  holding one is minted another by its assignee.
- **A mint and an operator's deletion each wait on agent tokens in one
  statement, which takes them in `token_hash` order; an organization's
  deletion takes them in its cascade's order and waits on none** (#1434;
  `internal/agent` `PostgresStore.MintAccessToken`,
  `migrations/000047`).
  - A mint holds the agent's execution row `FOR SHARE`, records its
    token, whose foreign key holds the audience's registration, and
    then deletes in one such statement every token past the two bounds
    above, the expired ones included. It waits on the audience's
    registration holding no token but its own.
  - Deleting an operator holds every agent token naming it as
    audience, in that order, before `migrations/000041`'s cascade
    deletes them.
  - Deleting an organization holds its agents' execution rows, then
    its operators' registrations, each in byte order, before its
    cascade: the order a mint takes an execution row and then an
    audience's registration in. The cascade then deletes the agents'
    tokens through `agent_executions` in its own order, and no other
    writer holds one: a mint holds its agent's execution row, which
    the deletion already holds.
- **A token shown to a hosted operator is forwarded, not presented.**
  The assignee may mint one naming the control service as its
  audience; garam accepts it on no route, and the control service
  forwards it where it forwarded the agent's certificate, on activation
  and execution introspection under `execution-fence.v2`. garam reads
  that token's row as it was minted, requires it to name the agent and
  the caller as its audience, and hands its serial and dates to the
  execution fence, which judges it as it judges a certificate: a token
  of an earlier epoch or lineage, or an expired one, is answered fenced
  while its row is kept, and not proven once a mint has swept or
  trimmed it; either way it may not act.
  Deleting the hosted operator takes the tokens shown to it
  (`migrations/000041`).
## Separated execution

**Built here is garam's half of four parts: managed creation and its
initial issuance (below), the controller proof
([operators.md](./operators.md) §Controller proof), the generation fence
([machine-identity.md](./machine-identity.md) §Generation and lineage),
and garam's holding no agent definition (below), which is garam's
alone and built.** The
adapter this repository ships reaches the fence through the lifecycle
authority, never directly: it asks that authority to activate the
generation beside it and carries the token it is answered with
([adapter.md](./adapter.md) §Activation and runtime status); it
introspects nothing. The Operator control service's use of managed
creation is garamsh/garam-agent-operator#211 and #218's, per #1167.
Acceptance with the Operator control service and the adapter is
unverified and tracked in #1157. The rest is the accepted
target of ADR-0077 and ADR-0078. Product implementation is tracked
in #1157.

- **Desired runtime values and templates leave garam.** The Operator
  control service is their one writable managed source and the cluster
  controller pulls from it; a standalone host keeps its own. The GRN
  and the assignment stay here (ADR-0077).
- **garam holds no agent definition** ([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)).
  Migration 000058 drops `agent_definitions` and the staged cut-over's
  table, deletes every agent whose execution was in legacy mode, the
  rows that depend on it going by cascade while messages stay, and
  drops the execution row's mode. It refuses, each by an exception
  naming what it found, a definition or a cut-over of an agent not in
  legacy mode and a delegation holding no operation but
  `agent:cutover`.
- **garam keeps no template of its own** ([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)). The templates
  ADR-0088 retired read-only and did not import are dropped with their
  table by migration 000057, which refuses to run while one is held; a
  new agent starts from a template the control service published.
- **A managed agent is created by request, not by claim.** The control
  service presents a member's `agent:create` reference and the request
  identifier it binds; garam rechecks the reference from current state
  before it creates or answers an earlier creation, and reads no
  lifecycle authority to create. The identity, its first assignment to
  the bound controller, its execution and the creation record are one
  transaction (migration 000030), so a failure leaves no agent, and the
  same reference answers the same GRN and epoch while the agent stays
  where its creation put it. Under `managed-enrollment.v2` the control
  service presents its token and nothing else changes: creation opens
  the agent's lineage under either value, and its controller then
  mints the agent's tokens in it (§Tokens in place of certificates)
  where it would have obtained the initial certificate, which takes no
  token.
- **A managed agent is fenced from birth.** Its execution starts
  fenced, with no activation, in a lineage holding no certificate, so
  the one credential predicate (ADR-0080) refuses every certificate
  naming it until its initial issuance records the first. Initial
  issuance signs over a key the controller generated,
  under the creation's own reference and the caller's lifecycle
  authority for the current controller: it signs before the execution
  lock and rechecks the assignment and every expiry under it, and an
  identical retry answers the stored public result. Recovery, not
  initial issuance, replaces a lost credential, and once it has the
  initial issuance is refused, retries included.
- **A generation is what an assignment does not fence.** Beside the GRN
  and the epoch, every agent has one execution row recording its
  active generation and its credential lineage. A lifecycle
  authority activates a generation after proving it owns the writer;
  the execution credential cannot activate one, and a report activates
  nothing. The machine listener checks the generation per request
  (`machine-identity.md` §Generation and lineage), and every write it
  protects takes the row's
  lock in its own transaction, which is what orders a replacement
  against writes already admitted (ADR-0078, ADR-0080). A restart is a
  new activation, not a new certificate; a revision still pending
  carries a durable `agent:configure` reference, rechecked fresh, and
  one the runtime already applied needs none. A managed agent's first
  activation carries its own creation's reference or such a configure
  reference, and an activation carrying no reference is refused until
  one has succeeded; the creation reference starts that first
  generation and no later one.
- **The browser API answers the generation last activated.**
  `getAgent` and `listAgents` answer `generation`: the most recent
  activation's, read from the execution row's latest activation, whether
  it still admits the agent's writes or has since ended. It is absent
  for an agent never activated. It says what garam last activated, not
  what is running: whether that generation is current is the machine
  listener's introspection to answer.
- **Applied configuration is the runtime's report**, at its GRN, epoch,
  generation and desired revision, kept apart from what the Operator
  rendered or provisioned. The Operator control service holds it; garam
  only answers whether that epoch and generation are current, and a
  report activates nothing. A missing or stale report is unknown, never
  success (ADR-0078).
- **Execution authority is a scoped, expiring delegation plus a
  high-trust permission**, both checked fresh before a pending revision
  activates (ADR-0078).
- **A lost credential is recovered for the same GRN**, replacing its
  lineage and fencing the old one, rather than by recreating the agent
  (ADR-0078). It is built: the control service presents an
  administrator's `agent:recover` handoff for the exact request body,
  and the agent's earlier certificates stop authorizing from the next
  request, on a connection already open too. Ordinary renewal stays in
  the lineage the renewing certificate is current in. Under
  `execution-fence.v2` it answers what may have been taken rather than
  what was lost: it signs nothing, ends the lineage and opens the next,
  so every token minted before it is refused from the next request, and
  the assignee mints in the new one. The handoff travels in a
  `Garam-Operation` header there, and the recovery records no
  certificate or chain (`migrations/000040`).
- **A recovery's answer under `execution-fence.v1` names its chain**:
  the authority that signed the recovered certificate and the garam
  server root the holder verifies garam by, as initial issuance's
  does, so a holder verifies
  the recovered credential from the answer alone
  ([ADR-0091](./adr/0091-a-recovered-credential-names-its-chain-and-garam-stores-it-with-the-recovery.md)).
  Both are what the signer produced and are stored with the recovery,
  so a retry answers them again. A recovery recorded before
  `migrations/000036` stored neither, and its retry answers the chain
  the signer signs under now: the same one while neither the
  organization's authority nor the listener's server root has been
  replaced since.

## Standalone hosts

**garam's half is built, and so is the host's runner; the joint
acceptance is not.** A standalone host enrolls with
`garam standalone prepare` and `enroll`, under
`standalone-enrollment.v2` for a host prepared now and v1 for one an
earlier release prepared, and a v1 host renews with `renew`
(`internal/cli/standalone_host.go`); `garam standalone run`
supervises the runtime, proves the old writer exited and hands the
adapter its activation (`internal/workload`, ADR-0085). The runner is
tested against a real rootless daemon and the pinned Sherlock images
with garam's activation stood in for; enrollment, a message and its
reply, and a restart that keeps memory, joined to garam, are
unverified (#1180, #1169).

- **An administrator approves the host's own keys.** The host
  generates its supervisor key, its agent key and a bootstrap secret
  before it asks, and keeps them with a request identifier; the
  approval binds the secret's hash, both fingerprints and that
  identifier to one organization, host name and agent, expiring within
  a day, on `agent:enroll` read from current state (ADR-0082). An
  identical approval retry answers the same binding; a withdrawn or
  expired one is not revived.
- **Spending is one transaction and retryable.** The host presents the
  secret and a request over each approved key; the approver's
  authority is read again, both certificates are signed over the
  host's keys, and a new agent is written with an execution fenced
  from birth, its first assignment to the supervisor and the first
  certificate of its lineage (migration 000031). An identical retry
  answers the recorded public result while that registration still
  holds the agent, and nothing otherwise; two identical first
  spendings queued on one approval create one agent and answer the
  second with the first's result. Withdrawing and spending one
  approval serialize on its row.
- **Under `standalone-enrollment.v2` the host is answered a credential,
  not certificates** (ADR-0100). An approval binding the host's keys is
  spent under v1 alone and one binding none under v2 alone, so neither
  contract spends the other's approval, and one fingerprint without
  the other is refused. A v2 spending signs nothing: it places the
  agent as v1 does, registers the supervisor holding no certificate,
  and answers a credential garam keeps only the hash of
  (`migrations/000042`). An identical retry, while the registration
  still holds the agent, re-issues it under the approval's row lock,
  and the credential answered before goes with every token it was
  exchanged for; garam keeps no audit trail of that or of any other
  void. The supervisor exchanges the credential for hour-long tokens
  on `exchangeToken`, mints its agent's tokens for garam alone, and on
  the execution routes forwards the agent token it minted for garam;
  that form is a supervisor's alone, since it is the assignee and the
  lifecycle authority at once, and an operator forwards a token shown
  to it. A revocation or a replacement ends the
  registration, and every read of the credential or a token joins a
  registration that has not ended.
- **The supervisor activates its own agent and nothing else.** On the
  execution routes it is authorized by its current registration, its
  being the agent's assignee and current policy, read per request; it
  holds `agent:activate` alone. Its registration is read again under
  the execution lock beside the assignment, so an activation queued
  behind a revocation or a replacement is refused rather than
  installing a token. Its first activation rests on the approval,
  where a managed creation's needs its reference. It reaches its own
  identity, its renewal and those three execution routes, and with its
  own token its agent's token mint as well, and nothing else:
  `ConfineSupervisors` in
  `internal/machineapi/standalone.go` refuses every other route at the
  router, so a supervisor posts, claims and reads no message and acts
  on no operator route. Every collaboration route admits through the
  one fence for both modes.
- **Replacement and revocation are an administrator's.** A replacement
  approval names the supervisor and epoch it replaces and, spent,
  reassigns the agent at the next epoch, ends the former registration
  and activation and moves the agent to a new lineage under the same
  GRN; an older approval of the replaced assignment then refuses.
  Revocation ends the registration, the activation and every unspent
  approval for the agent under the execution lock. An expired
  supervisor or agent credential is recovered the same way; nothing
  recovers it without an administrator.
- **A replacement answers the activation its successor names.** The
  new supervisor's first activation must name the agent's most recent
  activation, and a host whose supervisor never activated has no
  record of it. The spending records it as the execution lock found it
  (`migrations/000053`) and answers it as `replacesActivationId`, a
  replay the same one. `garam standalone run` seeds the supervisor's
  record with it where the record holds none, and never overwrites one
  it holds.
- **The approval names the placement.** `standalone`, the default, is
  the three containers §The host's runner starts; `direct` is a runtime
  the host already runs, which the supervisor attaches to
  (§Common managed and direct enrollment). A direct approval binds no
  key, so it is spent under v2 alone, and the schema refuses one
  binding keys (`migrations/000053`). The spending answers the
  placement and the host's enrollment record keeps it; a record an
  earlier release wrote names none and is a standalone host's.
- **An organization lists what it approved and registered.**
  `listAgentEnrollments` answers every approval, pending, spent,
  withdrawn or expired, and `listAgentSupervisors` every registration,
  active or revoked, with its host, agent, epoch and the latest
  activation of its agent at that epoch. Both are paged like any list
  and decided on `agent:read` from current state, as the writes beside
  them are. The store reads them into summary types
  (`internal/agent/standalone.go`, `EnrollmentSummary` and
  `SupervisorSummary`) with no field for a secret's hash, a key
  fingerprint, a request digest, a serial, a certificate or a
  credential, so no listing can answer one.
- **The supervisor's renewal replays for its predecessor.** It renews
  over a new key in the last third of its validity; the certificate a
  renewal replaced may replay that renewal's public answer for the
  identical request, and two identical renewals racing install one
  successor; a supervisor enrolled under v2 holds no certificate and
  renews none. `api/machine.yaml` §standalone is authoritative for the
  wire and both `standalone-enrollment` values.
- **`garam standalone` prepares and enrolls under v2 (#1352).**
  `prepare` writes the bootstrap secret and the request identifier and
  generates no key, so the approval it asks for binds none. `enroll`
  spends it and installs the supervisor's credential as `credential` in
  the supervisor's directory, owner-only, and the server root in both
  directories; the credential never enters the enrollment record.
  Interrupted before that record, `enroll` sends the same request, and
  garam re-issues the credential and voids the one written before. A
  v2 host has no certificate, so `renew` refuses it.
- **The record decides the contract, and no flag does.** A preparation
  or enrollment an earlier release wrote with keys goes on enrolling,
  renewing and running under v1. A flag would be a second source of
  truth that could disagree with the record, which already says which
  approval was asked for.
- **The host's files.** The supervisor's directory holds the prepared
  secret and key, its current certificate and key in one file a
  renewal replaces in one rename, and a pending renewal kept from
  before its request until its answer is installed. The agent's
  credential directory holds the agent's key and certificate. A v2
  host's supervisor directory holds the credential instead, and its
  agent directory the server root alone. Neither
  is the agent's memory, outbox or workspace, and no container mounts
  either: the supervisor copies what the adapter needs into it.
- **The host's commands own what they write.** The two directories
  never overlap; the preparation records the agent directory it owns
  and refuses one holding credentials it did not write. Each command
  holds an exclusive lock on each directory it writes, and refuses
  rather than waits where another holds it. Once the enrollment record
  exists, `enroll` installs nothing again, so a replay never replaces
  a credential renewed since; `renew` runs only after it, and the CLI
  writes the agent's directory only before it, so the agent's own
  renewal is never overwritten. Every request goes to an `https://`
  origin verified by the pinned garam server root, and a redirect is
  refused rather than followed.

### The host's runner

`garam standalone run` is the supervisor's process: in the
foreground, over one rootless Docker daemon whose socket it alone
holds (ADR-0085). It returns once the generation it supervises has
exited and been drained, and restarting it is the host's init's; a
supervisor that stops leaves the workload running.

- **A run waits for a create in flight under its own name.** A
  supervisor stopped while it creates a container leaves that create
  to finish in the daemon, so the next run's create is refused the
  name before the container answers by it. That run asks the engine
  again until the container answers, its context ends, or ten seconds
  pass, and adopts it by name and nonce as it adopts any create whose
  answer was lost (`inFlightCreateWait`, `internal/workload/containers.go`).
  Such a create took about 60 ms when measured. Without the wait, the
  next run lost that race every time and exited, and only a further
  start recovered (#1604). A name that still answers nothing after the
  bound fails the run as unavailable, and restarting it stays the host
  init's.
- **A generation is three containers sharing the runtime's network
  namespace.** The runtime runs the pinned `sherlock-agent` image as
  65532 with no capability, its gateway on loopback and no port
  published. The workspace runs the pinned `sherlock-workspace` image
  with its executor's five capabilities. The adapter runs garam's image
  as 65532 with no capability. Each image is named by a digest or an
  image ID.
- **Two volumes, each mounted where it is needed.** The memory volume
  goes into the runtime whole and into the adapter as its outbox
  directory alone; the workspace volume goes into the workspace alone,
  so no Exec child reaches memory and the adapter cannot open the
  store. The supervisor creates each volume with a nonce it records
  first, and refuses one that is gone or carries another nonce; it
  creates nothing to replace it, and a person decides what follows.
- **The outbox is a directory before the adapter mounts it.** The
  supervisor makes it in the runtime's first start, 0700 and owned by
  65532, and checks it again before the adapter is created; a link or
  anything else there refuses, and the path is never repaired. The
  store files the runtime creates beside it are 0600 and 65532's, the
  modes Sherlock keeps its state in from v0.5.0 on (sherlock#1025), so
  an account other than the runtime's on that volume can neither list
  what the agent is about to send nor read its memory. The adapter
  runs as 65532 and reads the outbox all the same.
- **`--require-store` follows the record.** Every start passes it once
  the memory volume's record says a runtime served on it, which the
  first runtime-status report saying `serving` writes; nothing infers
  it.
- **The intent is written before anything of it exists.** Container
  names derive from it and carry its nonce. A container under one of
  those names is adopted only when it carries the nonce, the intent,
  the image and the volumes, which recovers a lost create's container
  without making a second. Anything else there is left untouched, and
  the run stays pending. Once its ID is recorded, a container is found
  by that ID alone, whatever it is renamed to.
- **A drain never forces.** SIGTERM goes to the runtime and the
  supervisor waits for it to exit, however long that takes, so a turn
  keeps its tools; then the workspace and then the adapter are stopped
  the same way, and only then is the successor created. A recorded
  container the engine cannot account for keeps the drain pending. Any
  one of the three exiting ends the generation the same way, and so does
  its control listener stopping. A replacement is a changed revision,
  epoch or image, or `--replace`.
- **The adapter activates through a socket only its generation
  mounts.** The supervisor serves `agent-execution.v1` there over TLS,
  with a serving root minted for the generation and removed with it.
  It verifies the agent's leaf against the agent's issuer and forwards
  that leaf to garam. It forwards an activation only for the intent's
  placement token, epoch and revision, its runtime unrestarted, and a
  generation never asked for before. A retry repeats the first
  request, so garam answers the same activation with a newer token.
- **A v2 host serves `agent-execution.v2` on that socket instead**
  (ADR-0100). The listener asks for no certificate, and each request
  presents the generation's control token: 256 random bits the
  supervisor draws with the intent, records with it and never sends to
  garam. The supervisor forwards the agent token it minted for garam in
  place of the leaf, whichever it holds when it forwards. The control
  path never leaves the host, since the supervisor is the lifecycle
  authority, and `mintAgentToken` refuses a supervisor an audience; a
  token garam issued for control would be one garam accepts, which
  ADR-0100's audiences forbid.
- **Credentials are copied in, never mounted.** The runtime's
  configuration and the adapter's key, certificate, roots and
  placement token are written into each container before it starts,
  owned by 65532 and readable by it alone; the runtime's environment
  is the model credential. That credential is a limit rather than an
  isolation: every tool the runtime runs reads its environment, as
  Sherlock passes it on (ADR-0085), and the engine shows it to whoever
  reaches the rootless daemon, which is the host user alone.
- **A v2 host copies tokens in, and keeps the agent's current.** The
  adapter receives the agent token and the control token in place of
  a key and certificate (`adapter.md` §Token mode). The supervisor
  exchanges its credential for its own token and mints the agent's,
  holds both in memory, and replaces each at a point drawn from two
  thirds to three quarters of its life; a refresh that fails while the
  token held is live keeps it. Every thirty seconds it compares the
  agent token it holds with the one it handed the running adapter and
  copies the newer one in. A copy into a container removes and
  recreates the file rather than renaming it, which the adapter reads
  as undecided and retries; a bind mount that would allow a rename is
  what ADR-0085 refuses, and a rootless host cannot own a file as the
  adapter's account on one.
- **The runtime's ego and instructions are the host's.**
  `--ego-file` and `--instructions-file` are copied in beside the
  configuration and named to the runtime; with no ego, the runtime
  runs its own default. The configuration's `revision` names all
  three, so changing any of them is a new revision (ADR-0087). A host
  carries the `garam-message.v1` reply instruction this way.
- **The adapter may reach garam at its own address.**
  `--adapter-machine-url` is the machine listener as seen from the
  runtime's namespace, where a rootless host's loopback is not; it
  defaults to the supervisor's `--machine-url` (ADR-0087).
- **The runtime trusts garam's issuer by the key set it fetches
  itself, where the host names that issuer** (ADR-0101, step 4). With
  `--message-issuer` set, exactly as garam's `machine.issuer` names
  it, the supervisor adds one `issuers` entry to the runtime's
  configuration, once, when it creates the runtime: `issuer` is that
  value, `keys-url` is the adapter's address for garam followed by
  `/message-signing-keys`, and `keys-ca-file` is the garam server root
  the adapter verifies garam by, copied into the runtime for its owner
  alone. The host's own entries stay, and one naming garam's issuer is
  refused. **Without `--message-issuer` it renders no entry and copies
  no root**, as the Operator's renderer does (garam-agent-operator#337):
  the issuer is an identity, so no address the host reaches garam at
  stands in for it, and a host not ready to verify stays as it was.
  **The issuer and the URL differ, and that is deliberate.** The issuer
  is compared byte for byte against every signature and need not
  resolve; the URL is only where the runtime reaches the set, and the
  runtime shares the adapter's network namespace, where a rootless
  host's loopback is not the host's, so it reaches garam wherever the
  adapter does (ADR-0087). **`keys-url`'s host must be the adapter's
  machine-listener host.** Sherlock checks that host against the
  listener's certificate with standard TLS, `keys-ca-file` replacing
  the roots and not the hostname check; the adapter dials it the same
  way, so it is a name on the listener's certificate, and any other
  host fails every key fetch with 503 `issuer keys unavailable`. The
  runtime fetches the set again on a key it does not hold, which
  covers a rotation, a first key and a key retired at once, so nothing
  on the host refreshes it.
- **The same setting turns the adapter's signing on** (#1312). Where
  the supervisor renders garam's entry it also sets the adapter's
  `GARAM_ADAPTER_SIGN_MESSAGES`, and where it renders none the adapter
  signs nothing, so a host never signs to a runtime that does not trust
  garam, and an unconfigured host is as it was. The order
  `adapter.md` §Stable identifiers and the outbox asks for holds within
  one generation: the runtime starts with its entry before the adapter
  starts. **The floor is Sherlock `v0.4.0`, and naming the issuer is
  what asserts it.** `keys-ca-file` arrives there
  (garamsh/sherlock#1008), and nothing the supervisor reads from an
  image reference says which release it is. On an older runtime the
  rendered entry cannot reach garam's set either: a `0.3.0` runtime
  starts with it, reading no such member (`internal/config/issuers.go`
  at `v0.3.0`), and fetches against the system roots, which do not
  reach garam's private root. So a host on a runtime older than
  `v0.4.0` names no issuer, and one that does has every message
  refused and handed out again. `tests/standalone/signing_test.go`
  holds the configured case against the pinned Sherlock: a runtime that
  refuses anything unsigned takes the message. A generation started
  before the rendering keeps the configuration and the adapter it was
  given until it is replaced.
- **An engine it cannot reach decides nothing.** The supervisor starts,
  signals and activates nothing, and the generation keeps running with
  its adapter's token. A daemon stop may stop the runtime, and recovery
  after it is a new generation.
- **The memory moves only through a verified copy (ADR-0089).**
  `--move-memory-to <volume>` drains the generation and takes a store
  only from a runtime that exited 0 after its drain, which the drain
  records. A helper running `garam copy-memory` from the run's garam
  image copies the store files and the outbox into a volume the move
  creates, holding Sherlock's writer lock, with the old volume mounted
  read-only. The supervisor compares digests of the old volume before
  and after and of the copy, and the next generation starts on the
  copy with `--require-store`. Its first activation request is the
  copy accepted, and it names the old activation as replaced, so garam
  fences the old token. Any failure leaves the agent stopped on the
  old volume with the reason stated, keeps the refused volume and
  never uses it again; the old volume is never written or removed. A
  move not yet settled refuses every run that does not ask for it.

### Attached to a direct runtime

`garam standalone run` on a host enrolled under the direct placement
runs the supervisor's attach mode (ADR-0111, `internal/workload`
`attach.go`). It holds no engine, and it starts, signals and drains
nothing; restarting the runtime is the host's.

- **The runtime is named by a loopback literal.** `--runtime-address`
  is `127.0.0.1:<port>` or `[::1]:<port>`, never a name: `localhost`
  resolves to either family, and two runtimes could each hold one. The
  standalone placement's engine, image and model flags are refused
  here, and `--runtime-address` is refused there.
- **It reads the status as the adapter reads it.** The supervisor
  reads the runtime's status through the adapter's own reader,
  `delivery.HTTPGateway.Status`, so both hold the runtime to one parse.
  A status naming another agent or contract version, or no
  generation, is `placement_not_current`. An answer it cannot read is
  `undecided`, and so is a runtime still under its own ID, which
  answers the GRN's path 404 until it restarts under the GRN.
- **The adapter's files are written into the agent's directory.** The
  control's root, the placement token, the control token and the agent
  token go there owner-only, and the control socket is
  `control/control.sock` there. A host process reads its root once, so
  one root, kept in the supervisor's `attach/` directory, outlasts
  every intent. The agent token is rewritten whenever the supervisor
  holds a newer one, by rename. The run holds its lock on that
  directory as well as on its own.
- **An intent lasts as long as the runtime's generation.** The
  supervisor reads the status every five seconds once it has forwarded
  a generation. A status it cannot read decides nothing; another
  generation ends the intent, and the run returns for the host's init
  to start again, which opens the next intent with new tokens. Its
  first activation names the one before, so garam fences the old
  generation. A record holding a generation this host started as
  containers and never drained is refused.

What is still not settled is ADR-0085 §What this does not decide, but
for the storage handover ADR-0089 answers: the joint acceptance with
garam and a model, renewing the agent's certificate in a running
adapter, and a measured daemon stop. ADR-0085 is authoritative for the
arrangement and the socket's contract, and ADR-0089 for the move.
`make workload-integration` runs both against a real daemon, and `make
standalone-e2e` runs the move against a real garam and model too.
