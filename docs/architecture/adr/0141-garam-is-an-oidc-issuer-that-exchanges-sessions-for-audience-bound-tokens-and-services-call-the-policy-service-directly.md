# ADR 0141: garam is an OIDC issuer that exchanges sessions for audience-bound tokens, and services call the policy service directly

> Status: accepted; partly built. The issuer, the exchange of a CLI
> session and introspection are built (#1757); cowork's direct calls
> are built in cowork, which asks and feeds the policy service where
> its settings are set and the platform otherwise (#1758); the
> exchange of an agent's browser token and the direct calls of every
> other service are decided and not built;
> superseded in part by
> [ADR-0142](./0142-a-grn-names-its-organization-its-service-and-a-path-of-type-and-id-pairs-with-a-locator-the-service-reads.md)
> (the GRN forms of worked-example answers 4 and 5), by
> [ADR-0149](./0149-codehub-is-a-service-of-the-policy-service-whose-repositories-are-entities-and-whose-paths-are-context.md)
> (worked-example answers 4 and 5, decided in full) and by [ADR-0143](./0143-a-service-is-declared-by-one-manifest-that-is-its-cedar-schema-and-garam-serves-what-the-manifests-say.md)
> (§5 and §13, how the list of services is held and that an ADR adds
> one)
> Date: 2026-11-11

Drafted for #1754, which reserves this number and holds the owner's
rulings this records, decided with the PM on 2026-11-11. It
supersedes in part
[ADR-0127](./0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)
(§5, who asks the engine) and
[ADR-0140](./0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md)
(§2, who the callers are). It builds on
[ADR-0138](./0138-a-caller-of-the-policy-service-presents-a-short-lived-signed-token-and-owners-write-entities-through-a-sequenced-rpc.md)'s
token profile and signer, and it keeps what
[ADR-0005](./0005-server-side-sessions.md) decided about
sessions. garam is read at
`c2667176e08546ca0dfe19b917bbc0a0ff8df268`; a claim below that names
no revision holds for it.

## Context

- **garam is meant to be what services sit on.** The owner's
  direction is that services, including ones garam does not deploy,
  take who a caller is and what that caller may do from garam, as AWS
  services take both from IAM. Today garam answers that for itself
  and for cowork only: a session is an opaque server-side row that no
  other service can read, and the policy service is reached only by
  the platform.
- **The first outside consumer asked six questions.** codehub, a
  service that stores repositories and is not deployed by garam,
  asked how a service learns who is calling, which audience it
  verifies, how it asks the policy service, how its resources are
  named, how its tenants come to exist, and how its command line
  signs in. The decision answers them; the worked example below
  records each answer.
- **ADR-0127 §5 and ADR-0140 §2 route every check through the
  platform.** They were drafted when cowork was the only caller, so
  the platform asks the engine on cowork's behalf. A second service
  would add a second route through the platform for every question it
  asks, and the platform would hold each service's entity types.
- **ADR-0138 already built the half of an issuer this needs.** The
  policy service verifies an RS256 token from a JWKS of one or two
  keys, with `aud` a single string and a lifetime of at most 300 s,
  and ADR-0140 §12 has the platform sign the one token it needs. What
  that signer lacks is a second subject and a second audience.
- **The decision comes before policy slice 5.** Slice 5 is cowork's
  checks. Built before this, it would build the route this decision
  retires.

## Decision

**garam is a standard OIDC and OAuth issuer, built inside `platform/`,
that exchanges a person's session or an agent's token for a JWT bound
to one service. Services verify that JWT offline, call the policy
service directly with their own caller token, and own a Cedar
namespace each.**

1. **The issuer is garam's own.** It is built in `platform/` and no
   external identity product sits in front of it or behind it. An
   external product would hold the sessions, the agents and the
   revocation that garam already holds, so each would be held twice.
   The OIDC and OAuth endpoints are those the standards name:
   discovery, a JWKS, a token endpoint and an introspection endpoint.
2. **Sessions stay server-side.** A browser session and a CLI session
   remain the rows ADR-0005 decided, and revocation remains deleting
   the row. The issuer adds a way to turn one into a token for a
   service and takes nothing away from the session.
3. **A token is an exchange (RFC 8693).** A client presents a CLI
   session's access token or an agent's browser token as the
   `subject_token`, names one audience, and receives a JWT whose
   `aud` is that service and whose lifetime is at most 300 s. The
   token is never a session, and a service cannot refresh it: the
   client exchanges again.
   - **Accepted `subject_token_type`.** One value,
     `urn:ietf:params:oauth:token-type:access_token`, for both a CLI
     session's access token and an agent's browser token. The
     platform tells them apart by the row the token resolves to,
     since a client that sends the wrong type label is no more
     authentic than one that sends the right one. A refresh token, an
     ID token and a machine token (`machine-token.v2`) are refused.
   - **Requested type.** `requested_token_type` is the JWT type or
     absent, and the response is `token_type` `Bearer` with
     `issued_token_type` the access-token URN.
   - **Lifetime.** Always the shorter of 300 s and the subject
     session's remaining life, never longer, so an expiring session
     does not mint a token that outlives it.
   - **Who calls it.** The browser-facing listener serves it, as it
     serves the CLI's other session calls, and the subject token is
     the whole credential. No client authentication is added, because
     a client holding a valid subject token already holds the right
     to exchange it, and a client secret held by a command line would
     be one more secret nothing could keep.
4. **Introspection is offered beside offline verification
   (RFC 7662).** A service verifies a JWT offline by default, against
   the JWKS, and introspects only for an action that cannot wait out
   the token's remaining life. The endpoint is on the machine
   listener, and a service authenticates to it with the
   `private_key_jwt` assertion ADR-0129 decided for cowork's service
   principal, one key per service. A service may introspect only a
   token whose `aud` is that service, so introspection is never a way
   to learn what another service was sent.
5. **The audiences are a closed static list.** The platform holds the
   list in code, as ADR-0140 §1 holds the callers of the policy
   service, and an unknown `audience` is refused. A service is added
   by an ADR and a pull request, and no row exists before a caller
   proves it, so a deployment's audiences are the list the version it
   runs was built with. The first entries are `policy` and `codehub`.
6. **The claims are few.** `iss` is the issuer; `sub` is the subject's
   GRN, a user's or an agent's; `aud` is the one service; `iat` and
   `exp` are as ADR-0138 requires; `jti` is unique per token; `sid`
   is the id of the session or agent-token row the exchange read. The
   header is ADR-0138's `alg`, `kid` and `typ` and nothing else.
   - **No organization claim.** An organization named in a token would
     be a second source of tenancy beside the membership ADR-0137
     decided, and a token minted for one organization would be wrong
     the moment the subject joined another. A service asks the policy
     service, and the policy service answers from membership.
   - **`sid` serves introspection.** Introspection answers `active`
     from whether that row still exists and the subject still
     stands, so a signed-out session is inactive without the platform
     keeping a table of tokens it issued.
7. **The key and the profile are ADR-0138's.** The issuer signs with
   the signer ADR-0138 and ADR-0140 §12 built, and publishes that
   signer's JWKS at the issuer's JWKS endpoint, so the policy service
   and every other verifier trust one key set. A second key would be a
   second rotation, and a rotation is a rollout (ADR-0138), so it
   would be a second rollout. ADR-0140 §12's minimal signer is this
   issuer's first case: a caller's own token, with `sub` the caller
   and `aud` `policy`, which the platform mints for itself and does not
   exchange. The one issuer signs both kinds.
8. **A revoked subject refuses its next exchange.** The exchange
   reads the session or agent-token row at the moment it is asked, so
   a deleted, expired or revoked row refuses, and a suspended or
   deleted subject refuses. A token already issued stays valid until
   `exp`, so revocation reaches an offline verifier within 300 s plus
   the 30 s skew ADR-0138 allows, and reaches an introspecting
   service at once.
9. **The issuer is off when no signing key is configured.** The
   discovery, JWKS, token and introspection endpoints are not
   registered, and no session, agent-token or machine-token path
   changes. A deployment that configures no key behaves as it did.
10. **Where it lives.** The issuer is one configured identity, an
    named `GARAM_IDENTITY_ISSUER`, which is an identity and not an
    address. It is spelled https, a lowercase host, no empty port, the
    path `/api` and no trailing slash, and is compared byte for byte;
    it is not the spelling `GARAM_MACHINE_ISSUER` takes, which allows
    no path. It is the API's origin with the `/api` prefix the console
    rewrite already forwards, so no new rewrite and no new host
    exists. Discovery is at
    `<issuer>/.well-known/openid-configuration`, the JWKS and the
    token endpoint are at `<issuer>/oauth/jwks` and
    `<issuer>/oauth/token`, and introspection is at
    `/oauth/introspect` on the machine listener.
    The machine listener's existing `POST /oauth/token`
    (`exchangeClientAssertion`) is a different endpoint and is
    unchanged.
11. **What the browser does, and what is later.** A browser signs in
    and holds a session as it does today. The issuer adds nothing to
    that path. An authorization-code flow, for a third-party web
    application that signs a person in through garam, is later and
    undecided here; this decision builds the endpoints a service
    calls, not an end-user consent screen.
12. **Services call the policy service directly.** Each service holds
    a caller token the issuer mints for it, and asks the engine over
    the policy service's gRPC AuthZEN endpoint without the platform
    between. A service other than the platform obtains that token
    from the machine listener's `exchangeClientAssertion`, a
    client-credentials grant whose RFC 7523 assertion is signed with
    the per-service key ADR-0129 decided, requesting `audience`
    `policy`; it receives the issuer's JWT with `sub` its service
    principal. The browser-facing token endpoint gains no
    client-credentials grant, so no second client authentication
    exists. This route is built with cowork's direct calls and not
    with the issuer. Each service owns a Cedar namespace, its entity
    types and its actions, and feeds its own entities through
    `ApplyEntityChanges` under the per-entity sequence ADR-0138
    decided. An HTTP AuthZEN gateway (ADR-0130 §2) is added when a
    caller that does not speak gRPC needs one, and is not built
    before then.
    - **Supersedes ADR-0127 §5 in part, on whose table answers.**
      cowork's actions and resource kinds no longer join the table
      `checkPermissions` answers from, and cowork no longer asks the
      platform with the caller's credential before every read and
      write. cowork asks the policy service itself. What §5 decided
      about the organization roles' statements granting those actions
      by default, and about policies narrowing or widening them,
      stands.
    - **Supersedes ADR-0140 §2 in part, on who the platform asks for.**
      The platform no longer asks anything "since cowork's checks reach
      the engine through it": it asks for the types it owns, and each
      service asks for its own namespace. Which caller owns which type
      is unchanged, and the list is still closed and static
      (ADR-0140 §1).
13. **The list of services stays closed.** A caller is added by an ADR
    and a pull request, and no row exists before a caller proves it:
    codehub's caller entry is built when codehub calls, not before.
    This is ADR-0140 §1, unchanged.

### Worked example: codehub

codehub is the first outside consumer. Each answer is the decision
applied.

1. **Introspection and OIDC are both available.** codehub verifies the
   JWT offline against the JWKS and introspects for an action that
   must see a revocation at once, such as a repository deletion.
2. **It has a `codehub` audience.** The exchange names `codehub` and
   returns a JWT codehub alone accepts.
3. **It is a direct caller.** codehub holds a caller token whose `sub`
   is its service principal, owns a `Codehub` namespace, and feeds its
   own entities through `ApplyEntityChanges`.
4. **Its repositories are a `repo` GRN type that shares the org
   slug.** A repository is `grn:<org>:default:repo:<id>`, owned by
   the organization at project `default`, so it carries the
   organization's own slug and no project. A project-owned `repo` is
   a later step, taken when a repository needs a project's members.
5. **Its orgs are created lazily under the policy decision point.**
   codehub holds no organization it was not asked to act in. Its
   principal in an organization is
   `grn:<org>:default:service:codehub`, the form ADR-0129 gave
   cowork, and an organization's row exists once a call first needs
   it.
6. **`ch` reads the garam session or `GARAM_TOKEN_FILE`, with a
   `garam` noun for garam users.** codehub's command line
   exchanges the garam CLI session, or the agent's browser token,
   for a `codehub` token at the token endpoint. `garam` gains a noun
   for the person who signs in to garam and uses a service that
   rests on it.

## Consequences

- **A service rests on garam without garam holding its code.** The
  contract is an audience, a JWKS and a caller entry, and none of the
  three needs the service to be deployed with garam.
- **A token is bound to a service, so a stolen token reaches one
  service for at most 300 s.** The cost is an exchange per service
  and per five minutes, which a client caches until its token
  expires.
- **A revocation reaches offline verifiers late.** It does so by at
  most 330 s, and a service that cannot accept that introspects. A
  session's deletion needs no new machinery, since the exchange reads
  the row.
- **The platform no longer sits in the path of every check.** A
  service's check is one call to the policy service. The price is
  that each service writes its own entities, so the sequence of
  ADR-0138 carries more writers, each owning types the others do not.
- **The issuer is a second thing that signs.** It signs with the key
  the policy service already trusts, so the cost is the audience list
  and the endpoints and not a second key.
- **Nothing is built, and the order is fixed.** The issuer follows
  the policy outbox and delivery, and cowork's direct calls follow the
  issuer, each as its own issue. The HTTP gateway and codehub's caller
  entry are held until a caller needs them.

## Signs the decision was wrong

- A service asks for a token in a way no audience list can state, such
  as one token for two services, which would show audience binding
  was too narrow.
- Services introspect on most calls, which would show offline
  verification buys nothing and 300 s is the wrong bound.
- Two services write entities of one type, which would show a
  namespace per service did not hold the ownership ADR-0138 needs.
- A client needs an organization in the token to avoid a lookup,
  which would show the membership read is too slow where it counts.
- A deployment cannot add a service without a platform release, and
  the list of services grows to the point that release is the cost.

## Alternatives rejected

- **An external identity provider.** It would hold the sessions, the
  agents and the revocation garam already holds, and would be
  configured from garam's state it cannot read. A second store of who
  exists is the cost, and the standards give the same interface.
- **Introspection alone.** Every service call would then call the
  platform, which makes the platform the availability of every
  service and removes the offline verification a JWKS gives.
- **Long-lived JWTs.** A token valid for hours is a session nobody can
  revoke. The 300 s bound is what makes offline verification safe.
- **A per-service OAuth client flow.** Each service would register
  with garam, hold a client secret and run its own redirect for every
  command line. The exchange gives the same token with the session
  the person already holds.
- **Calls through the platform, as ADR-0127 §5 had them.** One route
  per service through the platform means the platform holds every
  service's entity types and answers for every service's latency.
