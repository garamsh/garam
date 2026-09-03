# ADR 0063: An operator enrols with a one-time token and keeps its key in its own cluster

> Status: accepted
> Date: 2026-09-04

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0015](./0015-operator-identity.md) left an operator's first
certificate "installed out of band, once", and
[ADR-0032](./0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)
shipped `garam issue-certificate` to perform that step. The command
writes three files, and a person then copies them into a Secret in the
operator's own cluster. Two things follow from that shape and neither
is a defect in the code.

The first is discoverability. The command is a root-level subcommand no
serving process references, so a person who registered an operator in
the console meets no route, no message and no page that names it. #992
measured what a person does today against the deployed system and found
six manual steps; these two are the first, and the issue records that
three of the other four turn from mistakes into dead ends because of
them — removing an operator, and recovering from a deleted
organization, both need a certificate operation the console cannot
reach.

The second is the private key. `issue-certificate` generates the key
pair, so the key exists on the machine that ran the command and travels
from there to the cluster. Every place it passes — a terminal, a
clipboard, a scrollback, a support conversation — is a place it can
stay. #991 proposed minting at registration and showing the key once,
which closes the discoverability hole and leaves the key having been in
a browser. The property worth having is not that the key was shown
briefly. It is that garam never had one.

Two facts about the machine listener bound what could be built, read
off the code rather than recalled.

- **`garam serve api` cannot sign.** It holds no key-encryption key and
  no certificate authority, which is what
  [ADR-0017](./0017-machine-process.md) put the machine identities in a
  process of their own for. So whatever answers a certificate signing
  request runs in `garam serve machine`.
- **`garam serve machine` admitted nobody without a certificate.**
  `internal/machine/listener.go` required one at the TLS layer and
  refused the handshake when `Authenticate` did not verify the chain,
  and `identify` then re-derived the identity from the presented
  certificate — a second derivation that read the SAN URI and not the
  chain, safe only because the handshake had already refused everything
  the chain did not cover.

## Decision

1. **Registering an operator mints a one-time enrollment token, and the
   two are one write.** The console answers it once. garam stores the
   token's SHA-256 hash and no route reads it back, so the response to
   `registerOperator` is the only place the token exists in a form
   anyone can present.
2. **A token is permission to ask for one certificate, once.** It is
   not a credential to the operator's identity: it authenticates no
   other operation, it names one operator GRN, and spending it removes
   the row.
3. **The operator generates its key pair in its own cluster** and sends
   a PEM-encoded PKCS#10 certificate signing request presenting the
   token. garam reads the public key and the request's own signature —
   which is what proves the sender holds the private half — and
   discards everything else in it. **What the certificate names comes
   from the token and never from the request**, so a request asking for
   another identity obtains a certificate for the operator the token
   named or nothing at all.
4. **garam verifies the token, signs, and the token is spent.** The
   answer carries the certificate, the issuing authority and the garam
   server root, and no private key, because garam never held one. It
   answers a schema with no key field rather than one with an empty
   field.
5. **The route lives on the machine listener** — `POST /enrollment` in
   `api/machine.yaml` — carrying `security: []`, the way an operation
   on the browser-facing listener opts out of the session cookie. It is
   the one operation there a caller reaches without a certificate.
6. **The listener no longer refuses a connection it cannot
   authenticate; it carries no identity for one.** The verified
   identity is written during the handshake by the call that verified
   it and read from there by every request on that connection, so
   nothing derives an identity a second time and a self-signed
   certificate naming any GRN reaches no handler that asks for one.
7. **A token stops being spendable ten minutes after it is minted**, a
   constant rather than configuration, and minting sweeps the tokens
   that have expired.
8. **Single use is the store's, not the process's.** Spending is one
   `DELETE ... WHERE token_hash = $1 AND expires_at > now() RETURNING
   operator_grn`, so two callers presenting one token leave one
   certificate. Unknown, spent and expired are one refusal: a caller
   that spends nothing learns nothing about which it was.
9. **An unspent token goes with the registration.** The row references
   `operators` with `ON DELETE CASCADE`, which decides the question
   before the route that would raise it exists — ADR-0045 offers no
   delete today, and when one arrives the token goes with the record
   rather than outliving the GRN.
10. **`garam issue-certificate` is untouched.** It stays the
    out-of-band path, and it is the recovery from a token nobody
    received.

### Why this does not breach ADR-0015

ADR-0015 refuses to create an issuing credential that can be stolen,
and ADR-0032 §Decision rests on no serving process minting an
operator's **first** certificate — narrowed to exactly that by its own
erratum, which records that `garam serve machine` already mints agent
identities and issues certificates on the listener it serves.

What ADR-0015 protects is that a serving process holds no credential
answering an unauthenticated request. This route answers exactly one
authenticated request per token, and the token was minted by a
signed-in member of the organization acting in a role that member
holds. **It is narrower than the agent-certificate route that already
issues on this listener**: `issueAgentCertificate` answers an operator
for every agent assigned to it at the current epoch, repeatedly, for as
long as the assignment stands. This one answers once, for one GRN, over
a key garam cannot choose, and is gone.

That is the comparison, and it is the argument. What this ADR takes
that ADR-0032 declined is narrower still than what ADR-0032 took.

## Consequences

- **Registering an operator is the whole of what a person does with
  garam**, and what follows is one command in the operator's own
  cluster. Two of #992's six manual steps go, and the three that ran
  through them stop being dead ends.
- **No private key crosses a wire, a screen or a clipboard.** garam
  cannot answer one for an enrolled operator because it never had one,
  and a caller that loses the key it generated registers again rather
  than recovering.
- **The listener's refusal moved from the handshake to the handler.** A
  certificate that does not authenticate now completes a connection and
  reaches a 401 instead of a TLS error. That is a worse diagnostic for
  a misconfigured operator and a better one for a person reading logs,
  and it is what leaves room for a caller that has no certificate yet.
  The property it replaces is stronger than the one it removes: an
  identity reaching a handler is the one `Authenticate` returned, where
  before it was one `identify` re-read off the certificate.
- **Every handler on that listener now carries the refusal it always
  had.** `caller` refuses a request with no identity, which was true
  before and was defence in depth; it is now the whole defence for
  every route but the enrollment one, and the specification is what
  says which route that is.
- **An enrolling operator must already hold the garam server root.**
  ADR-0040 §Consequences already records that it arrives out of band,
  and this changes nothing there: the root is public, one per
  deployment rather than one per operator, and it is what the operator
  verifies garam by on the enrollment connection. **An enrollment made
  without verifying garam hands the token to whoever answered**, which
  is the one way the design can be used wrongly.
- **A token is spent by the attempt rather than by the certificate.** A
  malformed request costs the token, so a caller cannot hold one open
  across attempts. The person registers again.
- **The database holds nothing anybody can present.** A dump yields
  token hashes, which enrol nothing.
- **`gagent-operator` owes the other half**, and this contract is what
  it must meet: generate an ECDSA P-256 key pair in the cluster, write
  it where only its own user can read it (ADR-0015), send the request
  and the token to `POST /enrollment` verifying garam against the
  pinned server root, and keep the certificate, the issuer and that
  root from the answer. It lands after this and nothing here waits for
  it.
- **Not decided: a route that mints a second token for an operator
  already registered.** Losing a token today means registering under
  another identifier or running `garam issue-certificate`, and both
  work. A re-mint route is the thing that would remove the out-of-band
  path entirely, and it is a decision about what a signed-in member may
  mint rather than a handler to add.

## Rejected alternatives

- **Minting the certificate at registration and showing the key once**
  (#991). It closes the discoverability hole with far less machinery
  and leaves the private key having passed through a browser, a
  clipboard and a scrollback. #992 filed this as the fallback if the
  token path were rejected, and it stays filed.
- **The enrollment route on the browser-facing listener.** It is the
  listener a token-bearing caller with no certificate naturally
  reaches, and `/auth/password/register` is the same shape there. It is
  refused because signing there would put a certificate authority's
  key-encryption key in `garam serve api`, which is precisely what
  ADR-0017 separated the processes to prevent.
- **A third listener, or a second port on the machine process, serving
  enrollment alone.** It leaves `NewServer`'s admission untouched and
  costs a port, a Service in the chart, a surface in
  [api-surface.md](../api-surface.md) and — under
  [ADR-0019](./0019-machine-openapi-spec.md)'s one specification per
  listener — a third specification, for one route. The `security: []`
  opt-out states the same thing in the file that already describes this
  listener.
- **Keeping the handshake refusal and admitting the enrolling caller by
  a self-signed certificate.** The connection would be made, and
  `identify` would then read an identity out of a SAN URI nothing
  signed. Closing that means carrying the verified identity from the
  handshake anyway, which is decision 6 with an extra step for the
  operator.
- **A token proved rather than transmitted** — an HMAC over the request
  keyed by the token, so an eavesdropper learns nothing replayable. It
  is a real improvement against a passive listener and none at all
  against the active one that matters: whoever answers an unverified
  connection returns a certificate and a server root of its own, and
  owns the operator from then on. Verifying garam is what closes that,
  and it closes the transmitted case too.
- **A configurable token lifetime.** What a deployment would tune it
  for is the same act every time — someone copying a token from a
  screen into a terminal — and a value nobody has a reason to change is
  a constant with a settings page around it.
