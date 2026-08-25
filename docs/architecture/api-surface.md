# API Surface

What garam serves to a caller outside it: which listeners exist, what admits a caller to each, and what the file describing each one is authoritative for.

## Contents
- What it is
- The surfaces
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
| Machine | `garam serve machine` | `api/machine.yaml` | an operator or an agent holding a certificate garam issued (ADR-0014, ADR-0015) |
| Policy engine | `garam serve authz` | `proto/garam/authz/v1` | one of the two listeners above, over gRPC (ADR-0003) |
| Probes and metrics | every serve command, and the web app | no specification | anything that can open the port (ADR-0021, ADR-0023) |

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

## What admits a caller

**Browser-facing.** The `garam_session` cookie, which is a server-side
row keyed by the token's hash
([ADR-0005](./adr/0005-server-side-sessions.md)). Signing in mints it,
either through a redirect provider — Google is the only one left
([ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md))
— or against a password garam verifies itself, which is an endpoint
rather than a provider
([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
Starting a login, its callback, the provider list, and the probes and
metrics this process serves from the same router carry no credential;
[ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)
names that set and bounds the one write in it a caller can make
unconditionally. A callback that finds no live record of the login it
claims to complete is refused as unauthenticated
([ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md)).

**Machine.** A client certificate whose SAN URI holds the caller's GRN
and nothing else — no roles and no claims
([ADR-0014](./adr/0014-agent-authentication.md)). The chain is verified
per connection against the authority the certificate's own organization
names, never against a pool holding every organization's, and both ends
read their certificate per handshake, so a replaced one is presented on
the next connection with nothing restarted
([ADR-0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)).
Nothing here has a session, a user row or an organization membership to
read, and an operator reaches it on the same terms an agent does
([ADR-0015](./adr/0015-operator-identity.md)).

**No listener mints a first certificate.** The command that issues one
is a root-level subcommand no serve path references, so a running
server cannot be made to grant an identity
([ADR-0032](./adr/0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)).
Every certificate after it is replaced over the connection the previous
one authenticated: an operator's on the machine listener
([ADR-0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md)),
an agent's on the route its claim admits it to (ADR-0040).

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
  scoped by the caller's own identity, and the renewal route names no
  subject, so there is nothing to authorize past the handshake
  ([ADR-0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md),
  ADR-0046).
- **What a machine caller may send** is the exception, and it is a
  policy question: the engine is asked about each recipient
  ([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)), after
  a precondition the listener holds itself — the recipient's
  organization is the caller's, or a consent relation between the two
  stands
  ([ADR-0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)).
- **Asking the engine has a precondition of its own**: a caller may ask
  only about a subject belonging to the object's organization
  ([ADR-0025](./adr/0025-engine-scope-is-a-caller-obligation.md)),
  which the engine now applies itself rather than trusting a caller to
  ([ADR-0038](./adr/0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md)).
- **Watching an agent is its own grant**, separate from reading one
  ([ADR-0022](./adr/0022-watching-an-agent.md)).
- **No surface serves a secret's value**, this one included
  ([ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)).

## What a specification is authoritative for

One specification per listener, and a third listener repeats that
rather than reopening it (ADR-0019). `api/openapi.yaml` is the source
of truth for what `garam serve api` serves (ADR-0004);
`api/machine.yaml` for what `garam serve machine` serves (ADR-0019).

That authority is enforced rather than asserted. `make gen` writes
`api/server.gen.go` and `api/machine/server.gen.go` from the two
specifications, `internal/api` and `internal/machineapi` implement what
is generated, and `make check-gen` — which `make ci` runs — fails when
the committed output and the specifications disagree. A path, a
parameter or a model therefore cannot diverge from the file describing
it.

The other two surfaces are fixed differently. The engine's wire
contract is `proto/garam/authz/v1`, generated and checked the same way.
The probes have no specification at all: their paths, and the ports
they answer on, are interface rather than implementation, so changing
one supersedes the ADR that set it instead of editing a handler
(ADR-0021, [ADR-0023](./adr/0023-web-probes.md)).

## What nothing checks

**Generation covers the shape of an operation and nothing else.** Every
`description` and every tag description in both specifications is prose
no generator reads. A decision that changes what an operation does
leaves the generated output identical, so `make ci` passes while the
text still states the old behaviour.

That is not hypothetical. ADR-0035 changed what garam does with a
message it may not route. [domains.md](./domains.md) moved with it and
`api/machine.yaml` did not, in two places: `postMessage`'s description
stated the new behaviour as explicitly not what happens, corrected in
#530, and the `message` tag's description, corrected in #532. Both were
found by someone running the service rather than by anything in the
repository.

## Where a change to a surface lands

A decision that changes what admits a caller, what a caller may reach,
or what an operation answers is not landed when the handlers are. It
has four places to reach, guarded by different things:

- **The ADR** that records it.
- **This document**, where the surfaces and what admits a caller are
  the present state. Rule 3 in [README.md](./README.md) pairs an ADR
  with the responsibility documents it affects, and this is the one the
  API has.
- **The specification for the listener it changed** — schema and prose
  both. The schema is guarded by `make check-gen`; the prose is guarded
  by nobody, which is the whole of §What nothing checks.
- **The responsibility document for the domain whose behaviour
  changed**, under the same rule 3.

The third is the one that has been missed twice. Reaching it depends on
an author having read this far, which is weaker than a check; §Not
decided says what closing that would take.

## Not decided

- **Whether rule 3 reaches a specification.** It names responsibility
  documents. This one is inside its reach; `api/openapi.yaml` and
  `api/machine.yaml` are not responsibility documents and are not.
  Listing the API as a concern closes the half a document can close —
  an ADR that changes an endpoint now has a responsibility document to
  be paired with — and leaves the half only a rule can. What that rule
  should say is the index's to decide and not this document's.
- **What garam promises a caller across versions.** Nothing records it.
  Both specifications carry `version: 0.1.0`, the repository holds no
  tag and no release, and no ADR names a deprecation path or what
  counts as a breaking change. Treat every surface as unversioned, and
  do not read that field as a promise.
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
  — the unauthenticated surface's one unconditional write is bounded at
  the store
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
  — the engine applies the organization scope itself
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
