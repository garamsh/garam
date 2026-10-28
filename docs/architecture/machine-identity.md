# Machine Identity

Who is calling on the listener a machine identity authenticates on
(ADR-0100), what an agent presents to the browser-facing listener
(ADR-0123), how a first-party service acts in an organization
(ADR-0129), and the evidence of how each caller authenticated
(ADR-0109). Implemented by `platform/internal/machine`.

Every machine caller authenticates by a token it presents, and the
listener asks for no client certificate
([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)):
garam signs no certificate for a machine caller, and the rows of
`certificate_authorities` an earlier release left stay unread. An
agent's generation has no legacy cohort: every agent's protected call
waits for an activation (ADR-0118). An operator's shared-secret
credential, its exchange and its enrollment token are gone
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md));
the supervisor's shared-secret exchange and `machine-token.v1` stay.

| Type | Responsibility |
|------|----------------|
| `Identity` | the GRN a live token names, and the token's own serial and dates — which is what the execution fence judges an agent's request by (ADR-0080) |
| `Recheck` | reads again, for one decision, what admitted an identity: its token's dates against the clock, and the organization's record of it |
| `Registration` | answers whether the organization still holds its record of an identity: an operator's record (ADR-0070) or a supervisor's current registration (ADR-0082) |
| `ServerCertificate` | supplies the certificate the listener presents, asked per handshake so a replaced one needs no restart (ADR-0040) |
| `Tokens` | answers what a token a request presents stands for, read for that request beside the record it rests on (ADR-0100) |
| `Evidence` | notes each caller's authentication by path, at most once a minute per caller and path per process, and never fails a request (ADR-0109) |
| `Recorder` | keeps that evidence; satisfied by `platform/internal/machine/postgres` |

An agent, an operator and a supervisor are the same kind of caller
here and the listener does not branch on which. It asks one question
of each and the branch is in the seam that answers it: an operator is
admitted while its organization still holds the record (ADR-0070), a
supervisor while its registration is current — a revocation or a
replacement ends it (ADR-0082) — and an agent is admitted by its
assignment and its token's lineage, which its token's read already
joins. What the token asserts is the GRN it was issued to and nothing
beside it: no user row, no session, no membership, so no foreign key on
`users` is touched and no handler taking a `*user.User` changes. The
identity carries the token's own serial and dates as well, which
assert nothing and describe which token authenticated — an agent
token's serial is what the execution fence judges its request by
(§Generation and lineage).

The package holds no store and reaches no database; the one store
beside it, `platform/internal/machine/postgres`, keeps the evidence
§Authentication evidence describes behind `Recorder`. `Tokens` and
`Registration` are seams the composition root satisfies
(`platform/internal/cli/machine.go`): `Tokens` from the domain a token's
prefix names, and `Registration` from `platform/internal/operator` for
an operator and from `platform/internal/agent` for a supervisor. A
token's read joins the record it rests on, so an organization deleting
an operator, or revoking a supervisor, refuses the next request on a
connection already open (§Tokens) — no list, no responder, just the
record the organization already keeps.

Three kinds of route read them again for their decision:

- **The controller proof**
  ([operators.md](./operators.md) §Controller proof) asks `Recheck` of
  the caller's identity — its token's dates against the clock, then its
  record — and the operator domain proves the controller token the
  caller forwards under `operation-authority.v2`.
- **The execution lifecycle routes** ask `Recheck` of the caller the
  same way, and the agent domain proves the agent token the caller
  forwards under `execution-fence.v2`: that it names the agent and was
  minted to be shown to the caller, or by a supervisor caller for
  garam. Whether it is valid now and in the current lineage is the
  execution fence's to judge (§Generation and lineage), so an expired or
  superseded agent token is answered as fenced rather than as unproven.
- **An agent's protected calls** are judged by the execution fence on
  the token's serial and dates as the request's read answered them
  (§Generation and lineage).

Every other route relies on listener admission, which reads them on
every request. `ServerCertificate` is the other seam, satisfied from
`platform/internal/keyfile`, and what it answers with is the certificate
every machine caller verifies garam by: it chains to the one root every
organization shares (ADR-0040), which garam answers as `serverRootPem`
beside every agent token it mints and every supervisor credential it
issues.

`garam serve machine` is where the listener runs — its own process and
its own port (ADR-0017), terminating TLS itself with the pair it holds
as two paths and re-reads per handshake, the private key under the
owner-only rule `platform/internal/keyfile` holds, and the root that
signed that pair beside them. It terminates TLS itself, so the
certificate a caller pins that root against is the one garam presents
(ADR-0040). It is not part of the local Docker stack: its key material
is per host, and compose has no step that generates it and hands a
container a file that container owns. The Kubernetes stacks under
`deploy/` have that step — an init container copies the key material as
the UID the listener runs as — so what compose lacks is the step, not
something a container cannot do.

## Asymmetric Operator exchange

**Built at garam; no Operator release exchanges yet.**
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)'s
signed client assertions are how an operator obtains a token, the
shared-secret exchange beside them being gone (ADR-0117).
`platform/api/machine.yaml` §exchangeClientAssertion is authoritative
for the exchange's wire and refusals, and §machineBearer for the alias.

- **`exchangeClientAssertion`, `POST /oauth/token`, is the client
  credentials grant authenticated by private_key_jwt under
  `machine-token.v2`.** It reads no `Authorization` header — one
  refuses as `invalid_request` — so a caller holds one credential there
  and falls back to none. Its RS256,
  identity, endpoint audience, lifetime and duplicate-member checks are
  `platform/internal/operator` `ParseClientAssertion` and
  `ClientAssertion.Verify`; the key is looked up by `kid` within the
  `client_id`'s registration alone. Every authentication failure is one
  `invalid_client`.
- **One assertion yields one token on every replica and across
  restarts.** `platform/migrations/000043`'s `operator_assertions`
  primary key, `(operator_grn, jti)`, is spent in the transaction that
  records the token and keeps the jti until `exp` plus the skew. The
  same transaction holds the key's row under the lock a withdrawal's
  update takes and measures the assertion's dates at the database's
  time, so a withdrawal either commits first and the exchange records
  nothing, or waits and deletes the token the exchange recorded. It
  holds the registration's row first, as an issuance does before it
  withdraws or retires a key, so whichever of an exchange and a rotation
  comes second waits for the first to commit, and neither holds a row
  the other is waiting on (#1399). A refused assertion spends no jti.
- **A store write acting for an operator takes that operator's
  registration row first, and a transaction holding several takes them
  in GRN byte order** (`platform/internal/operator/postgres` `Store`).
  - An exchange holds it `FOR KEY SHARE` for the client, and for the
    audience when the token names one.
  - An issuance, a withdrawal and a deletion hold it `FOR UPDATE`.
  - Deleting an organization holds every one of its operators' `FOR
    UPDATE` before its cascade deletes them, by the trigger on `orgs`
    `platform/migrations/000047` defines, after its agents' execution
    rows ([agents.md](./agents.md) §Tokens in place of certificates
    states why). An audience is an operator of the client's
    organization, so the registrations an exchange holds are among them.

  The `FOR KEY SHARE` holders hold it together. Every other pair acting
  for one operator waits on the registration for the first to commit,
  before either touches a key or a token, and none waits on one
  registration while holding a later one (#1418, #1424). One write
  reaches past the operator it acts for: a deletion also deletes other
  operators' tokens that name it as audience, holding only its own
  registration, and against those operators' writers the order is the
  `token_hash` order below.

  A writer taking it `FOR UPDATE` waits for every exchange in flight,
  and Postgres does not queue it ahead of later shared holders. What
  keeps that wait finite is the exchanges' per-process bound below, so
  a leaked key cannot hold off the withdrawal that answers it.
- **A transaction waits on operator tokens in one statement at most,
  which takes them in `token_hash` order and holds none before it but a
  token the transaction just wrote** (#1424). An operator's deletion
  then waits on the agent tokens naming it too, holding its operator
  tokens ([agents.md](./agents.md) §Tokens in place of certificates);
  that cannot cycle, since no holder of an agent token waits on an
  operator token.
  - A withdrawal's and an immediate rotation's deletion of a key's
    tokens, and a deletion's of every token the operator holds or is
    the audience of, are each that statement. The deletion runs its
    before the registration goes, so the cascades of
    `platform/migrations/000043` and `000041` find none; `000046`
    indexes `audience_grn` for the deletion's.
  - An exchange records its token, trims its key's tokens to the newest
    five live ones, and then sweeps the operator's expired tokens. The
    trim is that statement; the sweep skips a token another transaction
    holds, which that transaction is deleting or a later sweep takes, so
    it never waits.
  - An organization's deletion takes its operators' tokens in its
    cascade's order and waits on none: every other writer of them
    holds a registration the deletion already holds.
- **Token reads join the live key.** A token is resolved with the key
  it was exchanged under unwithdrawn and inside any rollover, so a
  withdrawal, an immediate replacement or a passed rollover refuses the
  next request on a connection already open
  ([operators.md](./operators.md) §Issued PEM identity). A read that
  cannot be made refuses rather than extending a cached proof.
- **`Authorization: Bearer <token>` is the same token as
  `Garam-Token`.** `machine.PresentedToken` is the one reading of that
  header the listener and the routes share, so the alias takes the
  same resolver, operations, contract values and header placement;
  `platform/api/machine.yaml` lists `machineBearer` beside every
  `machineToken` and a test holds the two lists equal. The scheme
  matches case-insensitively, and a `Bearer` value with no token after
  it is refused rather than read as no token at all.
- **One process answers at most 60 exchanges per client address and 10
  authenticated ones per operator per minute**
  (`platform/internal/operator` `ExchangesPerSource`,
  `ExchangesPerClient`), 429 with `Retry-After` past either. Each
  exchange holds the operator's registration, which is what the bound
  keeps a writer from waiting on for long. The address's count is taken
  before anything is read, so a flood from one address holds back no
  other; the operator's starts once its key verified, so a stranger
  knowing the GRN cannot lock the operator out. The listener terminates
  TLS itself, so the address is the peer's, and clients behind one NAT
  share it. An unconfigured token endpoint answers 503 and issues
  nothing.
- **What tells the cohorts apart.**
  `garam_machine_token_exchanges_total` counts exchanges by method —
  supervisor shared secret, operator private_key_jwt, or a credential
  neither recognises — and outcome, which `garam_machine_requests_total`
  cannot, since both cohorts present a token afterwards. An idle
  operator shows in no metric; §Authentication evidence is what an
  inventory reads. Neither carries a key, assertion or token.
- The agent's API token, its execution fence and Sherlock's message
  issuer verification remain distinct. `agent-enrollment.v1` admits
  managed placement alone, and a standalone approval admits direct
  placement,
  [agents.md](./agents.md) §Common managed and direct enrollment;
  message verification is [messaging.md](./messaging.md).

## Tokens

**Built for an operator, an agent and a standalone host's
supervisor, and the one way a machine caller authenticates**
(ADR-0100, ADR-0118). Each presents a short-lived token it obtained
with a credential garam issued: an operator exchanges an assertion its
key signed (§Asymmetric Operator exchange), a supervisor its
credential, and an agent holds the tokens its assignee mints for it
([adapter.md](./adapter.md) §Token mode,
[agents.md](./agents.md) §Standalone hosts).

- **A request presents a token**, as `Authorization: Garam-Token
  <token>` or, the same token, `Authorization: Bearer <token>`
  (ADR-0102). The listener asks `Tokens` for that request, and the
  identity it answers carries the GRN and the token's dates. An agent's
  token carries a serial too — `t` and hex — and the execution fence
  reads it: current while the token is inside its dates, minted at the
  agent's present assignee and epoch, and in the lineage the execution
  holds now. An operator's token carries none, so a route reading a
  credential's serial refuses it.
- **The read is per request, not per connection.** A value that is not a
  token's shape is refused before it is read, and a refusal is logged at
  debug: it is the caller's to fix, and only a read that could not be
  made is the listener's anomaly. The composition root satisfies
  `Tokens` from the domain the token's prefix names. An operator's read
  joins the token's row to the registration and its key, so deleting the
  operator or withdrawing the key refuses the next request on a
  connection already open; an agent's joins the agent's assignment and
  execution, so a reassignment or a recovery does. A token minted for
  another audience — a hosted operator it is to be shown to — answers as
  no token here; that operator forwards it in a request body instead,
  for the lifecycle decision it is shown for
  ([operators.md](./operators.md) §Controller proof,
  [agents.md](./agents.md) §Tokens in place of certificates).
- **The read is bounded.** It runs under `machine.ResolveDeadline`, the
  ten seconds the lifecycle routes hold their own reads to. A read the
  deadline ends — a store waiting on a lock, say — is answered
  unavailable rather than held until the store lets go (#1613).
- **One `Authorization` header.** A request carrying more than one
  while any of them presents a token carries no identity, and the reason
  travels with it, as it does for a token that proved nothing, so the
  handler's refusal says why.
- **Each contract has one value.**
  `platform/internal/machineapi/contract.go` holds the ones the listener
  serves to a token — `operation-authority.v2`, `execution-fence.v2`,
  `managed-enrollment.v2` and `durable-delivery.v2` — and a request
  under another value is refused with 400 naming the one served. A
  standalone host spends its approval under `standalone-enrollment.v2`,
  the supervisor's exchange and an agent token's mint take
  `machine-token.v1` and the client assertion exchange
  `machine-token.v2`, and an approval's admission takes
  `agent-enrollment.v1`. The values that travel beside the caller's
  token — an execution token, an operation authority or reference — take
  headers of their own, `Garam-Execution` and `Garam-Operation`.
- **A token reaches the operations `platform/api/machine.yaml` lists
  `machineToken` on.** Every operation lists it but the four that take
  no token — a standalone enrollment, the two exchanges and the
  published message signing keys — and a test in
  `platform/internal/machineapi` holds the specification to that. What a
  caller may do there is its identity's: a route reading a credential's
  serial refuses an operator's token, and `ConfineSupervisors` keeps a
  supervisor to its identity, its lifecycle routes and its agent's mint
  ([agents.md](./agents.md) §Standalone hosts).

## Browser tokens

**Unbuilt.**
[ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
lets an agent call the browser-facing listener as itself; today that
listener admits a session alone. What an agent will be authorized to
do there is [authz.md](./authz.md) §An agent on the browser-facing
listener.

- **An agent token whose audience is the browser API.** The agent's
  assignee mints it on `mintAgentToken` beside the agent's other
  tokens. A token of one audience is accepted by that audience alone:
  a browser token reaches no machine operation, and the token the
  adapter holds reaches no browser operation.
- **Presented as `Authorization: Bearer <token>`.** Its shape tells it
  from a session's token, and a value of either shape is looked up as
  that shape only.
- **Read on every request, beside the record it rests on**, as §Tokens
  reads an agent's token here: a reassignment, a recovery, deleting
  the operator holding the agent or deleting its organization refuses
  it from the next request. Its lifetime is the agent token's, and it
  carries no execution token.
- **Agents alone.** The mechanism is a machine token with the browser
  audience, resolved to a subject GRN; ADR-0123 admits agents to it,
  and an operator's or a supervisor's token takes no browser audience.
- **Its mint is a credential event of its own kind** (§Credential
  events), with the agent's GRN as subject.

## cowork's principal

**Unbuilt (#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
adds a fourth kind of machine caller beside an operator, an agent and
a supervisor. Today no service principal exists and cowork reaches no
queue.

- **One per organization.** `grn:<org>:default:service:cowork`, a
  record the platform writes with the organization and a migration
  writes for each existing one. No route creates one, and the
  organization's deletion takes it by cascade.
- **One deployment key for every organization.** cowork holds the
  private half in an owner-only file. The listener is configured with
  the public JWKs of each first-party service, read per exchange, at
  most two per service for a rotation; nothing on either listener
  issues or withdraws them.
- **The exchange is `exchangeClientAssertion`** under
  `machine-token.v2`, with the organization's principal GRN as
  `client_id`, `iss` and `sub`, and the key found by `kid` among the
  service's configured keys (§Asymmetric Operator exchange holds the
  rest of the profile). The token lives one hour and carries its own
  prefix.
- **Read on every request** joined to the principal's record and its
  incarnation, with the key still configured, so a revocation, a
  removed key or the organization's deletion refuses it from the next
  request.
- **The machine listener alone.** Its token reaches `postMessage` to
  an agent of its organization, `getMessageStatus` for a message it
  sent and `getIdentity`, and nothing else; it takes no browser
  audience (§Browser tokens).
- **Revoked and re-issued by a server command**, never by an
  organization. A re-issued principal revives no earlier token.
- **Recorded under its organization.** Its issuances and refusals are
  credential events and its exchanges authentication evidence, with
  the principal as subject.

## Authentication evidence

**Built**
([ADR-0109](./adr/0109-each-machine-callers-last-authentication-by-each-path-is-kept-in-postgres-for-the-g3-gate.md)):
the record of how each caller has authenticated by each path, which
ADR-0118 read before the certificate path went. This section covers
what records it.

- **One row per caller and path** in `machine_authentication_evidence`
  (`platform/migrations/000052`). The paths written are `token`,
  `exchange_shared_secret`, `exchange_private_key_jwt`, `agent_mint` and
  `audience_token`, and `machine.Path` names them; a `certificate` row
  an earlier release recorded stays, and the inventory reads it. A row
  holds when its
  path was first and last seen. For an issuance it also holds when the
  last recorded token expires and the last refresh, meaning an issuance
  made while that token was still accepted. No column holds a credential
  or any part of one.
- **Recorded where each path is decided.** `identify` notes the
  token path of every request it hands an identity. The
  exchange and mint handlers report what they issued through
  `machine.ReportIssuance`, and `recordIssuances` in
  `platform/internal/cli/machine_token.go` records it under the path the
  route and credential prefix name. A refused issuance reports nothing.
- **Best effort and coalesced.** One write per caller, path and process
  a minute, bounded at a second. A failed write is logged and the
  request goes on.
- **Kept until the tenant goes.** Rows cascade from the organization,
  and a deleted caller's rows stay as the evidence that it stopped.
- **A restart is not visible here.** It stays with joint acceptance
  (#1495).
- **The read is `platform/internal/machine/postgres/inventory.sql`**,
  run with `psql -f`. It is authoritative for the cohort each caller is
  counted in and for what an idle caller looks like: a registered row
  with every path empty.

## Credential events

**Built**
([ADR-0114](./adr/0114-every-machine-credential-issuance-renewal-and-refusal-is-an-audit-event-read-on-the-subjects-own-read.md)).
Every issuance and refusal of a supervisor credential or an agent token
is one row of `audit_credential_events` (`platform/migrations/000054`;
`000056` dropped the operator credential and enrollment token kinds with
them), held by the audit domain ([audit.md](./audit.md)); a row an
earlier release wrote for an operator or a supervisor certificate stays
and reads as it was. Unlike the evidence above, it is a record and not a
sampling: one row per answer, none coalesced.

- **What a row says.** The subject, the credential, `issue` or `renew`,
  issued or refused, whether an issued answer replayed an earlier
  identical one, a refusal's error kind, and for a token when it was
  minted and until when it is accepted. No column holds a key, token,
  credential or certificate.
- **Where it is written.** `EnrollStandaloneAgent` and
  `MintAgentToken` on this listener, after the outcome and before the
  answer, through the `CredentialAudit` port it declares
  (`WithCredentialAudit`). The browser-facing listener issues no
  machine credential and records none. A
  write that fails is logged with the event's fields and the answer
  stands.
- **What it does not record.** A refusal no subject is attributable
  to — an enrollment whose bootstrap secret did not resolve —
  and a refusal of the member asking. Token exchanges are not recorded
  yet.
- **The read is `listCredentialEvents`** on the browser-facing
  listener, one subject at a time, newest first, on that subject's own
  read ([api-surface.md](./api-surface.md) §What decides the rest).

## Generation and lineage

**garam's half is built, and so is the adapter's.** The adapter
carries the execution token its lifecycle authority obtained
([adapter.md](./adapter.md) §Activation and runtime status). A
standalone host's supervisor serves that adapter-facing contract
(ADR-0085), and so does the Operator control service
(`garamsh/garam-agent-operator`
`ad444ede20c83992701fd563e7a0c916827979a4`, `internal/execution`).
Acceptance with each, joined to garam and Sherlock, is recorded as
verified on #1157 and #1180; #1157 left scenario 5's managed move to
`garamsh/garam-agent-operator#296`. Until a
GRN's first activation its protected calls are refused, since it holds
no token for them to carry (ADR-0118).

ADR-0078 adds two questions an agent's state-changing call answers per
request rather than per connection, so an open connection loses them
the moment they change:

- **Is this the active generation?** The call carries the token of the
  activation of its current generation, in `Garam-Execution` beside its
  agent token. A missing token is refused, and so is one an
  activation, a deactivation, a recovery or a reassignment ended, or
  one the agent was never activated with (ADR-0080).
- **Is this agent token current?** It must be inside its dates by the
  database's clock, minted at the agent's present assignee and epoch,
  and in the lineage the execution holds now. The token is the one the
  request's read resolved; nothing in a body names it.

Claiming, renewing and completing a message, posting one, reporting
runtime state and forwarding a turn event each ask both inside the
transaction of their own write, so the answer holds when that write
commits (ADR-0080). Reading a sent
message's state writes nothing; it asks both in the transaction of its
read, so a refused caller reads nothing. `getIdentity` asks neither.
The routes and their schemes are `platform/api/machine.yaml`'s, under
the `executionHeader` scheme each declares.

The hosted control service is a lifecycle caller. It activates,
deactivates, inspects and recovers an agent's execution on the routes
tagged `execution` in `platform/api/machine.yaml`, which is
authoritative for their wire and their `execution-fence.v2` contract
value. A standalone host's supervisor is the other: on the same routes
it activates, deactivates and inspects the one agent it is registered
for and holds, and recovers nothing; its token reaches no route but
those, its identity and its agent's mint ([agents.md](./agents.md)
§Standalone hosts). The hosted control service alone creates a managed
agent, on the route tagged `enrollment`, authoritative for its wire and
`managed-enrollment.v2`, and prepares and admits one under
`agent-enrollment.v1` on those tagged `agent-enrollment`, which take its
token alone. Each request rereads the caller's token and registration;
each but creation and admission rereads its grant, or a supervisor's
registration and assignment, and current policy too, and those reread
the member's `agent:create` reference instead, or for a replacement the
`agent:replace` reference whose grant must cover both the controller the
agent is on and the one it moves to (ADR-0107). An agent's own token and
a controller's are refused. Activation, deactivation, recovery, creation
and admission are each bounded by a ten-second server deadline over both
their authority reads and their write, and past it answer unavailable
without saying whether the write committed (ADR-0080). Introspection
answers a forwarded token's standing and a generation's from one
snapshot; a 200 is a status answer, and only `credential` and
`generation` both `current`, with the expected binding, is positive. The
credential's GRN still answers who is calling; these answer whether that
caller is still the one allowed to act.
