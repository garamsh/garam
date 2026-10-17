# API Surface

What garam serves to a caller outside it: which listeners exist, what admits a caller to each, and what the file describing each one is authoritative for.

## Contents
- What it is
- The surfaces
- Authentication and enrollment profiles
- What admits a caller
- What decides the rest
- What a specification is authoritative for
- What nothing checks
- Where a change to a surface lands
- Not decided
- Decisions this rests on

## What it is

garam answers on more than one listener, and which one a caller reaches
decides what authenticates it, what authorizes it, and which file says
what it may ask for. The split follows what a caller reaches and
authenticates as rather than what a process contains
([ADR-0019](./adr/0019-machine-openapi-spec.md)). That split is what
this document holds.

It describes no operation. `api/openapi.yaml` and `api/machine.yaml`
are the source of truth for those
([ADR-0004](./adr/0004-openapi-first-api.md), ADR-0019), the chart at
`deploy/charts/garam/` is authoritative for the address each process
listens on and for what may stand in front of it
([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)),
and a fact one of them already answers is named here rather than
repeated. A second copy is a second thing to keep true.

## The surfaces

| Surface | Served by | Described by | A caller reaches it as |
|---|---|---|---|
| Browser-facing HTTP | `garam serve api` | `api/openapi.yaml` | a signed-in person, through the web app's same-origin `/api/*` rewrite (ADR-0009) |
| Machine | `garam serve machine` | `api/machine.yaml` | an operator or an agent holding a certificate garam issued (ADR-0014, ADR-0015), or an operator presenting a token it exchanged its credential for (ADR-0100) |
| Policy engine | `garam serve authz` | `proto/garam/authz/v1` | one of the two listeners above, over gRPC (ADR-0003) |
| Probes | every serve command, and the web app | no specification | anything that can open the port (ADR-0021, ADR-0023) |
| Metrics | `garam serve api` and `garam serve authz`, on the listener each already runs, and `garam serve machine`, on its probe port | no specification | anything that can open the port (§Not decided) |

The first two are separate processes
([ADR-0017](./adr/0017-machine-process.md)), and neither may call the
other. A watch spans them both — opened on the browser-facing listener,
fed on the machine one — and Postgres is the crossing rather than a
request between them
([ADR-0027](./adr/0027-a-watch-crosses-processes-through-postgres.md)).

No garam process dials a caller. An agent is reached through a queue it
polls, never by garam opening a connection to it
([ADR-0012](./adr/0012-agent-operator-model.md),
[ADR-0013](./adr/0013-agent-messaging.md)), which is why every rule
below is about what garam admits rather than about what it reaches.

## Authentication and enrollment profiles

**Built in part.** [ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
adds a standard client-assertion exchange and a common agent admission
contract. The exchange, the OAuth `Bearer` alias and key issuance are
served; `agent-enrollment.v1` is not, and the managed and standalone
v1/v2 paths are what admit an agent.

- `machine-token.v2` is `exchangeClientAssertion`, `POST /oauth/token`
  on the machine listener: RFC 7523's client assertion parameters, the
  client credentials grant and RFC 6749's token and error responses.
  `api/machine.yaml` §exchangeClientAssertion is authoritative for its
  wire. `machine-token.v1`'s exchange and mint are unchanged.
- The `Bearer` alias resolves the same opaque access tokens as
  `Garam-Token`, with the same route confinement, per-request reads, v2
  contract selection and refusal of combined credentials
  (`machine-identity.md` §Asymmetric Operator exchange). It changes no
  execution or operation
  header and reaches no certificate renewal route.
- `issueOperatorKey`, `listOperatorKeys` and `revokeOperatorKey` on
  the browser-facing listener answer a private PEM once and public
  metadata thereafter; no secret is read back
  ([operators.md](./operators.md) §Issued PEM identity).
- **`agent-enrollment.v1` is unbuilt.** It adds one admission contract,
  whose managed and direct cases differ by authorized placement; it
  allocates no workload and activates nothing. The current
  managed/standalone v1/v2 paths keep their approval bindings, retry
  behavior and refusal semantics.

[agents.md](./agents.md) §Common managed and direct enrollment owns
admission; [operators.md](./operators.md) §Issued PEM identity owns
registration/key custody. Message issuer publication stays ADR-0101's.

## What admits a caller

**Browser-facing.** The `garam_session` cookie, which is a server-side
row keyed by the token's hash
([ADR-0005](./adr/0005-server-side-sessions.md)). Signing in mints it,
either through a redirect provider — Google is the only one left
([ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md))
— or against a password garam verifies itself, which is an endpoint
rather than a provider
([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
Starting a login, by `GET` or by the `POST` that names where it lands,
its callback, the provider list, reading an invitation, registering
an account from one, and signing in with a password carry no
credential, as do the probes and metrics this
process serves from the same router. Which operations those are is
read from `api/openapi.yaml` rather than remembered: an operation
carrying `security: []` opts out of the cookie requirement the file
applies to every other one. The login write rate is bounded at the store
([ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)).
A callback that finds no live record of the login it claims to
complete is refused as unauthenticated
([ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md)).
Reading an invitation (`GET /invitations/{token}`) admits on the
token alone, because possession of it is the whole claim
([ADR-0006](./adr/0006-invitation-tokens.md)). It answers the
organization's identifier and name, the role and whether the
invitation is pending, accepted or expired, which is what accepting
it would reveal, and nothing about who invited or who belongs. An
unknown token, and one whose organization is gone, answer the
not-found body the accept operation answers. The request line, and
the `url.path` of the request's trace span, name the route pattern
rather than the path, so the token reaches neither the log nor an
exported span.

**Machine.** A client certificate whose SAN URI holds the caller's GRN
and nothing else — no roles and no claims
([ADR-0014](./adr/0014-agent-authentication.md)). The chain is verified
per connection against the authority the certificate's own organization
names, never against a pool holding every organization's, and both ends
read their certificate per handshake, so a replaced one is presented on
the next connection with nothing restarted
([ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)).
The controller proof is the one route that verifies the caller's
certificate and record again on every request
([machine-identity.md](./machine-identity.md)).
Admission reads that certificate and nothing else: no session and no
user row exist on this listener, and nothing about the caller is looked
up to admit it. What an admitted caller may do is decided after that
(§What decides the rest). An operator reaches the listener on the same
terms an agent does
([ADR-0015](./adr/0015-operator-identity.md)).

**Or a token, which is ADR-0100's direction and is built for an operator
and an agent its operator holds.** An operator exchanges the credential
its organization issued it for a one-hour token at `exchangeToken`, or
a client assertion its key signed for the same token at
`exchangeClientAssertion`, and presents the token as `Authorization:
Garam-Token <token>`, or `Bearer <token>`, with no certificate
([ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)).
The token is read on every request, beside the registration it rests on,
so a deleted operator or a re-issued credential is refused from the next
request on a connection already open; a request presenting a token
beside a certificate is refused rather than read as either. A token
reaches only the operations `api/machine.yaml` lists `machineToken` on —
`ConfineTokens` in `internal/machineapi/token.go` refuses it everywhere
else — and one minted for another audience reaches none. An agent's
token is minted by its operator at `mintAgentToken` and read against the
agent's current assignment and lineage, and beside it a fenced agent's
execution token travels in a `Garam-Execution` header of its own; a
durable submission it authenticates is served under
`durable-delivery.v2`. Every contract below whose transport ADR-0100
changes is served to a token under its v2 value and to a certificate
under v1, and a request whose value disagrees with its credential is
refused naming the value the credential takes; beside a token an
operation authority or reference travels in a `Garam-Operation`
header, since the token takes `Authorization`. A standalone host's
supervisor exchanges the credential its v2 enrollment answered for a
token on the same `exchangeToken`, prefixed `garam_st_`, and
`ConfineSupervisors` bounds its token as it bounds its certificate.

**The listener requests a client certificate and does not require
one** — `tls.RequestClientCert` in `internal/machine/listener.go` — so
a handshake completes for a caller that presents none at all. It has
to: an operator enrolling holds no certificate yet, so the route
serving `POST /enrollment` cannot demand from that caller the thing
enrolling exists to issue
([ADR-0064](./adr/0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)).
A connection whose certificate does not authenticate is not refused
there either; it carries no identity instead, and every operation but
five refuses a request that carries none. The identity a handler holds
is the one the handshake's own verification returned, so nothing
derives it a second time and a certificate naming any GRN under a
chain nothing signed reaches no handler that asks for one. Which
operations opt out is read from `api/machine.yaml` rather than
remembered: `enrollOperator` and `enrollStandaloneAgent` carry
`security: []`, as an operation on the browser-facing listener does,
and each is authenticated by what it is spent with instead.
`getMessageSigningKeys` carries it too and is authenticated by nothing,
because what it answers is public: the keys a verifier anywhere checks
garam's signature on a message with
([ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)),
read at the issuer followed by `/message-signing-keys`.
`exchangeClientAssertion` carries it as well, and is authenticated by
the client assertion in its body, refusing a certificate or an
`Authorization` header beside it
([ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
`exchangeToken` carries `machineCredential`, and is authenticated by
the operator's credential.

Why a connection did not authenticate is written to the listener's own
log and nowhere else: what crosses the wire says only that no identity
was resolved, and nothing about which organizations exist. A connection
presenting no certificate is the enrollment path above and is recorded
as ordinary traffic; one presenting a certificate that did not
authenticate is recorded as an anomaly.

**Each handler makes that refusal itself, and a test is what makes it
true of every route.** `internal/machineapi/identity_test.go`, in
`TestServer_EnrollmentsAndTheKeySetAreTheOnlyRoutesAnUncertifiedCallerReaches`,
is authoritative for that: every operation `api/machine.yaml` declares
except `POST /enrollment`, `POST /agent-enrollments`,
`GET /message-signing-keys`, `POST /tokens` and `POST /oauth/token`
answers `401` naming the missing identity to a caller carrying none,
sent from the same client that enrols, so what admitted those five was
the route rather than a listener that had stopped checking.
The routes it sends to are derived and not listed — read off the
registration `make gen` writes from that specification, and held
against the operation count of the generated interface — so an
operation added there arrives in the test with no case added by hand,
and one added without the guard fails it. That derivation is the whole
of its value: read as a fixture the test is a handful of paths someone
chose, and a route added unauthenticated would read as one nobody
thought to cover rather than as one this already covers.

**One listener mints two kinds of first certificate without a
certificate.** An operator's is obtained by enrolling: registering it
on the browser-facing listener mints a one-time token, the operator
generates its key pair in its own cluster, and `enrollOperator` signs
the request that presents the token and spends it (ADR-0064). A
standalone host's supervisor and agent obtain theirs together with
`enrollStandaloneAgent`, presenting the bootstrap secret whose hash an
administrator approved with `approveAgentEnrollment`, over the keys
whose fingerprints the approval names (ADR-0082). **What either
certificate names is the approval's or the token's to say and never
the request's**, and no private key is generated or answered.

**A token is minted on the browser-facing listener and nowhere else,
by two operations rather than one.** `registerOperator` mints the
first beside the record, and `mintOperatorEnrollment` mints another for
an operator that holds no certificate, which is what recovers one whose
token was lost or ran out before it was spent
([ADR-0067](./adr/0067-a-member-mints-a-second-enrollment-token-where-the-operator-holds-no-certificate.md)).
Both answer the token once and neither reads one back. The second is
authorized as the first is, on `operator:register`, and is refused for
an operator that already enrolled — with a reason, because whether a
member may issue a second credential path to an identity that has one
is not decided. No console page reaches it; #1036 is where that is
decided. The command ADR-0032 ships still mints out of band and is what
recovers the operator that already holds a certificate; it remains a
root-level subcommand no serve path references
([ADR-0032](./adr/0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)).

**An operator's credential is issued on the browser-facing listener
too, by two operations.** `registerOperator` issues the first in the
write that records the operator, and `reissueOperatorCredential`
issues the next, voiding the previous one and every token exchanged
for it (ADR-0100). Both answer the credential once and neither reads
one back. Re-issuing is authorized on `operator:reissue`, which owner
and admin hold, for the reason deleting has an action of its own: it
withdraws what an operator runs on. An operator registered before
credentials existed holds none, and re-issuing issues its first.

**`getOperator` and `updateOperator` read one operator and change
its name and description**, on `operator:read` and `operator:update`
([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)).
The identifier is the path's and not a field, so the update moves no
certificate, token or definition.

**`getAgentDefinition` and `renameAgentDefinition` read one definition
and change what the organization calls its agent**, on `agent:read`
and `agent:rename`. No operation writes a definition: `defineAgent`
is removed, and the schema refuses a new definition, a changed value
or a direct delete from any release
([ADR-0090](./adr/0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md)).
**The agent-template operations**
list and read an organization's templates on `agent-template:read`,
authorized against the template where the path names one
([ADR-0075](./adr/0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)).
No operation writes one: templates are retired read-only, and the
schema refuses a write from any release
([ADR-0088](./adr/0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)).

**`deleteOperator` is on the same listener and is the one operation
here that stops a machine identity.** It removes the organization's
record, which the machine listener reads per connection, so the
operator's certificate is refused from its next connection
([ADR-0070](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)).
It is authorized on `operator:delete` rather than on the action the
other two take, because registering grants nothing and this withdraws
what an operator runs on. It answers 204, and refuses with a reason
while an agent definition names the operator — nothing is deleted in
that case. No operation on `api/machine.yaml` deletes an operator: an
operator does not delete itself, and the one that would meet the
refusal is the one being deleted.

**Delegation and operation authority are versioned routes on both
listeners** (ADR-0078). The browser-facing listener grants, reads and
withdraws a hosted operator's delegation, sets and clears its control
endpoint, and mints a member's request-bound authority — for a read
or a template publication, bound to the exact request target as well
as the body (#1170), including `operator:read` for what the Operator
reports about the organization's operators (#1374); the machine
listener answers that authority
and its durable reference to the operator it names and no other, and
proves a controller's current authority to that operator before each
lifecycle decision. Each
requires and answers `Garam-Contract-Version: operation-authority.v1`
beside a certificate and `operation-authority.v2` beside a token, and a
request naming none or another is refused naming the one its
credential takes (ADR-0079, ADR-0100). The specifications are
authoritative for their forms, and
[operators.md](./operators.md) §Delegated operation authority for what
they decide.

**An agent's execution is a versioned contract of its own on the
machine listener** (ADR-0078). Activation, deactivation, introspection
and recovery each require and answer `Garam-Contract-Version:
execution-fence.v1` beside a certificate and `execution-fence.v2`
beside a token, and a request naming none or another is refused naming
the one its credential takes, not the operation authority's. An agent's
own protected calls carry `Authorization: Garam-Execution <token>` once
its GRN has cut over, under the `executionToken` scheme each declares.
`api/machine.yaml` is authoritative for both, and
[machine-identity.md](./machine-identity.md) §Generation and lineage
for what they decide. No route adds a 5xx of its own (ADR-0050): an
answer that cannot be decided from current state is the listener's
503.

**Standalone enrollment is a versioned contract on both listeners**
(ADR-0082). `approveAgentEnrollment`, `withdrawAgentEnrollment` and
`revokeAgentSupervisor` on the browser-facing listener are each
decided on `agent:enroll` from current state and carry no contract
version, as an operator's registration does not; on the machine
listener `enrollStandaloneAgent` and `renewSupervisorCertificate`
require and answer `Garam-Contract-Version: standalone-enrollment.v1`,
and `enrollStandaloneAgent` answers `standalone-enrollment.v2` as well,
the value the request names: an approval binding the host's keys is
spent under v1 alone and one binding none under v2 alone (ADR-0100). A
supervisor is admitted to those, to `getIdentity` and to the three
execution routes it holds authority on, and by its own token to its
agent's token mint as well, and refused on every other route at the
router before a handler runs. The
specifications are authoritative for their forms, and
[agents.md](./agents.md) §Standalone hosts for what they decide.

**Managed enrollment is a third versioned contract on the machine
listener** (ADR-0077, ADR-0078). Creating a managed agent and issuing
its first certificate each require and answer `Garam-Contract-Version:
managed-enrollment.v1`, and creation answers `managed-enrollment.v2`
to a token; a request naming none or another is refused naming the one
its credential takes. Initial issuance takes no token. Neither carries a
private key. The operator's bodyless `createAgent` and its
`issueAgentCertificate` stay as they were for legacy agents and reach no
managed one. `api/machine.yaml` is authoritative for the wire, and
[agents.md](./agents.md) §Separated execution for what they decide.

**Durable submission is a fourth versioned contract on the machine
listener**
([ADR-0083](./adr/0083-a-durable-reply-is-admitted-under-a-fixed-budget-from-its-creation-and-its-key-is-answered-after-it.md)).
`POST /outbox-messages` requires and answers `Garam-Contract-Version:
durable-delivery.v1` and is the message write `POST /messages` makes,
which is unchanged, with the entry's `createdAt` added: a new message
is stored only inside seven days of it on the database's clock, and a
key that already stores one is answered at any time. An adapter
branches on these answers, each carrying the header:

| Status | Kind | When |
|---|---|---|
| 201 | — | the message the key stores, stored now or earlier, or the cross-organization refusal |
| 400 | `invalid_argument` | no or another contract version, naming `durable-delivery.v1`; a malformed body, including a missing `createdAt` or one that is not RFC 3339 |
| 400 | `out_of_range` | `createdAt` later than the decision instant; the entry is kept and a later attempt may be admitted |
| 410 | `deadline_exceeded` | the key stores nothing and the budget has run out; no later attempt changes it |
| 409 | `already_exists` | the key stores a different message |
| 401, 403, 409 `failed_precondition` | as `POST /messages` | the execution fence's refusals (ADR-0080) |
| 404 | — | a garam without the route |

`api/machine.yaml` is authoritative for the wire, and
[messaging.md](./messaging.md) for what the budget decides.

**Cutover is a fifth versioned contract on the machine listener**
([ADR-0086](./adr/0086-a-legacy-agent-cuts-over-through-stages-garam-records-under-its-execution-lock.md)).
Reading a legacy agent's cutover and its freeze, switch and rollback
each require and answer `Garam-Contract-Version: agent-cutover.v1`
beside a certificate and `agent-cutover.v2` beside a token, and carry
an `agent:cutover` reference minted for that route's request target —
in `Authorization: Garam-Operation` beside a certificate, and in a
`Garam-Operation` header beside a token. A refusal there keeps `kind`
the errorx kind and names which one it is in the machine `Error`'s
`reason`, from the set the contract lists; `activateAgent` sets `reason`
only to `cutover_required`, and every other route and refusal leaves it
out. `api/machine.yaml` §cutover is authoritative for the wire, and
[agents.md](./agents.md) §Separated execution for what it decides.

Every certificate after the first is replaced over the connection the
previous one authenticated: an operator's on the machine listener
([ADR-0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md)),
an agent's on the route its claim admits it to (ADR-0040). A managed
agent's first is signed over its controller's key on the enrollment
route, and a supervisor renews its own on
`renewSupervisorCertificate`. The one exception is an agent's
recovery, which the hosted control service asks for under an
administrator's handoff and which replaces the agent's credential
lineage instead
([machine-identity.md](./machine-identity.md) §Generation and lineage).

**Policy engine.** Nothing. Both listeners hold a client of their own,
and the transport carries no credential because the processes run
inside one trust boundary
([ADR-0003](./adr/0003-single-cli-topology.md)). What keeps anything
else off that port is the deployment's, not garam's.

**Probes and metrics.** Nothing, by decision: a probe holds no
credential, so the answer is served where none is needed
([ADR-0021](./adr/0021-liveness-and-readiness.md)). A readiness failure
names the dependency and nothing more, because an error's own text
would otherwise reach a caller that proved nothing.

## What decides the rest

Admission says who is calling. What they may do is decided after it,
and not in the same place on every surface.

- **Browser-facing**: the policy engine decides every org-scoped
  request ([ADR-0002](./adr/0002-iam-style-policy-model.md)). How it
  decides is [authz.md](./authz.md).
- **Machine**: by construction more often than by policy. A read is
  scoped by the caller's own identity, and an operator's renewal route
  names no subject, so there is nothing to authorize past the handshake
  there
  ([ADR-0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md),
  ADR-0046). A state report is decided the same way and by a second
  thing besides: the assignment's epoch, which the route refuses a
  stale one of
  ([ADR-0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)).
  A sender reading its own message's state is scoped the same way from
  the other side — the caller is the sender the row is matched on, so a
  message it did not send answers as one that is not there
  ([ADR-0055](./adr/0055-a-sender-reads-the-state-of-a-message-it-sent.md)).
  An agent's protected calls, its own renewal among them, are also
  decided per request by its execution fence, and the lifecycle routes
  by the caller's current authority
  (`machine-identity.md` §Generation and lineage).
- **What a machine caller may send** is the exception, and the
  recipient's organization decides which of two things answers it. A
  recipient of the caller's own organization is a policy question: the
  engine is asked about that recipient
  ([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)). A
  recipient across the organization boundary is decided by the consent
  relation the two organizations hold, alone — the engine is not asked
  a cross-organization question at all
  ([ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)).
  Why no policy may reach past that boundary is
  [authz.md](./authz.md) §Policy attachment. A restriction meant to
  bear on a cross-organization send is therefore not something a policy
  can carry.
- **Asking the engine has a precondition of its own**: a caller may ask
  only about a subject belonging to the object's organization
  ([ADR-0025](./adr/0025-engine-scope-is-a-caller-obligation.md)). The
  caller is what establishes that membership and names the organization
  on the call; the engine holds no membership data of its own. What the
  engine now applies itself is the check on what it was handed — the
  organization named must be the object's, and a call naming none is
  refused
  ([ADR-0038](./adr/0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md)).
- **Watching an agent is its own grant**, separate from reading one
  ([ADR-0022](./adr/0022-watching-an-agent.md)).
- **Delegated operation authority is decided from current state on both
  listeners**: membership, roles and groups as read for the call, and
  the organization's persisted policies past the engine's cache, so an
  allow recorded earlier is never what a later check reads and a
  decision that cannot be made refuses. A controller proof is decided
  the same way for the hosted operator, on its groups and current
  policy beside the Allow its grant contributes
  ([ADR-0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)).
- **A read describing a write is decided on that write's action.**
  `getOrgDeletionPreview` answers what deleting an organization would
  destroy — a count per resource kind, and whether the organization
  holds a certificate authority — and is decided on `org:delete`
  rather than `org:read`. It reads and changes nothing, so `org:read`
  would be the shape to reach for; what that would publish is the size
  of an organization to every member who cannot delete it. Authorizing
  the description as the act keeps the two answers together: a caller
  who may not perform the delete is told nothing about it. No role
  gained an action for this, because the owner already holds `org:*`
  ([authz.md](./authz.md) §Statement collection).
- **A member may ask what it may do, and the answer decides nothing.**
  `checkPermissions` takes up to 50 pairs of an action and a resource
  of the caller's organization, the resource named by its kind and the
  identifier segment its own path takes. Every write is still decided
  when it arrives, so an answer is a hint the console renders by and
  not a grant
  ([ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md) §Role-aware UI).
  Each pair is answered allow, deny or unknown:
  - **allow or deny**, where enforcement is the engine's `Check` alone:
    the same statements and groups, put to the same `Check`. It never
    calls `Explain`, so no answer says why. A missing group or project
    answers as a denied one, so the answer tells no one what exists.
  - **deny or unknown** for an action a mint decides together with a
    delegation grant: `agent:create`, `agent:configure`,
    `agent:recover`, `agent:cutover`, `agent-template:publish`,
    `execution-profile:read` and `agent:execution-read`
    ([ADR-0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)).
    The pair is put to the check the mint runs, through the one
    function both call
    (`internal/operator/authority_service.go`, `AuthorizeOperation`).
    A deny there is the mint's refusal, so it is answered deny. An
    allow leaves the grant, which `checkPermissions` does not read, so
    it is answered unknown.
  - **allow or deny** for `operator:delegate`. It is put to the check
    every write of a delegation and of a control endpoint runs, through
    the one function they all call
    (`internal/operator/authority_service.go`, `AuthorizeDelegation`),
    and that check is all of their authority.
  - **unknown** for `agent:enroll`, whose enforcement reads current
    policy past the engine's cache and which this operation does not
    put to that read. A cached deny could hide a control the operation
    would allow.
  - **deny** for an action garam does not define, because a role's
    wildcard such as `org:*` would otherwise allow a name no operation
    checks.

  A defined action paired with a resource kind none of its operations
  address is refused with 400 naming the pair, and one pair the engine
  cannot decide fails the whole request rather than answering the rest.

One of ADR-0036's decisions is a commitment rather than a rule deciding
anything today: **no surface serves a secret's value**, garam's
listeners included. garam has no secret to serve — `secret` is a domain
it does not have yet ([domains.md](./domains.md) §Future domains) — so
what the commitment binds is the surface that serves that domain when it
arrives
([ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)).

## What a specification is authoritative for

One specification per listener, and a third listener repeats that
rather than reopening it (ADR-0019). `api/openapi.yaml` is the source
of truth for what `garam serve api` serves (ADR-0004);
`api/machine.yaml` for what `garam serve machine` serves (ADR-0019).

Each is also written for the caller its listener admits, and the two
are not held to agree: a value both files carry is described in each
for what that file's caller does with it, and each file defines every
member a caller of its own listener can meet
([ADR-0059](./adr/0059-the-two-specifications-prose-is-not-held-to-agree-and-each-defines-every-member-its-own-caller-meets.md)).
That prose lives in the two files rather than here: each
`info.description` names the caller its file is written for, and each
enum's `description` is authoritative for what its members mean to that
caller. Nothing compares one file with the other (§What nothing checks).

That authority is enforced rather than asserted. `make gen` writes
`api/server.gen.go` and `api/machine/server.gen.go` from the two
specifications, `internal/api` and `internal/machineapi` implement what
is generated, and `make check-gen` — which `make ci` runs — fails when
the committed output and the specifications disagree. A path, a
parameter or a model therefore cannot diverge from the file describing
it.

It is authority over the shape of what the listener serves, and not
over the whole of what a caller can meet. Each specification states two
limits on itself, in the file rather than here, because the reader they
address holds the specification and nothing under `docs/`:

- **A declared response set is not the complete set of statuses an
  operation can answer**, because a 5xx belongs to the listener rather
  than to a route
  ([ADR-0050](./adr/0050-a-specification-states-its-listeners-5xx-once-and-declares-none-per-operation.md)).
- **The `version` field promises nothing across versions**
  ([ADR-0049](./adr/0049-neither-published-specification-promises-anything-across-versions.md)),
  and the image tag of ADR-0039 names a build rather than either
  document. That stays true until a release carries a tested window
  for the contract a file defines: ADR-0079 accepts adjacent-release
  compatibility for the interservice contracts, unbuilt, and the pull
  request building that test changes the file's sentence.

What each limit covers is in the two files, named here and not copied.

A caller meets one more limit, which neither file states. **Both
listeners bound a request body at 1 MiB before anything decodes it**,
the figure the composition root holds as `maxRequestBody` in
`internal/cli/cli.go`. The browser listener answers a body past it
with 413 and its `Error`, kind `invalid_argument`, from the one decoder
every browser handler reads a body through. The machine listener
refuses it as an invalid argument with 400, because each of its
handlers decodes on its own. The bound is on the request rather than
a field, which is what leaves it standing where §What nothing checks
says a declared constraint does not.

**A refusal names its rule where the kind cannot.** The browser
listener's `Error` carries an optional `reason`, a stable code, where
one status and kind cover refusals a client must tell apart (#1380):
an invitation used against one that lapsed, and each rule a policy
document breaks, with `path`, a JSON Pointer into the document. The
codes are listed in `api/openapi.yaml` `Error.reason`; a client
branches on them and never on `message`, and one that does not know a
code reads the kind as before. A reason the engine gives crosses the
gRPC hop as an `ErrorInfo` status detail (`internal/wire/error.go`),
so the API answers the engine's code unchanged.

**A provider sign-in can land on a console path.** `POST
/auth/login/{provider}` starts the same flow as `GET` and takes `next`
in its form body. A path off the console is dropped by the rule the
console applies to the password sign-in (`internal/api/return_path.go`,
`web/src/lib/return-path.ts`), which bounds it in UTF-8 bytes and is
tested on both sides against one table,
`internal/api/testdata/return-path-vectors.json`. What is kept is held on the login's
`oauth_states` row, so it is spent with the state by the callback that
completes and expires with it. The callback redirects to it, checked
again, or to `/console`, and no answer repeats it. A cross-site form
can start such a login, and the most it decides is where inside the
console that login lands.

The other three surfaces are fixed differently. The engine's wire
contract is `proto/garam/authz/v1`, generated and checked the same way.
The probes have no specification at all: their paths, and the ports
they answer on, are interface rather than implementation, so changing
one supersedes the ADR that set it instead of editing a handler
(ADR-0021, [ADR-0023](./adr/0023-web-probes.md)). The metrics surface
is fixed by nothing at all (§Not decided).

## What nothing checks

**Generation copies prose and reads none of it.** Which prose it copies
differs by kind, measured on 2026-09-03 by perturbing one description
of each kind in each specification and running `make gen` and
`make web-gen`: a schema's `description` reaches `api/server.gen.go`,
`api/machine/server.gen.go` and `web/src/types/api.ts`, an operation's
reaches `web/src/types/api.ts` alone, and a tag's reaches none of the
three. So `make check-gen` and `make web-check-gen` fail on a schema
description edited without regenerating, and pass on any text at all:
each compares one specification against its own output, never one
against the other, and never prose against what the code does. A
decision that changes what an operation does leaves the text stating
the old behaviour with every check green.

That is not hypothetical. ADR-0035 changed what garam does with a
message it may not route. [domains.md](./domains.md) moved with it and
`api/machine.yaml` did not, in two places: `postMessage`'s description
stated the new behaviour as explicitly not what happens, corrected in
#530, and the `message` tag's description, corrected in #532. Neither
is a schema description and no TypeScript output is generated from that
file, so on the reach stated above neither moved a generated byte. Both
were found by someone running the service rather than by anything in
the repository.

**Nor does it cover which statuses an operation declares.** A response
is not part of what oapi-codegen writes: adding a `'500'` to one
operation and running `make gen` leaves `api/server.gen.go`
byte-identical, so `make check-gen` reads a response set in neither
direction — a status declared that cannot occur, and one that occurs
undeclared, are both invisible to it. #698 is the same fact in the
history: it changed four operations' response sets and moved no
generated Go file. What does move is `web/src/types/api.ts`, generated
from `api/openapi.yaml` and held against it by `make web-check-gen`, so
the browser client's types follow a response set while the server's do
not — and `api/machine.yaml` has no consumer of that kind at all. What
`make gen` writes from it is the generated Go
§What a specification is authoritative for names, which a response set
leaves as untouched as it leaves `api/server.gen.go`.

**Nor does it cover the two specifications against each other.**
`AgentProvisioningState` and `ProvisioningState` describe one stored
value under two names, as `AgentRuntimeState` and `RuntimeState` do. No
target pairs them, and ADR-0059 decides they are not held to agree in
any case. What is held is that each file defines every member a caller
of its own listener can meet, which is not a string relation between
two files: a reviewer is what checks it.

**Nor does it cover a constraint that is not a type.** The two
specifications carry 25 of them — `minimum`, `maximum`, `minLength`
and `maxLength` — and none reaches a running process: oapi-codegen
writes the field and no check, and neither listener installs a
request-validation middleware. What does bind is the type the field is
generated as, because the body is decoded into it exactly once and a
value that does not fit is refused there. So a bound a caller must not
cross is declared as the field's `format` where the bound is a width,
and otherwise held by the code that owns the rule.
`PolicyRollback.version` is both: `format: int32` is the width the
engine stores a version at, and `minimum: 1` is the engine's own rule,
which it refuses a version below (#865).

## Where a change to a surface lands

A decision that changes what admits a caller, what a caller may reach,
or what an operation answers is not landed when the handlers are. It
has four places to reach, guarded by different things:

- **The ADR** that records it.
- **This document**, where the surfaces and what admits a caller are
  the present state. "Decisions land in pairs" in
  [README.md](./README.md) pairs an ADR
  with the responsibility documents it affects, and this is the one the
  API has.
- **The specification for the listener it changed** — schema and prose
  both. The schema is guarded by `make check-gen`; the prose is guarded
  by nobody, which is the whole of §What nothing checks.
- **The responsibility document for the domain whose behaviour
  changed**, under the same rule.

The third is the one that has been missed twice. Reaching it depends on
an author having read this far, which is weaker than a check;
§Not decided says what closing that would take.

## Not decided

- **Whether "Decisions land in pairs" reaches a specification.** It
  names the
  responsibility documents and [README.md](./README.md) itself. This
  one is inside its reach; `api/openapi.yaml` and `api/machine.yaml`
  are neither and are not. Listing the API as a concern closes the
  half a document can close — an ADR that changes an endpoint now has
  a responsibility document to be paired with — and leaves the half
  only a rule can. What that rule should say is the index's to decide
  and not this document's.
- **What decides the metrics surface, on two processes of three.**
  `/metrics` is served by `garam serve api` and `garam serve authz`
  (§The surfaces) and decided by no ADR: every ADR that names it treats
  it as a surface that already exists. ADR-0014 and ADR-0017 name it as
  the shape a listener of its own follows, ADR-0021 puts the probes on
  the listener its metrics already use, and ADR-0030 counts it among
  the paths that write nothing — context and reasoning, never a
  decision. `garam serve machine` is the exception: it serves
  `/metrics` on its probe port because ADR-0100's migration plan gates
  removing the certificate path on a count of the listener's requests
  by how each authenticated, `garam_machine_requests_total`
  (`internal/cli/machine_token.go`). That decides that this process
  exposes the count, and nothing about what the other two expose.
- **Whether anything outside the cluster reaches any listener.** The
  chart renders four `ClusterIP` Services and refuses to offer a value
  that would expose one (ADR-0042), so exposure is the deployment's
  decision. What may stand in front of each listener differs between
  them, and `deploy/charts/garam/README.md` is where that is stated.

## Decisions this rests on

The decisions the state above is made of. An ADR that changes what a
caller reaches, what admits it, or what a listener answers belongs on
this list, which is the quickest test of whether a change is one this
document owes an update.

- [ADR-0002](./adr/0002-iam-style-policy-model.md) — policies decide
  every org-scoped request
- [ADR-0003](./adr/0003-single-cli-topology.md) — one binary, and the
  engine reached over gRPC inside the trust boundary
- [ADR-0004](./adr/0004-openapi-first-api.md) — `api/openapi.yaml` is
  the source of truth for what `serve api` serves
- [ADR-0005](./adr/0005-server-side-sessions.md) — a session is a row,
  and the cookie carries the token naming it
- [ADR-0009](./adr/0009-web-frontend.md) — the browser reaches the API
  through a same-origin `/api/*` rewrite, so the cookie needs no CORS
  layer
- [ADR-0012](./adr/0012-agent-operator-model.md) — garam never dials an
  agent
- [ADR-0013](./adr/0013-agent-messaging.md) — garam is a message API an
  adapter polls
- [ADR-0014](./adr/0014-agent-authentication.md) — a machine caller is
  a client certificate on a listener of its own, verified against the
  authority its own organization names
- [ADR-0015](./adr/0015-operator-identity.md) — an operator
  authenticates as an agent does, on the same listener
- [ADR-0017](./adr/0017-machine-process.md) — that listener is a
  process of its own, taking neither the browser API's proxy nor its
  cookie
- [ADR-0019](./adr/0019-machine-openapi-spec.md) — one specification
  per listener, and `api/machine.yaml` is the machine listener's
- [ADR-0021](./adr/0021-liveness-and-readiness.md) — the two probes
  answer where a caller needs no credential, and their paths are
  interface
- [ADR-0022](./adr/0022-watching-an-agent.md) — watching is a grant of
  its own
- [ADR-0023](./adr/0023-web-probes.md) — the web app answers the same
  two probes
- [ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md) — a
  policy decides what an agent may send
- [ADR-0025](./adr/0025-engine-scope-is-a-caller-obligation.md) — a
  caller asks the engine only inside the object's organization
- [ADR-0027](./adr/0027-a-watch-crosses-processes-through-postgres.md)
  — the two listeners cross through Postgres and never call each other
- [ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md) — a
  login's state is a record the callback spends
- [ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)
  — the login write rate is bounded at the store
- [ADR-0032](./adr/0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)
  — a first certificate comes from a command no serving process can
  reach
- [ADR-0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md)
  — the machine listener answers an operator its own definitions,
  scoped by the definition rather than by a policy
- [ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)
  — a cross-organization message needs both organizations' consent
- [ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)
  — no surface serves a secret's value
- [ADR-0038](./adr/0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md)
  — the engine checks the organization it was handed against the
  object's, and refuses a call naming none
- [ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
  — the machine listener presents a pinned garam server certificate,
  and both ends read theirs per handshake
- [ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
  — a password is garam's own credential, served as an endpoint rather
  than a provider
- [ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
  — the chart is authoritative for the addresses and for what may stand
  in front of them
- [ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  — the development-only sign-in provider is removed
- [ADR-0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md)
  — an operator renews its own certificate on the machine listener
- [ADR-0049](./adr/0049-neither-published-specification-promises-anything-across-versions.md)
  — neither specification promises anything across versions, and each
  says so in its own text
- [ADR-0050](./adr/0050-a-specification-states-its-listeners-5xx-once-and-declares-none-per-operation.md)
  — a specification states its listener's 5xx once and declares none
  per operation
- [ADR-0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)
  — an operator and an agent each report state on the machine listener,
  on a route of its own, fenced by the assignment's epoch
- [ADR-0055](./adr/0055-a-sender-reads-the-state-of-a-message-it-sent.md)
  — a sender reads the state of a message it sent, and garam announces
  nothing
- [ADR-0059](./adr/0059-the-two-specifications-prose-is-not-held-to-agree-and-each-defines-every-member-its-own-caller-meets.md)
  — the two specifications' prose is written for different callers and
  is not held to agree, and each defines every member its own caller
  meets
- [ADR-0064](./adr/0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)
  — one operation on the machine listener is reached without a
  certificate, authenticated by the one-time token registering an
  operator minted, and a connection that does not authenticate carries
  no identity rather than being refused
- [ADR-0067](./adr/0067-a-member-mints-a-second-enrollment-token-where-the-operator-holds-no-certificate.md)
  — a member mints an operator another enrollment token where it holds
  no certificate, and the refusal where it holds one is where that
  question stays open
- [ADR-0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)
  — a lifecycle authority activates an agent's generation, and its
  older generations and replaced lineages are refused per request, on
  the machine listener under a contract version of its own
- [ADR-0079](./adr/0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)
  — interservice contracts keep adjacent releases compatible in both
  upgrade orders, unbuilt
- [ADR-0083](./adr/0083-a-durable-reply-is-admitted-under-a-fixed-budget-from-its-creation-and-its-key-is-answered-after-it.md)
  — a durable reply is admitted on a versioned route of its own under
  a fixed seven-day budget from its creation, and its key is answered
  after it
- [ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)
  — garam signs every message the machine listener hands out, and that
  listener publishes the keys it signs with on an operation a caller
  with no certificate reaches
