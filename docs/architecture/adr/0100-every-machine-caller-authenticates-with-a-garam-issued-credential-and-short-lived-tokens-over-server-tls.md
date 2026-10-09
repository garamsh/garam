# ADR 0100: Every machine caller authenticates with a Garam-issued credential and short-lived tokens over server TLS

> Status: proposed
> Date: 2026-10-09

Drafted for #1251, which reserved this number. The direction is the
owner's, given on 2026-10-09 in the PM's session and recorded in that
issue; this record states it, justifies it and plans the move. It is
proposed: no status line of another ADR moves and no responsibility
document changes until it is accepted, and the migration plan is
[docs/plans/2026-10-09-machine-identity-migration.md](../../plans/2026-10-09-machine-identity-migration.md).

Facts about garam hold at `a7148f8`. Facts about the Operator hold at
`garamsh/garam-agent-operator@792f1d4fe65af33ddd32356bf014a71cd72f26d8`
(cited `OP:`), and about Sherlock at
`garamsh/sherlock@08c32c227d17acb850858189b3b3cad9a7a7d178` (cited
`SH:`), the head of each repository's `dev` on 2026-10-09.

## Contents
- Context
- Decision
- The control path inside the cluster
- What it supersedes and what survives
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives

## Context

Three kinds of machine caller reach the machine listener today, and
every one of them authenticates with a client certificate whose SAN
URI is its GRN, signed by its organization's certificate authority
([ADR-0014](./0014-agent-authentication.md),
[ADR-0015](./0015-operator-identity.md),
[ADR-0082](./0082-a-standalone-host-runs-its-agent-under-a-supervisor-an-administrator-approves-by-its-keys.md)):
an agent, through the adapter garam ships; an operator, which is both
the in-cluster controller and the hosted control service, each a
registered operator of its own
([operators.md](../operators.md) §Delegated operation authority); and
a standalone host's supervisor. The listener presents a garam server
certificate every caller pins
([ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)).

What that costs is read off the tree rather than estimated.

- **Seven routes and one command issue a certificate.**
  `api/machine.yaml` declares `renewIdentityCertificate`,
  `enrollOperator`, `issueAgentCertificate`, `issueInitialCertificate`,
  `recoverAgentCredential`, `enrollStandaloneAgent` and
  `renewSupervisorCertificate`, and `garam issue-certificate`
  ([ADR-0032](./0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md))
  mints out of band. Two of them generate the private key inside garam
  and answer it (`MachineCertificate` in `api/machine.yaml`, the shape
  `issueAgentCertificate` and `renewIdentityCertificate` share), so
  [ADR-0064](./0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)'s
  "garam never had one" holds for an operator's first certificate and
  not for the renewal after it.
- **Every organization's authority is sealed under one key-encryption
  key** ([ADR-0016](./0016-ca-key-custody.md)), and losing it loses
  every authority with no recovery short of reissuing every
  certificate
  ([certificate-authorities.md](../certificate-authorities.md)).
- **Revocation is three mechanisms.** An operator stops when its
  record is deleted
  ([ADR-0070](./0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)),
  a supervisor when its registration ends (ADR-0082), and an agent when
  its credential lineage is replaced
  ([ADR-0078](./0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)).
- **Two proofs rest on forwarding a certificate.** The controller proof
  and execution introspection take a leaf the hosted operator received
  in its own handshake ([operators.md](../operators.md) §Controller
  proof).
- **The Operator does not meet the model's rotation.** The control
  service loads its own pair once and never renews it
  (`OP:cmd/control/main.go:219-254`). An agent's credential is copied
  into the Pod once, by an init container, so a replaced Secret never
  reaches a running adapter
  (`OP:internal/controller/agent_statefulset.go:633-650`), and no code
  in the Operator renews an agent's certificate. Its renewer runs under
  leader election because only the newest certificate renews
  (`OP:internal/garam/renewer.go:15-22`).

Sherlock is not a party to any of this. It holds no client to garam or
to the control service; its only TLS is an optional client-certificate
check on its own gateway (`SH:internal/config/tls.go:11-50`), which
the Operator does not configure, and the adapter reaches it over
`http://` on loopback
(`OP:internal/controller/agent_statefulset.go:1047`). The adapter is
garam's own `garam adapter` (`internal/cli/delivery.go`).

The owner compared two ways to register an operator the way GitHub's
Actions Runner Controller registers with GitHub: a credential garam
issues, and a key pair the Operator generates with its public key
registered in garam. The owner chose the first, and judged that keeping
mTLS between the Operator and garam raises complexity. Token-
authenticated operators beside certificate-authenticated agents would
be more complex than either alone, so the direction covers every
machine caller.

## Decision

**Every machine caller holds a long-lived credential only where a
person put it, exchanges it for short-lived tokens, and presents a
token over TLS that authenticates the server alone.** The model is the
Actions Runner Controller's:

| ARC | garam |
|---|---|
| A GitHub App key, put in a Secret once | an operator credential garam issues, shown once, placed in the cluster's Secret by a person |
| App key to a one-hour installation token | operator credential to a one-hour operator token |
| The controller requests a just-in-time config per runner | the operator requests a short-lived agent token per agent and hands it to that agent's adapter |
| The runner long-polls GitHub with its token | the adapter long-polls garam with its agent token, as ADR-0013 has it |
| Server TLS only | server TLS only; per-organization authorities and client certificates are retired |

### Credentials

- **An operator credential is 256 random bits garam generates.**
  Registering an operator answers it once, the console shows it once,
  and garam stores its SHA-256 alone, as it stores a session's and an
  enrollment token's. It does not expire. It authenticates one
  operation, the token exchange, and nothing else.
- **Re-issue voids the previous credential at once.** An operator holds
  one credential. Re-issuing replaces the row in the statement that
  answers the new one, and every token minted under the old one goes
  with it. It is authorized on `operator:reissue`, an action of its
  own held by owner and admin: re-issuing withdraws what an operator
  runs on, which is the reason ADR-0070 gave `operator:delete` its own
  action rather than reading it off `operator:register`.
- **A supervisor credential has the same shape.** An administrator
  registers a standalone host's supervisor for one agent and one host
  name on `agent:enroll`, as ADR-0082 has it, and is answered the
  credential once. A replacement is a re-issue at the agent's next
  epoch, and revocation ends the registration and its tokens together.
- **A credential is key material.** It reaches a process as a file its
  owner alone reads, never through the environment (ADR-0015).

### Tokens

- **A token is opaque: 256 random bits, prefixed with its kind** —
  `garam_ot_` for an operator, `garam_st_` for a supervisor,
  `garam_at_` for an agent — so a scanner can recognise one and a
  reader can tell which they hold. **Nothing signs it.** garam generates
  it, stores its SHA-256 and verifies it by reading that row; no
  signing key exists to hold, rotate or lose.
- **The row names what the token stands for**: the subject GRN, its
  kind, its audience, its expiry, and the credential it was exchanged
  under — or, for an agent, the assignment epoch and credential
  lineage it was minted at.
- **An operator or supervisor token lives one hour**, a constant, from
  `POST /tokens` with `Authorization: Garam-Credential <credential>`.
  The caller refreshes it after two thirds of its life.
- **An agent token is minted by the agent's assignee** on
  `POST /agents/{agent}/tokens`, presenting its own token. garam
  answers an operator for an agent assigned to it at the current epoch
  and no other, which is ADR-0015's rule and may not be skipped for a
  fast path; it answers a supervisor for the one agent it holds. For a
  fenced agent the token joins the current lineage: initial issuance
  opens it and recovery replaces it, each under the reference it takes
  today (ADR-0078, ADR-0086). Its lifetime is configuration, one hour
  by default, bounded at both ends as `GARAM_CERTIFICATE_LIFETIME` is,
  and the assignee mints the next once two thirds of it has passed, at
  a point it jitters so a fleet does not refresh at one moment
  (ADR-0015).
- **A token is accepted only by its audience.** A token minted for
  garam is accepted on garam's routes. A token minted for a hosted
  operator's GRN is accepted by garam only on the introspection routes
  that hosted operator calls, and only from it. A certificate could be
  forwarded because it proved nothing without its key; a bearer token
  is the credential itself, so the audience is what keeps a token
  shown to the control service from being used at garam by it.
- **The caller's token travels in `Authorization: Garam-Token
  <token>`.** The values that share that header today move to headers
  of their own: `Garam-Execution`, `Garam-Operation` and
  `Garam-Placement`. Each contract whose transport changes takes a new
  value: `execution-fence.v2`, `operation-authority.v2`,
  `agent-cutover.v2`, `managed-enrollment.v2`,
  `standalone-enrollment.v2` and `agent-execution.v2`.

### Revocation holds because every request reads the row

**Each request reads the token's row joined to the record it rests
on**, in one indexed statement: the operator's registration, the
supervisor's registration, or the agent's assignment and execution.
Deleting an operator removes its credential and tokens by foreign key,
re-issuing removes the tokens of the old credential, a reassignment
refuses every agent token minted at the earlier epoch, and a recovery
refuses every token of the replaced lineage — each from the next
request, on a connection already open too. The token's lifetime is
not what bounds a revoked caller; it bounds a token nobody revoked.

The read was chosen over a lifetime short enough to stand in for it,
on this measurement, taken on 2026-10-09 against PostgreSQL 17.11 in a
container of its own (the image `deploy/local/` pins), with 10 000
operators and 100 000 tokens, one client, prepared statements, ten
seconds a run:

- **The read**, a token's row joined to its operator's record:
  0.100, 0.147 and 0.102 ms average latency over three runs.
- **The same round trip reading no table**, a `SELECT` of a SHA-256:
  0.030, 0.029 and 0.029 ms. The read itself is the difference, 0.07
  to 0.12 ms.
- **The plan** is a nested loop of `Index Scan using
  operator_tokens_pkey` and `Index Only Scan using operators_pkey`,
  cost 16.74, executing in 0.203 ms cold.

pgbench ran inside the database's container over its local socket, so
these numbers carry no network round trip. A deployment adds its own
round trip to each one, and the read can share the transaction a
protected call already opens for its execution fence (ADR-0080).

ADR-0070 measured a full TLS 1.3 handshake on the machine listener at
1.4 to 2.1 ms with one database read beside it. The read is a primary
key lookup like the one ADR-0070 added per connection. A lifetime
short enough to be a revocation bound would have to be minutes, and
every agent would then cost its assignee an exchange every few
minutes, for a bound that is still not immediate.

### Server TLS only

- **The listener stops requesting a client certificate.** It still
  terminates TLS itself and presents the garam server certificate
  ADR-0040 decided, signed by an authority garam does not hold, read
  per handshake. Every caller pins that root; it is shown beside a
  credential when one is issued and answered beside every agent token.
- **The per-organization authorities are retired** once no route
  issues a certificate: `internal/ca`, its sealer, the key-encryption
  key `garam serve machine` reads for them, and `garam
  issue-certificate`. The plan states when.

## The control path inside the cluster

Today the adapter presents the agent's leaf to the control service
over mTLS, and the control service forwards that leaf to garam's
execution introspection ([adapter.md](../adapter.md) §Activation and
runtime status; `OP:internal/execution/session.go:11-22`,
`OP:internal/execution/activation.go:140-175`). That is replaced, and
nothing else in the path moves.

- **The adapter authenticates to the control service with an agent
  token whose audience is that control service.** The assignee mints
  it beside the agent's garam token and hands both to the adapter. The
  adapter still verifies the control service against
  `GARAM_ADAPTER_CONTROL_ROOT_FILE`; the client-certificate settings
  go, and two token files replace them.
- **The control service verifies it by introspection at garam**, as it
  verifies an operation authority today. Wherever it forwards
  `certificatePem` now — execution introspection, activation — it
  forwards that token, and garam resolves it to the GRN, epoch and
  lineage, answering only the audience it names. The controller proof
  takes a controller's token minted for the control service in the
  same way.
- **The placement token stays, beside the agent token.** It answers a
  different question — which placement is current — and has a
  different life: it is long-lived for its placement, and its digest
  is one of the six fields ADR-0084 derives the activation's request
  identifier from. Replacing it with an hourly token would make every
  refresh new intent. It moves to the `Garam-Placement` header.
- **A standalone adapter presents to its supervisor the agent token
  the supervisor obtained for it.** The supervisor compares it, in
  constant time, with the token it handed that generation, over the
  socket and server-only TLS ADR-0085 decided; it asks garam nothing.
- **The adapter reads its token files per request**, as it reads the
  placement token per attempt today, so a refreshed token needs no
  restart. The Operator's copy of the credential into the Pod must
  therefore stay current rather than happen once at start; the plan
  names that change.

## What it supersedes and what survives

Status lines move when this is accepted, not before.

| ADR | Superseded | Survives |
|---|---|---|
| 0012 | nothing; checked only | garam never dials a cluster, the GRN, the assignment epoch, delivery started from the agent's side |
| 0013 | §Consequences' per-organization authority issuing the adapter's certificates | the long-poll, the adapter, every messaging decision |
| 0014 | the client certificate, the per-organization authority that signs it, revocation by lifetime, and the rejection of an opaque bearer credential | a listener of its own terminating TLS itself, no `users` row or membership, the path's agent equal to the authenticated GRN, attachments extending that rule |
| 0015 | mTLS for operators, the out-of-band first certificate, the shared expiry | one mechanism for agents and operators, an operator obtaining an agent's credential only for agents assigned to it, key material as owner-only files, jitter, never-authenticated told from stopped, reachability in provisioning state |
| 0016 | whole, once the authorities are retired | nothing |
| 0032 | whole | nothing |
| 0033 | the claim admitting the operator to a certificate route | the claim, admitting it to the agent-token route instead |
| 0040 | items 1, 4 and 6: client certificates, the issuing authority in the answer, the agent's self-renewal | the garam server certificate from an authority garam does not hold, the pinned root, per-handshake reading on the listener, per-organization server certificates deferred |
| 0045 | "a registration grants nothing; it issues no credential" | the registry, its actions, the supplied identifier, the definition's foreign key |
| 0046 | whole | the refusal of a credential that perpetuates itself: a token cannot mint a token |
| 0064 | the enrollment token, the certificate request, `POST /enrollment`, the identity written from `VerifyConnection` | a secret answered once and stored as a hash, signing kept out of `garam serve api` |
| 0067 | the second enrollment token | its open question is answered: a re-issue replaces and never adds, so no identity has two credential paths |
| 0070 | the record read per connection through a seam of the trust root's shape | the delete, `operator:delete`, the refusal while a definition names the operator, the record as what admits |
| 0078 | a lineage of certificates | the generation fence, delegated authority, recovery for the same GRN, now replacing a lineage of tokens |
| 0082 | the host's key pairs, spending by certificate requests, the supervisor's certificate renewal | a supervisor for one agent, an administrator standing it up, the assignee, its execution routes, replacement and revocation, the secret out of the runtime's reach |
| 0084 | the agent's leaf over mTLS as the transport's authentication, `Garam-Placement` in `Authorization` | the routes, bodies, refusals, the request identifier's derivation, the highest token version, the two modes |
| 0085 | the adapter presenting the agent's certificate on the socket | the three containers, the socket, the generation's serving root, the drain |
| 0086 | the operator's certificate route refusing a switched agent | the stages and the lock, the agent-token route refusing a switched agent's operator instead |
| 0091 | whole: a recovery answers no chain | nothing |

[ADR-0017](./0017-machine-process.md)'s process of its own is not
superseded. Its reason was the key-encryption key, which retiring the
authorities removes; whether the two processes merge is not decided
here.

## What this does not decide

- **Merging `garam serve machine` into `garam serve api`.** Its own
  ADR, on what the separate process still buys.
- **A rate bound on the exchange route.** The same reasoning as
  ADR-0064's enrollment route: guessing 256 bits is not the exposure.
- **The console's copy** for showing and re-issuing a credential.
- **The operator credential's storage in the cluster** beyond the file
  rule: which Secret, and whether an external store fills it, is the
  Operator's.

## Consequences

- **One mechanism for every machine caller**, and one revocation rule:
  the record a token rests on is read on every request.
- **No certificate authority, sealed key or key-encryption key** remains
  for machine identities, once the plan's last step lands. An
  organization's deletion no longer destroys something that cannot be
  made again.
- **A long-lived secret sits in the operator's cluster.** ADR-0014
  rejected exactly that, because such a secret leaks into a log, an
  image layer or an environment dump. It is accepted here with what
  ADR-0014 did not have: a re-issue and a delete that void it at once,
  and an exchange route that is the only place it is presented. Agents
  hold only short-lived tokens.
- **A stolen token is usable until it expires or its record moves.**
  The audience narrows where, and the pinned root keeps it from being
  handed to an impostor. An agent's certificate and key stolen today
  are usable for the rest of the certificate's lifetime, a day by
  default.
- **An agent depends on its assignee to stay current.** An Operator
  down for longer than the last third of an agent token's life stops
  that agent reaching garam, where today a certificate lives a day. The
  lifetime is the knob, and its upper bound is the trade.
- **Every machine request costs one indexed read**, measured above.
- **The Operator owes**: a credential Secret and an exchange loop for
  the manager and for the control service; minting both agent tokens
  per agent and keeping the Pod's copy current; the control service
  accepting tokens and introspecting them; and dropping enrollment,
  renewal and the certificate request paths
  (`OP:internal/garam/enroller.go`, `OP:internal/garam/renewer.go`,
  `OP:internal/desired/issuer.go`).
- **Sherlock owes nothing.** It reads no credential garam issues.
- **The responsibility documents this changes**, paired under rule 3
  in the implementation pull requests the plan names rather than here:
  [machine-identity.md](../machine-identity.md),
  [operators.md](../operators.md), [agents.md](../agents.md),
  [adapter.md](../adapter.md), [api-surface.md](../api-surface.md),
  [deployment-contract.md](../deployment-contract.md),
  [overview.md](../overview.md), [domains.md](../domains.md),
  [organizations.md](../organizations.md),
  [web-app.md](../web-app.md), and
  [certificate-authorities.md](../certificate-authorities.md), which
  is deleted with the authorities (rule 5).

## Signs the decision was wrong

- An operator or supervisor credential is found in a log, an image
  layer or an environment, which is the leak ADR-0014 predicted.
- Agents lose garam during an Operator outage short enough that a day's
  certificate would have carried them, often enough that the lifetime
  bound is raised toward a day.
- The per-request read shows in the machine listener's latency, or a
  route is found reading a token without the record beside it.
- A token minted for a control service is accepted on a garam route
  other than introspection.

## Rejected alternatives

- **Keep mTLS and change only enrolment** — a console credential
  exchanged for the first certificate. It keeps the seven issuing
  routes, the authorities and the key-encryption key, and the
  Operator's missing rotation stays missing; the owner's complexity
  judgement is about exactly what this keeps.
- **An Operator-generated key pair, its public key registered in
  garam**, the Operator signing a short assertion for each exchange.
  No secret would sit at garam or travel to the cluster. The owner
  weighed it against the issued credential and declined it on
  2026-10-09; it adds a registration step carrying a key the person
  generates in the cluster, a second verification path beside the
  token, and clock-sensitive assertions.
- **A mixed model**: tokens for operators, certificates for agents. Two
  mechanisms to secure and revoke, which ADR-0015 refused for the same
  reason, and the authorities stay for agents alone.
- **Signed tokens verified without a read.** It needs a signing key in
  `garam serve machine`, its rotation and a published verification key,
  and revocation still needs the record read per request, so the
  signature buys nothing the read does not.
- **The adapter refreshing its own agent token.** A stolen token would
  then perpetuate itself, which is what ADR-0046's lineage existed to
  bound; the assignee already holds the right to mint and the
  assignment check that limits it.
- **One agent token for garam and the control service.** The control
  service could then act at garam as the agent.
- **A projected ServiceAccount token federated to garam.** ADR-0015
  rejected it for needing issuer federation per cluster, and that still
  holds; it also gives a standalone host nothing.
