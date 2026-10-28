# Operators

What an organization records about an operator and what it
authenticates with: the record that the organization has it and that
its keys authenticate on (ADR-0045, ADR-0070), the keys whose signed
assertions it exchanges for the short-lived tokens it authenticates
with (ADR-0102), the authority an organization delegates to a hosted
operator and hands it one operation at a time, where that operator's
control service answers the console, and the proof of a controller's
current authority that hosted operator obtains before each lifecycle
decision (ADR-0078). An operator enters by a key alone: the one-time
enrollment token and the shared-secret credential are gone
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)),
and garam issues an operator no certificate
([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)).
Implemented by `platform/internal/operator`, which persists through
`platform/internal/operator/postgres` (ADR-0069): its records, keys,
tokens, grants and authorities.

| Type | Responsibility |
|------|----------------|
| `Operator` | an organization's record that it has an operator under this GRN: what it calls the operator and says it is for, who registered it and when, the control endpoint the console sends its operations to, and, for a control service, the cluster manager it belongs to |
| `ControlEndpoint` | a parsed `https` base URL a hosted operator's control service answers the console at |
| `DetailsPatch` | a parsed name and description, each absent where the caller said nothing: what a registration starts with and what an update changes |
| `Store` | registers one, a cluster manager and its control service together, or a control service under the cluster manager it names, and answers what it stored; links a control service to its cluster manager once; issues, lists and withdraws its keys, changes its name and description, sets or clears its control endpoint, deletes one and everything the identifier held, answers one by GRN, and lists an organization's own |
| `Service` | exchanges a client assertion a live key signed for a token, and verifies a token presented on another operator's behalf |
| `Token` | a token an operator exchanged a key's assertion for: the operator, the audience it is accepted by, and when it expires |
| `TokenHolder` | what a live token's row names, read beside the registration and the key it rests on |
| `TokenStore` | answers the live key an assertion names, admits a verified assertion and records its token, and resolves a presented token |
| `IssuedKey` | an RSA key garam generated for an operator, carrying the private PEM it answers once (ADR-0102) |
| `Key` | the public record of a key: its thumbprint, algorithm, verification key, state and when it last obtained a token |
| `KeyRotation` | how an issued key stands beside the keys held: first, planned or immediate |
| `KeyState` | where a key stands at the database's time: active, retiring or revoked |
| `ClientAssertion` | a compact JWT read far enough to name its key, verified by nothing yet |
| `VerifiedAssertion` | an assertion whose signature and identity held, admitted under its time bounds and jti |
| `Limiter` | bounds how often one client or operator exchanges or is issued a key in one process |
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
this domain unlike every other org resource. An operator is configured
with its identifier before it is registered, so a minted identifier
would name something other than the operator that exists. What the
domain applies is the GRN segment rule, and the schema applies it again
where nothing routes around it.

A record mints nothing. What it does is admit: the machine listener
reads it on every request, joined to the token the request presents,
so an operator authenticates only while its organization still holds
the record, and withdrawing the record stops it (ADR-0070, ADR-0102).
garam still never dials an operator, so nothing an operator says
reaches this domain and no state here is a report of one. The record is
read for two questions — the list of what the organization has
registered, which the console shows and `ListByOrg` exists for; and
whether a presented token still names an operator this organization
holds, which the machine listener asks through a seam of its own.

Apart from its control endpoint and the manager a control service
names once (§An Operator's two identities), the record's one update is
to its name and description
([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)).
Both are what the organization says about the operator and neither is
an identity: a name is not unique, one left out of a registration is
the identifier, and the update writes those two columns and nothing
keyed on the GRN, so its keys and their tokens stand as they were. It
is authorized on `operator:update`, which grants nothing and withdraws
nothing. The
bounds are `platform/api/openapi.yaml` §OperatorName and
§OperatorDescription, which is authoritative for them, and 000025 holds
the same ones. There is a delete, and it is the one write here that
takes something away (ADR-0070). It leaves the identifier holding
nothing: its keys and their tokens go by `platform/migrations/000043`'s
foreign keys.

The record's surface is the browser-facing listener, on the
organization's own paths: it registers, reads and renames one, links
a control service to its cluster manager once (`setOperatorManager`,
ADR-0124), and deletes one, which the console reaches on the
operator's own page (ADR-0074). Deleting is authorized on
`operator:delete` rather than on the action that registers, because
registering grants nothing and this withdraws what an operator runs on
(ADR-0070). The record is read on the machine listener too, through
the seams that admit a token, which are the one place either listener
reads it to decide whether a caller is admitted.

## An Operator's two identities

An Operator is a cluster manager and at most one control service,
each a record of its own with its own key. The pair is recorded on the
control service's record, which names its cluster manager by GRN
([ADR-0124](./adr/0124-an-operator-is-registered-as-a-pair-in-one-step-and-its-control-service-names-its-manager.md)).
That ADR's §Terms maps the API's names, `Operator` and `manager`
among them, to these terms.

- **The pair is registered in one step.** `registerOperator` with
  `controlService` records the cluster manager and its control service
  in one transaction (`Store.RegisterPair`): both, or neither.
- **A control service may join a cluster manager registered alone.**
  It can be registered with `manager`, or an existing record naming no
  manager can be linked once (`Store.SetManager`), authorized on
  `operator:register`. After that the link never moves.
- **Both paths refuse the same things.** `operator.ParseManager`
  refuses, as a 400, a value that is not another record of the
  organization. The store refuses each of the following as a 409, in
  the transaction that writes:
  - a cluster manager the organization holds no record of;
  - one that is itself a control service;
  - one another control service already names;
  - a record a control service names as its cluster manager;
  - for a link, a record that already names one.
- **The rows are held in a fixed order.** A registration inserts
  first and holds the cluster manager `FOR KEY SHARE` after, so it
  takes the organization's row before any record's, as an
  organization's deletion does (#1424). A link holds the record
  `FOR UPDATE`, which waits for a registration holding it as a cluster
  manager, and holds the two rows in GRN byte order.
- **It names the identity.** `platform/migrations/000062` holds the
  column unique, without a foreign key, and naming a record of the same
  organization other than the row. Deleting a cluster manager leaves its
  control service naming it, and registering that identifier again
  rejoins them.
- **It is read on the list.** `ListByOrg` answers every record with
  its manager, and the console joins them:
  - each record naming no manager is an Operator;
  - the record naming it is its control service;
  - a control service naming a cluster manager garam no longer holds
    is an Operator whose cluster manager is not registered.

  Nothing pairs by name or by grant. A grant is the authority a member
  hands a hosted operator (§Delegated operation authority), not a
  statement of which identities are one Operator.

`platform/api/openapi.yaml` §Operator and §OperatorRegistration are
authoritative for the pair's wire.

## Issued PEM identity

**garam's half is built; no Operator release exchanges yet.**
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
is how an operator proves its identity, and
[ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)
removed the shared-secret exchange beside it. The registration,
identifier and permissions remain, and the
Operator's single-mode cutover is its own repository's (Operator
ADR-0068).
`platform/api/openapi.yaml` §issueOperatorKey, §listOperatorKeys and
§revokeOperatorKey are authoritative for the key routes' wire.

- **A key is issued by `issueOperatorKey`; registering issues none.**
  The PM decided this on #1356, where the ADR leaves the issuing route
  open: the key route exists for rotation and lost-issuance
  replacement anyway, so one path serves the first key too, and no
  answer carries two secrets. The cost is a second step for an
  administrator: register, issue a key, place the PEM and `tokenUrl`,
  once per registered operator identity.
  An Operator has two, its manager and its control service
  (§An Operator's two identities), and each is issued its own key.
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
  `Cache-Control: no-store`; `platform/migrations/000043`'s
  `operator_keys` holds the JWK, algorithm, RFC 7638 thumbprint `kid`,
  issuer and state. The PEM exists in the generating process and the
  answer and in no column, log or metric. A lost answer is replaced with
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
  controller proof and assignment checks apply to them. Replacing a
  key recovers no agent lineage.
- **One process issues at most 3 keys per registered operator identity
  and 30 in all per minute** (`platform/internal/operator`
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

## Tokens

**An operator's tokens are what its key exchanges for.** Each is a
one-hour token prefixed `garam_ot_`, which `exchangeClientAssertion`
answers on the machine listener (§Issued PEM identity), stored as its
hash beside the key it was exchanged under.

- **An exchange bounds what it writes.** It sweeps the operator's
  expired tokens and keeps the key's newest five, so a leaked key
  cannot grow the table. A token never mints a token.
- **A token may name an audience** — another operator the
  organization registered, a hosted control service it is to be shown
  to — and garam accepts such a token on no route; it reads one only
  when that audience forwards it for a controller proof
  (§Controller proof). `platform/migrations/000041`'s foreign key
  refuses an audience nobody registered, and deleting that operator
  takes the tokens minted to be shown to it.
- **Revocation is a read on every request.** The machine listener
  resolves a presented token by its row joined to the registration and
  to its key while that key is live, so deleting the operator, which
  takes its keys and their tokens by foreign key, and withdrawing or
  replacing the key are each refused from the next request
  ([machine-identity.md](./machine-identity.md) §Tokens). A token's
  hour bounds one nobody revoked.

## Delegated operation authority

An organization delegates bounded execution authority to a hosted
operator, and a member hands that operator one operation at a time
(ADR-0078). The hosted operator and each controller it may act on are
distinct records of the same organization, holding distinct keys; a
grant naming another organization's operator, or the
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
and is decided on `agent:create` against the organization, and its
reference is what creates a managed agent and may start its first
generation
([agents.md](./agents.md) §Separated execution); configuration
targets the agent, is decided on `agent:configure` against it, and
records where the agent runs now.
Recovery targets the agent, is decided on `agent:recover` against it,
and records where the agent runs now; the recovery it authorizes is
answered once, and a second recovery under it is refused
([agents.md](./agents.md) §Separated execution). Replacement
targets the agent, is decided on `agent:replace` against it, records
where the agent runs now and binds the controller a managed
replacement moves it to; the grant must cover both controllers at the
mint and at every read, and the reference stays current at the
assignment the replacement writes
([ADR-0107](./adr/0107-a-managed-replacement-is-authorized-by-an-agent-replace-reference-naming-the-agent-its-assignment-and-the-controller-it-moves-to.md)).
Every role holds `agent:create`; only owner and admin hold
`agent:configure`, `agent:recover` and `agent:replace`, because
execution settings choose what runs on a host, a recovery fences every
token the agent held, and a replacement
moves the agent to another controller and fences its lineage.
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
`machineActions` table in `platform/internal/org/role.go`, which is
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

The routes and their wire forms are `platform/api/openapi.yaml`
§operator and `platform/api/machine.yaml` §authority, which are
authoritative for them; the browser-facing listener's carry the
`operation-authority.v1` contract version (ADR-0079), and the machine
listener's `operation-authority.v2` (ADR-0100, ADR-0118). A release that
does not know an operation refuses to mint it, so an older garam never
mints a read without its request target.

## Controller proof

**garam's half is built; its consumer is not.** Nothing in this
repository calls the route. The Operator control service adopts it
under garamsh/garam-agent-operator#211, #212 and #218, each open on
2026-10-04, so the caller's side below is an obligation on that
service, not a description of it.

A hosted operator proves a controller's current authority before each
lifecycle decision it makes for that controller (ADR-0078). It
forwards, under `operation-authority.v2`, the token the controller
exchanged its key for to be shown to that hosted operator (ADR-0100,
ADR-0102). garam verifies that token rather than any GRN the caller
asserts. A proof is answered only when every check passes, each read
on the request:

- the caller's own token is still within its dates and its
  registration is still held;
- the forwarded token is live, names the controller and names the
  caller as its audience, and the controller is still registered, which
  the token's read against the registration already decides;
- the caller's current grant covers `agent:activate` for that
  controller;
- a named agent belongs to the organization and is currently assigned
  to that controller, and the proof carries the assignment's epoch;
- current policy for the caller and the groups it belongs to now,
  beside an Allow of `agent:activate` that the grant contributes — on
  the organization for a session, on the agent for that agent's
  lifecycle — does not deny it, and an explicit Deny wins.

The caller's identity is `platform/internal/machine`'s to read again
([machine-identity.md](./machine-identity.md)) and a controller's token
is this domain's, which issued it (`Service.VerifyShown`); this domain
decides the grant, the placement and the policy on GRNs. A read that
cannot be made is unavailable, never an answer from something older. The
proof holds as of its reads and is no transaction with the caller's
store.

**A session proof releases no agent.** Before releasing an agent's
desired content or deciding its placement — the calls a controller
originates — the caller obtains that
agent's proof, compares the epoch with the one it expects, and asks
again after a long wait; it never reuses a proof across decisions. The
route, its fields and its refusal statuses are
`platform/api/machine.yaml` `introspectController`, which is
authoritative for them.

**Each token is proven by the route for its kind.** These are
obligations on the Operator control service; garam's routes for both
are built.

- **A controller's token** is proven here. The control service may keep
  the token a controller presented to it when it registered the same
  placement over an authenticated request, and forward it again for
  each fresh proof while it is live; validity, registration, grant,
  policy and assignment are read again every time, and no earlier
  positive answer is reused. Only an authenticated request from that
  controller for the same current placement replaces the kept token,
  and a replacement neither revives a superseded placement nor stands
  for its writer having stopped.
- **An agent's token** — what the adapter presents with a runtime
  status report or an activation request — is proven by execution
  introspection (`machine-identity.md` §Generation and lineage), never
  relayed through the controller to obtain a controller's token. Its
  200 is a status answer: a report is accepted only when `credential`
  and `generation` are both `current` and the identity, assignee, epoch
  and activation are the ones expected, and an expired or superseded
  token answers `fenced`.
- **An adapter's activation request** is checked by that
  introspection for the token and binding, the placement's controller
  is proven again here from its kept token, and then the control
  service activates. The new generation need not be current before it
  is activated; the both-current condition applies to the status
  reports that follow.
