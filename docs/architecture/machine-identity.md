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

- **The controller proof** ([operators.md](./operators.md) §Controller
  proof) asks `Recheck` of the caller's identity — its credential's
  dates against the clock, then its record — and `Verify` of the
  controller certificate the caller forwards, which proves the identity
  it is presented for by its SAN, by its organization's authority, by
  its validity at the decision and by its record. A controller token
  forwarded under `operation-authority.v2` is the operator domain's to
  prove instead. The chain is verified
  at a time inside the certificate's validity and the validity at the
  decision is checked apart from it, so a certificate nothing signed
  and one that has run out are told apart.
- **The execution lifecycle routes** ask `Recheck` of the caller the
  same way, and `Prove` of the agent certificate it forwards. That
  proof establishes only that the certificate names the agent and
  chains to its organization's authority; whether it is valid now and
  in the current lineage is the execution fence's to judge (§Generation
  and lineage), so an expired agent certificate is answered as fenced
  rather than as unproven. An agent token forwarded under
  `execution-fence.v2` is proven the same way by the agent domain, and
  judged by the same fence.
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

**Unbuilt.** [ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
replaces the target Operator shared-secret exchange with signed
client assertions. The current certificate admission and G1 token
paths below remain the deployed compatibility cohorts; acceptance of
the new decision does not make the new exchange available.

- `internal/machine/listener.go` §token admission and
  `api/machine.yaml` §token are authoritative for current admission.
  The listener currently reads `Garam-Token`, not OAuth `Bearer`.
- The new `machine-token.v2` exchange proves the existing operator
  registration using its bound public key; it issues an opaque token,
  never treats an assertion as an API credential, and verifies server
  TLS without a client certificate. ADR-0102 §Assertion exchange
  profile and §Replay, rotation and revocation define its wire,
  RSA algorithm, endpoint audience, time bounds and shared replay rule.
- Token reads must also join the live issuing key and registration
  incarnation. Revocation ends that key's tokens on the next request
  on an open connection. Failure to read current state refuses rather
  than extending authority from a cached proof.
- The planned `Bearer` alias uses the same token resolver, route
  confinement and v2 contract selection as `Garam-Token`. It must not
  fall through to certificate admission when malformed or coexist
  with a client certificate or another authentication credential.
- The agent's API token, its execution fence and Sherlock's message
  issuer verification remain distinct. Common enrollment is
  [agents.md](./agents.md) §Common managed and direct enrollment;
  message verification is [messaging.md](./messaging.md).

## Tokens

**Built for an operator, an agent and a standalone host's
supervisor.** ADR-0100 moves every machine caller from a client
certificate to a short-lived token it obtains with a credential garam
issued, and `docs/plans/2026-10-09-machine-identity-migration.md` is
the order it lands in. What runs now is its step G1, the four pull
requests G1a to G1d; a certificate still authenticates beside a token
until G3.

- **A request may present a token instead of a certificate**, as
  `Authorization: Garam-Token <token>`. The listener asks `Tokens` for
  that request, and the identity it answers carries `MethodToken`, the
  GRN and the token's dates. An agent's token carries a serial too —
  `t` and hex, which no certificate's decimal serial equals — and the
  execution fence reads it as it reads a certificate's: current while
  the token is inside its dates, minted at the agent's present
  assignee and epoch, and in the lineage the execution holds now. An
  operator's token carries none, so a route reading a credential
  refuses it, and no token reaches a route that renews a certificate.
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
  that proved nothing, so the handler's refusal says why.
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
(ADR-0085) and the Operator control service does not yet; acceptance
with either, joined to garam and Sherlock, is unverified (#1157,
#1180). Until a
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
theirs and `managed-enrollment.v1` and `.v2`.
Each request rereads the caller's credential and registration; each
but creation rereads its grant, or a supervisor's registration and
assignment, and current policy too, and creation rereads the member's
`agent:create` reference instead. An agent's own certificate and a
controller's are refused.
Activation, deactivation, recovery, creation and initial issuance are
each bounded by a ten-second server deadline over both their authority
reads and their write, and past it answer unavailable without saying
whether the write committed (ADR-0080). Introspection answers a
forwarded credential's standing and a generation's from one snapshot;
a 200 is a status answer, and only `credential` and `generation` both
`current`, with the expected binding, is positive. The credential's
GRN still answers who is calling; these answer whether that caller is
still the one allowed to act.
