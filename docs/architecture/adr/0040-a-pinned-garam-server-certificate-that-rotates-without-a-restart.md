# ADR 0040: The machine listener presents a pinned garam server certificate, and certificates rotate without a restart

> Status: accepted
> Date: 2026-08-24

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0037](./0037-the-machine-listener-presents-a-per-organization-certificate.md)
decided that the listener presents a per-organization certificate
selected by SNI. Nothing was built: `internal/machine/listener.go`
presented one certificate read once at startup, and #481 measured a
running listener answering every connection with the mounted pair —
including one carrying no SNI at all, which that ADR says gets no
answer. The consequence was that garam's own adapter could not connect,
because it verified garam against the organization issuer it is handed
with its certificate and the listener presents nothing that chains to
it.

Two documents already disagreed with each other and with the code: this
contract's key-material section called the listener's pair "safe to have
issued by cert-manager or any other issuer", and `adapter.md` stated
ADR-0037's expectation as a description of what is.

#482 put the fork to the project owner, who took neither branch. What
decided it was read off `garam-dev` on 2026-08-24 rather than recalled:
the Kubernetes API server presents a leaf from a **private, pinned**
root, its certificate authorities are separated **by what a certificate
is allowed to do** and never by whose tenant it is, and its own leaf
lives for a year against a root that lives for ten.

Two findings settled the rest.

- **Half of ADR-0037's mechanism cannot be built.** A TLS server chooses
  its certificate before the client sends one: `crypto/tls`'s
  `ClientHelloInfo`, which the selection callback receives, carries
  `ServerName` and no client certificate (checked with `go doc` at Go
  1.26.7, this module's directive). Only the SNI half is reachable, and
  it puts the customer's identity in cleartext in every ClientHello.
- **The isolation it promises is not the isolation it delivers.** Every
  organization's authority is sealed by one key-encryption key held by
  one process
  ([ADR-0016](./0016-ca-key-custody.md)). In the realistic compromise —
  that process — per-organization server certificates partition nothing.

Separately, [ADR-0014](./0014-agent-authentication.md) §Decision fixed
"Rotation is reissue and restart … nothing here reloads", and its
§Consequences accepted expiry as the only revocation there is. Those two
are in tension: expiry as revocation wants a short lifetime, and a
lifetime short enough to be a revocation is unusable when every rotation
is a restart.

## Decision

1. **Client certificates are unchanged.** Per organization, per
   identity, verified per connection against the authority the
   certificate's own SAN names. [ADR-0020](./0020-published-artifacts.md)'s
   prohibition on a pooled trust bundle stands untouched, and nothing
   here reads what the caller presented.
2. **The listener presents one garam server certificate, signed by a
   garam server certificate authority separate from the organization
   authorities and signing nothing else.**
3. **garam never holds that authority's key.** It holds the certificate
   it presents, that certificate's private key, and the authority's
   certificate — three files, the key under
   [ADR-0015](./0015-operator-identity.md)'s arrival rule and the two
   certificates public. Whoever owns the cluster owns the authority, and
   cert-manager issuing from an issuer dedicated to this listener
   satisfies it.
4. **Every machine identity pins that root**, and it travels on the
   route the identity already receives its certificate on, beside the
   certificate, the private key and the issuing authority. The issuing
   authority stays in that answer and stops being what garam is verified
   against: the two directions rest on different roots.
5. **Both ends read their certificate per handshake** — `GetCertificate`
   on the listener, `GetClientCertificate` on the adapter — so replacing
   the files replaces what the next connection presents, with no
   process restarted. **This supersedes ADR-0014 §Decision's rotation
   term.**
6. **An agent renews its own certificate over the connection its current
   one authenticated.** It grants nothing: the caller already holds that
   identity, and an expired certificate has no connection left to renew
   over, so the operator route is what recovers one.
7. **The certificate lifetime is configuration with a conservative
   default** — a day — bounded at both ends by `internal/ca`. The
   mechanism ships before the number is tightened; tightening it is a
   configuration change once rotation has been observed working.
8. **Per-organization server certificates are deferred, not rejected.**
   Selection by server name is additive: a customer who later requires
   garam's identity to rest on their own root can be given that as a
   per-tenant option without unpicking any of this.

## Consequences

- The trust model is asymmetric and says so: garam verifies an identity
  against its organization's authority, and an identity verifies garam
  against one root every organization shares. A neighbour organization's
  CA compromise still reaches nothing in either direction.
- **A compromise of the garam process yields no power to mint garam's
  identity afterwards.** It yields the current leaf and its key, which
  expire; the authority that could sign the next one is not there to
  take. That is the whole reason garam does not hold it, and it is what
  a short lifetime is worth something against.
- **The deployment owes three things it did not owe before**: an
  authority dedicated to this listener, the root certificate mounted so
  garam can hand it on, and files that stay current when the certificate
  is replaced. A shape that copies the material at pod start refreshes
  nothing afterwards, so rotation there is still a restart —
  [deployment-contract.md](../deployment-contract.md) states both
  obligations in the form a deployment acts on.
- **`garamsh/gitops` is affected and keeps cert-manager.** It gains an
  issuer that signs only this listener's certificate, and the root that
  issuer signs with becomes something every agent is given. The sentence
  in this repository's contract that told it any issuer would do was
  wrong and is corrected in the change that carries this ADR.
- **The server root's own rotation is a restart**, and changing it
  invalidates what every agent already holds. It is read once, it
  outlives the certificates it signs by years, and nothing here
  schedules its replacement.
- **An operator's own certificate still has no renewal path.** Its first
  one arrives out of band (ADR-0015) and so does the server root it
  needs to reach garam at all; the renewal above is an agent's, over a
  connection an operator's bootstrap does not yet have.
- Expiry becomes a revocation worth having: with rotation off the
  restart path, the lifetime can be lowered to the window a deployment
  is willing to leave a stolen certificate usable in.
- Reading two files per handshake is the cost, paid per connection
  rather than per request. This listener's callers hold long-lived
  connections, so it is not a rate anything here approaches.

## Rejected alternatives

- **ADR-0037 as written — per-organization, SNI-selected.** Half of it
  cannot be built (§Context), and the reachable half buys an isolation
  that ADR-0016's single key-encryption key does not deliver, at the
  price of a DNS name, a certificate and a rotation per tenant forever.
- **garam holding the server authority's key, sealed as the
  organization authorities are.** One fewer thing for a deployment to
  own, and it puts the authority that mints garam's identity inside the
  process most likely to be compromised — where the certificate it
  signs is already reachable anyway, so it adds exposure and no
  containment.
- **A certificate from a public authority.** Nothing to mount and
  nothing to hold, and it rests garam's identity to a machine on the
  public web PKI: a strictly wider set of issuers than the one root
  chosen here, adopted at the exact surface where machine identities
  live. ADR-0037 rejected this for the same reason and that reasoning
  survives it.
- **Reloading on a signal or a file watch instead of per handshake.**
  Fewer reads, and it adds a second mechanism that can be missed, a
  state to hold, and a window where what is on disk and what is served
  disagree. A read per handshake has no such window.
- **Keeping the last certificate that loaded, to serve while a
  replacement is half-written.** It hides a defect in how the files are
  replaced behind a listener that keeps working, and the deployment can
  close it for good by replacing them atomically — which the contract
  now requires.
