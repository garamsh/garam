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
  terminates TLS itself, requires a client certificate, and verifies it
  against the authority of the organization the certificate's own SAN
  names (ADR-0014). An agent and an operator authenticate the same way
  (ADR-0015), and neither touches a user row, a session or a membership.
  What that listener serves has a specification of its own (ADR-0019).
- An operator creating an agent and obtaining its certificate, on that
  listener. garam mints the agent's GRN in the operator's own
  organization and assigns it at an epoch; an operator obtains the
  certificate of an agent assigned to it at the current epoch and of no
  other, which is the whole of the authorization there (ADR-0015). The
  process that issues holds the key its authorities are sealed under
  and does not start without it (ADR-0016).
- An organization's members reading the agents it holds, over the
  browser API under `/orgs/{org}/agents`. An agent is answered with the
  state its operator reports and the state it reports itself, kept as
  the separate facts ADR-0012 makes them, and with the operator holding
  it at which epoch. That surface reads only: it assigns no agent and
  issues no certificate.
- An organization's members **defining** an agent, under
  `/orgs/{org}/agent-definitions` — which operator is to run it, and
  with what values (ADR-0026). A definition is a record rather than a
  command: garam mints the agent's GRN, writes no `agents` row, and
  knows nothing about whether an operator acted on it. It is immutable,
  because nothing fences a stale copy until the read an operator makes
  exists. Defining is granted by `agent:define`, named action by action
  because ADR-0012 puts provisioning outside garam and a wildcard would
  grant in advance what no endpoint may offer.
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
- Whom a caller may post to, decided by policy on that listener
  (ADR-0024). A recipient of another organization is refused before a
  policy is read, because an unattached policy of the recipient's
  organization would otherwise admit every sender there is. Inside the
  organization, a machine identity's own GRN grants it every recipient
  and a policy attached to it narrows that with a Deny; what is refused
  is answered 403 and never stored, which is the one refusal on this
  path that ADR-0013 does not shape.
- Watching an agent work, while somebody is (ADR-0022). A member of the
  organization a policy allows opens a server-sent stream under
  `/orgs/{org}/agents/{agent}/watch` and reads what the agent is saying
  now; the machine listener takes the turn events an adapter forwards and
  answers every claim and renewal with how many streams garam holds for
  that agent, which is what says whether to forward at all. Watching is
  granted by an action of its own, separate from reading one, and no role
  carries it: a grant arriving with membership is one nobody decided, so
  an organization that wants its members to watch says so in a policy and
  nobody watches until it does. Nothing
  accumulates for an agent nobody watches — no row, no event carried, no
  entry in either process's memory — and a stream starts where it was
  opened, because there was nothing to replay. The view is best effort
  and never a record: a gap in it cannot be told from an agent that said
  nothing, so what has to be true afterwards comes from the queue.
- Org / group / project structure; groups and projects are created,
  read, updated, deleted, and populated with members over the HTTP API
  under their organization's paths.
- Sign-in through a provider abstraction — Google OIDC, plus a
  development-only provider gated by `GARAM_DEV_AUTH` (ADR-0010);
  server-side sessions.
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
  the invitation accepted, groups and projects are read (ADR-0009). Its
  shell is a collapsible sidebar over `Organizations → organization →
  section`, and it addresses resources by the GRN's last segment rather
  than printing GRNs (ADR-0011).
- Each of the three processes answers a liveness and a readiness probe
  on a listener a scheduler reaches without credentials, liveness
  reading nothing outside its own process (ADR-0021). Readiness reports
  what a replica cannot serve without, which is why an engine whose
  policy-change listener is reconnecting stays in rotation and answers
  from a cache that is reloading (ADR-0029). The web app answers the
  same two paths, and neither of its probes reads the API, so an API
  outage leaves the console in rotation rather than dark (ADR-0023). It
  stays up without reporting the outage: `/login` answers 500 and the
  console pages answer empty, which is a defect in the pages rather
  than in the probes.
- One identifier system across every resource (GRN).

## Out of scope (today)

- The read an operator makes to learn of a definition. ADR-0026 decides
  the direction — garam holds what an agent should be, an operator asks
  — and leaves that surface, its authentication, the shape of the
  values, and the key an operator matches its own definitions by all
  undecided.
- **A definition becoming an agent.** The two paths do not conflict;
  they never meet. `agent.Service.Create` is the only route to an agent
  and to an assignment and it mints its own GRN, so the operator a
  definition names cannot claim it and the certificate route refuses it
  as unassigned. ADR-0026 expected a conflict between two ways of coming
  into existence and there is a gap instead. Closing it needs the
  operator read, a way to hold an agent garam did not mint for that
  caller, and a rule for which record wins.
- Secrets. Values an agent, a project or a cluster needs, held at
  organization and at project level, are part of the intended structure
  and nothing stores, serves or protects one today.
- Clusters as a resource an organization connects. Named in the intended
  structure and explicitly unplanned.
- Manifests for any cluster garam does not run. garam publishes two
  images and a contract — ports, probe paths, configuration and which
  of it is secret, and how key material must arrive (ADR-0020); the
  repository that owns a cluster owns what reconciles it.
- Workflow execution engine, scheduler, queue.
- Notifications.
- The rest of the agent surface. An operator creates an agent and
  obtains its certificate, and a message is posted, claimed, renewed
  and completed (see In scope); what the listener does not serve is the
  state an operator or an agent reports (ADR-0012) and deprovisioning
  an agent. The adapter is not built, and certificate renewal is
  reissue and restart rather than an endpoint (ADR-0014). No console
  page reads the agents the browser API answers.
- Telling a sender that its message exhausted its attempts. ADR-0018
  makes that garam's obligation because the agent platform notifies
  nobody; the queue stops redelivering an exhausted message and nothing
  yet reports one.
- The two ends of watching an agent work. garam's half is built (see In
  scope): the stream a browser reads, the watcher count on the poll, and
  the fan-out between the two listeners. Neither end of it is — no
  console route opens the stream, and the adapter that would forward turn
  events is not built (see The rest of the agent surface).
- Chat channels. ADR-0013 makes garam a message API and leaves Slack,
  LINE and the rest to consumers of it; none is built, and whether any
  ships first-party is undecided.
- Resource-based policies: a statement names no `Principal`, so a
  policy reaches subjects through an identity attachment only
  (ADR-0008).
- Per-org direct user roles: a role stored on the user applies in every
  org it belongs to; scoping needs subject × role × scope persistence.
- Upstream OAuth providers beyond Google. The dev provider (ADR-0010)
  is not one — it asserts a fixed local identity and contacts nothing.
  A user's GRN identifier is the provider's subject, so adding a
  provider whose subjects carry anything outside the segment rule
  ([grn-model.md](./grn-model.md) §identifier) means deriving a
  conforming identifier rather than loosening the rule. Google's
  subject is decimal and fits; `auth0|abc123` would not.
- Editing groups, projects and policies from the web console; those
  endpoints exist, the console reads them but does not write them.

## Module boundaries

- `api/` — OpenAPI specs (source of truth) + generated servers: the
  browser-facing listener's and the machine listener's (ADR-0019).
- `proto/` — authz gRPC service definition + buf-generated code.
- `cmd/garam/` — thin entrypoint for the cobra CLI.
- `migrations/` — SQL migrations (golang-migrate); sqlc schema source.
- `tests/e2e/` — store round-trips against real Postgres.
- `web/` — Next.js frontend; reaches the API through a same-origin
  `/api/*` rewrite (ADR-0009). Its own toolchain and gate (`make web-ci`).
  `/` is the public landing page and `/login` the sign-in page; the
  signed-in application lives under `/console`, whose shell and route
  structure are ADR-0011.
- `deploy/local/` — Docker Compose stack running Postgres, migrations,
  authz, api and (profile-gated) web on one machine; its `postgres`
  service also hosts the e2e tests' database (`make db-up`), kept
  separate from the stack's own.
- `internal/grn/` — identifier; shared primitive.
- `internal/errorx/` — typed errors; shared primitive.
- `internal/cli/` — cobra commands + wiring (`serve api` | `serve authz`
  | `serve machine` | `migrate`).
- `internal/health/` — what liveness and readiness mean, and the two
  probes every serve command mounts (ADR-0021).
- `internal/agent/` — agent identities and the operator each is
  assigned to.
- `internal/ca/` — the per-organization certificate authority and the
  certificates it issues.
- `internal/machine/` — the machine identity a client certificate
  names, and the listener that verifies one.
- `internal/machineapi/` — handlers implementing the ServerInterface
  generated from the machine listener's spec.
- `internal/message/` — the durable queue a message reaches an agent
  through; one reader claims a message at a time, and whose queue a
  caller may act on is decided here.
- `internal/watch/` — who is watching an agent work, and the turn events
  carried to them while they are (ADR-0022). It stores no event.
- `internal/api/` — handlers implementing the generated ServerInterface.
- `internal/authn/` — sign-in provider abstraction (Google OIDC,
  dev-only local), sessions.
- `internal/authz/` — policy engine: model, evaluator, policy store
  and per-org cache, gRPC servers.
- `internal/org/` — organizations, groups, projects, memberships, invitations.
- `internal/user/` — human identities.

`grn` and `errorx` are the only packages a domain shares; `health` is
shared by the three serve commands and imported by no domain. Domains
cooperate
through `grn.GRN`, never through each other's concrete types — which is
why `machine` declares the trust root it verifies against and `cli`
satisfies it from `ca`. `api`
sees several domains at once and enforces authentication (session
cookie) at the boundary; org-role checks go through the `Evaluator`
seam, implemented by the authz gRPC client (ADR-0003 stage 2). The
machine listener enforces its own authentication at its own boundary
and shares neither cookie nor session with `api`; `machineapi` composes
`agent` and `message` behind it, reaches the engine through an
`Evaluator` seam of its own to decide what a caller may send, and it
and `internal/api` never import each other. Neither imports
`internal/authz`; the wire contract is `proto/garam/authz/v1`. `watch` is
the one domain both compose: a watch is opened on the browser listener
and fed on the machine one, and neither listener may reach the other
(ADR-0017, ADR-0022).

## Core abstractions

| Type | Package | Responsibility |
|------|---------|----------------|
| `grn.GRN` | `internal/grn` | resource identifier (value type) |
| `errorx.Error` | `internal/errorx` | typed error with kind |
| `user.User` | `internal/user` | human identity |
| `agent.Agent` | `internal/agent` | autonomous actor an operator provisions |
| `agent.Assignment` | `internal/agent` | agent-to-operator binding, fenced by an epoch |
| `agent.Service` | `internal/agent` | creates an agent for an operator, issues the certificate of one assigned to it |
| `ca.Authority` | `internal/ca` | an organization's certificate authority, its key sealed |
| `ca.Certificate` | `internal/ca` | issued client certificate identified by a SAN URI |
| `ca.Sealer` | `internal/ca` | seals key material against a database dump |
| `machine.Identity` | `internal/machine` | the GRN a verified client certificate names |
| `machine.Service` | `internal/machine` | verifies a presented certificate against its organization's authority |
| `message.Message` | `internal/message` | one message addressed to a recipient |
| `message.Claim` | `internal/message` | one reader's hold on a message, fenced by a token |
| `watch.Event` | `internal/watch` | one forwarded turn event, carried and never stored |
| `watch.Subscription` | `internal/watch` | one open watch: the stream its holder reads |
| `watch.Service` | `internal/watch` | opens a watch, carries a forwarded event to the watchers of that agent, counts them |
| `org.Organization` | `internal/org` | top-level tenant |
| `org.Group` | `internal/org` | user/agent bundle |
| `org.Project` | `internal/org` | unit of work |
| `org.Membership` | `internal/org` | user in an org, carries a `Role` |
| `org.Invitation` | `internal/org` | single-use token into an org |
| `authn.Provider` | `internal/authn` | sign-in provider (Google OIDC; dev-only, ADR-0010) |
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
| Dev sign-in is a provider, not a bypass | Sessions stay identical to Google's; with the flag off no dev route exists. | [adr/0010-dev-auth-provider.md](./adr/0010-dev-auth-provider.md) |
| Invitations are token-keyed, not GRN-addressed | The bearer token is the identifier; no new GRN service_type. | [adr/0006-invitation-tokens.md](./adr/0006-invitation-tokens.md) |
| The console is a sidebar shell over org sections | One structure for switching orgs, moving between sections, and knowing where you are. | [adr/0011-console-shell.md](./adr/0011-console-shell.md) |
| Agents are provisioned by operators garam never dials | An operator is a peer, so a user can supply their own; garam is never the side that connects. | [adr/0012-agent-operator-model.md](./adr/0012-agent-operator-model.md) |
| Messages reach an agent by polling, not by push | A queue in Postgres has no connection to route to, so any instance answers any poll. | [adr/0013-agent-messaging.md](./adr/0013-agent-messaging.md) |
| Agents authenticate by mTLS on a listener of their own | A GRN in a SAN needs no user row; a separate listener needs no trusted proxy header. | [adr/0014-agent-authentication.md](./adr/0014-agent-authentication.md) |
| An operator is a machine identity of the same kind as an agent | One mechanism to secure and rotate; the assignment row already bounds what an operator may fetch. | [adr/0015-operator-identity.md](./adr/0015-operator-identity.md) |
| A CA's key is sealed by a key the process holds, not by a KMS | A database dump opens nothing; issuing depends on no hosted service, which is what lets a user bring their own operator. | [adr/0016-ca-key-custody.md](./adr/0016-ca-key-custody.md) |
| Machine identities are served by a process of their own | The surface that issues certificates holds every organization's key-encryption key, and the process serving browsers must not. | [adr/0017-machine-process.md](./adr/0017-machine-process.md) |
| An agent is watched only while someone is watching | garam cannot dial an adapter, so the poll it already holds carries the watcher count; an unwatched agent costs nothing. | [adr/0022-watching-an-agent.md](./adr/0022-watching-an-agent.md) |
| garam publishes images and a contract, not manifests | Cluster state changes in one place; a registry credential is unavailable both locally and on the target cluster. | [adr/0020-published-artifacts.md](./adr/0020-published-artifacts.md) |
| A message is completed by the turn-end event naming it | It is the only signal that pairs deterministically; a lost one redelivers, so delivery is at-least-once rather than exactly-once. | [adr/0018-delivery-acknowledgement.md](./adr/0018-delivery-acknowledgement.md) |
| The machine listener has a specification of its own | One contract per listener: the browser client generates no types for a surface it cannot call, and whoever writes an operator has something to build against. | [adr/0019-machine-openapi-spec.md](./adr/0019-machine-openapi-spec.md) |
| Liveness and readiness answer on a credential-free port | Restarting is the expensive answer, so only readiness reads a dependency; the mTLS listener admits no probe, so that process gets a port for them. | [adr/0021-liveness-and-readiness.md](./adr/0021-liveness-and-readiness.md) |
| The web app answers the same two probes | A fourth surface configured differently is configured wrong silently; probing page routes makes navigation a deployment interface, and a readiness that reads the API darkens the console during the outage it should report. | [adr/0023-web-probes.md](./adr/0023-web-probes.md) |
| Readiness reports what a replica cannot serve without | A probe gated on a dependency every replica shares fails on all of them at once, so it withdraws the fleet instead of degrading it; a replica reloading its policy cache can still answer, and staleness that ever needs refusing is refused by the check. | [adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md) |
| A policy decides what an agent may send | An attachment nothing reads is a control that is not there; the identity's own GRN grants its organization, so closing the gap breaks no working agent, and the boundary out of it is a precondition because no statement could hold it. | [adr/0024-policy-decides-what-an-agent-sends.md](./adr/0024-policy-decides-what-an-agent-sends.md) |

## Non-functional goals

- **Deterministic decisions** — the engine's `Explain` RPC can
  reproduce why a request was allowed or denied.
- **Transport-agnostic errors** — `errorx` flows over HTTP, gRPC, CLI
  without translation at the source; each HTTP surface maps a kind to a
  status at its own boundary (`internal/api`, `internal/machineapi`).
- **Testable in isolation** — every domain exposes an interface; unit
  tests substitute at module boundaries (mocks, in-memory test doubles
  where they exist).
- **Storage-agnostic** — Postgres, SQLite, Redis plug into the same
  `Store` interface without domain code changes.
- **Reproducible generation** — oapi-codegen, sqlc, buf, and mockery
  output is committed; CI regenerates and fails on drift
  (`make check-gen`).
