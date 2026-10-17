# Machine Identity

Who is calling on the listener a machine identity authenticates on
(ADR-0014, ADR-0015, ADR-0100). Implemented by `internal/machine`.

| Type | Responsibility |
|------|----------------|
| `Identity` | the GRN a verified client certificate names, and the certificate's own serial and dates — which is what a caller asking to replace it is answered by (ADR-0046) |
| `Service` | recovers that GRN, verifies the certificate against the organization's authority, and refuses an identity the organization no longer holds a record of; for a decision, verifies again — the caller's identity against the clock and its record, and a certificate presented on another identity's behalf against that identity. `Prove` is the certificate half alone: the SAN and the chain, establishing neither validity now, nor a record, nor any authority to act |
| `TrustRoot` | supplies the authority a presented certificate has to chain to |
| `Registration` | answers whether the organization still holds its record of the identity a certificate names: an operator's record (ADR-0070) or a supervisor's current registration (ADR-0082) |
| `ServerCertificate` | supplies the certificate the listener presents, asked per handshake so a replaced one needs no restart (ADR-0040) |
| `Tokens` | answers what a token a request presents stands for, read for that request beside the record it rests on (ADR-0100) |
| `Method` | how a request's identity was proven: a certificate the handshake verified, or a token the request presented |

An agent, an operator and a supervisor are the same kind of caller
here and the listener does not branch on which. It asks one question
of each and the branch is in the seam that answers it: an operator is
admitted while its organization still holds the record (ADR-0070), a
supervisor while its registration is current — a revocation or a
replacement ends it (ADR-0082) — and an agent is held without a
read. What the certificate asserts is the
GRN in its SAN URI and nothing beside it: no user row, no session, no
membership, so no foreign key on `users` is touched and no handler
taking a `*user.User` changes. The identity carries the certificate's
own serial and dates as well, which assert nothing and describe which
certificate authenticated — the one question a caller asking to replace
it is answered by (ADR-0046).

The SAN carries exactly one URI and it is parsed whole as a GRN. That
one parse is the whole of the check on the URI: `grn.Parse` splits on
every colon and holds every segment to the character rule
([grn-model.md](./grn-model.md) §identifier), so a URI carrying a
separator that would move a segment — a colon outside the four, a `?`,
a `#` — is not a GRN and names no identity. The rule lives in
`internal/grn` and is pinned there; this boundary calls it rather than
keeping a copy. Which authority the certificate must then chain to is
read off the organization segment of that GRN, so a certificate one
organization's CA signed for another organization's identity is
refused by the trust root rather than by a check. Chaining is not the
whole of that verification either: at the handshake it is
`crypto/x509`'s, which in the same call refuses a certificate outside
its validity period and one not marked for client authentication, so
neither refusal at authentication is code this repository holds. What
is read again after the handshake is below.

The package holds no store and reaches no database: `TrustRoot` is the
seam the composition root satisfies from `internal/ca`, which keeps the
two domains cooperating through `grn.GRN` alone. Verification needs the
authority's certificate and never its key. `Registration` is a seam of
the same shape, satisfied from `internal/operator` for an operator and
from `internal/agent` for a supervisor, and it is what turns an
organization deleting an operator, or revoking a supervisor, into a
certificate that stops authenticating — no list, no responder and no
stored serial, just the record the organization already keeps, read
once per connection after the chain verifies. Both reads are per
connection and a resumed handshake takes them too, because
`Authenticate` runs from
`VerifyConnection` (ADR-0064): a check that ran only on a full
handshake would let a deleted operator keep connecting for as long as
its tickets lasted.

Three kinds of route read them again on every request:

- **The controller proof**
  ([operators.md](./operators.md) §Controller proof) asks `Recheck` of
  the caller's identity — its credential's dates against the clock,
  then its record — and `Verify` of the controller certificate the
  caller forwards, which proves the identity it is presented for by its
  SAN, by its organization's authority, by its validity at the decision
  and by its record. A controller token
  forwarded under `operation-authority.v2` is the operator domain's to
  prove instead. The chain is verified
  at a time inside the certificate's validity and the validity at the
  decision is checked apart from it, so a certificate nothing signed
  and one that has run out are told apart.
- **The execution lifecycle routes** ask `Recheck` of the caller the
  same way, and `Prove` of the agent certificate it forwards. That
  proof establishes only that the certificate names the agent and
  chains to its organization's authority; whether it is valid now and
  in the current lineage is the execution fence's to judge
  (§Generation and lineage), so an expired agent certificate is
  answered as fenced rather than as unproven. An agent token forwarded
  under `execution-fence.v2` is proven the same way by the agent
  domain, and judged by the same fence.
- **An agent's protected calls** are judged by the execution fence on
  the certificate's serial and dates as the handshake read them, so a
  certificate that expired on an open connection is refused there
  (§Generation and lineage).

Every other route relies on listener admission, which reads them once
per connection; an operator's renewal also reads the certificate's
dates, to decide its renewal window ([operators.md](./operators.md)).
`ServerCertificate` is the
other seam, satisfied from `internal/keyfile`, and what it answers with
is a different trust root: garam verifies an identity by its
organization's authority, and an identity verifies garam by the one
root every organization shares (ADR-0040).

`garam serve machine` is where the listener runs — its own process and
its own port (ADR-0017), terminating TLS itself with the pair it holds
as two paths and re-reads per handshake, the private key under the
owner-only rule `internal/keyfile` holds, and the root that signed that
pair beside them for every identity it issues a certificate to. It
terminates TLS itself because a forwarded client-certificate header is
trustworthy only where every deployment strips it at the edge
(ADR-0014). It is not part of the local Docker stack: its key material
is per host, and compose has
no step that generates it and hands a container a file that container
owns. The Kubernetes stacks under `deploy/` have that step — an init
container copies the key material as the UID the listener runs as — so
what compose lacks is the step, not something a container cannot do.

## Asymmetric Operator exchange

**Built at garam; no Operator release exchanges yet.**
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
adds signed client assertions beside the certificate and shared-secret
cohorts §Tokens describes, which remain. `api/machine.yaml`
§exchangeClientAssertion is authoritative for the exchange's wire and
refusals, and §machineBearer for the alias.

- **`exchangeClientAssertion`, `POST /oauth/token`, is the client
  credentials grant authenticated by private_key_jwt under
  `machine-token.v2`.** It reads no client certificate and no
  `Authorization` header — either refuses as `invalid_request` — so a
  caller holds one credential there and falls back to none. Its RS256,
  identity, endpoint audience, lifetime and duplicate-member checks are
  `internal/operator` `ParseClientAssertion` and
  `ClientAssertion.Verify`; the key is looked up by `kid` within the
  `client_id`'s registration alone. Every authentication failure is one
  `invalid_client`.
- **One assertion yields one token on every replica and across
  restarts.** `migrations/000043`'s `operator_assertions` primary key,
  `(operator_grn, jti)`, is spent in the transaction that records the
  token and keeps the jti until `exp` plus the skew. The same
  transaction holds the key's row under the lock a withdrawal's update
  takes and measures the assertion's dates at the database's time, so
  a withdrawal either commits first and the exchange records nothing,
  or waits and deletes the token the exchange recorded. It holds the
  registration's row first, as an issuance does before it withdraws
  or retires a key, so whichever of an exchange and a rotation comes
  second waits for the first to commit, and neither holds a row the
  other is waiting on (#1399). A refused assertion spends no jti.
- **A store write acting for an operator takes that operator's
  registration row first, and a transaction holding several takes them
  in GRN byte order** (`internal/operator` `PostgresStore`).
  - An exchange, by shared secret or by key, holds it `FOR KEY SHARE`
    for the client, and for the audience when the token names one.
  - A re-mint of an enrollment token holds it `FOR KEY SHARE`.
  - An issuance, a withdrawal, a credential re-issue and a deletion
    hold it `FOR UPDATE`.
  - Deleting an organization holds every one of its operators' `FOR
    UPDATE` before its cascade deletes them, by the trigger on `orgs`
    `migrations/000047` defines, after its agents' execution rows
    ([agents.md](./agents.md) §Tokens in place of certificates states
    why). An audience
    is an operator of the client's organization, so the registrations
    an exchange holds are among them.

  The `FOR KEY SHARE` holders hold it together. Every other pair
  acting for one operator waits on the registration for the first to
  commit, before either touches a credential, key, token or enrollment
  row, and none waits on one registration while holding a later one
  (#1418, #1424). One write reaches past the operator it acts for: a
  deletion also deletes other operators' tokens that name it as
  audience, holding only its own registration, and against those
  operators' writers the order is the `token_hash` order below.

  A writer taking it `FOR UPDATE` waits for every exchange in flight,
  and Postgres does not queue it ahead of later shared holders. What
  keeps that wait finite is the exchanges' per-process bound below,
  which counts both forms, so a leaked credential cannot hold off the
  re-issue that answers it.
- **A transaction waits on operator tokens in one statement at most,
  which takes them in `token_hash` order and holds none before it but a
  token the transaction just wrote** (#1424). An operator's deletion
  then waits on the agent tokens naming it too, holding its operator
  tokens ([agents.md](./agents.md) §Tokens in place of certificates);
  that cannot cycle, since no holder of an agent token waits on an
  operator token.
  - A withdrawal's and an immediate rotation's deletion of a key's
    tokens, a re-issue's of the credential's, and a deletion's of every
    token the operator holds or is the audience of are each that
    statement. The re-issue and the deletion run theirs before the
    credential or the registration goes, so the cascades of
    `migrations/000038` and `000041` find none; `000046` indexes
    `audience_grn` for the deletion's.
  - An exchange records its token, trims its issuance to the newest
    five live ones, and then sweeps the operator's expired tokens. The
    trim is that statement; the sweep skips a token another
    transaction holds, which that transaction is deleting or a later
    sweep takes, so it never waits.
  - An organization's deletion takes its operators' tokens in its
    cascade's order and waits on none: every other writer of them
    holds a registration the deletion already holds.
- **Minting an enrollment token sweeps other operators' expired ones
  without their registrations, and skips any another transaction
  holds** (#1424). A re-mint waits on its operator's registration,
  held first, and on that operator's enrollment row, and on no other
  operator's row; registering mints for an operator no other
  transaction can see yet.
- **Token reads join the live key.** A token exchanged under a key is
  resolved with that key unwithdrawn and inside any rollover, so a
  withdrawal, an immediate replacement or a passed rollover refuses the
  next request on a connection already open
  ([operators.md](./operators.md) §Issued PEM identity). A read that
  cannot be made refuses rather than extending a cached proof.
- **`Authorization: Bearer <token>` is the same token as
  `Garam-Token`.** `machine.PresentedToken` is the one reading of that
  header the listener and the routes share, so the alias takes the
  same resolver, route set, v2 contract values and header placement;
  `api/machine.yaml` lists `machineBearer` beside every `machineToken`
  and a test holds the two lists equal. The scheme matches
  case-insensitively, and a `Bearer` value with no token after it is
  refused rather than read as no token at all.
- **One process answers at most 60 exchanges per client address and 10
  authenticated ones per operator per minute** (`internal/operator`
  `ExchangesPerSource`, `ExchangesPerClient`), 429 with `Retry-After`
  past either. The bounds count the shared-secret and the
  private_key_jwt exchange together, since each holds the operator's
  registration. The address's count is taken before anything is read,
  so a flood from one address holds back no other; the operator's
  starts once its key verified or its credential resolved, so a
  stranger knowing the GRN cannot lock the operator out. The listener
  terminates TLS itself, so the address is the peer's, and clients
  behind one NAT share it. An unconfigured token endpoint answers 503
  and issues nothing.
- **What tells the cohorts apart.**
  `garam_machine_token_exchanges_total` counts exchanges by method —
  operator shared secret, supervisor shared secret, operator
  private_key_jwt — and outcome, which `garam_machine_requests_total`
  cannot, since both cohorts present a token afterwards. An idle
  operator shows in no metric; its keys' `last_exchanged_at` and its
  tokens' issuance, credential or key, are what an inventory reads.
  Neither carries a key, assertion or token.
- The agent's API token, its execution fence and Sherlock's message
  issuer verification remain distinct. Common enrollment admits
  managed placement alone,
  [agents.md](./agents.md) §Common managed and direct enrollment;
  message verification is [messaging.md](./messaging.md).

## Tokens

**Built for an operator, an agent and a standalone host's
supervisor.** ADR-0100 moves every machine caller from a client
certificate to a short-lived token it obtains with a credential garam
issued, and `docs/plans/2026-10-09-machine-identity-migration.md` is
the order it lands in. What runs now is its step G1, the four pull
requests G1a to G1d, and G2, the adapter presenting an agent token
from a file and a standalone host running on its supervisor credential
([adapter.md](./adapter.md) §Token mode,
[agents.md](./agents.md) §Standalone hosts); a certificate still
authenticates beside a token until G3.

- **A request may present a token instead of a certificate**, as
  `Authorization: Garam-Token <token>` or, the same token,
  `Authorization: Bearer <token>` (ADR-0102). The listener asks `Tokens`
  for that request, and the identity it answers carries `MethodToken`,
  the GRN and the token's dates. An agent's token carries a serial too —
  `t` and hex, which no certificate's decimal serial equals — and the
  execution fence reads it as it reads a certificate's: current while
  the token is inside its dates, minted at the agent's present assignee
  and epoch, and in the lineage the execution holds now. An operator's
  token carries none, so a route reading a credential refuses it, and no
  token reaches a route that renews a certificate.
- **The read is per request, not per connection.** A value that is not a
  token's shape is refused before it is read, and a refusal is logged at
  debug: it is the caller's to fix, and only a read that could not be
  made is the listener's anomaly. The composition root satisfies
  `Tokens` from the domain the token's prefix names. An operator's read
  joins the token's row to the registration, so deleting the operator or
  re-issuing its credential refuses the next request on a connection
  already open; an agent's joins the agent's assignment and execution,
  so a reassignment or a recovery does. A token minted for another
  audience — a hosted operator it is to be shown to — answers as no
  token here; that operator forwards it in a request body instead,
  for the lifecycle decision it is shown for
  ([operators.md](./operators.md) §Controller proof,
  [agents.md](./agents.md) §Tokens in place of certificates).
- **A token or a certificate, never both.** A request presenting a
  token over a connection whose certificate authenticated carries no
  identity, and the reason travels with it, as it does for a token
  that proved nothing, so the handler's refusal says why. So does a
  request carrying more than one `Authorization` header while any of
  them presents a token.
- **A token takes its contract's v2 value.** Each contract whose
  transport changes is served to a token under its v2 value and to a
  certificate under v1, and a request whose value and credential
  disagree is refused with 400 naming the value its credential takes;
  `internal/machineapi/contract.go` holds the pairs. The values that
  share `Authorization` beside a certificate — an execution token, an
  operation authority or reference — travel in a header of their own
  beside a token, `Garam-Execution` and `Garam-Operation`.
- **A token reaches the operations `api/machine.yaml` lists
  `machineToken` on, and no other.** `ConfineTokens` in
  `internal/machineapi/token.go` is where that is decided, as
  `ConfineSupervisors` decides a supervisor's reach, and a test holds
  its route set against that file. A route that reads the certificate
  a connection presented — a renewal, the execution fence — refuses a
  token's identity by itself as well, so that refusal does not rest on
  the route set alone.

## Generation and lineage

**garam's half is built, and so is the adapter's.** A fenced adapter
carries the execution token its lifecycle authority obtained
([adapter.md](./adapter.md) §Activation and runtime status). A
standalone host's supervisor serves that adapter-facing contract
(ADR-0085), and so does the Operator control service
(`garamsh/garam-agent-operator`
`ad444ede20c83992701fd563e7a0c916827979a4`, `internal/execution`).
Acceptance with each, joined to garam and Sherlock, is recorded as
verified on #1157 and #1180; #1157 left scenario 5's managed move to
`garamsh/garam-agent-operator#296`. Until a
GRN's first activation it is in the legacy cohort: its calls carry no
token, and the certificate check below still applies to each of them.
A legacy GRN's first activation waits for its cutover's switch
([agents.md](./agents.md) §Separated execution).
An agent created managed is never in it: it is fenced from birth
([agents.md](./agents.md) §Separated execution).

ADR-0078 adds two questions an agent's state-changing call answers per
request rather than per connection, so an open connection loses them
the moment they change:

- **Is this the active generation?** Once a GRN is fenced, the call
  carries `Authorization: Garam-Execution <token>` from the activation
  of its current generation. A missing token is refused, and so is one
  an activation, a deactivation, a recovery or a reassignment ended. A
  token offered for a legacy GRN is refused rather than read as a
  cutover: the mode is the server's (ADR-0080).
- **Is this certificate current?** It must be inside its validity by
  the database's clock and, once a recovery or a managed creation has
  started a lineage, in that lineage. The certificate is the one the
  connection's handshake verified; nothing in a body names it.

Claiming, renewing and completing a message, posting one, reporting
runtime state, forwarding a turn event and renewing the agent's own
certificate each ask both inside the transaction of their own write,
so the answer holds when that write commits (ADR-0080). Reading a sent
message's state writes nothing; it asks both in the transaction of its
read, so a refused caller reads nothing. `getIdentity` asks neither.
The routes and their schemes are `api/machine.yaml`'s, under the
`executionToken` scheme each declares.

The hosted control service is a lifecycle caller. It activates,
deactivates, inspects and recovers an agent's execution on the routes
tagged `execution` in `api/machine.yaml`, which is authoritative for
their wire and their `execution-fence.v1` and `.v2` contract values,
and cuts a legacy agent over on the routes tagged `cutover`,
authoritative for theirs and `agent-cutover.v1` and `.v2`. A
standalone host's supervisor is the other: on the same routes it
activates, deactivates and inspects the one agent it is registered
for and holds, and recovers nothing; its certificate reaches no
route but those, its own renewal and its identity
([agents.md](./agents.md) §Standalone hosts). The hosted control
service alone creates a managed agent and obtains its first
certificate, on the routes tagged `enrollment`, authoritative for
theirs and `managed-enrollment.v1` and `.v2`, and prepares and admits
one under `agent-enrollment.v1` on those tagged `agent-enrollment`,
which take its token alone.
Each request rereads the caller's credential and registration; each
but creation and admission rereads its grant, or a supervisor's
registration and assignment, and current policy too, and those reread
the member's `agent:create` reference instead. An agent's own
certificate and a controller's are refused.
Activation, deactivation, recovery, creation, admission and initial
issuance are each bounded by a ten-second server deadline over both
their authority reads and their write, and past it answer unavailable
without saying whether the write committed (ADR-0080). Introspection
answers a forwarded credential's standing and a generation's from one
snapshot; a 200 is a status answer, and only `credential` and
`generation` both `current`, with the expected binding, is positive. The
credential's GRN still answers who is calling; these answer whether that
caller is still the one allowed to act.
