# API Surface

What garam serves to a caller outside it: which listeners exist, what
admits a caller to each, and what the file describing each one is
authoritative for.

## Contents
- What it is
- The surfaces
- Authentication and enrollment profiles
- What admits a caller
- What decides the rest
- The registry
- An agent on the browser-facing listener
- cowork's principal on the machine listener
- The issuer's endpoints
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

It describes no operation. `platform/api/openapi.yaml`,
`platform/api/machine.yaml` and the vendored
`platform/api/cluster-connection/v1alpha1/openapi.yaml` are the source
of truth for those
([ADR-0004](./adr/0004-openapi-first-api.md), ADR-0019,
[ADR-0186](./adr/0186-the-cluster-connection-routes-are-served-from-the-interfaces-own-specification-vendored-at-a-pinned-commit.md)), the chart at
`deploy/charts/garam/` is authoritative for the address each process
listens on and for what may stand in front of it
([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)),
and a fact one of them already answers is named here rather than
repeated. A second copy is a second thing to keep true.

## The surfaces

| Surface | Served by | Described by | A caller reaches it as |
|---|---|---|---|
| Browser-facing HTTP | `garam serve api` | `platform/api/openapi.yaml` | a signed-in person, through the web app's same-origin `/api/*` rewrite (ADR-0009) |
| Machine | `garam serve machine` | `platform/api/machine.yaml` | an operator, an agent or a standalone host's supervisor presenting a token garam issued (ADR-0100, ADR-0118) |
| Cluster connection | `garam serve machine`, on the machine listener, under `/cluster-connection/v1alpha1/` | `platform/api/cluster-connection/v1alpha1/openapi.yaml`, garam-bot-operator's, vendored (ADR-0186) | a connected cluster's connector, an instance of `garam-bot-connector`: registering with no token, then presenting an instance's token (ADR-0184, ADR-0192) |
| Probes | every serve command, and the web app | no specification | anything that can open the port (ADR-0021, ADR-0023) |
| Metrics | `garam serve api`, on the listener it already runs, and `garam serve machine`, on its probe port | no specification | anything that can open the port (§Not decided) |

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
contract. The exchange, the OAuth `Bearer` alias, key issuance and
`agent-enrollment.v1`'s managed placement are served. Direct placement
is admitted by a standalone approval that names it, not by
`agent-enrollment.v1` (ADR-0111).

- `machine-token.v2` is `exchangeClientAssertion`, `POST /oauth/token`
  on the machine listener: RFC 7523's client assertion parameters, the
  client credentials grant and RFC 6749's token and error responses.
  `platform/api/machine.yaml` §exchangeClientAssertion is authoritative
  for its wire. `machine-token.v1`'s exchange and mint are unchanged.
- The `Bearer` alias resolves the same opaque access tokens as
  `Garam-Token`, with the same per-request reads and the same refusal
  of a second `Authorization` header (`machine-identity.md`
  §Asymmetric Operator exchange). It changes no execution or operation
  header.
- `issueOperatorKey`, `listOperatorKeys` and `revokeOperatorKey` on
  the browser-facing listener answer a private PEM once and public
  metadata thereafter; no secret is read back
  ([operators.md](./operators.md) §Issued PEM identity).
- `agent-enrollment.v1` is `prepareAgentEnrollment` and
  `admitAgentEnrollment`, served to a token alone, whose managed and
  direct cases differ by placement; it allocates no workload and
  activates nothing, and only managed placement admits. Its refusals
  name a `reason`. Direct placement's is `direct_placement_gated`, and
  it is permanent, since `approveAgentEnrollment` with
  `placement: direct` is what admits a direct agent. Managed and
  standalone enrollment keep their approval bindings, retry behavior
  and refusal semantics.

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
an account from one, signing in with a password, and the device
grant's code and token requests carry no credential, as do the probes
and metrics this process serves from the same router. Which operations
those are is read from `platform/api/openapi.yaml` rather than
remembered: an operation carrying `security: []` opts out of the session
requirement, cookie or bearer, the file applies to every other one. The
login write rate is bounded at the store
([ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)).
The same session is accepted as `Authorization: Bearer <token>` wherever
the cookie is, which is how the CLI presents its own, and a request
carrying an `Authorization` header is read by that header alone. The CLI
obtains its session by the device grant: requesting a device code and
polling the token endpoint carry no credential, and approving a code is
done on a session. Issuing is bounded at the store as a login state is,
and a poll earlier than its code's interval answers `slow_down`
([ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md);
[sign-in.md](./sign-in.md) §The CLI's session). A callback that finds no
live record of the login it claims to complete is refused as
unauthenticated
([ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md)). Reading
an invitation (`GET /invitations/{token}`) admits on the token alone,
because possession of it is the whole claim
([ADR-0006](./adr/0006-invitation-tokens.md)). It answers the
organization's identifier and name, the role and whether the invitation
is pending, accepted or expired, which is what accepting it would
reveal, and nothing about who invited or who belongs. An unknown token,
one revoked
([ADR-0136](./adr/0136-a-pending-invitation-is-listed-by-an-id-that-admits-no-one-and-revoking-deletes-it.md)),
and one whose organization is gone, answer the not-found body the accept
operation answers. The request line, and the `url.path` of the request's
trace span, name the route pattern rather than the path, so the token
reaches neither the log nor an exported span.

**Machine.** A token, presented as `Authorization: Garam-Token
<token>` or `Bearer <token>`, and nothing else: the listener asks for
no client certificate
([ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md),
[ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md),
[ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)).
An operator exchanges a client assertion its key signed for a one-hour
token at `exchangeClientAssertion`. An agent's token is minted by its
operator, or by the supervisor holding it, at `mintAgentToken` and read
against the agent's current assignment and lineage. A standalone
host's supervisor exchanges the credential its enrollment answered for
a token at `exchangeToken`, prefixed `garam_st_`. The token is read on
every request, beside the registration, key or assignment it rests on,
so a deleted operator, a withdrawn key, a reassignment or a recovery is
refused from the next request on a connection already open
([machine-identity.md](./machine-identity.md) §Tokens). The read is
bounded by a ten-second deadline, past which the request is answered
unavailable rather than held. A request
presenting a token in more than one `Authorization` header is refused,
and a token minted for another audience reaches none of garam's
operations. No session and no user row exist on this listener. What an
admitted caller may do is decided after that (§What decides the rest);
the lifecycle routes and the controller proof read the caller's token
dates and record again for their decision. An operator reaches the
listener on the same terms an agent does
([ADR-0015](./adr/0015-operator-identity.md)).

Beside the token, a fenced agent's execution token travels in a
`Garam-Execution` header of its own, and an operation authority or
reference in a `Garam-Operation` header, since the token takes
`Authorization`. Every versioned contract on this listener is served
under one value, and a request naming none or another is refused naming
that value. `ConfineSupervisors` bounds a supervisor's token to the
routes it holds, which standalone enrollment below names.

**The listener asks for no client certificate**
(`platform/internal/machine/listener.go`), so a handshake completes for
every caller, and what authenticates a request is the token it presents.
A request presenting none carries no identity, and a token that proves
nothing leaves its request with none and the reason beside it; every
operation but four refuses a request that carries none. Which operations
opt out is read from `platform/api/machine.yaml` rather than remembered:
`enrollStandaloneAgent` carries `security: []`, as an operation on the
browser-facing listener does, and is authenticated by the bootstrap
secret it spends instead. `getMessageSigningKeys` carries it too and is
authenticated by nothing, because what it answers is public: the keys a
verifier anywhere checks garam's signature on a message with
([ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)),
read at the issuer followed by `/message-signing-keys`.
`exchangeClientAssertion` carries it as well, and is authenticated by
the client assertion in its body, refusing an `Authorization` header
beside it
([ADR-0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
`exchangeToken` carries `machineCredential`, and is authenticated by a
supervisor's credential.

Why a token did not authenticate is written to the listener's own log
and nowhere else: what crosses the wire says only that the token was
not accepted, and nothing about which organizations exist. A token
that resolves nothing is recorded as ordinary traffic; a read that
could not be made is recorded as an anomaly.

**Each handler makes that refusal itself, and a test is what makes it
true of every route.** `platform/internal/machineapi/identity_test.go`,
in
`TestServer_TheExemptRoutesAreTheOnlyOnesAnUnauthenticatedCallerReaches`,
is authoritative for that: every operation `platform/api/machine.yaml`
declares except `POST /agent-enrollments`, `GET /message-signing-keys`,
`POST /tokens` and `POST /oauth/token` answers `401` naming the missing
identity to a caller carrying none, sent from the same client those four
answer, so what admitted those four was the route rather than a listener
that had stopped checking. The routes it sends to are derived and not
listed — read off the registration `make gen` writes from that
specification, and held against the operation count of the generated
interface — so an operation added there arrives in the test with no case
added by hand, and one added without the guard fails it. That derivation
is the whole of its value: read as a fixture the test is a handful of
paths someone chose, and a route added unauthenticated would read as one
nobody thought to cover rather than as one this already covers.

**A standalone host enrolls without a token.** Its supervisor obtains
its credential with `enrollStandaloneAgent`, presenting the bootstrap
secret whose hash an administrator approved with
`approveAgentEnrollment` (ADR-0082, ADR-0100). **What the enrollment
answers is the approval's to say and never the request's**, and garam
keeps only the credential's hash.

**`registerOperator` writes the record and issues nothing.** An
operator enters by a key that `issueOperatorKey` issues on the
registration
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)).

**`getOperator` and `updateOperator` read one operator and change
its name and description**, on `iam:operator.read` and
`iam:operator.update`
([ADR-0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)).
The identifier is the path's and not a field, so the update moves no key
or token.

**`getAgent` and `listAgents` answer the generation garam last
activated for each agent**, on `iam:agent.read`, beside the assignment
that holds it; [agents.md](./agents.md) §Separated execution states what
the field does and does not say.

**No operation serves an agent definition or names an agent**: garam
holds neither ([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)).
**No operation serves a template of garam's own**: they are gone
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)). `iam:agent-template.read` authorizes reading the templates the
control service publishes, on the organization.

**`deleteOperator` is on the same listener and is the one operation
here that stops a machine identity.** It removes the organization's
record, which the machine listener reads on every request, so the
operator's token is refused from its next request
([ADR-0070](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)).
It is authorized on `iam:operator.delete` rather than on the action the
other two take, because registering grants nothing and this withdraws
what an operator runs on. It answers 204. No operation on
`platform/api/machine.yaml` deletes an operator: an operator does not
delete itself.

**Delegation and operation authority are versioned routes on both
listeners** (ADR-0078). The browser-facing listener grants, reads and
withdraws a hosted operator's delegation, sets and clears its control
endpoint, and mints a member's request-bound authority — for a read
or a template publication, bound to the exact request target as well
as the body (#1170), including `iam:operator.read` for what the Operator
reports about the organization's operators (#1374); the machine
listener answers that authority
and its durable reference to the operator it names and no other, and
proves a controller's current authority to that operator before each
lifecycle decision. The browser-facing routes require and answer
`Garam-Contract-Version: operation-authority.v1` and the machine
listener's `operation-authority.v2`, and a request naming none or
another is refused naming the one its listener serves (ADR-0079,
ADR-0100, ADR-0118). The specifications are
authoritative for their forms, and
[operators.md](./operators.md) §Delegated operation authority for what
they decide.

**An agent's execution is a versioned contract of its own on the
machine listener** (ADR-0078). Activation, deactivation, introspection
and recovery each require and answer `Garam-Contract-Version:
execution-fence.v2`, and a request naming none or another is refused
naming it, not the operation authority's. A recovery replaces the
agent's credential lineage, so every token minted in the one before is
refused from the next request. An agent's own protected calls carry its
activation's token, under the `executionHeader` scheme each declares.
`platform/api/machine.yaml` is authoritative for both, and
[machine-identity.md](./machine-identity.md) §Generation and lineage
for what they decide. No route adds a 5xx of its own (ADR-0050): an
answer that cannot be decided from current state is the listener's
503.

**Standalone enrollment is a versioned contract on both listeners**
(ADR-0082). `approveAgentEnrollment`, `withdrawAgentEnrollment` and
`revokeAgentSupervisor` on the browser-facing listener are each
decided on `iam:agent.enroll` from current state and carry no contract
version, as an operator's registration does not, and
`listAgentEnrollments` and `listAgentSupervisors` beside them are
decided on `iam:agent.read` the same way and answer no secret, hash, key
fingerprint, certificate or credential; on the machine
listener `enrollStandaloneAgent` requires and answers
`Garam-Contract-Version: standalone-enrollment.v2`, and an approval
binds no key (ADR-0100, ADR-0118). A supervisor's token is admitted to
`getIdentity`, to the three execution routes it holds authority on and
to its agent's token mint, and refused on every other route at the
router before a handler runs (`ConfineSupervisors`). The
specifications are authoritative for their forms, and
[agents.md](./agents.md) §Standalone hosts for what they decide.

**Managed enrollment is a third versioned contract on the machine
listener** (ADR-0077, ADR-0078). Creating a managed agent requires and
answers `Garam-Contract-Version: managed-enrollment.v2`, and a request
naming none or another is refused naming it. It carries no private
key, and no other route creates an agent. `platform/api/machine.yaml` is
authoritative for the wire, and
[agents.md](./agents.md) §Separated execution for what they decide.

**Durable submission is a fourth versioned contract on the machine
listener**
([ADR-0083](./adr/0083-a-durable-reply-is-admitted-under-a-fixed-budget-from-its-creation-and-its-key-is-answered-after-it.md)).
`POST /outbox-messages` requires and answers `Garam-Contract-Version:
durable-delivery.v2` and is the message write `POST /messages` makes,
which is unchanged, with the entry's `createdAt` added: a new message
is stored only inside seven days of it on the database's clock, and a
key that already stores one is answered at any time. An adapter
branches on these answers, each carrying the header:

| Status | Kind | When |
|---|---|---|
| 201 | — | the message the key stores, stored now or earlier, or the cross-organization refusal |
| 400 | `invalid_argument` | no or another contract version, naming `durable-delivery.v2`; a malformed body, including a missing `createdAt` or one that is not RFC 3339 |
| 400 | `out_of_range` | `createdAt` later than the decision instant; the entry is kept and a later attempt may be admitted |
| 410 | `deadline_exceeded` | the key stores nothing and the budget has run out; no later attempt changes it |
| 409 | `already_exists` | the key stores a different message |
| 401, 403, 409 `failed_precondition` | as `POST /messages` | the execution fence's refusals (ADR-0080) |
| 404 | — | a garam without the route |

`platform/api/machine.yaml` is authoritative for the wire, and
[messaging.md](./messaging.md) for what the budget decides.

**Policy engine.** Removed. The platform no longer serves a decision
surface; the policy service is a service of its own with its own
contract ([ADR-0177](./adr/0177-the-earlier-policy-engine-its-serve-command-its-protobuf-module-and-its-tables-are-removed.md)).

**Probes and metrics.** Nothing, by decision: a probe holds no
credential, so the answer is served where none is needed
([ADR-0021](./adr/0021-liveness-and-readiness.md)). A readiness failure
names the dependency and nothing more, because an error's own text
would otherwise reach a caller that proved nothing.

## What decides the rest

Admission says who is calling. What they may do is decided after it,
and not in the same place on every surface.

- **Browser-facing**: the policy service decides an org-scoped
  request's enforcement, `checkPermissions` and the holders list, one
  question per pair and per candidate over delivered state, and the
  request is answered 503 `unavailable` where it cannot be asked
  ([ADR-0171](./adr/0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)).
  The lists and the search filter are the service's too, each object
  of a page one probe in one `Evaluations` call
  ([ADR-0172](./adr/0172-the-lists-and-search-filter-through-the-policy-services-evaluations-as-probes-and-checkpermissions-answers-permit-write.md)),
  and `checkPermissions` answers `iam:permit.write` from it. The
  explanation is the service's too (ADR-0174). The probes are
  each `checkPermissions` pair, each holder candidate, each search
  kind and each object a list filters. How it decides is
  [authz.md](./authz.md).
- **Machine**: by construction more often than by policy. A read is
  scoped by the caller's own identity, so there is nothing to authorize
  past the token there. A state report is decided the same way and by a
  second
  thing besides: the assignment's epoch, which the route refuses a
  stale one of
  ([ADR-0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)).
  A sender reading its own message's state is scoped the same way from
  the other side — the caller is the sender the row is matched on, so a
  message it did not send answers as one that is not there
  ([ADR-0055](./adr/0055-a-sender-reads-the-state-of-a-message-it-sent.md)).
  An agent's protected calls are also decided per request by its
  execution fence, and the lifecycle routes
  by the caller's current authority
  (`machine-identity.md` §Generation and lineage).
- **What a machine caller may send** is the exception, and the
  recipient's organization decides which of two things answers it. A
  recipient of the caller's own organization is a policy question the
  policy service answers, except that a recipient naming no agent garam
  holds is admitted unasked and stored as a routing failure
  ([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md),
  [ADR-0168](./adr/0168-the-machine-listeners-send-is-the-policy-services-decision-consent-included.md)).
  A recipient across the organization boundary that is not an agent is
  refused unasked, and an agent is asked of the service, whose tenant
  isolation holds the boundary. The refusal returns to the sender as a
  message and tells it nothing of the other organization.
  [authz.md](./authz.md) says how the service decides.
- **Watching an agent is its own grant**, separate from reading one
  ([ADR-0022](./adr/0022-watching-an-agent.md)).
- **Delegated operation authority is decided from current state on both
  listeners**: the policy service is asked the actor, the action and the
  object read current, so an allow recorded earlier is never what a
  later check reads and a decision that cannot be made refuses. A
  controller proof is decided the same way for the hosted operator,
  once the platform has found its grant covers the operation
  ([ADR-0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)).
- **A read describing a write is decided on that write's action.**
  `getOrgDeletionPreview` answers what deleting an organization would
  destroy — a count per resource kind, and whether the organization
  holds a certificate authority — and is decided on `iam:org.delete`
  rather than `iam:org.read`. It reads and changes nothing, so
  `iam:org.read` would be the shape to reach for; what that would
  publish is the size of an organization to every member who cannot
  delete it. Authorizing the description as the act keeps the two
  answers together: a caller who may not perform the delete is told
  nothing about it. No role gained an action for this, because the owner
  already holds `iam:org.*` ([authz.md](./authz.md) §Statement
  collection).
- **A machine identity's credential events are read on its own
  read.** `listCredentialEvents` answers what garam issued, renewed and
  refused of one operator's, agent's or supervisor's credentials,
  newest first and paged like any list, on `iam:operator.read` on the
  organization for an operator, `iam:agent.read` on the agent for an
  agent and `iam:agent.read` on the organization for a supervisor
  ([ADR-0114](./adr/0114-every-machine-credential-issuance-renewal-and-refusal-is-an-audit-event-read-on-the-subjects-own-read.md)).
  No event carries a key, token, credential or certificate
  ([machine-identity.md](./machine-identity.md) §Credential events).
- **An organization's policy writes and denials are read on
  `iam:audit.read`.** `listAuditRecords`
  (`GET /orgs/{org}/audit-records`, with `pageSize` and `pageToken`)
  relays the policy service's `ListAuditRecords` as the signed-in
  user, newest first and paged
  ([ADR-0176](./adr/0176-the-platform-relays-the-policy-services-audit-record-to-the-console-and-the-cli.md),
  [ADR-0148](./adr/0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)).
  The service decides the read and its refusal is the answer: 403 for
  a reader it refuses, 400 for a token it never issued, 503 when it
  cannot be reached and 504 when the call times out. The platform
  stores and filters nothing
  ([audit.md](./audit.md) §The policy service's record). The CLI
  derives `garam audit-records list` from it.
- **An organization's role links, guardrails and permits are relayed
  to the policy service.** `/orgs/{org}/iam-policies` serves
  `createIamPolicy`, `listIamPolicies`, `getIamPolicy`,
  `deleteIamPolicy`, `createIamPolicyVersion`, `activateIamPolicy` and
  `rollbackIamPolicy`, the kind (`role-link`, `guardrail`, `permit`)
  in the body. The platform names the signed-in user as the writer and
  stores none of the body, so the service's answer is the response:
  403 for a writer it refuses (only an owner writes a permit), 400 for
  an invalid body, 404 and 409 as it made them, 503 when it cannot be
  reached, 504 when the call times out, and a write refused stores
  nothing. The platform answers 400 itself for a role link it
  derives from a membership. `listIamPolicies` is the service's read,
  not filtered here. The `/orgs/{org}/policies*` operations are removed
  ([ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md)),
  and the store and tables behind them wait for round 14 of #1796
  ([ADR-0169](./adr/0169-the-platform-relays-an-organizations-role-links-guardrails-and-permits-to-the-policy-service.md),
  [authz.md](./authz.md) §Managing policies over HTTP).
- **A list answers only the objects its caller may read.**
  `listGroups`, `listProjects` and `listAgents` are
  authorized on their read action on the organization, and then drop
  each object whose own read the caller is denied: the read
  `getGroup`, `getProject` and `getAgent` each
  make on the object. A Deny on one object therefore hides it from the
  list as it refuses its read. Every object of a page is asked in one
  `Evaluations` call to the policy service, as a probe, and an error of
  the service is a 503 `unavailable`, never an empty page; a policy is
  asked on its organization, so it is hidden only with the whole list
  ([ADR-0172](./adr/0172-the-lists-and-search-filter-through-the-policy-services-evaluations-as-probes-and-checkpermissions-answers-permit-write.md),
  [authz.md](./authz.md)). **A short page does not mean the end.** The
  page is filtered as read, so `limit` and `offset` count the stored
  rows, and a page can hold fewer objects than its limit with more
  after it; a client pages until a page comes back empty. Lists gated
  on a parent object — a group's or project's members, an operator's
  keys — are not filtered: the
  parent's read is what decides them.
- **A caller's own teams are one filtered list.** `listGroups` with
  `member=me` answers only the groups the signed-in caller belongs to,
  ordered, paged and filtered as the unfiltered list is, and authorized
  on the same `iam:group.read` on the organization. The filter names the
  caller and nobody else, because who else belongs to a group is
  `listGroupMembers`', which is decided per group.
- **A name search answers each kind as its list would.** `searchOrg`
  takes `q`, 1 to 100 characters once trimmed, and finds the
  organization's people, agents, teams, policies and projects whose
  name or identifier holds it, case aside. It is the console palette's
  read (ADR-0113). `%` and `_` in `q` match only themselves. Each kind
  is authorized on its list's own check on the organization, and a
  kind the caller fails is left out of the answer. Agents, teams,
  policies and projects then drop each object whose own read is
  denied, in one `Evaluations` call per kind, as the filtered lists
  above do (ADR-0172). A person is found by nickname or identifier, and
  an agent
  by its name or identifier, answered with its name where it has one
  ([ADR-0119](./adr/0119-a-managed-agent-carries-a-display-name-on-its-own-record-set-at-creation-and-renamed-on-agent-rename.md)). Each kind answers its
  first `limit` objects (1 to 20, 5 by default) in its list's order,
  and counts the readable matches past them. The match runs in each
  store, so a search reads the rows that match and not the whole list.
  People are the exception: the organization's memberships are read
  whole, and the user store matches among them, since memberships and
  nicknames are two domains'. Policies are the other: the policy
  service stores them and has no search, so the platform reads the
  organization's list through the relay and matches each name or
  identifier itself, in identifier order.
- **A member may ask what it may do, and the answer decides nothing.**
  `checkPermissions` takes up to 50 pairs of an action and a resource
  of the caller's organization, the resource named by its kind and the
  identifier segment its own path takes. Every write is still decided
  when it arrives, so an answer is a hint the console renders by and
  not a grant
  ([ADR-0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md) §Role-aware UI).
  A `cowork-page`, a `cowork-channel`, a `cowork-task` and a
  `cowork-artifact` are the kinds no path here addresses: cowork keeps
  them and names them, and asks `cowork:page.read` and
  `cowork:page.write`, `cowork:channel.read` and
  `cowork:channel.post`, `cowork:task.read` and `cowork:task.write`,
  or `cowork:artifact.read` and `cowork:artifact.write`, on the
  organization or on one of them, before it acts. Every role grants
  all eight, and a policy narrows them like any other action
  ([ADR-0127](./adr/0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)).
  Each pair is answered allow, deny or unknown:
  - **allow or deny**, where enforcement is the policy service's
    decision alone, put as enforcement puts it. No answer says why. A
    missing group or project answers as a denied one, so the answer
    tells no one what exists.
  - **deny or unknown** for an action a mint decides together with a
    delegation grant: `iam:agent.create`, `iam:agent.configure`,
    `iam:agent.recover`, `iam:agent.replace`,
    `iam:agent-template.publish`,
    `iam:execution-profile.read` and `iam:agent.execution-read`
    ([ADR-0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)).
    The pair is put to the check the mint runs, through the one
    function both call
    (`platform/internal/operator/authority_service.go`,
    `AuthorizeOperation`). A deny there is the mint's refusal, so it is
    answered deny. An allow leaves the grant, which `checkPermissions`
    does not read, so it is answered unknown.
  - **allow or deny** for `iam:operator.delegate`. It is put to the
    check every write of a delegation and of a control endpoint runs,
    through the one function they all call
    (`platform/internal/operator/authority_service.go`,
    `AuthorizeDelegation`), and that check is all of their authority.
  - **allow or deny** for `iam:agent.enroll`. It is put to the check
    every approval, withdrawal and revoke of a standalone host runs, on
    the policy service's current decision, through the one function
    they all call (`platform/internal/agent/standalone_service.go`,
    `AuthorizeEnroll`), and that check is all of their authority. A new
    host is asked on the organization, and an approval, withdrawal or
    revoke that acts on an agent on that agent.

  So `unknown` is answered exactly where a delegation grant is left to
  decide, and nowhere else.
  - **deny** for an action garam does not define, because a role's
    wildcard such as `iam:org.*` would otherwise allow a name no
    operation checks.

  A defined action paired with a resource kind none of its operations
  address is refused with 400 naming the pair, and one pair the policy
  service cannot decide fails the whole request rather than answering
  the rest.
- **A member may ask why, from the inputs enforcement uses.**
  `explainPermission` takes one pair, named as `checkPermissions`
  names one, and answers the decision `checkPermissions` answers it,
  asked of the policy service's `Evaluate` as a probe
  ([ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md)).
  Its grounds are the service's reasons: a role link, named by its
  reserved identifier and the path it reaches the caller by (the
  organization, a team or a project); a guardrail or a permit, named
  by GRN; a system policy, named by its file name; or an error of
  evaluation. An allow names each policy that permits, a deny each
  that forbids, and a deny that nothing forbids names none.
  - **A policy the caller cannot read is named without content.** A
    guardrail or a permit carries its name and text only where the
    caller holds `iam:policy.read` on the organization; otherwise it
    is named by its identifier alone.
  - **An action decided past the check is explained no further than
    its decision**: the mint-decided actions, `iam:operator.delegate`
    and `iam:agent.enroll` answer `checkPermissions`' decision with no
    grounds, since their operation is what decides. An action garam
    does not define is answered deny as such.
- **Who holds an action is a list read on `iam:org.member-list`.**
  `listPermissionHolders` puts one pair to the policy service for each
  member and agent of the organization, each asked as enforcement asks
  it: a member with its role-derived statements and groups, an agent,
  which holds no role, with its groups alone. It is authorized on
  `iam:org.member-list`, the action `listOrgMembers` is, because the
  answer names members; agents are asked only where the caller also
  holds `iam:agent.read`. A page asks at most fifty candidates —
  members, then agents, each in GRN order — and answers those the
  service allows with the offset of the next page, so one request costs
  the service what one `checkPermissions` request does. Its `limit` is
  therefore its own and not the one other lists share: 1 to 50, 50 when
  absent, and 0 refused rather than read as no limit. An action decided
  past the service is refused with 400, since the service is not what
  decides who holds it.

One of ADR-0036's decisions is a commitment rather than a rule deciding
anything today: **no surface serves a secret's value**, garam's
listeners included. garam has no secret to serve — `secret` is a domain
it does not have yet ([domains.md](./domains.md) §Future domains) — so
what the commitment binds is the surface that serves that domain when it
arrives
([ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)).

## The registry

**Built (#1788).**
[ADR-0143](./adr/0143-a-service-is-declared-by-one-manifest-that-is-its-cedar-schema-and-garam-serves-what-the-manifests-say.md)
item 6 adds two read-only operations to the browser-facing listener,
both answered from `lib/grn`'s generated catalogue and neither reading
an entity.

- **`getRegistry`** (`GET /registry`) lists each service with its types
  and its actions.
- **`describeGrn`** (`GET /registry/grn?grn=<name>`) answers a name's
  service, type, entity type, parent, locator and the actions that
  apply to its type. The name is a query parameter, never a path
  segment. A name of a service or type no manifest declares is answered
  `known: false` with status 200; a string that is no GRN is refused
  with 400.
- **What admits a caller** is a session, and nothing decides the rest:
  neither operation asks the policy service.

## An agent on the browser-facing listener

**Unbuilt.**
[ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
admits an agent to the browser-facing listener as itself. Until it is
built, that listener admits a session alone (§What admits a caller),
and `platform/api/openapi.yaml` declares no scheme for anything else.

- **What admits it** is an agent token whose audience is the browser
  API, presented as `Authorization: Bearer` and read on every request
  beside the agent's assignment and lineage
  ([machine-identity.md](./machine-identity.md) §Browser tokens).
- **What decides the rest** is the policy service, on the agent's GRN
  and its groups with no role statements ([authz.md](./authz.md) §An
  agent on the browser-facing listener).
- **What it never reaches**, whatever a policy grants: any operation
  outside `/orgs/{org}` for the organization its GRN names — sign-in,
  sessions, the device grant, `/me`, the organization list and
  creation, and invitations by token — and `issueOperatorKey`,
  `createInvitation`, `mintOperationAuthority` and
  `approveAgentEnrollment` inside it.
- **What records it.** The request line names the agent's GRN in the
  machine principal's field, not the user's.

## cowork's principal on the machine listener

**Unbuilt (#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
admits a fourth machine caller: each organization's cowork principal.
Until it is built, no token the listener reads names one.

- **What admits it** is a token from `exchangeClientAssertion`, whose
  assertion cowork signs with its one deployment key and whose
  `client_id` is the organization's principal; it is read on every
  request beside the principal's record and the configured key
  ([machine-identity.md](./machine-identity.md) §cowork's principal).
- **What it reaches** is `postMessage` to an agent of its
  organization, `getMessageStatus` for a message it sent and
  `getIdentity`. Every other operation on this listener refuses it,
  and it takes no browser audience.
- **What decides the rest** is `iam:message.send`, as for any sender
  ([authz.md](./authz.md) §cowork's principal).

## The issuer's endpoints

**Built (#1757).**
[ADR-0141](./adr/0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
adds four endpoints, served only when a signing key is configured.
With none, none is registered and nothing else changes.

- **The issuer is one identity**, `GARAM_IDENTITY_ISSUER`, the API's
  origin with its `/api` prefix, so the console's rewrite forwards all
  three public paths and no new host exists.
- **Discovery and the JWKS** are
  `<issuer>/.well-known/openid-configuration` and
  `<issuer>/oauth/jwks`, on the browser-facing listener and answered
  without a credential.
- **The token endpoint** is `<issuer>/oauth/token`, on the
  browser-facing listener. The subject token is the whole credential
  ([machine-identity.md](./machine-identity.md) §The issuer).
- **Introspection** is `/oauth/introspect` on the machine listener,
  admitting a first-party service's principal
  (`grn:<org>/iam/service/<name>`) by its `private_key_jwt` assertion
  and answering for tokens of that service's audience alone.
- **A principal's token** is asked of the machine listener's
  `/oauth/token` with its principal's GRN as `client_id` and `audience`
  `policy`, and answered the issuer's JWT for the policy service
  ([machine-identity.md](./machine-identity.md) §cowork's principal).

**Built in part, decided by
[ADR-0192](./adr/0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md):
apps replace the callers named here.** Built (#2081): the
cluster-connection surface's caller is an instance of the
`garam-bot-connector` app, registering with no token and then
presenting an instance's token, and the browser-facing surface serves
`/orgs/{org}/apps`, `/orgs/{org}/installations` and
`/orgs/{org}/installations/{installation}/instances` in place of the
cluster routes ([apps.md](./apps.md)). Unbuilt: introspection and a
principal's `policy` token admitting an installation authenticated by
its app key, `grn:<org>/iam/installation/<slug>`, for the audiences its
app's record lists, in place of `grn:<org>/iam/service/<name>`.

## What a specification is authoritative for

One specification per listener, and a third listener repeats that
rather than reopening it (ADR-0019), with one exception.
`platform/api/openapi.yaml` is the source of truth for what `garam
serve api` serves (ADR-0004), and `platform/api/machine.yaml` for what
`garam serve machine` serves (ADR-0019). The exception is the paths
under `/cluster-connection/v1alpha1/`, which the same listener serves
from `platform/api/cluster-connection/v1alpha1/openapi.yaml`.
That file is garam-bot-operator's interface, vendored byte for byte at
the commit `CLUSTER_CONNECTION_PIN` in `platform/Makefile` names. Its
refusals carry the interface's `{error, message}` body (T-5), not the
listener's. `make -C platform check-vendor` fails when the copy is not
that commit's file, and the PM moves the pin (ADR-0186).

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
`platform/api/server.gen.go` and `platform/api/machine/server.gen.go`
from the two specifications, `platform/internal/api` and
`platform/internal/machineapi` implement what is generated, and
`make check-gen` — which `make ci` runs — fails when the committed
output and the specifications disagree. A path, a parameter or a model
therefore cannot diverge from the file describing it.

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
`platform/internal/cli/cli.go`. The browser listener answers a body past
it with 413 and its `Error`, kind `invalid_argument`, from the one
decoder every browser handler reads a body through. The machine listener
refuses it as an invalid argument with 400, because each of its handlers
decodes on its own. The bound is on the request rather than a field,
which is what leaves it standing where §What nothing checks says a
declared constraint does not.

**A refusal names its rule where the kind cannot.** The browser
listener's `Error` carries an optional `reason`, a stable code, where
one status and kind cover refusals a client must tell apart (#1380): an
invitation used against one that lapsed, and each rule a policy document
breaks, with `path`, a JSON Pointer into the document. The codes are
listed in `platform/api/openapi.yaml` `Error.reason`; a client branches
on them and never on `message`, and one that does not know a code reads
the kind as before. A reason the policy relay's gRPC client reads from
the service's `ErrorInfo` status detail
(`platform/internal/wire/error.go`) is the code the API answers
unchanged.

**A provider sign-in can land on a path of one of garam's apps.**
`POST /auth/login/{provider}` starts the same flow as `GET` and takes
`app` and `next` in its form body (ADR-0135). `app` names the console
or cowork, the console where it is absent, and any other value is
refused. A path off that app is dropped by the rule the site applies to
the password sign-in (`platform/internal/api/return_path.go`,
`lib/platform-sdk-ts/src/return-path.ts`), which bounds it in UTF-8
bytes and is tested on both sides against one table,
`platform/internal/api/testdata/return-path-vectors.json`. What is kept
is held on the login's `oauth_states` row: the console path, or the
site's `/handoff` for cowork carrying the cowork path. It is spent with
the state by the callback that completes and expires with it. The
callback redirects to it, checked again, or to `/console`, and no answer
repeats it. A cross-site form can start such a login, and the most it
decides is where inside one of garam's apps that login lands.

The other surfaces are fixed differently. The earlier engine's wire
contract, `platform/proto/garam/authz/v1`, is removed with it. The
probes have no specification at all: their paths, and the
ports they answer on, are interface rather than implementation, so
changing one supersedes the ADR that set it instead of editing a handler
(ADR-0021, [ADR-0023](./adr/0023-web-probes.md)). The metrics surface is
fixed by nothing at all (§Not decided).

## What nothing checks

**Generation copies prose and reads none of it.** Which prose it copies
differs by kind, measured on 2026-09-03 by perturbing one description
of each kind in each specification and running `make gen` and
`make -C platform-web gen`: a schema's `description` reaches
`platform/api/server.gen.go`, `platform/api/machine/server.gen.go` and
`platform-web/src/types/api.ts`, an operation's reaches
`platform-web/src/types/api.ts` alone, and a tag's reaches none of the
three. So `make check-gen` and `make -C platform-web check-gen` fail on
a schema description edited without regenerating, and pass on any text
at all: each compares one specification against its own output, never
one against the other, and never prose against what the code does. A
decision that changes what an operation does leaves the text stating the
old behaviour with every check green.

That is not hypothetical. ADR-0035 changed what garam does with a
message it may not route. [domains.md](./domains.md) moved with it and
`platform/api/machine.yaml` did not, in two places: `postMessage`'s
description stated the new behaviour as explicitly not what happens,
corrected in
#530, and the `message` tag's description, corrected in #532. Neither
is a schema description and no TypeScript output is generated from that
file, so on the reach stated above neither moved a generated byte. Both
were found by someone running the service rather than by anything in
the repository.

**Nor does it cover which statuses an operation declares.** A response
is not part of what oapi-codegen writes: adding a `'500'` to one
operation and running `make gen` leaves `platform/api/server.gen.go`
byte-identical, so `make check-gen` reads a response set in neither
direction — a status declared that cannot occur, and one that occurs
undeclared, are both invisible to it. #698 is the same fact in the
history: it changed four operations' response sets and moved no
generated Go file. What does move is `platform-web/src/types/api.ts`,
generated from `platform/api/openapi.yaml` and held against it by
`make -C platform-web check-gen`, so the browser client's types follow a
response set while the server's do not — and `platform/api/machine.yaml`
has no consumer of that kind at all. What `make gen` writes from it is
the generated Go §What a specification is authoritative for names, which
a response set leaves as untouched as it leaves
`platform/api/server.gen.go`.

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
policy service stores a version at, and `minimum: 1` is its own rule,
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
  one is inside its reach; `platform/api/openapi.yaml` and
  `platform/api/machine.yaml` are neither and are not. Listing the API
  as a concern closes the half a document can close — an ADR that
  changes an endpoint now has a responsibility document to be paired
  with — and leaves the half only a rule can. What that rule should say
  is the index's to decide and not this document's.
- **What decides the metrics surface.** `/metrics` is served by `garam
  serve api` and `garam serve machine` (§The surfaces) and decided by no
  ADR: every ADR that names it treats it as a surface that already
  exists. ADR-0014 and ADR-0017 name it as the shape a listener of its
  own follows, ADR-0021 puts the probes on the listener its metrics
  already use, and ADR-0030 counts it among the paths that write nothing
  — context and reasoning, never a decision. `garam serve machine` is
  the exception: it serves `/metrics` on its probe port because
  ADR-0100's migration plan gated removing the certificate path on a
  count of the listener's requests by how each authenticated,
  `garam_machine_requests_total`
  (`platform/internal/cli/machine_token.go`). That decides that this
  process exposes the count, and nothing about what `serve api` exposes.
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
- [ADR-0003](./adr/0003-single-cli-topology.md) — one binary; the
  earlier engine's process is removed (ADR-0177)
- [ADR-0004](./adr/0004-openapi-first-api.md) —
  `platform/api/openapi.yaml` is the source of truth for what
  `serve api` serves
- [ADR-0005](./adr/0005-server-side-sessions.md) — a session is a row,
  and the cookie carries the token naming it
- [ADR-0009](./adr/0009-web-frontend.md) — the browser reaches the API
  through a same-origin `/api/*` rewrite, so the cookie needs no CORS
  layer
- [ADR-0012](./adr/0012-agent-operator-model.md) — garam never dials an
  agent
- [ADR-0013](./adr/0013-agent-messaging.md) — garam is a message API an
  adapter polls
- [ADR-0015](./adr/0015-operator-identity.md) — an operator
  authenticates as an agent does, on the same listener
- [ADR-0017](./adr/0017-machine-process.md) — that listener is a
  process of its own, taking neither the browser API's proxy nor its
  cookie
- [ADR-0019](./adr/0019-machine-openapi-spec.md) — one specification
  per listener, and `platform/api/machine.yaml` is the machine
  listener's
- [ADR-0021](./adr/0021-liveness-and-readiness.md) — the two probes
  answer where a caller needs no credential, and their paths are
  interface
- [ADR-0022](./adr/0022-watching-an-agent.md) — watching is a grant of
  its own
- [ADR-0023](./adr/0023-web-probes.md) — the web app answers the same
  two probes
- [ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md) — a
  policy decides what an agent may send
- [ADR-0027](./adr/0027-a-watch-crosses-processes-through-postgres.md)
  — the two listeners cross through Postgres and never call each other
- [ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md) — a
  login's state is a record the callback spends
- [ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)
  — the login write rate is bounded at the store
- [ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)
  — a cross-organization message needs both organizations' consent
- [ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)
  — no surface serves a secret's value
- [ADR-0176](./adr/0176-the-platform-relays-the-policy-services-audit-record-to-the-console-and-the-cli.md)
  — `listAuditRecords` relays the policy service's audit record as the
  signed-in user, the service deciding the read on `iam:audit.read`
- [ADR-0171](./adr/0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)
  — the browser listener's enforcement asks the policy service, and a
  failure of it is a 503
- [ADR-0136](./adr/0136-a-pending-invitation-is-listed-by-an-id-that-admits-no-one-and-revoking-deletes-it.md)
  — a pending invitation is listed by an id that admits no one, and
  revoking deletes it
- [ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
  — the machine listener presents a pinned garam server certificate,
  read per handshake
- [ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
  — a password is garam's own credential, served as an endpoint rather
  than a provider
- [ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
  — the chart is authoritative for the addresses and for what may stand
  in front of them
- [ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  — the development-only sign-in provider is removed
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
- [ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
  — every machine caller authenticates with a garam-issued credential
  and short-lived tokens over server TLS
- [ADR-0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)
  — garam signs every message the machine listener hands out, and that
  listener publishes the keys it signs with on an operation a caller
  with no credential reaches
- [ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)
  — garam keeps no legacy path no live deployment uses: an operator
  enters by a key alone
- [ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)
  — every legacy path goes now with nothing migrated: the browser
  surface serves no definition and names no agent, and the machine
  listener authenticates no certificate
- [ADR-0119](./adr/0119-a-managed-agent-carries-a-display-name-on-its-own-record-set-at-creation-and-renamed-on-agent-rename.md)
  — a managed agent carries a name on its own record, set at creation
  on both machine routes and renamed on the browser surface under
  `iam:agent.rename`, authorized on the agent itself
- [ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md)
  — the CLI signs in by the device grant, and the browser API accepts
  its session as a bearer wherever it accepts the cookie
- [ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
  — an agent calls the browser API with an agent token of the browser
  audience, authorized by the policies reaching its GRN, inside a
  floor no policy lifts; unbuilt
- [ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
  — cowork acts in each organization as a principal the platform
  writes with it, proving one deployment key for a machine-listener
  token that posts to that organization's agents alone; unbuilt
- [ADR-0141](./adr/0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
  — the issuer's discovery, JWKS, token and introspection endpoints,
  served only with a signing key; built for a CLI session's exchange
  (#1757)
- [ADR-0186](./adr/0186-the-cluster-connection-routes-are-served-from-the-interfaces-own-specification-vendored-at-a-pinned-commit.md)
  — the paths under `/cluster-connection/v1alpha1/` are served from
  garam-bot-operator's specification, vendored at a pinned commit
