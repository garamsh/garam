# Agents

Autonomous identity, provisioned outside garam (ADR-0012). Implemented
by `internal/agent`.

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Definition` | what an organization says an agent should be: which operator runs it and with what values (ADR-0026), and not what it is built from — it names no image. It names its subject where an `Agent` names itself, and carries none of the state an `Agent` carries, so a reader cannot take one for the other; it writes no `agents` row until the operator it names claims it (ADR-0033) |
| `Template` | a starting point an organization keeps for defining agents: a name, a description and values a definition copies once (ADR-0075). It names no operator and no agent, and no definition records the template it came from, so editing or deleting one reaches no agent |
| `Name` | what an organization calls a defined agent or a template, separate from the GRN: trimmed, one to 100 characters, and not unique |
| `Assignment` | what holds an agent — an operator, or a standalone host's supervisor (ADR-0082) — and the epoch that binding holds |
| `Certificate` | what an agent authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Service` | creates an agent for the calling operator, issues the certificate of one assigned to it while the agent has not cut over, answers an operator with the definitions naming it so it can claim one (ADR-0033), and records the state each party reports (ADR-0053) |
| `Issuer` | signs a certificate for a subject GRN and hands back the garam server root beside it |
| `ExecutionService` | what a lifecycle authority does to an agent's execution — creates a managed agent and issues its first certificate, activates a generation, deactivates it, inspects a certificate and a generation, recovers the credential lineage — and how an agent renews its own certificate, legacy or fenced, inside the lineage that certificate is current in (ADR-0040, ADR-0077, ADR-0078) |
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
none reaches a supervisor-held agent. A definition names one the
organization has registered as well, which the browser surface decides
against the operators domain and the schema decides again (ADR-0045);
this domain reads no record of its own and addresses an operator by GRN. Every assignment raises the
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

A definition's operator and values are written once. Its name is the
organization's word for the agent and changes alone: renaming touches
no GRN, operator, value, assignment, membership or attachment, and no
operator is told (ADR-0075). A definition created without a name, and
every one written before names existed, is called by its identifier.

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
one for the organization's members, which reads the agents and writes
only definitions, their names and templates: provisioning is an
operator's, from outside garam, and a definition writes no `agents`
row until the operator it names claims it (ADR-0033). The queue a
message reaches an agent through is the messaging domain.

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

## Separated execution

**Built here is garam's half of three parts: managed creation and its
initial issuance (below), the controller proof
([operators.md](./operators.md) §Controller proof) and the generation
fence ([machine-identity.md](./machine-identity.md) §Generation and
lineage).** The adapter this repository ships reaches the fence
through the lifecycle authority, never directly: it asks that
authority to activate the generation beside it and carries the token
it is answered with ([adapter.md](./adapter.md) §Activation and
runtime status); it introspects nothing. The Operator control
service's use of managed
creation is garamsh/garam-agent-operator#211 and #218's, per #1167.
Acceptance with the Operator control service and the adapter is
unverified and tracked in #1157. The rest is the accepted
target of ADR-0077 and ADR-0078, and everything above describes what
runs for a GRN that has not cut over. Product implementation is tracked
in #1157.

- **Desired runtime values and templates leave garam.** The Operator
  control service is their one writable managed source and the cluster
  controller pulls from it; a standalone host keeps its own.
  `Definition` values and `Template` are what move, per GRN, with no
  dual write and no new workload or volume. The GRN, the `Name` and the
  assignment stay here, and the console still copies a template once
  (ADR-0077).
- **A managed agent is created by request, not by claim.** The control
  service presents a member's `agent:create` reference and the request
  identifier it binds; garam rechecks the reference from current state
  before it creates or answers an earlier creation, and reads no
  lifecycle authority to create. The identity, its first assignment to
  the bound controller, its execution and the creation record are one
  transaction (migration 000030), so a failure leaves no agent, and the
  same reference answers the same GRN and epoch while the agent stays
  where its creation put it. The operator's own route still creates in
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
  nothing. The first activation cuts the GRN over, so the operator's
  certificate route and its definition no longer reach it. The machine
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
  the lineage the renewing certificate is current in.

## Standalone hosts

**garam's half is built; the host's runner is not.** A standalone host
enrolls and renews with `garam standalone prepare`, `enroll` and
`renew` (`internal/cli/standalone_host.go`); what supervises the
runtime, proves the old writer exited and hands the adapter its
activation is #1180's, and the joint message, task and reply
acceptance with Sherlock is unverified (#1168, #1169).

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
  is the agent's memory, outbox or workspace; which account and mount
  reach each is #1180's.
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
