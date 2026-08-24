# ADR 0014: Agents authenticate with mTLS on a listener of their own

> Status: accepted — §Decision's rotation term is superseded by [ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
> Date: 2026-08-16

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

ADR-0012 settled how an agent is identified and ADR-0013 how a message
reaches it. Neither can be built until an agent can prove which agent
it is, and garam has no machine authentication of any kind.

ADR-0012 also recorded that an agent GRN as a policy subject is
something "ADR-0008 does not currently allow and a later ADR must
supersede". **That is wrong, and this ADR corrects it.** Attaching a
policy to `grn:<org>:default:agent:<hex>` returns 201 today — read
first and then exercised against a running stack. `AttachPolicy` in
`internal/api/policy.go` parses the subject and validates nothing
further, the engine's only subject validation is `grn.Parse` in
`internal/authz/server.go`, and `GRN.Validate` requires four non-empty
segments and nothing else. ADR-0008 said so in its own consequences:
the engine "does not validate that a subject GRN names a user or a
group". Nothing is superseded and no model change follows.

What actually blocks an agent is narrower than the model, and it is
not in Go:

- The session cookie is the only way a request acquires an identity,
  and `currentUser` in `internal/api/auth.go` then requires a row in
  `users`. There is no bearer, header or certificate path beside it.
- Two foreign keys enforce that: `sessions.user_grn` and
  `org_members.user_grn` both reference `users`. An agent can hold
  neither a session nor an org membership.
- `group_members`, `project_members` and `policy_attachments` carry no
  such key, so an agent GRN can already be a group member and a policy
  subject. The attachment trigger admits a `root`-owned subject and
  org-scopes every other, which binds an agent to its own org for
  free.
- No TLS exists anywhere. `crypto/tls` and `crypto/x509` are imported
  nowhere, api↔authz gRPC is deliberately plaintext, and the API is
  served by `http.ListenAndServe` behind the TLS-terminating proxy
  that the `Secure` cookie flag assumes.

## Decision

- **An agent presents a client certificate whose SAN URI is its GRN.**
  A GRN is not a hostname, so it belongs in a URI SAN rather than a
  common name. The certificate carries no roles and no claims: it
  answers which agent is calling, and every grant is looked up.
- **The per-organization CA of ADR-0013 signs it, and garam verifies
  the chain against the CA of the org named in the SAN.** A
  certificate issued by another org's CA fails to chain, so tenancy is
  enforced by the trust root rather than by a check that could be
  forgotten.
- **A separate listener terminates TLS itself and requires client
  auth.** The browser-facing API keeps its proxy and its cookie; the
  agent surface takes neither. This is the reason to avoid a forwarded
  client-certificate header, which is trustworthy only where every
  deployment strips it at the edge. A listener of its own is the shape
  this repository already uses for metrics and for gRPC.
- **The agent surface does not enter the user path.** No `users` row,
  no org membership, no `currentUser`. The handler's rule is that the
  authenticated GRN equals the agent named in the path — ADR-0012's
  "authorization stops at who may use this agent", read from the other
  side. Neither foreign key is touched and no handler that takes a
  `*user.User` changes.
- **Policy attachments extend that rule and do not gate it.** An agent
  GRN is already a valid subject and a valid group member, so a
  broader grant needs a policy and no new machinery. An agent that
  holds nothing can still reach its own queue.
- **Rotation is reissue and restart.** ADR-0013 fixed the same terms
  for the loopback certificates; nothing here reloads.

## Consequences

- garam operates a certificate authority per organization: issuing,
  storing a private key, and choosing a lifetime. How that key is held
  is the first question the implementation must answer, and this ADR
  does not answer it.
- Revocation is by lifetime, not by list. There is no CRL and no OCSP
  responder, so a compromised certificate stays valid until it
  expires. Short lifetimes are therefore a security parameter and not
  a tuning knob, and an operator restart is the cost of every
  rotation.
- The loopback certificates of ADR-0013 and these ingress
  certificates share a CA but not a SAN, so a gateway certificate
  presented to garam fails the SAN check. The shared root is a
  convenience; the SAN is the boundary.
- Two listeners means a second port to expose and to configure, and a
  deployment that forgets it fails closed — an agent cannot connect.
- Losing the CA for an org invalidates every agent in it. The CA
  becomes availability-critical the moment an agent depends on it,
  which the queue of ADR-0013 already was.
- Nothing here is implemented. The listener, the CA, the certificate
  issuing and the message endpoints all remain to be built.

## Rejected alternatives

- **An opaque bearer credential issued at provisioning.** It needs no
  new infrastructure and reuses a pattern the repository already has
  right — 32 random bytes, only a SHA-256 stored, expiry filtered in
  SQL. It was the recommendation, and it was rejected because a
  long-lived secret in an operator's environment is a secret that
  leaks into a log, an image layer or an environment dump, and a
  private key that never leaves the host does not.
- **mTLS through the existing proxy, via a forwarded certificate
  header.** One listener instead of two, at the cost of trusting every
  deployment to strip an inbound header it did not set. The failure is
  silent and total.
- **Giving each agent a `users` row.** Everything would work unchanged,
  which is the problem: it makes "user" mean two things and discards
  the org-owned GRN ADR-0012 chose deliberately.
- **Generalizing the authenticated caller to a principal type.** The
  structurally honest refactor — and it touches every handler that
  takes a `*user.User` in order to give an agent role machinery it
  never uses.
- **A second CA for ingress, separate from the loopback one.** More to
  run and rotate for a separation the SAN check already draws.

## Errata

- 2026-08-18 — §Consequences' "Policy attachments extend that rule and do not gate it" predates ADR-0024: on the machine listener an attached Deny narrows what an identity may send, so an attachment can gate. The authentication decision this ADR takes is unchanged.
