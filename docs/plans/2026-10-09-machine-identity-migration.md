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
| G1 | garam | the token store, `POST /tokens`, `POST /agents/{agent}/tokens`, the operator and supervisor credentials, issue and re-issue on the browser API and in the console, every machine route accepting `Garam-Token` under the v2 contract values beside the v1 ones, introspection accepting a token where it takes `certificatePem`, a metric counting requests by how they authenticated | ADR-0100 accepted |
| G2 | garam | the adapter's token mode — two token files, read per request, `Garam-Placement` as its own header, `agent-execution.v2` — and the standalone commands on a supervisor credential | G1 |
| O1 | garam-agent-operator | an Operator release carrying four pull requests, landed on `dev` in the order §O1 is one release of four pull requests gives | a garam release carrying G1 and G2, deployed |
| G3 | garam | the certificate path removed: the listener stops requesting a client certificate, the seven issuing routes, `garam issue-certificate` and the v1 contract values go | O1 deployed on every operator garam knows of, and the G1 metric reading no certificate-authenticated request for seven days |
| G4 | garam | the authorities retired: `internal/ca`, the sealer, the key-encryption key `garam serve machine` reads, and a migration dropping the certificate and enrollment tables | G3 released |

G1 and G2 may land in one release; G3 and G4 may not, because G4's
migration drops what G3's predecessor still reads.

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
   initial-certificate route and the certificate half of recovery go.

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
  only, no certificate fallback. It needs a garam carrying G1 and G2 already deployed, so
  the upgrade order is fixed — garam, then the Operator. That is the
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
- **G1** is what makes this possible: introspection taking a token.

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

## How a running operator moves over

For each registered operator, after G1 and G2 are deployed:

1. An owner or admin issues the operator a credential in the console.
   Issuing it voids nothing the operator holds: its certificate keeps
   authenticating, because G1 still accepts one.
2. The person puts the credential in the operator's Secret, beside the
   material that is there.
3. The Operator is upgraded to the release carrying O1. The manager exchanges the
   credential, mints both tokens for each agent it runs, and rolls each
   agent's Pod onto the adapter's token mode. The control service does
   the same for itself.
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

ADR-0100 is proposed and pairs with no responsibility document in
its own pull request. The pull request that accepts it moves the
status lines of the ADRs it supersedes, and rule 3 then binds the
implementation pull requests:

- **G1**: [machine-identity.md](../architecture/machine-identity.md),
  [operators.md](../architecture/operators.md),
  [agents.md](../architecture/agents.md),
  [api-surface.md](../architecture/api-surface.md) and
  [web-app.md](../architecture/web-app.md), each describing the token
  path beside the certificate one.
- **G2**: [adapter.md](../architecture/adapter.md) and
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
