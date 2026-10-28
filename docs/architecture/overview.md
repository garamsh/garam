# Architecture Overview

garam is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

Legacy execution mode, the staged cut-over, every agent definition and
the certificate path are gone
([ADR-0118](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md)),
as are the operator enrollment token and shared-secret credential,
garam's retired templates and the browser's `operatorGrn`
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)).

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
- A listener machine identities authenticate on, `garam serve machine` —
  a process of its own, the third from the one image (ADR-0017). It
  terminates TLS itself, asks for no client certificate, and reads the
  token a request presents on every request
  ([ADR-0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md),
  ADR-0118). A request that presents none carries no identity rather
  than being refused there — which is what leaves a standalone host's
  enrollment and the token exchanges reachable to a caller that holds
  none, and costs nothing elsewhere, because every other operation
  refuses a request carrying no identity. An agent, an operator and a
  supervisor authenticate the same way, and none authenticates through
  a user row, a session or a membership.
  What that listener serves has a specification of its own (ADR-0019).
  garam is verified by a different root: the listener presents one
  certificate signed by a garam server authority the deployment holds
  and garam never does, every caller is given that root beside the
  credential garam issues it, and the listener reads its pair per
  handshake so a replaced one is served without a restart (ADR-0040).
  The process holds the key-encryption key its message signing keys are
  sealed under and does not start without it (ADR-0016, ADR-0101).
- An operator reporting what it sees of an agent's pod, and an agent
  reporting what it says about itself, each on a route of its own on
  the machine listener (ADR-0053). They are the two facts ADR-0012 puts
  on an agent and neither route reaches the other's. A report carries
  the epoch its assignment is on and is accepted only at the current
  one, so the pod a replaced operator left running writes nothing; the
  operator route is bounded by the assignment besides, exactly as the
  token mint is.
- An organization's members reading the agents it holds, over the
  browser API under `/orgs/{org}/agents`. An agent is answered with the
  state its operator reports and the state it reports itself, kept as
  the separate facts ADR-0012 makes them, and with the operator or
  standalone supervisor holding it at which epoch. That surface reads
  only: it assigns no agent and issues no credential.
- An organization **registering** an operator, under
  `/orgs/{org}/operators`, and its members reading the list (ADR-0045).
  A record is a GRN, who registered it and when, and it reports
  nothing, because garam never dials an operator. Registering issues
  nothing: the operator enters by a key issued on the record, whose
  signed assertions it exchanges for short-lived tokens (ADR-0102,
  ADR-0117). What the record grants is admission: the
  machine listener reads it on every request for a token, so an
  operator authenticates while its organization holds the record and
  stops when the record goes (ADR-0070). The identifier is supplied
  rather than minted, since the operator is configured with it before
  it is registered. A name and description beside it are
  the organization's words about the record and not its identity
  (ADR-0074). Registering is granted by `operator:register`, reading by
  `operator:read`, renaming and describing by `operator:update`,
  replacing or withdrawing a key by `operator:reissue` and deleting by
  `operator:delete`, each named action by action for the reason the
  agent actions are — re-keying and deleting separately from
  registering, because each withdraws what an operator runs on.
- An organization **delegating** bounded execution authority to a
  hosted operator, under `/orgs/{org}/operators/{operator}/delegation`,
  and a member handing it one request-bound operation under
  `/orgs/{org}/operation-authorities`, which the operator reads back on
  the machine listener (ADR-0078), where it also proves a controller's
  current authority before each lifecycle decision, creates a managed
  agent under a member's `agent:create` reference (ADR-0077), and
  activates, inspects,
  deactivates and recovers an agent's execution. A grant is
  immutable and a replacement or withdrawal is final for what was
  minted under it; every mint, read back and proof is decided from
  current state. Delegating is `operator:delegate`, creating
  `agent:create`, configuring `agent:configure`, recovering
  `agent:recover` and moving one to another controller `agent:replace`
  (ADR-0107). Reading the operator's published templates and
  profiles, publishing a template version and reading an agent's
  execution are each an operation of their own, bound to the exact
  request target, and the console sends every one only to the control
  endpoint an administrator registered under `operator:delegate`
  (#1170); [operators.md](./operators.md) §Delegated operation authority
  holds the rest.
- An administrator **approving a standalone host** for one agent,
  under `/orgs/{org}/agent-enrollments`, by the hash of the host's
  bootstrap secret and the placement its supervisor runs the agent
  under, its own three containers or a runtime the host runs
  directly (ADR-0111), and revoking
  the supervisor that approval registered under
  `/orgs/{org}/agent-supervisors` (ADR-0082). The host spends the
  approval on the machine listener for its supervisor's credential,
  which the supervisor exchanges for tokens, mints its agent's tokens
  with, and activates that one agent on the execution routes with
  (ADR-0100).
  No operator, grant or hosted service is involved. Approving,
  withdrawing and revoking are `agent:enroll`;
  [agents.md](./agents.md) §Standalone hosts holds the rest, and what
  supervises the host's runtime is `garam standalone run`
  (ADR-0085), and its joint acceptance with garam and Sherlock is
  unverified (#1180).
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
  acknowledgement of it. It first obtains its generation's activation
  from the lifecycle authority and reports the runtime's status back
  (ADR-0084).
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
  registered and the organization's operators read (ADR-0045), the
  organization's agents are read — each with the two states its
  operator and it report (ADR-0053) — and the organization's
  cross-organization consent relations are read, its own half of one
  granted and withdrawn (ADR-0009, ADR-0035).
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
- The rest of the agent surface. An operator creates an agent, mints
  its tokens and reports what it sees of the pod, the agent reports
  itself, and a message is posted, claimed, renewed and completed (see
  In scope); what the listener does not serve is deprovisioning an
  agent. A state is reported and never expired, so nothing tells an
  agent that has gone quiet from one that is running (ADR-0053). The
  adapter carries messages and forwards nothing (see the watching entry
  below). A standalone host's supervisor runs on the host, not in garam
  (ADR-0085).
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
  time, and garam's own templates are gone rather than moved
  (ADR-0117); ADR-0078 fences a runtime by its activated
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

The layout is ADR-0132's, and
[structure.md](./structure.md) §Services holds the rules it keeps. The
list below is today's tree
([ADR-0125](./adr/0125-the-repository-is-a-monorepo-of-services-laid-out-by-responsibility.md),
[ADR-0132](./adr/0132-a-root-folder-is-an-executable-unit-named-service-surface-and-code-libraries-sit-under-lib.md)).

- `platform/api/` — OpenAPI specs (source of truth) + generated servers:
  the browser-facing listener's and the machine listener's (ADR-0019).
- `platform/proto/` — authz gRPC service definition + buf-generated
  code.
- `platform/cmd/garam/` — thin entrypoint for the server's cobra
  command tree.
- `platform/migrations/` — SQL migrations (golang-migrate); sqlc schema
  source.
- `platform/tests/integration/` — store round-trips, and the services
  over them, wired in process against real Postgres.
- `platform/tests/e2e/` — the built server binary run as the processes
  a deployment runs, and the built client CLI against them.
- `platform/tests/image/` — black-box checks against a built image
  rather than against the app (`make web-image-e2e`).
- `platform-web/` — the Next.js console, on a host of its own
  (ADR-0131); reaches the API through a same-origin `/api/*` rewrite
  (ADR-0009). Its own toolchain and gate (`make -C platform-web ci`).
  The signed-in application lives under `/console`, whose shell and
  route structure are ADR-0011.
- `site-web/` — the Next.js site: `/` is the public landing page,
  `/login` the sign-in page and `/register` where an invitation becomes
  a password account (ADR-0041). It keeps the same rewrite, and sends
  every `/console` address to the console's host (ADR-0131).
- `lib/design/` — the components, styles and tokens both apps import;
  `lib/platform-sdk-ts/` — the client of the platform's API both apps
  call it through.
- `platform-cli/` — the client CLI, the `garam` a person signs in with
  (ADR-0120, [cli.md](./cli.md)): `login` | `logout` | `whoami` |
  `orgs` | `operators` | `agents` and the commands derived from the
  browser API. It reaches the platform through
  `lib/platform-sdk-go/`, the Go client generated from
  `platform/api/openapi.yaml`, and never imports `platform/internal/`.
- `lib/grn/`, `lib/errorx/` and `lib/clisession/` — the identifier,
  the typed errors, and a CLI's session with the platform, each a
  library module both the platform's and cowork's modules can link.
- `deploy/charts/garam/` — the Helm chart a cluster installs garam
  from (ADR-0042).
- `deploy/local/` — Docker Compose stack running Postgres, migrations,
  authz, api and (profile-gated) the console and the site on one
  machine; its `postgres` service also hosts the e2e tests' database
  (`make db-up`), kept separate from the stack's own.
  `deploy/local/README.md` §Databases is authoritative for which
  databases that one server holds, which of them a suite truncates or
  migrates, and what collects the ones a removed worktree leaves
  behind.
- `deploy/local/minikube/` — the same processes on a Kubernetes cluster
  on this machine, in the `garam-local` namespace
  (`make -C deploy/local minikube-up`); a development artefact rather
  than manifests for a cluster garam does not run.
  `deploy/local/minikube/README.md` is authoritative for what it runs,
  how it is brought up and torn down, and where its key material and its
  images come from; `minikube/README.md` §What this proves and what it
  does not is authoritative for what running it establishes about a real
  deployment.
- `platform/internal/grn/` — identifier; shared primitive. It forwards
  to `lib/grn/` (ADR-0125, ADR-0132) until #1708.
- `platform/internal/errorx/` — typed errors; shared primitive. It
  forwards to `lib/errorx/` (ADR-0125, ADR-0132) until #1708.
- `platform/internal/wire/` — the conversions onto the wire formats the
  request surfaces share: the statement `Effect` and a policy's status
  with their authz-proto mappings, a statement rendered onto the authz
  contract, errorx kinds rendered as HTTP statuses and as gRPC codes and
  read back from them, and a gRPC error read back as an errorx error;
  shared primitive.
- `platform/internal/keyfile/` — the owner-only rule every key material
  file is read under (ADR-0015); shared primitive.
- `platform/internal/listfilter/` — the offset/limit filter a domain's
  list call is scoped by, and the needle a search matches by; shared
  primitive.
- `platform/internal/pgxerr/` — the mapping from postgres driver errors
  to errorx kinds every postgres-backed store answers through. The five
  copies the volume audit found have been collapsed into this one.
  Shared primitive.
- `platform/internal/pgxtype/` — the timestamptz conversion, both
  directions, every postgres-backed store binds and scans through, and
  the `ILIKE` pattern a search binds. Shared primitive.
- `platform/internal/cli/` — the server's cobra commands + wiring
  (`serve api` | `serve authz` | `serve machine` | `migrate` | `probe`
  | `adapter`).
- `platform/internal/health/` — what liveness and readiness mean, and
  the two probes every serve command mounts (ADR-0021).
- `platform/internal/operator/` — the record that an organization has an
  operator (ADR-0045), which holds no reported state; the operator's
  keys and the tokens it exchanges them for (ADR-0102).
- `platform/internal/agent/` — agent identities and the operator each is
  assigned to.
- `platform/internal/ca/` — the sealer key material is sealed under
  against a database dump, opened by the key-encryption key (ADR-0016).
- `platform/internal/machine/` — the machine identity a token names, the
  listener that reads one, and the evidence of how each caller
  authenticated, kept by `platform/internal/machine/postgres`
  (ADR-0109).
- `platform/internal/machineapi/` — handlers implementing the
  ServerInterface generated from the machine listener's spec.
- `platform/internal/message/` — the durable queue a message reaches an
  agent through; one reader claims a message at a time, whose queue a
  caller may act on is decided here, and every hand-out is signed under
  keys kept here (ADR-0101) or, where the deployment configures one, in
  an OpenBao Transit key `platform/internal/message/transit` reaches
  (ADR-0104).
- `platform/internal/audit/` — the record of every message send garam
  decides, written with the send's outcome and never with its body
  (ADR-0106).
- `platform/internal/delivery/` — the adapter's delivery loop: it claims
  a message for one agent, posts it into that agent's gateway under
  garam's identifier, completes the claim the platform says committed,
  forwards the platform's outbox, and holds its generation's activation
  ([adapter.md](./adapter.md), ADR-0018, ADR-0079, ADR-0084). It runs
  beside the agent rather than inside garam, and holds what it is
  carrying in memory alone.
- `platform/internal/watch/` — who is watching an agent work, and the
  turn events carried to them while they are (ADR-0022). It stores no
  event.
- `platform/internal/workload/` — a standalone host's supervisor over
  its agent's workload: the runtime, workspace and adapter containers of
  one generation, the volumes they share, and the socket the adapter
  activates through ([agents.md](./agents.md) §Standalone hosts,
  ADR-0085). It runs on the host rather than inside garam, and
  `platform/internal/workload/docker` is its engine.
- `platform/internal/api/` — handlers implementing the generated
  ServerInterface.
- `platform/internal/authn/` — sign-in provider abstraction, implemented
  by Google OIDC alone (ADR-0044), password verification, sessions.
- `platform/internal/authz/` — policy engine: model, evaluator, policy
  store and per-org cache, gRPC servers.
- `platform/internal/org/` — organizations, groups, projects,
  memberships, invitations.
- `platform/internal/user/` — human identities.

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
caller may send, and it and `platform/internal/api` never import each
other. Neither imports `platform/internal/authz`; the wire contract is
`platform/proto/garam/authz/v1`, and the conversions onto it are
declared once in `platform/internal/wire`. Both surfaces compose four
boundaries — the `agent`, `message` and `operator` domains, and `watch`,
which is not a domain (`domains.md` §Sub-responsibilities in a boundary
of their own). `watch` is extracted rather than held by either of them
because a watch has a lifecycle neither `message` nor `agent` contains,
and its two ends fall on different listeners — opened on the browser
one, fed on the machine one — which may not reach each other (ADR-0017,
ADR-0022). `delivery` is the one package that runs outside garam: it is
a client of the machine listener rather than part of it, so it reads
that listener's specification through the models generated from it and
imports no request surface and no domain.

## Core abstractions

| Type | Package | Responsibility |
|------|---------|----------------|
| `grn.GRN` | `platform/internal/grn` | resource identifier (value type) |
| `errorx.Error` | `platform/internal/errorx` | typed error with kind |
| `user.User` | `platform/internal/user` | human identity |
| `operator.Operator` | `platform/internal/operator` | an organization's record that it has an operator |
| `agent.Agent` | `platform/internal/agent` | autonomous actor an operator provisions |
| `agent.Assignment` | `platform/internal/agent` | agent-to-operator binding, fenced by an epoch |
| `agent.Service` | `platform/internal/agent` | records the state each party reports, and mints an agent's tokens for the operator or supervisor holding it |
| `ca.Sealer` | `platform/internal/ca` | seals key material against a database dump |
| `operator.Service` | `platform/internal/operator` | exchanges a key's signed assertion for a token, and proves an operator by a token shown to another |
| `machine.Identity` | `platform/internal/machine` | the GRN a token names, with the token's serial and dates |
| `machine.Recheck` | `platform/internal/machine` | reads an identity's token dates and registration again for one decision |
| `machine.ServerCertificate` | `platform/internal/machine` | what the listener presents, asked per handshake so a replaced one needs no restart |
| `machine.Evidence` | `platform/internal/machine` | each caller's last authentication by path, the G3 gate's inventory, never a credential (ADR-0109) |
| `message.Message` | `platform/internal/message` | one message addressed to a recipient |
| `message.Claim` | `platform/internal/message` | one reader's hold on a message, fenced by a token |
| `message.Keyring` | `platform/internal/message` | the keys every hand-out is signed under: opens the one signing, rotates them, publishes the set (ADR-0101) |
| `message.Custodian` | `platform/internal/message` | holds a signing key's private half: sealed under the key-encryption key, or in an OpenBao Transit key (`platform/internal/message/transit`) that signs for it (ADR-0104) |
| `audit.Send` | `platform/internal/audit` | one message send as garam decided it: the parties, the decision and the message it stored, never a body (ADR-0106) |
| `watch.Event` | `platform/internal/watch` | one forwarded turn event, carried and never stored |
| `watch.Subscription` | `platform/internal/watch` | one open watch: the stream its holder reads |
| `watch.Service` | `platform/internal/watch` | opens a watch, carries a forwarded event to the watchers of that agent, counts them |
| `delivery.Service` | `platform/internal/delivery` | the loop beside one agent: claims a message, posts it into the agent's gateway, completes the claim a turn named |
| `workload.Supervisor` | `platform/internal/workload` | a standalone host's supervisor: brings one generation up or adopts it, serves its adapter's activation, and drains it without force |
| `workload.Engine` | `platform/internal/workload` | the container engine the supervisor alone holds; it has no stop with a timeout |
| `org.Organization` | `platform/internal/org` | top-level tenant |
| `org.Group` | `platform/internal/org` | user/agent bundle |
| `org.Project` | `platform/internal/org` | unit of work |
| `org.Membership` | `platform/internal/org` | user in an org, carries a `Role` |
| `org.Invitation` | `platform/internal/org` | single-use token into an org |
| `authn.Provider` | `platform/internal/authn` | sign-in provider; Google OIDC implements it and nothing else does (ADR-0044) |
| `authn.Password` | `platform/internal/authn` | a password as typed; renders redacted everywhere (ADR-0041) |
| `authn.Verifier` | `platform/internal/authn` | stored Argon2id verifier, carrying its own parameters (ADR-0041) |
| `authn.Session` | `platform/internal/authn` | authenticated browser session |
| `authz.Object` | `platform/internal/authz` | target interface |
| `authz.Evaluator` | `platform/internal/authz` | policy evaluator (native, IAM-style) |
| `authz.Policy` | `platform/internal/authz` | stored policy, versioned append-only |
| `authz.PolicyCache` | `platform/internal/authz` | per-org parsed statement set |

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
| The CLI signs in by the device grant and holds a session as a bearer | A terminal can drive neither a redirect provider nor the console's password form without seeing the password; a session row keeps one revocation for both. | [adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md) |
| The development-only sign-in provider is removed | A password is a way in that ships (ADR-0041), and the provider forced `GARAM_COOKIE_SECURE=false` on any deployment that enabled it. | [adr/0044-the-development-only-sign-in-provider-is-removed.md](./adr/0044-the-development-only-sign-in-provider-is-removed.md) |
| A password is garam's own credential, not a provider | Argon2id verifies it and the verifier carries its parameters, so they can be raised later; an invitation is what mints the account. | [adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md) |
| Invitations are token-keyed, not GRN-addressed | The bearer token is the identifier; no new GRN service_type. | [adr/0006-invitation-tokens.md](./adr/0006-invitation-tokens.md) |
| The console is a sidebar shell over org sections | One structure for switching orgs, moving between sections, and knowing where you are. | [adr/0011-console-shell.md](./adr/0011-console-shell.md) |
| Agents are provisioned by operators garam never dials | An operator is a peer, so a user can supply their own; garam is never the side that connects. | [adr/0012-agent-operator-model.md](./adr/0012-agent-operator-model.md) |
| Messages reach an agent by polling, not by push | A queue in Postgres has no connection to route to, so any instance answers any poll. | [adr/0013-agent-messaging.md](./adr/0013-agent-messaging.md) |
| garam signs every message it hands out | An agent judges a message by a signature that travels with it, not by the path it arrived on, so it can verify garam without trusting garam's transport. | [adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md) |
| A deployment may hold message signing keys in OpenBao Transit; the file key-encryption key stays the default | Transit signs, so no private half reaches the database or garam's memory, and an optional provider a deployment hosts itself is no vendor every deployment must reach. | [adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md](./adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md) |
| Every machine caller authenticates with a garam-issued credential and short-lived tokens over server TLS | A token read on every request is withdrawn on the next one, where a certificate stays valid until it expires; a separate listener needs no trusted proxy header. | [adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md) |
| Every legacy path goes now, and the certificate path with them | The owner directed it, and aurora's evidence showed no caller on the certificate path; the sealed authority rows stay until the owner decides their drop. | [adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md](./adr/0118-every-legacy-path-goes-now-with-nothing-migrated-and-the-certificate-path-goes-with-them.md) |
| garam presents one pinned server certificate that rotates without a restart | A server picks its certificate before the client sends one, so per-organization selection is unreachable; every caller pins one root, and a replaced pair is served on the next handshake. | [adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md) |
| An organization registers an operator | The registration is the record the console lists and the machine listener admits an operator on, so an identity nobody registered is one nothing reports. | [adr/0045-an-organization-registers-an-operator.md](./adr/0045-an-organization-registers-an-operator.md) |
| An organization deletes an operator, and the record it withdraws is what that operator authenticated on | A route that removed a record and left the credential working was what ADR-0045 refused; the record is what admits the operator's token on every request. | [adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md) |
| An operator is a machine identity of the same kind as an agent | One mechanism to secure and rotate; the assignment row already bounds what an operator may fetch. | [adr/0015-operator-identity.md](./adr/0015-operator-identity.md) |
| Key material garam stores is sealed by a key the process holds, not by a KMS | A database dump opens nothing, and sealing depends on no hosted service. | [adr/0016-ca-key-custody.md](./adr/0016-ca-key-custody.md) |
| Machine identities are served by a process of their own | The surface that signs messages holds the key-encryption key, and the process serving browsers must not. | [adr/0017-machine-process.md](./adr/0017-machine-process.md) |
| An agent is watched only while someone is watching | garam cannot dial an adapter, so the poll it already holds carries the watcher count; an unwatched agent costs nothing. | [adr/0022-watching-an-agent.md](./adr/0022-watching-an-agent.md) |
| garam publishes a chart that describes its deployment | An executable artefact is checked against the source, and prose was falsified three times in one night; cluster state stays in one place because the consumer holds a values file rather than manifests. | [adr/0042-a-chart-is-the-description-of-a-garam-deployment.md](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md) |
| A message is completed by the turn-end event naming it | It is the only signal that pairs deterministically; a lost one redelivers, so delivery is at-least-once rather than exactly-once. | [adr/0018-delivery-acknowledgement.md](./adr/0018-delivery-acknowledgement.md) |
| The machine listener has a specification of its own | One contract per listener: the browser client generates no types for a surface it cannot call, and whoever writes an operator has something to build against. | [adr/0019-machine-openapi-spec.md](./adr/0019-machine-openapi-spec.md) |
| Liveness and readiness answer on a credential-free port | Restarting is the expensive answer, so only readiness reads a dependency; the machine listener serves its specification's operations alone, so that process gets a port for them. | [adr/0021-liveness-and-readiness.md](./adr/0021-liveness-and-readiness.md) |
| The web app answers the same two probes | A fourth surface configured differently is configured wrong silently; probing page routes makes navigation a deployment interface, and a readiness that reads the API darkens the console during the outage it should report. | [adr/0023-web-probes.md](./adr/0023-web-probes.md) |
| Readiness reports what a replica cannot serve without | A probe gated on a dependency every replica shares fails on all of them at once, so it withdraws the fleet instead of degrading it; a replica reloading its policy cache can still answer, and staleness that ever needs refusing is refused by the check. | [adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md) |
| A policy decides what an agent may send | An attachment nothing reads is a control that is not there; the identity's own GRN grants its organization, so closing the gap breaks no working agent, and the boundary out of it is a precondition because no statement could hold it. | [adr/0024-policy-decides-what-an-agent-sends.md](./adr/0024-policy-decides-what-an-agent-sends.md) |
| A cross-organization message needs both organizations' consent | No policy can hold that boundary — a statement names no principal and an attachment cannot name a foreign subject — so what opens it is a relation row both organizations hold and either withdraws. | [adr/0035-a-cross-organization-message-needs-both-organizations-consent.md](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md) |

## Non-functional goals

- **Deterministic decisions** — the engine's `Explain` RPC can
  reproduce why a request was allowed or denied.
- **Transport-agnostic errors** — `errorx` flows over HTTP, gRPC, CLI
  without translation at the source; the kind-to-status mapping the
  HTTP surfaces answer with is declared once in
  `platform/internal/wire`.
- **Testable in isolation** — every domain exposes an interface; unit
  tests substitute at module boundaries (mocks, in-memory test doubles
  where they exist).
- **Storage behind each domain's own interface** — each domain declares
  the store it needs, and its Postgres implementation moves to
  `platform/internal/<domain>/postgres/`
  ([ADR-0069](./adr/0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md));
  a second implementation would plug in behind that domain's interface.
  [domains.md](./domains.md) §Store implementation status records which
  domains have moved, and that no other store is planned.
- **Reproducible generation** — oapi-codegen, sqlc, buf, and mockery
  output is committed; CI regenerates and fails on drift
  (`make check-gen`).
