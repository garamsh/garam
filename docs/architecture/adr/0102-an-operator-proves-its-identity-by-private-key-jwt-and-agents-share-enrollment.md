# ADR 0102: An operator proves its identity by private_key_jwt and agents share enrollment

> Status: proposed
> Date: 2026-10-13

Operator authentication and agent enrollment direction for #1353,
which reserves this number. The owner approved the asymmetric direction
on 2026-10-13, recorded in
[the owner's direction](https://github.com/garamsh/garam/issues/1353#issuecomment-6146292449).
The Garam PM confirmed the initial one-time issued-PEM UX in the
worker dispatch the same day. Acceptance of
this record is the PM's; no implementation ships in this change.

## Context

At Garam `3beb1569b804f6461b2e956601e2a0377b81152e`, G1a through G1d
of ADR-0100 are built. `internal/operator/token.go` and
`internal/machineapi/identity.go` implement an opaque shared-secret
exchange, not a signed assertion. `internal/machineapi/contract.go`
serves certificate v1 and token v2 contracts side by side. Managed
creation and standalone approval/spending are distinct enrollment
paths (`internal/agent/execution_service.go`,
`internal/agent/standalone_service.go`, `api/machine.yaml`). The
adapter and standalone CLI still use certificates
(`internal/cli/delivery.go`, `internal/cli/standalone_host.go`).

The owner keeps operator registration and operator_id, and asks for a
private PEM proving that identity before short-lived API tokens. No
application catalog, installation entity or GitHub integration is
required. Provisioning a Pod and connecting an existing runtime must
lead to the same agent identity and execution rules.

Primary standards consulted on 2026-10-13:

| Standard | Basis for the decision |
|---|---|
| [RFC 7523](https://www.rfc-editor.org/rfc/rfc7523) | Client assertion parameters and processing; interoperability requires agreement on identifiers, keys, lifetime and replay; RS256 is mandatory to implement |
| [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700) | “Client Authentication” recommends asymmetric client authentication; bearer access tokens still need audience restriction and leakage protection |
| [RFC 6749](https://www.rfc-editor.org/rfc/rfc6749) | Client credentials grant and standard token response/error forms |
| [OpenID Connect Core 1.0, errata set 2](https://openid.net/specs/openid-connect-core-1_0.html#ClientAuthentication) | Names private_key_jwt and binds assertion issuer and subject to client_id |
| [RFC 7518](https://www.rfc-editor.org/rfc/rfc7518) | RS256 RSA minimum size and ES256 P-256 signature representation |
| [RFC 7638](https://www.rfc-editor.org/rfc/rfc7638) | Public JWK thumbprints identify keys independently of PEM formatting |
| [RFC 8725](https://www.rfc-editor.org/rfc/rfc8725) | Pin algorithms, validate issuer/subject/audience, and separate JWT purposes |
| [RFC 7468](https://www.rfc-editor.org/rfc/rfc7468) | PEM textual encodings distinguish private keys, public keys and certificates |

## Decision

An operator proves its registered identity with private_key_jwt, and
Garam issues short-lived opaque access tokens over server TLS. A common
agent enrollment contract separates identity admission from runtime
provisioning; placement and lifecycle authority express managed/direct
differences while the existing execution fences remain mandatory.

### Operator identity and custody

- Registration and the existing operator identifier remain. OAuth
  `client_id` is the full canonical operator GRN derived from the
  organization's registration and operator_id; an unqualified segment
  alone is not globally unique. `iss` and `sub` must both equal it.
  A name, public key or client-supplied organization grants no rights.
- The initial UX issues an RSA private key once from Garam to the
  authenticated person registering or replacing that operator's key.
  Garam retains only the verification public JWK, algorithm, `kid`,
  registration binding and key state. It never persists, logs, seals
  for later retrieval or reads back the private key. It is necessarily
  exposed in Garam's generating process and the authenticated download
  path; “Garam never holds it” would be false.
- Issue a 3072-bit RSA key as PKCS#8 `PRIVATE KEY` PEM and its public
  fingerprint. The person installs the key in the operator's own
  owner-only file, using the cluster's secret mechanism. No environment
  variable, command argument, image layer, source control, browser
  persistent storage or runtime mount carries it. Responses prohibit
  caching; application/proxy logs redact the response and assertions.
- One declared external/admin key writer supplies that stable PEM;
  the Operator exchanges it but never renews or writes the same key
  Secret. Replace the owner-only file atomically and reload it for
  each exchange, deriving `kid` from the same key snapshot used to
  sign. Reading a key and an independently changing `kid` file would
  mix two issuances. Operator ADR-0008's old certificate renewal writer
  must be superseded/paired in its repository before cutover, not
  left competing with an external key writer. Raw Secret projection
  or `fsGroup` alone does not prove process ownership and mode 0600;
  the exact owned copy/mount mechanism is the Operator's.
- A download is a one-time answer, not a retrievable secret. Lost
  delivery requires authenticated replacement: revoke the unanswered
  issuance and its tokens, then issue anew. It never retries by
  returning a stored private key. Registration retries recover the
  public record and direct the person to replacement, never create a
  second operator or quietly replace a working key.
- Initial key issuance is authorized with registration. Adding,
  replacing or revoking a key on an existing operator uses
  `operator:reissue`, checked from current organization authority.
  Deletion retains `operator:delete`; existing definitions may still
  prevent it. Authentication never replaces current policy, delegated
  operation authority, controller proof or assignment checks.
- Local generation with authenticated public-key enrollment is safer
  for private-key exposure and was recommended to the PM. It adds key
  generation/upload and proof-of-possession steps to the person's
  initial workflow. The PM chose the owner's issued-PEM workflow for
  initial support; local generation is a deferred alternative, not a
  second silently introduced UX. If adopted later, fingerprint
  confirmation and proof of possession must precede enabling its key.
- The controller and hosted control service keep separate operator
  records and keys. An operator key is not a client certificate: PEM
  is encoding, and no X.509 chain, CSR or client TLS handshake proves
  this identity. Server certificate verification remains required.

### Assertion exchange profile

This new profile is `machine-token.v2`, not an edit to the deployed
`machine-token.v1` shared-secret exchange. Implement and publish it in
`api/machine.yaml` before a caller selects it. Its endpoint is
`POST /oauth/token` on the machine listener, over verified server TLS
with no client certificate and no redirects. The configured absolute
HTTPS token endpoint URL is also the one accepted assertion audience;
Garam publishes that exact URL with registration/key issuance. It is
independent of ADR-0101's message issuer, which need not resolve.

Use `application/x-www-form-urlencoded`, with each parameter once:

| Parameter | Value |
|---|---|
| `grant_type` | `client_credentials` |
| `client_id` | canonical registered operator GRN |
| `client_assertion_type` | `urn:ietf:params:oauth:client-assertion-type:jwt-bearer` |
| `client_assertion` | one compact signed JWT, never the private PEM |
| `audience` | optional Garam extension: another registered operator GRN in the same organization; absent means Garam API access |

No `assertion` authorization grant, Basic/shared-secret authentication,
refresh token or caller-supplied scope is accepted in this profile.
The optional token audience is distinct from the JWT's `aud`, which
always names the exchange endpoint. Unsupported audience or scope never
silently broadens authority. A normal OAuth library supplies the four
standard parameters; the audience extension is Garam-specific.

| JWT member | Required value and check |
|---|---|
| Protected `alg` | RS256, exactly the algorithm recorded for this key |
| Protected `kid` | base64url SHA-256 RFC 7638 public JWK thumbprint; lookup only within this client registration |
| Protected `typ` | `JWT`; message JWTs have a different type and key namespace |
| `iss`, `sub` | both the exact `client_id`; the key belongs to that current registration |
| `aud` | one string equal byte for byte to the configured token endpoint URL; no array, alias or origin-only substitute |
| `iat`, `exp` | integer NumericDates with `0 < exp - iat <= 60` seconds; default client lifetime 60 seconds |
| `jti` | a fresh base64url identifier with at least 128 random bits, bounded to 128 characters |
| `nbf` | optional integer NumericDate, no later than `exp`, checked if supplied |

Both sides implement RS256 as RFC 7523 requires, accepting RSA keys
from 2048 through 4096 bits. Issued keys use RS256 and 3072 bits.
P-256/ES256 has smaller keys and signatures but selecting it alone
would omit the RFC 7523 interoperability floor. The initial exchange
allows RS256 alone; adopting ES256 is a later profile change, separate
from ADR-0101's already-decided ES256 message signatures. Reject `none`,
MAC algorithms and any algorithm/key mismatch. Use a stock JOSE
implementation, not a new proprietary signature format. Public JWKs
carry `use: sig` and the registered `alg`; private members are never
accepted in key enrollment.

Reject duplicate JSON members, malformed claims and unrecognized
critical headers. Accept only `alg`, `kid`, `typ` in the protected
header; no caller-supplied `jwk`, `jku`, `x5u` or certificate chain can
choose verification material. Other claims convey no authorization.
At the server's database decision time, allow at most 30 seconds of
clock skew: require `iat <= now + 30`, `exp > now - 30` and, if present,
`nbf <= now + 30`. The lifetime bound is checked independently of skew.
These tighter lifetime, audience and replay requirements are this
profile's choices, not requirements RFC 7523 imposes on every client.

### Replay, rotation and revocation

- After signature/claims verification, consume `(client_id, jti)` and
  issue the access-token row atomically, rereading the current key and
  registration under the same serialization as key revocation. All
  replicas share this replay store; a restart never clears it. Keep
  used identifiers until `exp + 30 seconds`. A changed `kid` or a
  different signature never bypasses reuse of the same identifier.
- Two concurrent valid copies yield at most one successful exchange.
  A duplicate is `invalid_client`, not a replay of the bearer answer.
  If the answer is lost, use a fresh assertion and `jti`; the earlier
  token may exist until expiry. Never retry an assertion automatically
  after timeout. Existing live-token bounds still apply per operator.
- Key/replay/store unavailability issues no token and returns 503;
  unsupported profile or malformed parameters is 400. Use OAuth error
  bodies: `invalid_request`, `unsupported_grant_type`, `invalid_scope`
  as appropriate, and 400 `invalid_client` for bad signature, wrong
  identity/audience/time, unknown/revoked key or replay, without
  revealing registration existence. Rate-limit exchanges and key
  issuance before release with a documented bound; saturation answers
  429 with `Retry-After`, never falls back to a shared secret.
- An expired or stale short-lived access token pauses protected calls,
  not the ability to authenticate with an unchanged valid private key.
  Retry exchange with bounded backoff and a fresh assertion and `jti`
  on every attempt, including after an outage spanning access expiry.
  On success, obtain fresh controller/assignment proof before resuming
  decisions. An adapter instead waits for its assignee's refreshed
  atomic agent-token file and rechecks current execution binding;
  it never exchanges an agent token for its successor. Exchange
  rejection of identity/key requires a credential or registration/
  configuration change; refreshing access cannot clear that rejection
  or a generation/lineage/placement fence. A successful mint/exchange
  alone is never evidence the writer or its authorization is current,
  and neither same-`jti` retry nor shared-secret fallback is allowed.
- Planned rotation allows two active keys per operator. An authorized
  person issues the successor, confirms its fingerprint, installs it
  atomically and proves an exchange before withdrawing the old key.
  Keep the old key for a declared maximum 24-hour rollout window; then
  revoke it and its tokens. A third live key is refused until one is
  withdrawn. Confirm every replica/process has moved, not just one.
- Tokens record their issuing key and registration incarnation. Key
  withdrawal invalidates that key's tokens on the next request,
  including on open connections; deletion invalidates every key and
  token. A reused operator_id cannot revive an earlier incarnation.
  Check the live key binding on each token read, not just at exchange.
- Compromise or lost delivery uses immediate replacement with no
  overlap. Revoke the old key and its tokens atomically before
  enabling the new one. A race cannot mint a token under a key already
  revoked; protected execution writes retain their existing lock.
  An agent token previously minted by an operator still has its own
  assignment/lineage: key replacement alone does not recover agents.
  Incident response must separately end affected agent lineages.

Return 200 with OAuth `access_token`, `token_type: Bearer`,
`expires_in: 3600`, `Cache-Control: no-store` and `Pragma: no-cache`.
The access token remains opaque and hash-stored; the assertion is not
an access token and asymmetric exchange does not sender-constrain the
bearer. Add `Authorization: Bearer` as an explicit alias for the same
opaque token on token-enabled machine routes, preserving
`Garam-Token` and the v2 fences/headers. The alias is unbuilt and must
be covered by route/version tests before OAuth clients use it. Audience
tokens remain accepted only by their named control service and Garam's
introspection on its behalf. Changing exchange authentication does not
re-sign or change the payloads of those downstream contracts.

### Common agent enrollment

The new `agent-enrollment.v1` is one semantic contract for both
placements. It is unbuilt and must get its published wire form and
refusal vocabulary in the Garam implementation issue; old enrollment
values remain served unchanged. It comprises approval, admission and
credential delivery, independently of creating a workload:

1. **Prepare authority.** Bind an authenticated approval to one
   organization, request identifier, intended agent (new or existing),
   assignee/lifecycle authority and placement. Managed admission uses
   the hosted service's current grant, controller proof and member's
   `agent:create` reference. Direct admission uses an administrator's
   current `agent:enroll` approval and a one-agent authority using the
   supervisor registration/authorization shape. An
   expiring single-use bootstrap capability may deliver that approval
   to the host; its digest and the complete binding are stored, not
   its cleartext. A copied approval cannot choose another authority.
2. **Admit identity.** Under the execution lock, reread approval,
   registration, permissions and expected assignment. Allocate one GRN
   only for new enrollment; for replacement keep the GRN, require the
   expected assignee and epoch, raise the epoch and replace the lineage.
   Both placements start fenced, with no active generation. Record the
   request and exact binding with the public answer. Admission creates
   no Pod, process, memory, volume or message-channel permission.
3. **Deliver scoped credentials.** The registered assignee mints the
   same short-lived Garam agent token in the current epoch and lineage.
   Managed placement also supplies a token for its control service;
   direct placement's supervisor remains scoped to its one agent and
   uses the existing supervisor exchange. A supervisor credential's
   issuance/replacement is an authority-bootstrap step, never an
   operator key given to the runtime. This ADR changes no supervisor
   credential algorithm; migrating that exchange needs its own scope.
4. **Activate the runtime.** Its connector asks the authorized
   lifecycle authority to activate the runtime-reported generation and
   revision with the current placement. The authority proves exclusive
   writer ownership first. Credential delivery, enrollment and status
   reports never activate by themselves. Every protected call carries
   the separate execution token; replacement and writes share
   ADR-0080's execution lock.

An identical enrollment retry returns the recorded public binding,
never a second GRN, new placement or fresh activation. Another binding
under the same request identifier conflicts. A superseded assignment,
revoked authority or spent approval reused for another intent refuses;
an unavailable check is not success. After a lost bearer delivery the
assignee mints again in the same lineage. Existing standalone v2
bootstrap retries still re-issue the supervisor credential and void
its tokens; that behavior is not silently applied to common public
admission retries. The new wire must distinguish those operations.

“Direct” means a runtime provisioned independently of the Operator;
it still needs a scoped lifecycle authority/connector to establish
exclusive writer ownership and refresh API access. It does not mean
an unauthenticated runtime can register itself or activate a writer.
An already-running Sherlock can connect through a narrowly scoped
connector bound to that supervisor identity model; it need not be
recreated, wrapped in the Docker three-container runner or placed in
Kubernetes. The existing standalone supervisor is one implementation
of the authority, not a prerequisite for admission. The direct
connector must observe its writer and stop protected calls if that
ownership cannot be established; “direct” never bypasses the fence.
No new
organization-wide operator privilege is manufactured for a direct
agent, and an agent bearer cannot mint its successor or enroll peers.

### Runtime and messaging trust

- Runtime ownership includes its stable identity, generation/revision
  reports, memory, inbox/outbox replay state, configured trusted
  message issuers and authenticated message origin. A connector owns
  delivery and its narrow API access; a lifecycle authority owns
  provisioning, placement, token refresh and writer replacement.
  Hosting these on one machine never merges their authority.
- Sherlock's incoming message verification stays ADR-0101's. Garam
  retains its message-signing private key; the runtime receives public
  issuer trust, not that key or an Operator long-lived key. Garam API
  credentials prove the runtime/connector to Garam; issuer keys prove
  messages to Sherlock. Neither enrollment nor private_key_jwt grants
  command admission or makes content claiming a sender trustworthy.
- The Sherlock PM's compatibility reply checked
  `garamsh/sherlock@2281328366515c1c67a72bdf978c2266b842cdec`:
  ADR-0026 and `signature.go` already own configured issuer
  verification and authenticated Origin. Its ADR-0014 leaves
  per-caller authorization above Sherlock. The PM retains
  that current authorization boundary (Option A of
  [proposal #1067](https://github.com/garamsh/sherlock/issues/1067)).
  It compares channel-admission cases; runtime-local sender/channel
  authorization (Option B) requires a concrete gap and an owner
  decision, not an implementation prerequisite for Operator exchange.
  Garam's organization permissions remain Garam's. No SimpleX adapter,
  terminal-attach surface or signing of agent replies is decided here.

### Direct attachment proof remains an implementation gate

The owner approved attaching an independently running Sherlock, not a
new lease protocol. Current `internal/workload` writer proof rests on
recorded rootless Docker container IDs/nonces and observed exit; it
cannot establish ownership of an arbitrary independently started
process. Runtime status alone reports a generation and does not prove
exclusive writer ownership. Direct admission can bind identity, but
its connector must not activate or make protected calls until the
missing proof contract is approved and implemented.

The recommendation sent to the PM is an authenticated local attachment
binding plus a host-owned exclusive lease for the agent/generation,
with replacement requiring observed predecessor termination or an
explicit authorized lineage recovery. Another option is to restrict
direct attachment to a host supervisor that can observe and exclusively
own the existing process, without reprovisioning it. Neither option is
selected: lease expiry alone would not prove an old process stopped,
and recovery fences Garam calls without proving local writers exited.
The owner and both project PMs must compare actual host/process proof,
restart, disconnect, stale-lease and replacement cases and select the
contract. An unverifiable writer is a refusal, never a guessed exit.

The PM kept these details as a blocking gate for direct-attachment
implementation on 2026-10-13. It does not block independently scoped
Operator private-key exchange or existing managed/supervisor paths.
The common identity/approval/fence semantics above remain the target;
no lease, direct connector or new command authorization is claimed to
be built or accepted by this ADR.

### Supersession and compatibility

On acceptance, ADR-0100 is superseded only for Operator long-lived
shared-secret issuance/exchange and its separate target enrollment
model. Its server TLS, short-lived opaque tokens, audience boundaries,
refresh overlap, assignment/generation/lineage fences, supervisor
credential behavior and v1/v2 coexistence survive. ADR-0101 stays
accepted without supersession; message key custody is unchanged.

| Contract | Treatment |
|---|---|
| `machine-token.v1` | Keep shared-secret operator/supervisor exchange and agent mint for existing callers |
| `machine-token.v2` | Add private_key_jwt Operator exchange and OAuth response on the new endpoint |
| `managed-enrollment.v1`, `.v2` | Keep certificate/token creation cohorts while common enrollment is added |
| `standalone-enrollment.v1`, `.v2` | Keep fingerprint-bound certificate and keyless credential approval cohorts; no cross-spending |
| `agent-enrollment.v1` | New common admission, with placement and authority rather than two identity models |
| `operation-authority.v1`, `.v2` | Preserve current proof and authorization; an asymmetric exchange still obtains an ordinary operator token |
| `execution-fence.v1`, `.v2`, `agent-cutover.v1`, `.v2` | Preserve generation, lineage, legacy cutover and lock rules |
| `agent-execution.v1`, `.v2` | Keep control transport cohorts; G2 must implement the token path without weakening placement |
| `durable-delivery.v1`, `.v2`, `garam-message.v1` | Keep durable identifiers/budgets and signed message content unchanged |

The Operator PM's response checked
`garamsh/garam-agent-operator@4d2cbc6ca37668b912c4c0f0ae211c11c90bf5f3`
([compatibility reply](https://github.com/garamsh/garam/issues/1353#issuecomment-6146268863)).
Both the manager and control service exchange their own distinct keys
at Garam. Manager-to-control authentication uses an operator access
token minted for that control service and fresh controller
introspection; no private key or assertion is forwarded there. Before
every release/status/credential/placement decision, including after a
long poll, the service obtains a fresh agent-bound assignment proof
and compares the revision's epoch. Access-token validity alone proves
none of that. Server TLS and `server-root.pem` survive; the adapter's
TLS and Sherlock's `keys-ca-file` consumers move atomically if their
mount changes. An assertion lasts at most 60 seconds, an access token
one hour, a placement token its placement, and a private key until
revocation; refreshing one does not replace the others.

Garam ships additive support first. Operator ADR-0068 forbids keeping
development compatibility paths: its coordinated cutover release has
one authentication mode, after Garam supports the profile, and adds no
dual-auth switch. Old/new deployment cohorts coexist at Garam, not
inside a new Operator binary. Existing cohorts continue using
their explicit contracts; a new client facing an unsupported profile
fails by name rather than falling back after an authentication refusal.
No tested adjacent-release window is claimed at the checked revision.
Before advertising one, test both upgrade orders with the exact Garam,
Operator and Sherlock revisions/digests recorded. Removal is later
work, gated by inventory, successful refresh/rotation/restart evidence
and seven days without old authentication for every known cohort,
including idle operators; a traffic metric alone proves nothing about
an idle registered caller. The existing plan carries that sequence.

## Consequences

- Operator assertion keys need replay state and explicit rotation;
  public-key storage alone reduces stored long-lived secret exposure
  but does not remove bearer theft or private-key custody duties.
- Issued PEM reduces initial setup steps while putting private-key
  bytes through Garam and the person's download path. Local generation
  would reduce that exposure; it is deferred by the PM's UX choice.
- RSA/RS256 is the interoperability floor and the initial exchange
  algorithm; message signing retains its separate ES256/P-256 profile.
  No new certificate authority is needed for an Operator key.
- Garam/Operator/Sherlock implementations and acceptance remain
  separate issues. #1352's G2 must follow this direction. G3/G4,
  deployments and destruction are outside #1353; gitops#14 and
  infra#331 remain owner gates. Message signing still needs its sealed
  keys, so retiring certificate authorities does not remove that
  key-encryption requirement.

## Signs the decision was wrong

- A stock OAuth/JOSE client cannot exchange an issued RSA key without
  a custom signing format or parameter substitution.
- A replay succeeds on another replica or after restart, or revoking
  a key leaves its access tokens usable on an open connection.
- Planned key rotation causes an outage inside the declared overlap,
  or issued private-key bytes appear in storage, logs or a runtime.
- Managed and direct admission yield different identity/fence rules,
  or enrollment retry creates another GRN, lineage or writer.
- A valid API token bypasses message signature verification or command
  admission, or an enrollment path installs a signing private key
  into Sherlock.
