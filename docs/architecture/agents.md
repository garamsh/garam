# Agents

Autonomous identity, provisioned outside garam (ADR-0012). Implemented
by `internal/agent`.

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Definition` | what an organization said an agent should be: which operator runs it and with what values (ADR-0026), and not what it is built from — it names no image. It names its subject where an `Agent` names itself, and carries none of the state an `Agent` carries, so a reader cannot take one for the other; it writes no `agents` row until the operator it names claims it (ADR-0033). garam accepts no new one: each is read, claimed and cut over as written, and nothing writes one (ADR-0090) |
| `Template` | a starting point an organization kept for defining agents: a name, a description and values a definition copied once (ADR-0075). It names no operator and no agent, and no definition records the template it came from. Templates are retired read-only: each is read as the record of what it said, and nothing writes one (ADR-0088) |
| `Name` | what an organization calls a defined agent or a template, separate from the GRN: trimmed, one to 100 characters, and not unique |
| `Assignment` | what holds an agent — an operator, or a standalone host's supervisor (ADR-0082) — and the epoch that binding holds |
| `Certificate` | what an agent authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Service` | creates an agent for the calling operator, issues the certificate of one assigned to it while its execution is legacy and its cutover is not switched (ADR-0086), answers an operator with the definitions naming it so it can claim one (ADR-0033), and records the state each party reports (ADR-0053) |
| `Issuer` | signs a certificate for a subject GRN and hands back the garam server root beside it |
| `ExecutionService` | what a lifecycle authority does to an agent's execution — creates a managed agent and issues its first certificate, activates a generation, deactivates it, inspects a certificate and a generation, recovers the credential lineage, cuts a legacy agent over (ADR-0086) — and how an agent renews its own certificate, legacy or fenced, inside the lineage that certificate is current in (ADR-0040, ADR-0077, ADR-0078) |
| `ExecutionStore` | the execution row every agent has, its activations, its credential lineage and its cutover attempts, written under the row's lock (ADR-0080), and a managed agent's creation and the initial issuance that starts its lineage |
| `Cutover` | a legacy agent's source as the Operator imports it — its definition, or none — with the digest over it and the agent's most recent attempt (ADR-0086) |
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
none reaches a supervisor-held agent. A definition names one the
organization has registered as well, which the schema's foreign key
decides (ADR-0045), as the browser surface also did while it wrote
definitions (ADR-0090); this domain reads no record of its own and
addresses an operator by GRN. Every assignment raises the
epoch, and a state report is accepted only at the current one — the
operator a reassignment replaced reports nothing about the agent it
lost. An agent is never an organization member (ADR-0014), so nothing
here references `users`.

`Service` is what an operator does to the agents it runs. It creates
one in the operator's own organization, garam minting the identifier,
and it answers with the certificate of an agent assigned to that
operator at the current epoch and of no other — the whole of the
authorization on the operator route, which ADR-0015 states is not an
optimisation and may not be skipped, so no operator reaches an issuer
without it. Comparing the operator on the row is the epoch fence as
well, and it is the tenancy check too, since a GRN carries its
organization. What that operator reports about the agent's pod rests
on the same comparison (ADR-0053), so it writes state exactly where it
obtains a certificate and nowhere wider. A caller that is not an
operator is refused, except on the two routes an agent reaches as
itself: renewing its own certificate over the connection that
certificate authenticated, which grants it nothing it does not already
hold (ADR-0040), and reporting its own runtime state. Both are bounded
by the identity the connection proved rather than by an assignment, and
both run the execution fence first, so the certificate must still be
inside its validity and current in its lineage on that request, and
once the GRN has cut over the call carries its activation's token
([machine-identity.md](./machine-identity.md) §Generation and
lineage). Renewal is `ExecutionService`'s, not `Service`'s: it is the
one route the assignment comparison does not stand on, and the
successor joins the renewing certificate's lineage. An expired
certificate has no such connection, so the operator route is what
recovers one while the GRN has not cut over; after cutover that route
refuses it and the control service's recovery is what replaces a lost
certificate (§Separated execution).

garam accepts no desired value (ADR-0090): no route writes a
definition, and migration 000035's trigger refuses an insert into
`agent_definitions`, an update changing anything but the name, and a
delete other than its organization's cascade, from whichever release
sends it. A definition's operator and values therefore stay as they
were written, and none is deleted except with its organization. Its
name is the organization's word for the agent and changes alone:
renaming touches no GRN, operator, value, assignment, membership or
attachment, and no operator is told (ADR-0075). A definition written
without a name, and every one written before names existed, is called
by its identifier.

`Store` answers `Service` and, org by org, the read the browser-facing
listener serves people: an organization's agents and the assignments
holding them, each a batch read rather than one per agent. An agent no
operator holds contributes no assignment, so the two lists are not one
length. Both aggregates are rehydrated inside this package — their
fields are unexported and no exported constructor restores a stored
row's state or timestamps — which is why the store that reads them back
lives here rather than below. A `Template` is a data carrier with
exported fields (ADR-0069), and a `Definition`'s name is an exported
field beside its accessors; the stores stay beside the others until
#1083 moves this domain's store out of the package.

Signing happens through the `Issuer` seam the composition root
satisfies from `internal/ca`, so the two domains still cooperate
through `grn.GRN` alone and this one never holds key material. The
HTTP surfaces are the machine listener — where the operator reports the
pod and the agent reports itself, each on a route of its own and
neither reaching the other's fact (ADR-0053) — and the browser-facing
one for the organization's members, which reads the agents and
templates and writes only a definition's name: provisioning is an
operator's, from outside garam or through its control service, and a
definition writes no `agents` row until the operator it names claims
it (ADR-0033). The
queue a message reaches an agent through is the messaging domain.

What an agent is built from is the operator's. A `Definition` names no
image, and `values` is not a way around that: garam stores the keys and
the strings and interprets neither (`api/machine.yaml` §Definition), so
a value that happened to name an image would be one garam still never
reads — which leaves the shape of that payload where ADR-0026 left it,
undecided, rather than deciding it here. The images an agent's Pod is
built from and the size of the volume it keeps state on are named by
flags that Garam Agent Operator's `cmd/main.go` declares
([`garamsh/garam-agent-operator`](https://github.com/garamsh/garam-agent-operator)),
which is authoritative for what every agent that operator constructs
is built from. Read at
[`2d795f5`](https://github.com/garamsh/garam-agent-operator/blob/2d795f5/cmd/main.go),
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
[`eeea6a9`](https://github.com/garamsh/sherlock/blob/eeea6a9/internal/config/config.go#L79-L82),
read when the repository was named `gagent`.

Nothing in garam requires a version or a capability of an agent today.
Such a requirement is the one thing on this boundary garam could
properly make, because what its own machine contract needs is a fact
garam holds rather than one about somebody's registry. ADR-0079 accepts
one: each interservice contract works with the adjacent supported
release of its peer in both upgrade orders, and unsupported semantics
are refused by name. None of it is built, so garam still publishes no
release of that contract and no compatibility window (ADR-0049).

## Tokens in place of certificates

**Built for an agent's operator and for the control service's
contracts; a supervisor-held agent is not yet.**
[ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
replaces an agent's certificate with a short-lived token, and the
order is the plan's,
`docs/plans/2026-10-09-machine-identity-migration.md`: this is steps
G1b and G1c; a supervisor minting its agent's tokens, and its own
credential, are G1d. Until then a certificate still authenticates as
it did.

- **The assignee mints, at the current epoch.** `mintAgentToken`
  answers an operator for an agent assigned to it now and no other,
  which is ADR-0015's rule. The token names that assignee and epoch
  and the lineage the agent's execution holds, read under the
  execution row; it is admitted where the certificate route would be —
  an agent still legacy whose cutover is not switched — and for an
  agent whose lifecycle authority opened a lineage, inside it.
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
- **A token shown to a hosted operator is forwarded, not presented.**
  The assignee may mint one naming the control service as its
  audience; garam accepts it on no route, and the control service
  forwards it where it forwarded the agent's certificate, on activation
  and execution introspection under `execution-fence.v2`. garam reads
  that token's row as it was minted, requires it to name the agent and
  the caller as its audience, and hands its serial and dates to the
  execution fence, which judges it as it judges a certificate: a token
  of an earlier epoch or lineage is answered fenced, not unknown.
  Deleting the hosted operator takes the tokens shown to it
  (`migrations/000041`).
- **An unfenced agent's token comes the same way.** A `Garam`-source
  agent of the Operator, whose adapter runs in garam's legacy mode
  with no control settings (garam-agent-operator ADR 0068), is held by
  its operator at an epoch like any other, so the operator mints its
  token on the same route and refreshes it by minting the next before
  the current one runs out. What changes for that adapter is only
  where it reads the token from, which is step G2's.

## Separated execution

**Built here is garam's half of four parts: managed creation and its
initial issuance (below), the controller proof
([operators.md](./operators.md) §Controller proof), the generation
fence ([machine-identity.md](./machine-identity.md) §Generation and
lineage) and a legacy agent's cutover (below); closing new
definitions and retiring garam's templates (below) are garam's alone
and built.** The adapter this
repository ships reaches the fence through the lifecycle authority,
never directly: it asks that authority to activate the generation
beside it and carries the token it is answered with
([adapter.md](./adapter.md) §Activation and runtime status); it
introspects nothing. The Operator control service's use of managed
creation is garamsh/garam-agent-operator#211 and #218's, per #1167.
Acceptance with the Operator control service and the adapter is
unverified and tracked in #1157. The rest is the accepted
target of ADR-0077 and ADR-0078, and everything above describes what
runs for a GRN that has not cut over. Product implementation is tracked
in #1157.

- **Desired runtime values and templates leave garam.** The Operator
  control service is their one writable managed source and the cluster
  controller pulls from it; a standalone host keeps its own.
  `Definition` values are what move, per GRN, with no dual write and no
  new workload or volume. The GRN, the `Name` and the assignment stay
  here, and the console copies an Operator template once (ADR-0077).
- **garam accepts no new definition** (ADR-0090). `defineAgent` and
  `agent:define` are gone, and migration 000035's trigger refuses an
  insert into `agent_definitions`, an update changing anything but
  the name and a direct delete, from whichever release sends it; it
  orders itself against a write in flight as 000034 does. A rename is
  admitted, and a delete reached through the organization's own
  deletion is too, so a definition still goes with its organization.
  Every definition held stays readable, claimable by the operator it
  names and cut over below, so the legacy agents a cutover has to
  move are those garam held when 000035 ran.
- **garam's templates are retired read-only and not imported**
  (ADR-0088). Migration 000034's trigger refuses every insert, update
  and delete on `agent_templates`, from whichever release sends it:
  creating it waits for a write already holding the table, which
  commits first, and a write reaching the table after it is refused. A
  delete reached through the organization's own deletion is admitted.
  No route or action writes a template; `listAgentTemplates` and
  `getAgentTemplate` read them as records on `agent-template:read`, and
  no definition or revision changes.
- **A legacy agent cuts over in stages garam records under its
  execution lock** (ADR-0086). The Operator reads the agent's source —
  its definition, or empty values where none describes it — and the
  SHA-256 of its RFC 8785 JSON, imports it inactive, and asks garam to
  freeze it: under the execution row and then the definition row, garam
  rechecks the assignment and the grant and records the attempt `frozen`
  only if the source's digest there is the import's. garam then records
  `switched`, before the Operator releases anything. A frozen attempt
  may be rolled back and a switched one may not; each attempt is keyed
  by the import's identifier, so a retry answers what its stage recorded
  and nothing reaches an ended one. From the freeze the operator's
  definition poll omits the agent; from the switch its certificate route
  refuses it, and the agent's first activation is admitted. That route
  signs first and decides under the row after: a switch committing
  between its two reads leaves a certificate signed and discarded, never
  answered. Each stage carries an owner's or administrator's
  `agent:cutover` reference minted for that stage's route alone,
  rechecked. The definition is never written or deleted: a switched one
  is archival, and the browser read says so, and migration 000033's
  rollback refuses while a cutover is held. `garam cutover-inventory`
  prints every source record of an organization with a digest of each
  value and no value. The Operator's import and release are unbuilt
  (garamsh/garam-agent-operator#217).
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
  token. The operator's own route still creates in
  two writes, as `Service.Create` in `internal/agent/service.go` says,
  and creates legacy agents alone.
- **A managed agent is fenced from birth.** Its execution starts
  fenced, with no activation, in a lineage holding no certificate, so
  the one credential predicate (ADR-0080) refuses every certificate
  naming it until its initial issuance records the first; the
  operator's certificate route and definition claiming never reach
  it. Initial issuance signs over a key the controller generated,
  under the creation's own reference and the caller's lifecycle
  authority for the current controller: it signs before the execution
  lock and rechecks the assignment and every expiry under it, and an
  identical retry answers the stored public result. Recovery, not
  initial issuance, replaces a lost credential, and once it has the
  initial issuance is refused, retries included.
- **A generation is what an assignment does not fence.** Beside the GRN
  and the epoch, every agent has one execution row recording its mode,
  its active generation and its credential lineage. A lifecycle
  authority activates a generation after proving it owns the writer;
  the execution credential cannot activate one, and a report activates
  nothing. A legacy agent's first activation waits for its cutover's
  switch (below), and from the switch the operator's certificate route
  and its definition no longer reach it. The machine
  listener checks the generation per request
  ([machine-identity.md](./machine-identity.md) §Generation and
  lineage), and every write it protects takes the row's lock in its own
  transaction, which is what orders a replacement against writes
  already admitted (ADR-0078, ADR-0080). A restart is a new
  activation, not a new certificate; a revision still pending carries
  a durable `agent:configure` reference, rechecked fresh, and one the
  runtime already applied needs none. A managed agent's first
  activation carries its own creation's reference or such a configure
  reference, and an activation carrying no reference is refused until
  one has succeeded; the creation reference starts that first
  generation and no later one.
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
acceptance is not.** A standalone host enrolls and renews with
`garam standalone prepare`, `enroll` and `renew`
(`internal/cli/standalone_host.go`), and `garam standalone run`
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
- **The supervisor activates its own agent and nothing else.** On the
  execution routes it is authorized by its current registration, its
  being the agent's assignee and current policy, read per request; it
  holds `agent:activate` alone. Its registration is read again under
  the execution lock beside the assignment, so an activation queued
  behind a revocation or a replacement is refused rather than
  installing a token. Its first activation rests on the approval,
  where a managed creation's needs its reference. Its certificate
  reaches its own identity, its renewal and those three execution
  routes and nothing else: `ConfineSupervisors` in
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
- **The supervisor's renewal replays for its predecessor.** It renews
  over a new key in the last third of its validity; the certificate a
  renewal replaced may replay that renewal's public answer for the
  identical request, and two identical renewals racing install one
  successor. `api/machine.yaml` §standalone is authoritative for the
  wire and `standalone-enrollment.v1`.
- **The host's files.** The supervisor's directory holds the prepared
  secret and key, its current certificate and key in one file a
  renewal replaces in one rename, and a pending renewal kept from
  before its request until its answer is installed. The agent's
  credential directory holds the agent's key and certificate. Neither
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
  supervisor makes it in the runtime's first start and checks it again
  before the adapter is created; a link or anything else there
  refuses, and the path is never repaired.
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
- **Credentials are copied in, never mounted.** The runtime's
  configuration and the adapter's key, certificate, roots and
  placement token are written into each container before it starts,
  owned by 65532 and readable by it alone; the runtime's environment
  is the model credential. That credential is a limit rather than an
  isolation: every tool the runtime runs reads its environment, as
  Sherlock passes it on (ADR-0085), and the engine shows it to whoever
  reaches the rootless daemon, which is the host user alone.
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
  on the host refreshes it. **`keys-ca-file` arrives in Sherlock
  `v0.4.0`** (garamsh/sherlock#1008). A `0.3.0` runtime starts with
  the entry, reading no such member (`internal/config/issuers.go` at
  `v0.3.0`), and fetches the set against the system roots, which do
  not reach garam's private root, so it refuses a signed message and
  accepts an unsigned one as before. The runtime refuses a signed
  message from an issuer it has no entry for, which is why this lands
  before the adapter forwards a signature (#1258); until then the
  entry admits nothing new. A generation started before the rendering
  keeps the configuration it was given until it is replaced.
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

What is still not settled is ADR-0085 §What this does not decide, but
for the storage handover ADR-0089 answers: the joint acceptance with
garam and a model, renewing the agent's certificate in a running
adapter, and a measured daemon stop. ADR-0085 is authoritative for the
arrangement and the socket's contract, and ADR-0089 for the move.
`make workload-integration` runs both against a real daemon, and `make
standalone-e2e` runs the move against a real garam and model too.
