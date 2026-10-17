# Operators

What an organization records about an operator, where that operator
stands in obtaining a certificate, and what it authenticates with: the
record that the organization has it and that its certificate
authenticates on (ADR-0045, ADR-0070), the one-time token registering
it mints and a member mints again where it has not enrolled (ADR-0064,
ADR-0067), which certificate garam last issued it, together with
what it takes to replace that with the next (ADR-0046), the
authority an organization delegates to a hosted operator and hands it
one operation at a time, where that operator's control service answers
the console, and the proof of a controller's current
authority that hosted operator obtains before each lifecycle decision
(ADR-0078), and the credential an operator exchanges for the
short-lived tokens it authenticates with in place of a certificate
(ADR-0100), and the keys whose signed assertions exchange for the
same tokens (ADR-0102). Implemented by `internal/operator`,
whose grants and authorities persist through
`internal/operator/postgres`.

| Type | Responsibility |
|------|----------------|
| `Operator` | an organization's record that it has an operator under this GRN: what it calls the operator and says it is for, who registered it and when, where that operator stands in obtaining a certificate, and the control endpoint the console sends its operations to |
| `ControlEndpoint` | a parsed `https` base URL a hosted operator's control service answers the console at |
| `DetailsPatch` | a parsed name and description, each absent where the caller said nothing: what a registration starts with and what an update changes |
| `EnrollmentState` | where an operator stands: holding a certificate, holding a token nobody has spent, or holding neither |
| `Enrollment` | the one-time token an operator obtains its first certificate with: which operator it enrols, who minted it, and when it stops being spendable |
| `Store` | registers one with the token it enrols with and the credential it exchanges, and answers what it stored, re-issues that credential, issues, lists and withdraws its keys, mints that token again for one that has not enrolled, changes its name and description, sets or clears its control endpoint, deletes one and everything the identifier held, answers one by GRN, and lists an organization's own |
| `EnrollmentStore` | spends a token, answering the operator it enrols |
| `Credential` | the certificate a connection authenticated with — the identity it names, which certificate it is, and how long it is good for |
| `CertificateRequest` | a certificate signing request as it arrived, holding the public key an enrolling operator generated |
| `Certificate` | what an operator authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `IssuedCertificate` | a `Certificate` garam generated the key pair for, carrying the private key it returns once |
| `Current` | the certificate garam last issued an operator: the record a renewal is measured against |
| `CertificateStore` | records what garam last issued, and moves that record to the successor only where the presented certificate is still it |
| `Service` | signs an enrolling operator's request, mints a certificate out of band and adopts it, renews the one an operator presents, and exchanges a credential, or a client assertion a live key signed, for a token |
| `IssuedCredential` | a credential garam generated for an operator, carrying the value it answers once (ADR-0100) |
| `Token` | a token an operator exchanged its credential for: the operator, the audience it is accepted by, and when it expires |
| `TokenHolder` | what a live token's row names, read beside the registration it rests on |
| `CredentialHolder` | the operator a presented credential belongs to, and which issuance of it that is |
| `TokenStore` | answers the operator a credential belongs to, records a token under that issuance, answers the live key an assertion names, admits a verified assertion and records its token, and resolves a presented token |
| `IssuedKey` | an RSA key garam generated for an operator, carrying the private PEM it answers once (ADR-0102) |
| `Key` | the public record of a key: its thumbprint, algorithm, verification key, state and when it last obtained a token |
| `KeyRotation` | how an issued key stands beside the keys held: first, planned or immediate |
| `KeyState` | where a key stands at the database's time: active, retiring or revoked |
| `ClientAssertion` | a compact JWT read far enough to name its key, verified by nothing yet |
| `VerifiedAssertion` | an assertion whose signature and identity held, admitted under its time bounds and jti |
| `Limiter` | bounds how often one client or operator exchanges or is issued a key in one process |
| `Issuer` | signs a certificate for a subject GRN, over a key it generates or over one a request carries, and hands back the garam server root beside it |
| `Grant` | an immutable delegation to a hosted operator: its controllers, operations and expiry, and whether a replacement or withdrawal ended it |
| `Binding` | what one operation authority binds: actor, organization, audience, operation, target, grant, request identifier, body digest, for an operation on an existing agent the placement, and for a read or publication the request target |
| `RequestTarget` | a parsed origin-form request target: the escaped path and raw query the Operator receives |
| `GrantStore` | writes a grant under a request identifier and an expected current grant, ends one, and answers the current one or one by identifier |
| `AuthorityStore` | records an operation under its request identifier with a short token, replaces that token for the same binding, and answers a binding by token or by reference |
| `Authorizer` | decides from current state whether a user may perform an action, refusing whatever it cannot decide |
| `AssignmentReader` | answers where an agent runs now |
| `AuthorityService` | grants and withdraws delegations, sets and clears a hosted operator's control endpoint, mints operation authorities, and introspects and rechecks them for their audience |
| `ControllerProof` | a positive proof for one decision: the controller, its organization and, where an agent was named, a `ProvenAgent` — that agent and its current assignment's opaque epoch |
| `AudienceAuthorizer` | decides from current state whether a hosted operator may perform an action, beside the one Allow its grant contributes, refusing whatever it cannot decide |
| `ProofService` | proves a controller's current authority to its hosted operator from the current grant, a named agent's current placement and current policy |

The identifier is the caller's rather than garam's, which is what makes
this domain unlike every other org resource. An operator's GRN is
already the subject of a certificate wherever `issue-certificate` has
run (ADR-0032), so a minted identifier would name something other than
the operator that exists. What the domain applies is the GRN segment
rule, and the schema applies it again where nothing routes around it.

A record mints no certificate, and the token beside it grants no more
than permission to ask for one certificate, once; the credential issued
with it is §Credential and tokens'. What the record does is admit: the
machine listener reads it per connection for a certificate and per
request for a token, so an operator authenticates only while its
organization still holds the record, and withdrawing the record stops
it (ADR-0070, ADR-0100).
garam still never dials an operator, so nothing an operator says
reaches this domain and no state here is a report of one. The record is
read for three questions — whether this organization has the operator a
definition names, which the browser surface asks there; the list of
what the organization has registered, which the console shows and
`ListByOrg` exists for; and whether a presented certificate still names
an operator this organization holds, which the machine listener asks
through a seam of its own. The `agent_definitions` foreign key is the
copy of the definition's check no call path routes around, and it is
what refuses a delete as well.

That list also answers where each operator stands in obtaining a
certificate, derived from the two tables beside the record and stored in
neither: a certificate garam issued is `enrolled`, a token still inside
its window is `pending`, and neither is `expired`. The order is the
whole of it. A certificate outranks a token, because it is the outcome
the token exists to produce, so an operator holding one reads `enrolled`
however much of its token is left — a case that is reachable rather than
hypothetical, since `garam issue-certificate` records a certificate for
a GRN nobody registered and registering that GRN afterwards mints a
token beside it. What the three mean to a browser caller is
`api/openapi.yaml` §OperatorEnrollmentState, which is authoritative for
that (ADR-0059).

Registering therefore answers the record as stored rather than the one
it was handed: the state is read in the same transaction, so a GRN that
already holds a certificate answers `enrolled` on the call that minted
it a token it does not need.

`enrolled` says a certificate was obtained and not that the operator is
alive. garam records no expiry beside the row and asks the operator
nothing, so an operator that enrolled and then died reads `enrolled` and
that is correct. The timestamp beside the state is the current
certificate's issue time, which a renewal moves (ADR-0046): it answers
when garam last signed for the operator and never when the operator
enrolled. Recording the latter would be a history nothing else reads,
which is what `operator_certificates` declines by keeping the newest row
alone.

The record's one update is to its name and description
([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)).
Both are what the organization says about the operator and neither is
an identity: a name is not unique, one left out of a registration is
the identifier, and the update writes those two columns and nothing
keyed on the GRN, so the certificate lineage, an unspent token and the
definitions naming the operator stand as they were. It is authorized
on `operator:update`, which grants nothing and withdraws nothing. The
bounds are `api/openapi.yaml` §OperatorName and §OperatorDescription,
which is authoritative for them, and 000025 holds the same ones.
There is a delete, and it is the one write here that takes something
away (ADR-0070). It leaves the identifier holding nothing — the unspent
token goes by the foreign key that was declared for this route ahead of
it, the credential and its tokens go by `migrations/000038`'s, and the
record of the certificate garam last issued has no such key
and is removed by name in the same transaction. That second removal is
load bearing rather than tidy: left standing, the row would answer
`enrolled` against the next registration of the same identifier, and
the re-mint refusal below would then deny that operator the token it
needs. An operator an agent definition names is refused, and nothing is
deleted; the definitions move or go first.

The certificate half still reads no record above it. An operator renews
with what it holds, and the renewal rule is measured against the
lineage rather than against the registration. What reads the record is
the listener under both halves, before either is reached: a handshake
that finds no record carries no identity, so an operator without one
reaches neither the renewal nor anything else (ADR-0070). What crosses
between the two halves goes the other way still — the record half's two
queries join both tables under the certificate half to derive the state
above, in the direction a renewal never takes.

An operator's first certificate is obtained by enrolling (ADR-0064).
Registering mints a token, the operator generates its key pair where the
certificate will be used, and this domain spends the token and signs the
request. **The subject is the token's and never the request's**, so a
request naming another identity obtains a certificate for the operator
the token named or nothing at all, and what comes back carries no
private key because none was generated. Enrolling adopts the lineage
exactly as the out-of-band mint does, so an enrolled operator renews on
the same rule as any other.

Three things bound what a token is worth. It stops being spendable ten
minutes after it is minted. Spending it removes it, in the statement
that reads it, so two callers presenting one token leave one
certificate. And the attempt spends it whether or not the signing
succeeds, so a caller cannot hold one open across malformed requests —
what a refused enrollment costs is the token, and the recovery is to
mint another.

Minting another is a member's to do, for an operator that holds no
certificate (ADR-0067). It grants nothing a registration does not:
where nothing was ever issued, it is what deleting the record and
registering the same identifier again would be, which is why it is
authorized on the action that registers rather than on one of its own.
That pair of calls is now one a member holding both actions can
actually make, where ADR-0067 argued from an act nobody could perform;
what the argument rested on is unchanged, because it is what the two
calls would grant and not whether they existed.
An operator holds at most one token whatever mints it — the schema
carries a unique key on the operator and the minting statement upserts
on it, so the newest token replaces what was there and one identity is
never reachable by two.

It is refused for an operator that already enrolled, and the refusal
says why. That operator holds a certificate, which is the outcome the
token exists to produce, so a second token would be a second credential
path to one identity — closer to issuing another credential for an
existing subject than to registering. Whether a member may do that is
undecided: it has no forcing case, and garam cannot tell a member
recovering from a compromise from the compromise itself. The message
the refusal carries is where that stands, so a person meeting it learns
a decision was taken rather than that something broke. Which half a
request falls in is the certificate-outranks-token order above, read
inside the same transaction as the write, by the derivation the
registration reads it by and not a second one.

Unknown, spent and expired are one refusal. A caller that spends nothing
presented no credential either way, and telling the three apart would
answer questions about tokens the caller does not hold. Only the hash
reaches the database, as it does for a session, so a row read out of a
dump enrols nothing.

The command ADR-0032 ships still mints a first certificate out of band,
and it is what recovers the operator that already holds one, for which
no token is minted again. An operator with no certificate and no token
is recovered by minting another instead. Every
certificate after the first is issued here, over the connection the
certificate being replaced authenticated, so neither bootstrap is a
standing obligation.

Four things refuse a renewal, and each is a decision rather than a
guard. A certificate whose subject is not an operator is refused before
any window is measured: what this domain renews is an operator's
certificate. A certificate that has expired renews nothing: it
authenticates nothing, so no identity reaches this route with it, and
the rule states the same boundary as the empty end of its window — what
follows expiry is a token to enrol with, or a mint out of band. A
certificate with more than a third of its validity left is refused as
too early, measured off the certificate rather than off a configured
number, so the window holds whatever lifetime signed it. And
only the certificate garam last issued an operator may renew it: garam
records that certificate and moves the record to the successor in the
statement that checks it, so every other copy of that identity stops
being renewable at once. That last one is what bounds a stolen
certificate belonging to an operator the organization still wants — the
operator it was taken from renews, the copy loses the lineage, and it
expires. It withdraws nothing: a certificate that can no longer renew
keeps authenticating until it runs out, and a caller holding the one
garam has just replaced authenticates on it meanwhile. What ends a
theft rather than bounding it is deleting the operator, which stops
every certificate naming it at once (ADR-0070).

An operator with no record yet is the one case that comparison has
nothing to make, and it is not a refusal: the write that moves the
record inserts one where none exists, so the first renewal adopts the
lineage. That is what an operator whose certificate predates the record
needs, and it is why the fourth refusal bounds a copy only once a
lineage exists to lose.

The mint adopts whatever it issued, so re-running `garam
issue-certificate` takes the lineage from every certificate that already
names the operator, and so does an enrollment. That is the recovery from
an answer nobody received. It is not the revocation: it narrows what a
copy can do next and leaves it authenticating, where deleting the
operator refuses every copy from its next connection.

`CertificateStore` holds one row per operator and has no read: what the
rule needs is not the current certificate but whether the presented one
is still it, and the write answers that itself. Its table is read by the
record half, for whether a row is there and when it was written; that is
not a read on this interface, and nothing the record half answers names
which certificate the row holds — the serial is what decides a renewal
and stays on the machine listener. `EnrollmentStore` has no
read for the same shape of reason: nothing can answer a token without
also spending it, which is what makes single use the store's rather than
a check a caller performs. Signing happens through the `Issuer` seam the
composition root satisfies from `internal/ca`, as the agents domain's
does, so this one never holds key material; the successor is signed
before the record moves, and a caller that lost the lineage receives
nothing. The certificate half's HTTP surface is the
machine listener — the renewal a certificate authenticates, and the
enrollment reached without one — and `internal/cli` is its other caller,
`garam issue-certificate`. The record half's surface is the
browser-facing listener, on the organization's own paths, and that is
also where a token is minted and where a record is deleted: registering
writes the record and the token in one transaction, so a registration
never exists without the token that enrols it; the same surface mints
another where the operator has not enrolled — a route no console page
reaches yet — reads and renames one, and deletes one, all three of
which the console reaches on the operator's own page (ADR-0074).
Deleting
is authorized on `operator:delete` rather than on the action that
registers, because registering grants nothing and this withdraws what
an operator runs on (ADR-0070). The record is read on the machine
listener too, through the seam that admits a certificate, which is the
one place either listener reads it to decide whether a caller is
admitted. The
agent's certificate is not
this domain's: an agent is admitted to its own by the claim its operator
made (ADR-0033) and renews on the route ADR-0040 gave it.

## Issued PEM identity

**garam's half is built; no Operator release exchanges yet.**
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
replaces the target for §Credential and tokens' shared-secret
exchange. The registration, identifier, permissions and the
shared-secret and certificate cohorts remain, and the Operator's
single-mode cutover is its own repository's (Operator ADR-0068).
`api/openapi.yaml` §issueOperatorKey, §listOperatorKeys and
§revokeOperatorKey are authoritative for the key routes' wire.

- **A key is issued by `issueOperatorKey`; registering issues none.**
  The PM decided this on #1356, where the ADR leaves the issuing route
  open: the key route exists for rotation and lost-issuance
  replacement anyway, so one path serves the first key too, and no
  answer carries two secrets while the shared-secret cohort lasts. The
  cost is a second step for an administrator: register, issue a key,
  place the PEM and `tokenUrl`, once per registered operator identity.
  An Operator has two, its manager and its control service, and each
  is issued its own key.
- **The first key of an operator that has never been issued one is
  `operator:register`, as registering it was; any other is
  `operator:reissue`.** A withdrawn key still counts, so re-keying
  after a revocation is `operator:reissue`'s: the PM's review of #1360
  held that any key change on an existing operator is, as
  ADR-0102 §Operator identity and custody says. The store refuses a
  first issuance under the registration's row lock, in the transaction
  that would write, so a concurrent issuance cannot split the check
  from the write.
- **garam answers the private key once and keeps the public JWK.** The
  key is RSA 3072 as PKCS#8 `PRIVATE KEY` PEM, answered with
  `Cache-Control: no-store`; `migrations/000043`'s `operator_keys`
  holds the JWK, algorithm, RFC 7638 thumbprint `kid`, issuer and
  state. The PEM exists in the generating process and the answer and
  in no column, log or metric. A lost answer is replaced with
  `rotation` `immediate`, never read back.
- **The answer carries `tokenUrl`, the configured
  `GARAM_MACHINE_TOKEN_URL`, which an assertion's `aud` equals byte
  for byte.** The PM decided on #1356 that this is a variable of its
  own, an absolute https URL with no query or fragment: reusing
  `GARAM_MACHINE_ISSUER`, ADR-0101's message issuer, would couple two
  contracts, and the Operator dials garam at an address that need not
  be its public origin, so it cannot derive `aud` from the address.
  Unset, no key is issued: issuing one is refused with 409
  `failed_precondition` and the reason `key_issuance_unconfigured`,
  since the state holds until the deployment sets the URL rather than
  being a fault (#1394), and the exchange answers 503
  (`deployment-contract.md` §Operator key and enrollment rollout).
- **Planned rotation holds two live keys.** The predecessor reads
  `retiring` for `RolloverWindow`, 24 hours, and is then withdrawn
  without a write; a third live key is refused. `immediate` withdraws
  every live key and deletes their tokens in the write that records
  the successor. `revokeOperatorKey` withdraws one key and its tokens,
  and the row stays, listed `revoked`.
- **A key proves identity and grants no organization permission.**
  Tokens it obtains are ordinary operator tokens: delegated authority,
  controller proof and assignment checks apply as they do to a
  shared-secret token. Replacing a key recovers no agent lineage.
- **One process issues at most 3 keys per registered operator identity
  and 30 in all per minute** (`internal/operator`
  `KeyIssuancesPerOperator`), and answers 429 with `Retry-After` past
  either; generating the key is the costliest thing a member asks of the
  browser-facing listener.
- **The Operator's half is unbuilt.** One external writer supplies the
  stable PEM, replaced atomically and reloaded at each exchange, with
  `kid` derived from the same read; the Operator refreshes tokens in
  memory with a fresh assertion and `jti` each attempt and never
  writes that key. Its PM supersedes ADR-0008's certificate renewal
  writer before cutover; garam edits no Operator convention or source.

`machine-identity.md` §Asymmetric Operator exchange owns the exchange
and the token reads.

## Credential and tokens

**What is built is the operator's half: its credential, its tokens and
their revocation.** ADR-0100 replaces an operator's certificate with
these, and the plan,
`docs/plans/2026-10-09-machine-identity-migration.md`, says when the
certificate path goes; until then an operator may present either.

- **Registering issues the credential, in the write that records the
  operator**, and the console shows it once. garam stores its SHA-256
  alone, so a dump authenticates nothing. It does not expire.
- **An operator holds one credential, and re-issuing replaces it.**
  `reissueOperatorCredential` deletes the row and inserts its successor
  in one transaction; every token minted under the previous one goes
  with it by `migrations/000038`'s foreign key, so the operator's next
  request on any of them is refused. Re-issuing is `operator:reissue`,
  which owner and admin hold, because it withdraws what an operator runs
  on until the new credential is placed. An operator registered before
  credentials existed holds none, and re-issuing issues its first.
- **The credential exchanges for a one-hour token, and for nothing
  else.** `exchangeToken` on the machine listener answers a token
  prefixed `garam_ot_`, stored as its hash beside the credential
  issuance it was minted under; the exchange sweeps the operator's
  expired tokens and keeps the issuance's newest five, so a leaked
  credential cannot grow the table. A value that is not a credential's
  shape is refused before anything is read. A token never mints a
  token. One may name an
  audience — another operator the organization registered, a hosted
  control service it is to be shown to — and garam accepts such a
  token on no route; it reads one only when that audience forwards it
  for a controller proof (§Controller proof).
  `migrations/000041`'s foreign key refuses an audience nobody
  registered, and deleting that operator takes the tokens minted to be
  shown to it.
- **Revocation is a read on every request.** The machine listener
  resolves a presented token by its row joined to the registration, so
  deleting the operator, which takes the credential and its tokens by
  foreign key, and re-issuing are both refused from the next request
  ([machine-identity.md](./machine-identity.md) §Tokens). A token
  exchanged under a key rests on that key instead of a credential, so
  re-issuing the credential leaves it standing and withdrawing the key
  does not (§Issued PEM identity). A token's hour bounds one nobody
  revoked.

## Delegated operation authority

An organization delegates bounded execution authority to a hosted
operator, and a member hands that operator one operation at a time
(ADR-0078). The hosted operator and each controller it may act on are
distinct records of the same organization, holding distinct
certificates; a grant naming another organization's operator, or the
hosted operator as its own controller, is refused.

**A grant never changes.** Writing one ends the current grant and
creates another, so an operation authorized under the first is never
authorized under the second, and a withdrawal is final for everything
minted under it. Each write carries a request identifier and the grant
the caller read as current: a repeat answers the grant it wrote while
that grant is current and is refused once it has been ended, and a
write from a view that has since moved is refused rather than
overwriting what moved it. Ended grants are kept, because their request
identifiers are what refuse a replay; deleting the organization or the
hosted operator removes them and everything minted under them, and
deleting a controller removes it from every grant. Granting and
withdrawing are `operator:delegate`, which owner and admin hold.

**An operation authority is a short opaque token over a durable
reference.** Minting reads the member's authority from current state
and binds the actor, the audience, the operation, its target, the
current grant, the caller's request identifier and the digest of the
exact body the Operator will receive; garam never sees that body. It
answers the token once, stores only its hash, and lets it live five
minutes or until the grant expires. Creation targets the controller
and is decided on `agent:create` against the organization, as defining
an agent is, and its reference is what creates a managed agent,
issues its first certificate and may start its first generation
([agents.md](./agents.md) §Separated execution); configuration
targets the agent, is decided on `agent:configure` against it, and
records where the agent runs now.
Recovery targets the agent, is decided on `agent:recover` against it,
and records where the agent runs now; the recovery it authorizes is
answered once, and a second recovery under it is refused
([agents.md](./agents.md) §Separated execution). Cutover targets the
agent, is decided on `agent:cutover` against it, records where the
agent runs now and binds the request target of one of garam's cutover
routes, so each stage of a legacy agent's cutover takes a reference of
its own, rechecked when it is presented (ADR-0086).
Every role holds `agent:create`; only owner and admin hold
`agent:configure`, `agent:recover` and `agent:cutover`, because
execution settings choose what runs on a host, a recovery fences every
certificate the agent held, and a cutover moves which service owns the
agent's settings.
`agent:activate` is a grant operation only, bounding the hosted
operator's own lifecycle authority; no member mints it.

**A read or a template publication is its own operation, bound to
its exact request target** (#1170). Reading published templates is
`agent-template:read` and reading published execution profiles
`execution-profile:read`; both target the organization, name no
controller and are held by every role. Publishing the next immutable
template version is `agent-template:publish`, also on the
organization, held by owner and admin. Reading an agent's pending,
rendered and runtime-reported execution is `agent:execution-read`,
which targets the agent, records where it runs now as configuration
does, and is held by owner and admin; `agent:read` stays identity
access and grants none of it. Each is minted only under a grant that
lists it, so a create or configure grant authorizes no read, and a
policy may delegate any of them to someone else while an explicit
Deny still wins. An empty body's digest is the same for every read,
so these four also bind the request target the Operator receives —
the escaped path and raw query with no origin, bounded, checked for
origin-form and holding no dot segment — and the Operator compares it
byte for byte, with the operation's one fixed method, before reading
anything. Create,
configure and recover bind none and refuse one, so their authority is
what it was. Publishing execution profiles stays with the deployment
and no operation here grants it. The role defaults are the one
`machineActions` table in `internal/org/role.go`, which is
authoritative for which role holds what.

**Reading what the Operator reports about the organization's
operators is `operator:read`** (#1374, #1047). The operation is
delegable and mintable like `agent-template:read`, targets the
organization, names no controller, binds the exact request target and
is held by every role, because it is the action that lists the
operators garam holds. The status belongs to the control service
(ADR-0077), which serves it at `GET /v1/orgs/{org}/operators`
(garam-agent-operator #364 at
`ba691a69ad0612ff5653d2facce5668ea12cbec9`, its ADR 0074). The answer
holds one entry per controller that reached that service:
- its GRN;
- its images, each a name, a reference and, where known, a digest;
- `reportedAt` and `lastContactAt`.

An absent `reportedAt` or `lastContactAt` means never reported or
never contacted, never now. A service answers only for the controllers
that reached it, so the console asks every hosted operator whose grant
carries the read and merges the answers by GRN
([web-app.md](./web-app.md) §What it serves, Operators).

**The registered control endpoint is where the console sends an
authority, and only the delegating act moves it.** Each operator may
carry one `https` base URL with a host, an optional port and path
prefix, and no credentials, query, fragment, dot segment or trailing
slash. Setting and clearing
it are `operator:delegate`, decided from current state as a grant is,
and on routes of their own; changing an operator's name or
description does not reach it. An authority names its audience by
GRN, so moving the endpoint moves no audience of one already minted.
Allowed browser origins, CORS and what the control service answers are
the Operator's (`garamsh/garam-agent-operator#220`).

**Recovery from the console is unbuilt.** The `agent:recover` handoff
garam mints is built, and so is the Operator route an administrator
finalizes a prepared recovery through (`garamsh/garam-agent-operator`
`ad444ede20c83992701fd563e7a0c916827979a4`,
`docs/architecture/control.md`, its ADR 0057); the console offers no
recovery control.

The same binding under the same grant mints a fresh token for the same
reference, which is how a handoff that expired or whose answer was lost
is retried. Anything else under that request identifier — another
digest, a replaced grant, a moved agent — is refused, and a newly
authorized operation takes a new one.

**Every read back is decided again.** Introspection answers the audience
alone, and an unknown, expired or other audience's token is one
not-found. It, and the reference check an Operator makes before
activating a new desired revision, refuse once the grant has ended or
stopped covering the operation, the bound controller is no longer
registered, a configuration change's agent has moved, or the member is
no longer authorized; neither consumes anything. A decision that cannot
be read from current state is unavailable and never an answer. A
reference is not a restart capability: restarting an applied revision
is lifecycle authority (ADR-0078).

The routes and their wire forms are `api/openapi.yaml` §operator and
`api/machine.yaml` §authority, which are authoritative for them; each
carries the `operation-authority.v1` contract version (ADR-0079), and
the machine listener's answers `operation-authority.v2` to a token
(ADR-0100). A
release that does not know an operation refuses to mint it, so an
older garam never mints a read without its request target.

## Controller proof

**garam's half is built; its consumer is not.** Nothing in this
repository calls the route. The Operator control service adopts it
under garamsh/garam-agent-operator#211, #212 and #218, each open on
2026-10-04, so the caller's side below is an obligation on that
service, not a description of it.

A hosted operator proves a controller's current authority before each
lifecycle decision it makes for that controller (ADR-0078). It
forwards the controller's credential exactly as the controller
presented it: under `operation-authority.v1` its leaf certificate,
and under `operation-authority.v2` the token the controller exchanged
its credential for to be shown to that hosted operator (ADR-0100).
garam verifies that credential rather than any GRN the caller asserts.
A proof is answered only when every check passes, each read on the
request, because the caller's connection authenticated once when it
was made:

- the caller's own credential is still within its validity and its
  registration is still held;
- the forwarded certificate names the controller, chains to the
  organization's authority, is within its validity, and the controller
  is still registered; or the forwarded token is live, names the
  controller and names the caller as its audience, which the token's
  read against the registration already decides;
- the caller's current grant covers `agent:activate` for that
  controller;
- a named agent belongs to the organization and is currently assigned
  to that controller, and the proof carries the assignment's epoch;
- current policy for the caller and the groups it belongs to now,
  beside an Allow of `agent:activate` that the grant contributes — on
  the organization for a session, on the agent for that agent's
  lifecycle — does not deny it, and an explicit Deny wins.

Certificates are `internal/machine`'s to verify
([machine-identity.md](./machine-identity.md)) and a controller's token
is this domain's, which issued it (`Service.VerifyShown`); this domain
decides the grant, the placement and the policy on GRNs. A read that
cannot be made is unavailable, never an answer from something older. The
proof holds as of its reads and is no transaction with the caller's
store.

**A session proof releases no agent.** Before releasing an agent's
desired content, deciding its placement or forwarding its certificate
request — the calls a controller originates — the caller obtains that
agent's proof, compares the epoch with the one it expects, and asks
again after a long wait; it never reuses a proof across decisions. The
route, its fields and its refusal statuses are `api/machine.yaml`
`introspectController`, which is authoritative for them.

**Each certificate is proven by the route for its kind, and so is the
token that replaces it under the v2 values.** These are obligations on
the Operator control service; garam's routes for both are built.

- **A controller's leaf** is proven here. The control service may keep
  the exact public leaf a controller presented when it registered the
  same placement over an authenticated request, and forward it again
  for each fresh proof; validity, registration, grant, policy and
  assignment are read again every time, and no earlier positive answer
  is reused. Only an authenticated request from that controller for
  the same current placement replaces the kept leaf, and a replacement
  neither revives a superseded placement nor stands for its writer
  having stopped.
- **An agent's leaf** — what the adapter presents with a runtime status
  report or an activation request — is proven by execution
  introspection (`machine-identity.md` §Generation and lineage), never
  relayed through the controller to obtain a controller's leaf. Its 200
  is a status answer: a report is
  accepted only when `credential` and `generation` are both `current`
  and the identity, assignee, epoch and activation are the ones
  expected, and an expired or superseded certificate answers
  `fenced`.
- **An adapter's activation request** is checked by that
  introspection for the certificate and binding, the placement's
  controller is proven again here from its kept leaf, and then the
  control service activates. The new generation need not be current
  before it is activated; the both-current condition applies to the
  status reports that follow.
