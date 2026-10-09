# Moving machine identity to credentials and short-lived tokens

The order in which garam, `garamsh/garam-agent-operator` and the
adapter move from client certificates to
[ADR-0100](../architecture/adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)'s
credentials and tokens, how a running operator moves over, and which
repository each step lands in. Nothing here starts before the owner
accepts ADR-0100.

Checked against garam `a7148f8`, and against
`garamsh/garam-agent-operator@792f1d4fe65af33ddd32356bf014a71cd72f26d8`,
cited `OP:`, and Sherlock at `08c32c2`, the head of each repository's
`dev` on 2026-10-09.

## Contents
- The shape of the move
- The steps
- Answers to the Operator's questions
- How a running operator moves over
- What is paired where
- What would stop the plan

## The shape of the move

**garam carries both mechanisms for one window; the Operator carries
one.** garam ships first and accepts a certificate or a token on every
machine route. The Operator then cuts over in a single release with no
certificate mode left in it, which is what its owner directed for
development. When every operator garam knows of has moved, garam
removes the certificate path and then the authorities.

The adapter is garam's (`garam adapter`), and the Operator renders it
into each agent's Pod, so the adapter's token mode ships in garam's
first step and is switched on by the Operator's release. Sherlock is
not touched.

## The steps

| Step | Repository | What lands | Needs |
|---|---|---|---|
| G1a | garam | ADR-0100 accepted; the operator credential, issued on registration and re-issued on `operator:reissue`, in the browser API and the console; `POST /tokens` under `machine-token.v1`; the listener reading `Garam-Token` per request on the operator routes that carry no contract value; the metric counting requests by how they authenticated | ADR-0100 accepted |
| G1b | garam | `POST /agents/{agent}/tokens`, minted by the assignee at the current epoch and recorded in the agent's lineage; every agent route accepting a token, `Garam-Execution` in its own header and `durable-delivery.v2` | G1a |
| G1c | garam | introspection accepting a token minted for the caller where it takes `certificatePem`; `operation-authority.v2`, `execution-fence.v2`, `agent-cutover.v2` and `managed-enrollment.v2` | G1b |
| G1d | garam | the supervisor credential under `standalone-enrollment.v2`, and supervisor tokens on the execution routes | G1a |
| G2 | garam | the adapter's token mode — two token files, read per request, `Garam-Placement` as its own header, `agent-execution.v2` — and the standalone commands on a supervisor credential | G1a to G1d |
| O1 | garam-agent-operator | an Operator release carrying four pull requests, landed on `dev` in the order §O1 is one release of four pull requests gives | on each deployment, a garam carrying G1 and G2 deployed there and both credentials placed; on the lab, that is the garam `garamsh/gitops` pins |
| G3 | garam | the certificate path removed: the listener stops requesting a client certificate, the seven issuing routes (`recoverAgentCredential` in its v1 form only), `garam issue-certificate` and the v1 contract values go, and with them ADR-0091's chain | O1 deployed on every operator garam knows of, and the G1 metric reading no certificate-authenticated request for seven days |
| G4 | garam | the authorities retired: `internal/ca`, the sealer, the key-encryption key `garam serve machine` reads, and a migration dropping the certificate and enrollment tables | G3 released |

G1 is four pull requests in that order, each keeping every v1 path
passing its own tests unchanged; G1 below names all four. G1 and G2 may
land in one release; G3 and G4 may not, because G4's migration drops
what G3's predecessor still reads.

### O1 is one release of four pull requests

O1 is an Operator release, not a pull request. The release carries
these four, landed on the Operator's `dev` in this order:

1. **The manager's credential exchange and per-agent token minting.**
   The manager exchanges its credential and mints both tokens for each
   agent it runs.
2. **The rendered Pod.** The Pod gets the two token files, kept
   current, and `Garam-Placement` as its own header.
3. **The control service's identity, and its operator check by
   introspection.** The control service runs on a credential of its
   own and checks operators by introspection.
4. **The certificate paths removed.** Enrollment, renewal, the
   initial-certificate route and the certificate half of recovery go
   (§What replaces credential recovery).

Nothing of the Operator is deployed on the token path until all four
are on `dev`, and they are released together. A reader therefore
expects one release and four reviews, each of which can be checked
against garam's G1 and G2 on its own.

## Answers to the Operator's questions

The garam-agent-operator PM asked three questions on #1251, each to be
answered per step.

### Which side ships first, and is it a hard cut

garam ships first, and the cut is hard on the Operator's side alone.

- **G1, G2.** garam accepts both. Nothing the Operator runs today
  breaks, and no Operator release is waiting on garam.
- **O1.** A hard cut: one release of four pull requests, token mode
  only, no certificate fallback. It needs a garam carrying G1 and G2
  already deployed on the deployment it reaches — on the lab, the
  garam gitops pins (§How a running operator moves over) — so the
  upgrade order is fixed: garam, then the Operator. That is the
  price of the Operator carrying no compatibility, and it is
  admissible now because no release yet claims a tested compatibility
  window ([ADR-0079](../architecture/adr/0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)
  §Consequences, "None of this is built").
- **G3, G4.** garam's own cut, once the metric shows nobody on the old
  path.

**A hard cut on both sides was weighed and is not the plan.** It needs
garam and the Operator deployed at one moment. Between the two, every
operator and every agent on the deployment fails to authenticate —
on the cluster this project's own agents run on, recorded in
`garamsh/gitops`, that is all of them — and rolling either back means
rolling back both. Keeping both paths in garam for one window costs
garam a second authentication path for the life of that window and
costs the Operator nothing.

### How the control service verifies operator tokens

By introspection at garam, as it verifies an operation authority
today.

- **O1.** The manager, a controller, presents to the control service
  an operator token minted with the control service's GRN as its
  audience. The control service forwards it to
  `introspectController` where it forwards the controller's leaf
  today (`OP:internal/distribution/session.go:21-29`), and garam
  answers only the audience the token names. The control service
  already asks for a fresh proof before every decision and never
  reuses one ([operators.md](../architecture/operators.md)
  §Controller proof), so this adds no round trip.
- **O1.** The control service's own identity at garam is a credential
  and a token like any operator's, replacing the pair it loads once
  (`OP:cmd/control/main.go:219-254`).
- **G1c** is what makes this possible: introspection taking a token.

### Whether the per-agent credential replaces the placement token

It sits beside it.

- **G2, O1.** The adapter carries three things: its garam token, its
  control-service token and the placement token. The placement token
  says which placement is current and lives as long as that placement;
  its digest is one of the six fields the activation's request
  identifier is derived from (ADR-0084). An agent token is replaced
  hourly, so folding the two together would make every refresh new
  intent and every refresh a new activation.
- **G2.** The placement token leaves `Authorization`, which the
  control-service token now occupies, for a `Garam-Placement` header;
  its value and its file are unchanged.

### What replaces credential recovery

The Operator built recovery on `recoverAgentCredential` under an
`agent:recover` handoff (ADR-0091). ADR-0100 §Recovery under hourly
tokens splits it in two:

- **A lost credential: re-minting, with no route.** The assignee mints
  another agent token, as it does every refresh. O1's fourth pull
  request removes the key, the certificate request and the chain the
  Operator's recovery handles today.
- **A token that may have been taken: recovery, without a
  certificate.** The handoff, the route and `agent:recover` stay, and
  under `execution-fence.v2` the route ends the lineage and answers no
  certificate or chain. O1 keeps the handoff path and sends the v2
  body: `requestId` and `epoch`.
- **G3** removes the v1 form, its certificate request and ADR-0091's
  chain. `agent:recover` and the v2 route stay.

The writer fence and the placement token are unaffected.

### The control service's listener

**Its server TLS stays, and only the client-certificate requirement
goes.**

- **What O1 keeps.** The control service serves its own certificate
  as it does today, and the adapter and the manager verify it against
  the roots they hold for it.
- **What O1 removes.** O1's third pull request removes
  `ClientAuth: tls.RequestClientCert` and the reading of a peer leaf
  (`OP:cmd/control/main.go:201-217`,
  `OP:internal/execution/session.go:11-22`,
  `OP:internal/distribution/session.go:21-29`).
- **How callers authenticate instead.** Every caller authenticates by
  its `Authorization: Garam-Token` header, and the control service
  checks that token by introspection.

### The control service's own credential

**Two credentials, issued separately.** The control service is a
registered operator of its own, distinct from the controller it acts
for (ADR-0078), so it has its own credential. That credential replaces
the certificate pair the control service loads once
(`OP:cmd/control/main.go:219-254`).

- **Who issues.** An owner or admin issues each credential on that
  operator's own page in the console. The two are separate acts, and
  re-issuing one leaves the other untouched.
- **Who places.** Whoever issues a credential places it, because the
  console shows it to them once and garam keeps only its hash.
  - The controller's credential goes in the manager's Secret.
  - The control service's goes in a Secret the control Deployment
    mounts.
  - Each is an owner-only file in the process that reads it
    (ADR-0015).
- **The lab.** Both Secrets are deployment changes. `garamsh/gitops`
  needs them in place before O1 reaches the lab: the credentials are
  issued once the lab's garam carries G1, and placed by its secret
  mechanism, never committed as values.

### Token lifetime and the refresh margin

ADR-0100 §Tokens is authoritative for these values.

- **Lifetime.** An agent token lives one hour by default. Its
  configuration is bounded to 20 minutes at least and 24 hours at
  most. The operator and supervisor tokens live one hour, a constant.
- **Refresh.** The Operator mints the next agent token at a random
  point between two thirds and three quarters of the current one's
  life, so at least a quarter remains: 15 minutes at the default, 5
  at the lower bound.
- **Overlap.** A refresh voids nothing. The older token stays good to
  its expiry, so the Pod may hold either while the Secret update
  propagates.
- **The margin the Operator keeps.** The new token must reach the file
  the adapter reads before the old one expires. That path is the
  kubelet's propagation of the Secret, about a minute by default, plus
  the interval of whatever keeps the Pod's owner-only copy current
  (O1's second pull request). If that sum does not fit inside a
  quarter of the lifetime, the deployment raises the lifetime.
- **Both files.** The adapter reads the garam token and the
  control-service token per request, so a rewrite of either needs no
  restart. A file is replaced by a rename, never written in place.

### The contract values the Operator sends and checks

ADR-0100 §Contract values is authoritative for these. O1 moves the
Operator's clients and servers as follows, and sends or accepts no v1
value afterwards.

| Value | The Operator | Where |
|---|---|---|
| `machine-token.v1` | sends | the manager and the control service, to `POST /tokens`; the manager, to `POST /agents/{agent}/tokens` |
| `operation-authority.v2` | sends | the control service, to `introspectOperationAuthority`, `checkOperationReference` and `introspectController` |
| `execution-fence.v2` | sends | the control service, to `activateAgent`, `deactivateAgent`, `introspectAgentExecution` and `recoverAgentCredential` |
| `agent-cutover.v2` | sends | the control service, to the four cutover routes |
| `managed-enrollment.v2` | sends | the control service, to `createManagedAgent` |
| `agent-execution.v2` | checks and answers | the control service's `activations` and `runtime-status`, refusing any other value |

Two routes the Operator calls today take no v2 value and go at G3:

- `issueInitialCertificate`;
- `issueAgentCertificate`.

Three values are not the Operator's:

- `standalone-enrollment.v2` is `garam standalone`'s;
- `durable-delivery.v2` is the adapter's, sent to garam;
- the definitions read, the claim and the provisioning report carry
  no value. On them the manager sends only `Garam-Token`.

The Operator's ports may go on refusing an unknown value with 503
(the Operator's #293). That choice is the Operator's, and garam
answers each value with the same value.

## How a running operator moves over

**On the lab, O1's precondition is that the lab runs G1 and G2, not
that a release carries them.** `garamsh/gitops` pins the garam image
the lab runs as well as the Operator's, so a garam release carrying G1
and G2 changes nothing on the lab until gitops moves that pin and the
rollout completes. The order on the lab is therefore:

1. gitops moves the garam pin to a release carrying G1 and G2, and the
   lab runs it.
2. Both credentials are issued against that garam and placed (§The
   control service's own credential).
3. gitops moves the Operator pin to the release carrying O1.

The lab's rollout in progress — the Operator at `4c425a7`, waiting on
a Sherlock agent image that accepts `--assignment-epoch` — is on the
certificate path and is unaffected. G1 still accepts it.

For each registered operator, on any deployment, after G1 and G2 are
deployed there:

1. An owner or admin issues a credential in the console to each
   operator record the deployment runs. That is the controller and,
   where there is one, the hosted control service: two separate issues.
   Issuing voids nothing an operator holds. Its certificate keeps
   authenticating, because G1 still accepts one.
2. The person puts each credential in the Secret its process mounts,
   beside the material already there.
3. The Operator is upgraded to the release carrying O1. The manager
   exchanges the credential, mints both tokens for each agent it runs,
   and rolls each agent's Pod onto the adapter's token mode. The
   control service does the same for itself.
4. The old certificate is no longer presented, and it expires within
   its lifetime, a day by default. Nothing has to be deleted.

An agent moves when its Pod rolls, so for the length of that roll some
agents of one operator authenticate by certificate and some by token.
G1 accepts both, so the roll stops nothing.

A standalone host moves by an administrator registering its supervisor
again under G2's commands, which is ADR-0082's replacement. No
standalone host is accepted against garam yet (#1180), so there may be
nothing to move.

The window ends at G3. Its condition is measured rather than dated:
every operator garam knows of has exchanged a credential, and the G1
metric reads no certificate-authenticated request for seven days.

## What is paired where

ADR-0100 lands as `accepted` with G1a, the first implementation pull
request, together with the status lines of the ADRs it supersedes.
Rule 3 then binds each implementation pull request to the documents it
moves:

- **G1a**: [machine-identity.md](../architecture/machine-identity.md),
  [operators.md](../architecture/operators.md),
  [api-surface.md](../architecture/api-surface.md),
  [web-app.md](../architecture/web-app.md) and
  [deployment-contract.md](../architecture/deployment-contract.md), for
  the operator's credential and token path beside the certificate one
  and the metrics the machine process now serves.
- **G1b**: [agents.md](../architecture/agents.md) and
  [machine-identity.md](../architecture/machine-identity.md)
  §Generation and lineage, for agent tokens.
- **G1c**: [operators.md](../architecture/operators.md) §Controller
  proof and [agents.md](../architecture/agents.md) §Separated
  execution.
- **G1d** and **G2**: [adapter.md](../architecture/adapter.md) and
  [agents.md](../architecture/agents.md) §Standalone hosts.
- **G3**: [machine-identity.md](../architecture/machine-identity.md),
  [operators.md](../architecture/operators.md),
  [agents.md](../architecture/agents.md),
  [api-surface.md](../architecture/api-surface.md),
  [overview.md](../architecture/overview.md) and
  [domains.md](../architecture/domains.md), with the certificate path
  gone.
- **G4**: [certificate-authorities.md](../architecture/certificate-authorities.md)
  deleted (rule 5),
  [deployment-contract.md](../architecture/deployment-contract.md)
  without the key-encryption key,
  [organizations.md](../architecture/organizations.md) without the
  authority a deletion destroys, and the index.
- **O1** pairs in the Operator's own documents.

## What would stop the plan

- The owner declining ADR-0100, or accepting it with changes; the plan
  then follows the accepted text.
- An operator garam knows of that cannot be upgraded to O1, which
  holds G3 open indefinitely. The metric is what finds it.
- Anything other than the machine identities found reading the
  key-encryption key by the time G4 is reached, which leaves the key in
  place and retires only the authorities.
