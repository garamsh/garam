# Architecture Overview

garam is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

## Contents
- In scope
- Out of scope (today)
- Module boundaries
- Core abstractions
- Decisions
- Non-functional goals

## In scope

- Identities for users, including organization membership. Agent
  identity and the operator an agent is assigned to are persisted
  (ADR-0012); an agent is never an org member (ADR-0014).
- A certificate authority per organization, created on demand, which
  issues the client certificates machine identities authenticate with
  (ADR-0014). The subject's GRN travels in a SAN URI; the CA private key
  reaches Postgres sealed under a key-encryption key.
- A listener machine identities authenticate on, `garam serve machine` —
  a process of its own, the third from the one image (ADR-0017). It
  terminates TLS itself, asks for a client certificate, and verifies
  the one it is given against the authority of the organization the
  certificate's own SAN names (ADR-0014). A connection that presents
  none, and one whose certificate does not verify, completes the
  handshake carrying no identity rather than being refused there —
  which is what leaves enrolling reachable to an operator that holds
  none yet, and costs nothing elsewhere, because every other operation
  refuses a request carrying no identity (ADR-0064). An agent and an
  operator authenticate the same way (ADR-0015), and neither
  authenticates through a user row, a session or a membership.
  What that listener serves has a specification of its own (ADR-0019).
  The other direction rests on a different root: the listener presents
  one certificate signed by a garam server authority the deployment
  holds and garam never does, every identity is given that root with its
  own certificate, and both ends read their pair per handshake so a
  replaced one is served without a restart (ADR-0040).
- An operator creating an agent and obtaining its certificate, on that
  listener. garam mints the agent's GRN in the operator's own
  organization and assigns it at an epoch; an operator obtains the
  certificate of an agent assigned to it at the current epoch and of no
  other, which is the whole of the authorization there (ADR-0015). The
  process that issues holds the key its authorities are sealed under
  and does not start without it (ADR-0016).
- An operator renewing its own certificate on that listener, by
  presenting the one it holds (ADR-0046). garam records the certificate
  it last issued each operator and renews that one alone, so a copy of
  it stops renewing the moment the operator does; a certificate outside
  its renewal window and one that has expired are both refused, and what
  follows an expired one is the out-of-band mint (ADR-0032). Nothing
  on this route withdraws a certificate: it narrows what a copy may do
  next and leaves it authenticating, and what withdraws one is deleting
  the operator (ADR-0070).
- An operator reporting what it sees of an agent's pod, and an agent
  reporting what it says about itself, each on a route of its own on
  the machine listener (ADR-0053). They are the two facts ADR-0012 puts
  on an agent and neither route reaches the other's. A report carries
  the epoch its assignment is on and is accepted only at the current
  one, so the pod a replaced operator left running writes nothing; the
  operator route is bounded by the assignment besides, exactly as the
  certificate route is.
- An organization's members reading the agents it holds, over the
  browser API under `/orgs/{org}/agents`. An agent is answered with the
  state its operator reports and the state it reports itself, kept as
  the separate facts ADR-0012 makes them, and with the operator or
  standalone supervisor holding it at which epoch. That surface reads
  only: it assigns no agent and issues no certificate.
- An organization **registering** an operator, under
  `/orgs/{org}/operators`, and its members reading the list (ADR-0045).
  A record is a GRN, who registered it and when, and it reports
  nothing, because garam never dials an operator. Registering issues
  the operator's credential in the same write, shown once, which it
  exchanges for short-lived tokens; re-issuing it voids the previous one
  and its tokens (ADR-0100). What the record grants is admission: the
  machine listener reads it per connection for a certificate and per
  request for a token, so an operator authenticates while its
  organization holds the record and stops when the record goes
  (ADR-0070). The identifier is supplied rather than minted, since an
  operator that already holds a certificate has to be registered under
  the GRN that certificate names. A name and description beside it are
  the organization's words about the record and not its identity
  (ADR-0074). Registering is granted by `operator:register`, reading by
  `operator:read`, renaming and describing by `operator:update`,
  re-issuing the credential by `operator:reissue` and deleting by
  `operator:delete`, each named action by action for the reason the
  agent actions are — re-issuing and deleting separately from
  registering, because each withdraws what an operator runs on.
- An organization **delegating** bounded execution authority to a
  hosted operator, under `/orgs/{org}/operators/{operator}/delegation`,
  and a member handing it one request-bound operation under
  `/orgs/{org}/operation-authorities`, which the operator reads back on
  the machine listener (ADR-0078), where it also proves a controller's
  current authority before each lifecycle decision, creates a managed
  agent and issues its first certificate under a member's
  `agent:create` reference (ADR-0077), and activates, inspects,
  deactivates and recovers an agent's execution. A grant is
  immutable and a replacement or withdrawal is final for what was
  minted under it; every mint, read back and proof is decided from
  current state. Delegating is `operator:delegate`, creating
  `agent:create`, configuring `agent:configure`, recovering
  `agent:recover` and cutting a legacy agent over `agent:cutover`
  (ADR-0086). Reading the operator's published templates and
  profiles, publishing a template version and reading an agent's
  execution are each an operation of their own, bound to the exact
  request target, and the console sends every one only to the control
  endpoint an administrator registered under `operator:delegate`
  (#1170); [operators.md](./operators.md) §Delegated operation authority
  holds the rest.
- An administrator **approving a standalone host** for one agent,
  under `/orgs/{org}/agent-enrollments`, by the hash of the host's
  bootstrap secret and the fingerprints of its two keys, and revoking
  the supervisor that approval registered under
  `/orgs/{org}/agent-supervisors` (ADR-0082). The host spends the
  approval on the machine listener for its supervisor's and its
  agent's first certificates, and the supervisor then activates that
  one agent on the execution routes and renews its own certificate.
  No operator, grant or hosted service is involved. Approving,
  withdrawing and revoking are `agent:enroll`;
  [agents.md](./agents.md) §Standalone hosts holds the rest, and what
  supervises the host's runtime is `garam standalone run`
  (ADR-0085), and its joint acceptance with garam and Sherlock is
  unverified (#1180).
- The agents an organization's members **defined**, under
  `/orgs/{org}/agent-definitions` — which operator is to run each, and
  with what values (ADR-0026). A definition is a record rather than a
  command: garam minted the agent's GRN, writes no `agents` row, and
  knows nothing about whether an operator acted on it. The operator the
  definition names reads its definitions on the machine listener and
  **claims** one: the claim binds the operator to the definition's
  agent GRN — the assignment, at a bumped epoch — and admits it to that
  agent's certificate route (ADR-0033). A definition is immutable and
  claimed once, and a definition's agent and one `POST /agents`
  created can never be the same agent. The operator a definition names
  is one the organization registered, which the schema's foreign key
  holds (ADR-0045). garam accepts no new definition: no route and no
  role writes one, and the schema refuses one, any changed value and a
  direct delete from whichever release sends it; every definition it
  holds stays readable, claimable and cut over as written, goes only
  with its organization, and an agent is created through its
  operator's control service (ADR-0090). A definition
  carries a name the organization calls the agent by, which
  `agent:rename` changes without touching the identity, operator or
  values (ADR-0075).
- An organization's **agent templates**, under
  `/orgs/{org}/agent-templates` — starting points a definition copied
  values from once, with nothing linking the two afterwards
  (ADR-0075). They are retired read-only: the schema refuses every
  write, and listing and reading one on `agent-template:read` is all
  that remains (ADR-0088).
- Messages addressed to an agent are held in a queue one reader at a
  time claims from, stored once per sender key and claimable again when
  a reader dies holding one (ADR-0013). A message is bounded by an
  attempt count and a claim is renewable under a ceiling, which is what
  ADR-0018 rests completion on.
- Posting, claiming, renewing and completing a message, on the machine
  listener. A caller acts on messages for the agent it authenticates as
  and no other; a claim is a long poll, and a message addressed to an
  agent that does not exist is stored and answered with success rather
  than refused (ADR-0013).
- A sender reading how far a message it sent has got, on the same
  listener (ADR-0055). It reads its own messages and no others, and the
  four states — waiting, held under a live claim, delivered, handed out
  no further — are derived from columns the queue already wrote.
  Absence is the answer for a message the thirty-day sweep deleted, and
  the read carries no body: a completed message has none (ADR-0034).
- **The adapter's delivery loop**, run beside one agent as `garam
  adapter` ([adapter.md](./adapter.md)): it claims a message for that
  agent, posts it into the agent platform's gateway over loopback under
  garam's message identifier with the sender garam verified, and
  completes the claim when the platform says a turn that consumed it
  committed. A turn that fails is an attempt and every completion that
  does not arrive is a redelivery, which the platform recognises by the
  identifier (ADR-0018, ADR-0079). It forwards the replies the platform
  records in its outbox to garam and removes each only on an
  acknowledgement of it. For an agent cut over to activated execution
  it first obtains its generation's activation from the lifecycle
  authority and reports the runtime's status back (ADR-0084).
  Forwarding what a turn says to whoever is watching is not part of it
  (see Out of scope).
- Whom a caller may post to, decided by policy on that listener
  (ADR-0024). A recipient of another organization is refused before a
  policy is read, unless the consent relation between the two
  organizations stands (ADR-0035): the boundary is a precondition
  rather than a policy question, because an unattached policy of the
  recipient's organization would otherwise admit every sender there
  is, and a relation both organizations hold and either withdraws is
  what may open it. A post no relation carries is not rejected — the
  refusal reaches the sender as an ordinary inbound message
  (ADR-0013). Inside the organization, a machine identity's own GRN
  grants it every recipient and a policy attached to it narrows that
  with a Deny; what is refused is answered 403 and never stored, which
  is the one refusal on this path that ADR-0013 does not shape.
- Watching an agent work, while somebody is (ADR-0022). A member of the
  organization a policy allows opens a server-sent stream under
  `/orgs/{org}/agents/{agent}/watch` and reads what the agent is saying
  now; the machine listener takes the turn events an adapter forwards
  and answers every claim and renewal with how many streams garam holds
  for that agent, which is what says whether to forward at all. Watching
  is granted by an action of its own, separate from reading one, and no
  role carries it: a grant arriving with membership is one nobody
  decided, so an organization that wants its members to watch says so in
  a policy and nobody watches until it does. Nothing accumulates for an
  agent nobody watches — no row, no event carried, no entry in either
  process's memory — and a stream starts where it was opened, because
  there was nothing to replay. The view is best effort and never a
  record: a gap in it cannot be told from an agent that said nothing, so
  what has to be true afterwards comes from the queue.
- Org / group / project structure; groups and projects are created,
  read, updated, deleted, and populated with members over the HTTP API
  under their organization's paths.
- An organization granting and withdrawing its own half of a
  cross-organization consent, and reading the relations it is a side
  of, over the browser API under `/orgs/{org}/consents`. All three are
  org-admin actions (ADR-0035).
- Sign-in through a provider abstraction — Google OIDC; server-side
  sessions.
- Sign-in with a username and a password, which is not a provider: it
  is verified against a stored Argon2id verifier and issues the same
  session (ADR-0041). Registering one needs an invitation.
- Invitation-based org membership with org roles (`owner`, `admin`,
  `member`) enforced on org-scoped HTTP APIs — including the group and
  project endpoints — through the authz evaluator seam.
- An OpenAPI-first HTTP API; server code is generated from the spec.
- Postgres-backed stores (sqlc) with migrations.
- Authorization decisions, expressed as IAM-style policy documents.
- Org-managed policy documents: stored, versioned append-only,
  activated and rolled back over the authz service, and cached per org
  in the engine. Org admins drive that lifecycle over the HTTP API
  under `/orgs/{org}/policies`, and attach a policy to any identity —
  a user, a group, or an agent (ADR-0008, ADR-0014); an unattached
  active policy still grants org-wide.
- A web console under `/console` for the org structure the API exposes:
  organizations are listed and created, members are invited by link and
  the invitation accepted, groups and projects are read, an operator is
  registered and the organization's operators read (ADR-0045), an agent
  is defined and the organization's definitions and agents read — each
  agent with the two states its operator and it report (ADR-0026,
  ADR-0053) — and the organization's cross-organization consent
  relations are read, its own half of one granted and withdrawn
  (ADR-0009, ADR-0035).
  Its shell is a collapsible sidebar over
  `Organizations → organization → section`, and it addresses resources
  by the GRN's last segment rather than printing GRNs (ADR-0011).
- Each of the three processes answers a liveness and a readiness probe
  on a listener a scheduler reaches without credentials, liveness
  reading nothing outside its own process (ADR-0021). Readiness reports
  what a replica cannot serve without, which is why an engine whose
  policy-change listener is reconnecting stays in rotation and answers
  from a cache that is reloading (ADR-0029). The web app answers the
  same two paths, and neither of its probes reads the API, so an API
  outage leaves the console in rotation rather than dark (ADR-0023).
- One identifier system across every resource (GRN).

## Out of scope (today)

- Secrets. What one is, is decided: garam holds a reference to a value
  an organization's own store keeps — never the value — and no surface
  of garam's serves one
  ([ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)).
  What is out of scope is the whole of it: no resource, no store, no
  surface, and the `secret` service type stands in the catalogue as
  planned ([grn-model.md](./grn-model.md)).
- Clusters as a resource an organization connects. Named in the intended
  structure and explicitly unplanned.
- A first account on an empty database. `/register` mints one only from
  an invitation (ADR-0006) and an invitation comes from an org admin, so
  a database holding neither is reached with real Google credentials or
  with rows written against it directly. The development-only provider
  that used to stand in for this was removed
  ([ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md))
  and nothing replaced it.
- Reconciling a cluster garam does not run. garam publishes two images
  and the chart at `deploy/charts/garam/` that describes a deployment
  (ADR-0042); which address each process answers on and which of them
  leaves the cluster stays the repository that owns the cluster's, and
  the chart renders no exposure at all.
- Workflow execution engine, scheduler, queue.
- Notifications.
- The rest of the agent surface. An operator creates an agent, obtains
  its certificate and reports what it sees of the pod, the agent
  reports itself, and a message is posted, claimed, renewed and
  completed (see In scope); what the listener does not serve is
  deprovisioning an agent. A state is reported and never expired, so
  nothing tells an agent that has gone quiet from one that is running
  (ADR-0053). The adapter carries messages and forwards nothing (see
  the watching entry below). An agent renews its own certificate on the
  certificate route (ADR-0040) and an operator renews its own by
  presenting it (ADR-0046); an operator's first is obtained by enrolling
  with the token registering it minted (ADR-0064), and an agent's first
  still arrives on the route its operator's claim admits it to, unless
  the agent was created managed: then it is signed over its
  controller's key under the creation's reference, or created
  standalone: then it is signed over the host's approved key with its
  supervisor's (ADR-0082). Once an agent's GRN cuts over, its renewal
  is inside its current lineage and a lost certificate is recovered
  rather than reissued (ADR-0078). A standalone host's supervisor runs
  on the host, not in garam (ADR-0085).
- Revoking a certificate. There is still no revocation list, no
  responder, and no serial the TLS handshake checks, so an agent's
  certificate authenticates a connection until it expires (ADR-0014).
  What it may then do is narrower: once the agent has been recovered,
  a certificate outside the new lineage is refused per request by the
  execution fence, though its connection still authenticates
  ([machine-identity.md](./machine-identity.md)
  §Generation and lineage). An operator is the other exception and it is
  not a certificate mechanism either: deleting the operator withdraws
  the record its certificate authenticates on, so every certificate
  naming it is refused from the next connection while none of them is
  revoked (ADR-0070). A standalone host's supervisor is the same:
  revoking or replacing it ends the registration its certificate
  authenticates on (ADR-0082).
- Proving an operator is the operator a record names by any means but
  the certificate. There is no service-account token and no exchange
  route: what registering hands over is a one-time enrollment ticket,
  which buys one certificate and authenticates nothing else (ADR-0064).
  `garam issue-certificate` still mints one out of band reading no
  record, and whether it should require a registered operator is a real
  question ADR-0045 raises and leaves open.
- Telling a sender that its message exhausted its attempts. ADR-0018
  makes that garam's obligation because the agent platform notifies no
  sender (Sherlock at
  [`40ef7acd02d134037132b8179380f54c7286d9a8`](https://github.com/garamsh/sherlock/tree/40ef7acd02d134037132b8179380f54c7286d9a8),
  read when the repository was named `gagent`); the queue stops
  redelivering
  an exhausted message and garam announces one to nobody. What a
  sender can do is ask (see In scope), which answers the question
  without discharging the obligation: ADR-0018 says garam tells, and a
  sender that never asks is still told nothing.
- The two ends of watching an agent work. garam's half is built (see In
  scope): the stream a browser reads, the watcher count on the poll, and
  the fan-out between the two listeners. Neither end of it is — no
  console route opens the stream, and the adapter forwards nothing: its
  delivery loop is built and the
  §Watching half of [adapter.md](./adapter.md) is not.
- Whether anything can ask the adapter whether it is healthy. What
  readiness would mean for it is decided — a monitoring subscription it
  opens fresh, so an answer says the gateway served a request rather
  than that a port is bound
  ([ADR-0013](./adr/0013-agent-messaging.md),
  §Readiness of [adapter.md](./adapter.md)) — and none of it is built:
  nothing opens that subscription and the adapter binds no listener, so
  it answers no Kubernetes probe. Its calls are bounded and a failed one
  is logged, so a stalled peer is no longer silent inside the pod; what
  is missing is anything outside it learning that the loop stopped
  making progress.
- Separated execution. ADR-0077 moves desired runtime values and
  templates to the Operator control service, cutting over one GRN at a
  time, and garam's own templates are retired read-only rather than
  moved (ADR-0088); ADR-0078 fences a runtime by its activated
  generation and credential lineage; ADR-0079 bounds interservice
  compatibility and replay. All three are accepted; garam's half of the
  generation fence is built (ADR-0080), the adapter this repository
  ships uses it under ADR-0084's contract, which a standalone host's
  supervisor serves (ADR-0085) and the Operator control service does not
  yet, acceptance with its peers is unverified (#1157), and the rest is
  unbuilt.
  [agents.md](./agents.md) §Separated execution carries the target.
- Chat channels. ADR-0013 makes garam a message API and leaves Slack,
  LINE and the rest to consumers of it; none is built, and whether any
  ships first-party is undecided.
- Resource-based policies: a statement names no `Principal`, so a
  policy reaches subjects through an identity attachment only
  (ADR-0008).
- Per-org direct user roles: a role stored on the user applies in every
  org it belongs to; scoping needs subject × role × scope persistence.
- Upstream OAuth providers beyond Google. A user's GRN identifier is
  the provider's subject, so adding a
  provider whose subjects carry anything outside the segment rule
  ([grn-model.md](./grn-model.md) §identifier) means deriving a
  conforming identifier rather than loosening the rule. Google's
  subject is decimal and fits; `auth0|abc123` would not. A subject
  under the reserved `local-` prefix is refused rather than adopted,
  because that prefix names a password account (ADR-0041).
- Editing groups and projects from the web console; those endpoints
  exist, the console reads them but does not write them.

## Module boundaries

- `api/` — OpenAPI specs (source of truth) + generated servers: the
  browser-facing listener's and the machine listener's (ADR-0019).
- `proto/` — authz gRPC service definition + buf-generated code.
- `cmd/garam/` — thin entrypoint for the cobra CLI.
- `migrations/` — SQL migrations (golang-migrate); sqlc schema source.
- `tests/integration/` — store round-trips, and the services over
  them, wired in process against real Postgres.
- `tests/e2e/` — the built binary run as the processes a deployment
  runs.
- `tests/image/` — black-box checks against a built image rather than
  against the app (`make web-image-e2e`).
- `web/` — Next.js frontend; reaches the API through a same-origin
  `/api/*` rewrite (ADR-0009). Its own toolchain and gate (`make web-ci`).
  `/` is the public landing page, `/login` the sign-in page and
  `/register` where an invitation becomes a password account
  (ADR-0041); the signed-in application lives under `/console`, whose
  shell and route structure are ADR-0011.
- `deploy/charts/garam/` — the Helm chart a cluster installs garam
  from (ADR-0042).
- `deploy/local/` — Docker Compose stack running Postgres, migrations,
  authz, api and (profile-gated) web on one machine; its `postgres`
  service also hosts the e2e tests' database (`make db-up`), kept
  separate from the stack's own. `deploy/local/README.md` §Databases is
  authoritative for which databases that one server holds, which of them
  a suite truncates or migrates, and what collects the ones a removed
  worktree leaves behind.
- `deploy/local/minikube/` — the same processes on a Kubernetes cluster
  on this machine, in the `garam-local` namespace (`make minikube-up`);
  a development artefact rather than manifests for a cluster garam does
  not run. `deploy/local/minikube/README.md` is authoritative for what
  it runs, how it is brought up and torn down, and where its key
  material and its images come from; that file's
  §What this proves and what it does not is authoritative for what
  running it establishes about a real deployment.
- `internal/grn/` — identifier; shared primitive.
- `internal/errorx/` — typed errors; shared primitive.
- `internal/wire/` — the conversions onto the wire formats the request
  surfaces share: the statement `Effect` and a policy's status with
  their authz-proto mappings, a statement rendered onto the authz
  contract, errorx kinds rendered as HTTP statuses and as gRPC codes
  and read back from them, and a gRPC error read back as an errorx
  error; shared primitive.
- `internal/keyfile/` — the owner-only rule every key material file is
  read under (ADR-0015); shared primitive.
- `internal/listfilter/` — the offset/limit filter a domain's list call
  is scoped by; shared primitive.
- `internal/pgxerr/` — the mapping from postgres driver errors to
  errorx kinds every postgres-backed store answers through. The five
  copies the volume audit found have been collapsed into this one.
  Shared primitive.
- `internal/pgxtype/` — the timestamptz conversion, both directions,
  every postgres-backed store binds and scans through. Shared
  primitive.
- `internal/cli/` — cobra commands + wiring (`serve api` | `serve authz`
  | `serve machine` | `migrate` | `probe` | `issue-certificate` |
  `adapter`).
- `internal/health/` — what liveness and readiness mean, and the two
  probes every serve command mounts (ADR-0021).
- `internal/operator/` — the record that an organization has an
  operator (ADR-0045), which holds no reported state; the operator's
  credential and the tokens it exchanges it for (ADR-0100); and the
  operator's own certificate: which one garam last issued it, and what
  it takes to replace that with the next (ADR-0046).
- `internal/agent/` — agent identities and the operator each is
  assigned to.
- `internal/ca/` — the per-organization certificate authority and the
  certificates it issues.
- `internal/machine/` — the machine identity a client certificate
  names, and the listener that verifies one.
- `internal/machineapi/` — handlers implementing the ServerInterface
  generated from the machine listener's spec.
- `internal/message/` — the durable queue a message reaches an agent
  through; one reader claims a message at a time, whose queue a caller
  may act on is decided here, and every hand-out is signed under keys
  kept here (ADR-0101).
- `internal/delivery/` — the adapter's delivery loop: it claims a
  message for one agent, posts it into that agent's gateway under
  garam's identifier, completes the claim the platform says committed,
  forwards the platform's outbox, and holds its generation's activation
  ([adapter.md](./adapter.md), ADR-0018, ADR-0079, ADR-0084). It runs
  beside the agent rather than inside garam, and holds what it is
  carrying in memory alone.
- `internal/watch/` — who is watching an agent work, and the turn events
  carried to them while they are (ADR-0022). It stores no event.
- `internal/workload/` — a standalone host's supervisor over its
  agent's workload: the runtime, workspace and adapter containers of
  one generation, the volumes they share, and the socket the adapter
  activates through ([agents.md](./agents.md) §Standalone hosts,
  ADR-0085). It runs on the host rather than inside garam, and
  `internal/workload/docker` is its engine.
- `internal/api/` — handlers implementing the generated ServerInterface.
- `internal/authn/` — sign-in provider abstraction, implemented by
  Google OIDC alone (ADR-0044), password verification, sessions.
- `internal/authz/` — policy engine: model, evaluator, policy store
  and per-org cache, gRPC servers.
- `internal/org/` — organizations, groups, projects, memberships,
  invitations.
- `internal/user/` — human identities.

`grn`, `errorx` and `listfilter` are the packages domains share with
each other; `keyfile` holds the one rule key material files are read
under, reached
by `ca`, by the `cli` wiring that reads the machine listener's pair
and the adapter's, and by `workload` for its record and serving key;
`wire` is reached by three packages and no others —
`api`, `machineapi` and `authz`, the first two at their request
boundaries and `authz` from its model, where the statement `Effect` it
declares is the type every surface shares
([authz.md](./authz.md) §Components) — and `health` is shared by the
three serve commands and imported by no domain. `machine` declares the
trust root it verifies against and `cli` satisfies it from `ca`
([structure.md](./structure.md) §Boundaries and dependency direction).
`api` sees several domains at once and enforces authentication (session
cookie) at the boundary; org-role checks go through the `Evaluator`
seam, implemented by the authz gRPC client (ADR-0003 stage 2). The
machine listener enforces its own authentication at its own boundary
and shares neither cookie nor session with `api`; `machineapi` reaches
the engine through an `Evaluator` seam of its own to decide what a
caller may send, and it and `internal/api` never import each other.
Neither imports `internal/authz`; the wire contract is
`proto/garam/authz/v1`, and the conversions onto it are declared once
in `internal/wire`. Both surfaces compose four boundaries — the
`agent`, `message` and `operator` domains, and `watch`, which is not a
domain ([domains.md](./domains.md)
§Sub-responsibilities in a boundary of their own). `watch` is extracted
rather than
held by either of them because a watch has a lifecycle neither
`message` nor `agent` contains, and its two ends fall on different
listeners — opened on the browser one, fed on the machine one — which
may not reach each other (ADR-0017, ADR-0022). `delivery` is the one
package that runs outside
garam: it is a client of the machine listener rather than part of it, so
it reads that listener's specification through the models generated from
it and imports no request surface and no domain.

## Core abstractions

| Type | Package | Responsibility |
|------|---------|----------------|
| `grn.GRN` | `internal/grn` | resource identifier (value type) |
| `errorx.Error` | `internal/errorx` | typed error with kind |
| `user.User` | `internal/user` | human identity |
| `operator.Operator` | `internal/operator` | an organization's record that it has an operator |
| `agent.Agent` | `internal/agent` | autonomous actor an operator provisions |
| `agent.Assignment` | `internal/agent` | agent-to-operator binding, fenced by an epoch |
| `agent.Service` | `internal/agent` | creates an agent for an operator, issues or renews the certificate of one, answers an operator the definitions naming it and claims one, and records the state each party reports |
| `ca.Authority` | `internal/ca` | an organization's certificate authority, its key sealed |
| `ca.Certificate` | `internal/ca` | signed client certificate identified by a SAN URI, over a key garam generated or one a request carried |
| `ca.Sealer` | `internal/ca` | seals key material against a database dump |
| `operator.Credential` | `internal/operator` | the certificate a connection authenticated with, as the renewal rule reads it |
| `operator.Service` | `internal/operator` | signs an enrolling operator's certificate request, mints one out of band, renews the one an operator presents, and exchanges a credential for a token |
| `operator.IssuedCredential` | `internal/operator` | the credential garam issues an operator and answers once (ADR-0100) |
| `operator.Enrollment` | `internal/operator` | the one-time token an operator obtains its first certificate with |
| `machine.Identity` | `internal/machine` | the GRN a verified client certificate names, and which certificate named it |
| `machine.Service` | `internal/machine` | verifies a presented certificate against its organization's authority |
| `machine.ServerCertificate` | `internal/machine` | what the listener presents, asked per handshake so a replaced one needs no restart |
| `message.Message` | `internal/message` | one message addressed to a recipient |
| `message.Claim` | `internal/message` | one reader's hold on a message, fenced by a token |
| `message.Keyring` | `internal/message` | the keys every hand-out is signed under: opens the one signing, rotates them, publishes the set (ADR-0101) |
| `watch.Event` | `internal/watch` | one forwarded turn event, carried and never stored |
| `watch.Subscription` | `internal/watch` | one open watch: the stream its holder reads |
| `watch.Service` | `internal/watch` | opens a watch, carries a forwarded event to the watchers of that agent, counts them |
| `delivery.Service` | `internal/delivery` | the loop beside one agent: claims a message, posts it into the agent's gateway, completes the claim a turn named |
| `workload.Supervisor` | `internal/workload` | a standalone host's supervisor: brings one generation up or adopts it, serves its adapter's activation, and drains it without force |
| `workload.Engine` | `internal/workload` | the container engine the supervisor alone holds; it has no stop with a timeout |
| `org.Organization` | `internal/org` | top-level tenant |
| `org.Group` | `internal/org` | user/agent bundle |
| `org.Project` | `internal/org` | unit of work |
| `org.Membership` | `internal/org` | user in an org, carries a `Role` |
| `org.Invitation` | `internal/org` | single-use token into an org |
| `authn.Provider` | `internal/authn` | sign-in provider; Google OIDC implements it and nothing else does (ADR-0044) |
| `authn.Password` | `internal/authn` | a password as typed; renders redacted everywhere (ADR-0041) |
| `authn.Verifier` | `internal/authn` | stored Argon2id verifier, carrying its own parameters (ADR-0041) |
| `authn.Session` | `internal/authn` | authenticated browser session |
| `authz.Object` | `internal/authz` | target interface |
| `authz.Evaluator` | `internal/authz` | policy evaluator (native, IAM-style) |
| `authz.Policy` | `internal/authz` | stored policy, versioned append-only |
| `authz.PolicyCache` | `internal/authz` | per-org parsed statement set |

## Decisions

| Decision | Reason | Where |
|----------|--------|-------|
| GRN org/project = owning resource identifiers | Tenancy is structural; reserved `root` owns organizations. | [grn-model.md](./grn-model.md), [ADR-0001](./adr/0001-grn-segments.md) |
| Policies are IAM-style documents, native evaluator | Deterministic deny-overrides; no runtime dependency. | [authz.md](./authz.md), [ADR-0002](./adr/0002-iam-style-policy-model.md) |
| Single cobra CLI, independent api/authz packages | One binary, two processes; engine reached over gRPC only. | [ADR-0003](./adr/0003-single-cli-topology.md) |
| Statements come from two origins, unioned | Org tables stay the source of truth for roles; the engine stays domain-free. | [authz.md](./authz.md), [ADR-0007](./adr/0007-hybrid-statement-collection.md) |
| `Evaluator` is an interface | Swap in-process / gRPC-client without touching call sites. | [authz.md](./authz.md) |
| Store interfaces live next to their domain | Implementation swap at the package boundary. | [domains.md](./domains.md) |
| OpenAPI spec is the API source of truth | Handlers implement a generated interface; drift fails CI. | [adr/0004-openapi-first-api.md](./adr/0004-openapi-first-api.md) |
| Sessions are server-side rows | Logout must revoke; opaque tokens, only hashes persisted. | [adr/0005-server-side-sessions.md](./adr/0005-server-side-sessions.md) |
| The development-only sign-in provider is removed | A password is a way in that ships (ADR-0041), and the provider forced `GARAM_COOKIE_SECURE=false` on any deployment that enabled it. | [adr/0044-the-development-only-sign-in-provider-is-removed.md](./adr/0044-the-development-only-sign-in-provider-is-removed.md) |
| A password is garam's own credential, not a provider | Argon2id verifies it and the verifier carries its parameters, so they can be raised later; an invitation is what mints the account. | [adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md) |
| Invitations are token-keyed, not GRN-addressed | The bearer token is the identifier; no new GRN service_type. | [adr/0006-invitation-tokens.md](./adr/0006-invitation-tokens.md) |
| The console is a sidebar shell over org sections | One structure for switching orgs, moving between sections, and knowing where you are. | [adr/0011-console-shell.md](./adr/0011-console-shell.md) |
| Agents are provisioned by operators garam never dials | An operator is a peer, so a user can supply their own; garam is never the side that connects. | [adr/0012-agent-operator-model.md](./adr/0012-agent-operator-model.md) |
| Messages reach an agent by polling, not by push | A queue in Postgres has no connection to route to, so any instance answers any poll. | [adr/0013-agent-messaging.md](./adr/0013-agent-messaging.md) |
| garam signs every message it hands out | An agent judges a message by a signature that travels with it, not by the path it arrived on, so it can verify garam without trusting garam's transport. | [adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md) |
| A deployment may hold message signing keys in OpenBao Transit; the file key-encryption key stays the default (unbuilt) | Transit signs, so no private half reaches the database or garam's memory, and an optional provider a deployment hosts itself is no vendor every deployment must reach; the authorities stay on the key-encryption key because they go at G4. | [adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md](./adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md) |
| Agents authenticate by mTLS on a listener of their own | A GRN in a SAN needs no user row; a separate listener needs no trusted proxy header. | [adr/0014-agent-authentication.md](./adr/0014-agent-authentication.md) |
| garam presents one pinned server certificate and certificates rotate without a restart | A server picks its certificate before the client sends one, so per-organization selection is unreachable; expiry is the only revocation, and a lifetime short enough to be one is unusable when every rotation is a restart. | [adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md) |
| An organization registers an operator, and a definition may name no other | A definition naming an operator nobody registered was a definition no operator would ever claim, and nothing reported it. | [adr/0045-an-organization-registers-an-operator.md](./adr/0045-an-organization-registers-an-operator.md) |
| An organization deletes an operator, and the record it withdraws is what that operator authenticated on | A route that removed a record and left the certificate working was what ADR-0045 refused; making the record what admits the certificate reverses ADR-0014's revocation-by-lifetime for an operator without a list, a responder or a stored serial. | [adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md) |
| An operator renews its certificate by presenting the one it holds | Provisioning one on a schedule needs a standing write path to the key-encryption key, for every tenant; and with no revocation, only a record of the certificate garam last issued keeps a stolen copy from renewing forever. | [adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md) |
| An operator is a machine identity of the same kind as an agent | One mechanism to secure and rotate; the assignment row already bounds what an operator may fetch. | [adr/0015-operator-identity.md](./adr/0015-operator-identity.md) |
| A CA's key is sealed by a key the process holds, not by a KMS | A database dump opens nothing; issuing depends on no hosted service, which is what lets a user bring their own operator. | [adr/0016-ca-key-custody.md](./adr/0016-ca-key-custody.md) |
| Machine identities are served by a process of their own | The surface that issues certificates holds every organization's key-encryption key, and the process serving browsers must not. | [adr/0017-machine-process.md](./adr/0017-machine-process.md) |
| An agent is watched only while someone is watching | garam cannot dial an adapter, so the poll it already holds carries the watcher count; an unwatched agent costs nothing. | [adr/0022-watching-an-agent.md](./adr/0022-watching-an-agent.md) |
| garam publishes a chart that describes its deployment | An executable artefact is checked against the source, and prose was falsified three times in one night; cluster state stays in one place because the consumer holds a values file rather than manifests. | [adr/0042-a-chart-is-the-description-of-a-garam-deployment.md](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md) |
| A message is completed by the turn-end event naming it | It is the only signal that pairs deterministically; a lost one redelivers, so delivery is at-least-once rather than exactly-once. | [adr/0018-delivery-acknowledgement.md](./adr/0018-delivery-acknowledgement.md) |
| The machine listener has a specification of its own | One contract per listener: the browser client generates no types for a surface it cannot call, and whoever writes an operator has something to build against. | [adr/0019-machine-openapi-spec.md](./adr/0019-machine-openapi-spec.md) |
| Liveness and readiness answer on a credential-free port | Restarting is the expensive answer, so only readiness reads a dependency; the mTLS listener admits no probe, so that process gets a port for them. | [adr/0021-liveness-and-readiness.md](./adr/0021-liveness-and-readiness.md) |
| The web app answers the same two probes | A fourth surface configured differently is configured wrong silently; probing page routes makes navigation a deployment interface, and a readiness that reads the API darkens the console during the outage it should report. | [adr/0023-web-probes.md](./adr/0023-web-probes.md) |
| Readiness reports what a replica cannot serve without | A probe gated on a dependency every replica shares fails on all of them at once, so it withdraws the fleet instead of degrading it; a replica reloading its policy cache can still answer, and staleness that ever needs refusing is refused by the check. | [adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md) |
| A policy decides what an agent may send | An attachment nothing reads is a control that is not there; the identity's own GRN grants its organization, so closing the gap breaks no working agent, and the boundary out of it is a precondition because no statement could hold it. | [adr/0024-policy-decides-what-an-agent-sends.md](./adr/0024-policy-decides-what-an-agent-sends.md) |
| A cross-organization message needs both organizations' consent | No policy can hold that boundary — a statement names no principal and an attachment cannot name a foreign subject — so what opens it is a relation row both organizations hold and either withdraws. | [adr/0035-a-cross-organization-message-needs-both-organizations-consent.md](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md) |

## Non-functional goals

- **Deterministic decisions** — the engine's `Explain` RPC can
  reproduce why a request was allowed or denied.
- **Transport-agnostic errors** — `errorx` flows over HTTP, gRPC, CLI
  without translation at the source; the kind-to-status mapping the
  HTTP surfaces answer with is declared once in `internal/wire`.
- **Testable in isolation** — every domain exposes an interface; unit
  tests substitute at module boundaries (mocks, in-memory test doubles
  where they exist).
- **Storage-agnostic** — Postgres, SQLite, Redis plug into the same
  `Store` interface without domain code changes.
- **Reproducible generation** — oapi-codegen, sqlc, buf, and mockery
  output is committed; CI regenerates and fails on drift
  (`make check-gen`).
