# Machine Identity

Who is calling on the listener a machine identity authenticates on
(ADR-0014, ADR-0015). Implemented by `internal/machine`.

| Type | Responsibility |
|------|----------------|
| `Identity` | the GRN a verified client certificate names, and the certificate's own serial and dates — which is what a caller asking to replace it is answered by (ADR-0046) |
| `Service` | recovers that GRN, verifies the certificate against the organization's authority, and refuses an identity the organization no longer holds a record of; for a decision, verifies again — the caller's identity against the clock and its record, and a certificate presented on another identity's behalf against that identity |
| `TrustRoot` | supplies the authority a presented certificate has to chain to |
| `Registration` | answers whether the organization still holds its record of the identity a certificate names (ADR-0070) |
| `ServerCertificate` | supplies the certificate the listener presents, asked per handshake so a replaced one needs no restart (ADR-0040) |

An agent and an operator are the same kind of caller here and the
listener does not branch on which. It asks one question of each and
the branch is in the seam that answers it: an operator is admitted
while its organization still holds the record, and every other identity
is held without a read (ADR-0070). What the certificate asserts is the
GRN in its SAN URI and nothing beside it: no user row, no session, no
membership, so no foreign key on `users` is touched and no handler
taking a `*user.User` changes. The identity carries the certificate's
own serial and dates as well, which assert nothing and describe which
certificate authenticated — the one question a caller asking to replace
it is answered by (ADR-0046).

The SAN carries exactly one URI and it is parsed whole as a GRN. That
one parse is the whole of the check on the URI: `grn.Parse` splits on
every colon and holds every segment to the character rule
([grn-model.md](./grn-model.md) §identifier), so a URI carrying a
separator that would move a segment — a colon outside the four, a `?`,
a `#` — is not a GRN and names no identity. The rule lives in
`internal/grn` and is pinned there; this boundary calls it rather than
keeping a copy. Which authority the certificate must then chain to is
read off the organization segment of that GRN, so a certificate one
organization's CA signed for another organization's identity is
refused by the trust root rather than by a check. Chaining is not the
whole of that verification either: it is `crypto/x509`'s, which in the
same call refuses a certificate outside its validity period and one not
marked for client authentication, so neither refusal is code this
repository holds.

The package holds no store and reaches no database: `TrustRoot` is the
seam the composition root satisfies from `internal/ca`, which keeps the
two domains cooperating through `grn.GRN` alone. Verification needs the
authority's certificate and never its key. `Registration` is a seam of
the same shape, satisfied from `internal/operator`, and it is what
turns an organization deleting an operator into a certificate that
stops authenticating — no list, no responder and no stored serial, just
the record the organization already keeps, read once per connection
after the chain verifies. Both reads are per connection and a resumed
handshake takes them too, because `Authenticate` runs from
`VerifyConnection` (ADR-0064): a check that ran only on a full
handshake would let a deleted operator keep connecting for as long as
its tickets lasted.

One route reads them again on every request: the controller proof
([operators.md](./operators.md) §Controller proof). It asks `Recheck`
of the caller's identity — its certificate's dates against the clock,
then its record — and `Verify` of the controller certificate the
caller forwards, which proves the identity it is presented for by its
SAN, by its organization's authority, by its validity at the decision
and by its record. The chain is verified at a time inside the
certificate's validity and the validity at the decision is checked
apart from it, so a certificate nothing signed and one that has run
out are told apart. Every other route relies on listener admission,
which reads them once per connection; renewal also reads the
certificate's dates, to decide its renewal window
([operators.md](./operators.md)).
`ServerCertificate` is the
other seam, satisfied from `internal/keyfile`, and what it answers with
is a different trust root: garam verifies an identity by its
organization's authority, and an identity verifies garam by the one
root every organization shares (ADR-0040).

`garam serve machine` is where the listener runs — its own process and
its own port (ADR-0017), terminating TLS itself with the pair it holds
as two paths and re-reads per handshake, the private key under the
owner-only rule `internal/keyfile` holds, and the root that signed that
pair beside them for every identity it issues a certificate to. It
terminates TLS itself because a forwarded client-certificate header is
trustworthy only where every deployment strips it at the edge
(ADR-0014). It is not part of the local Docker stack: its key material
is per host, and compose has
no step that generates it and hands a container a file that container
owns. The Kubernetes stacks under `deploy/` have that step — an init
container copies the key material as the UID the listener runs as — so
what compose lacks is the step, not something a container cannot do.

## Generation and lineage

**Nothing in this section is built.** Today a connection is admitted
once, when it is made, and only the controller proof reads the
caller's certificate and record again per request; a message call
compares the caller with the recipient and nothing else
(`Service.Claim` in `internal/message/service.go`).

ADR-0078 adds two questions an agent's state-changing call must answer,
per request rather than per connection, so an open connection loses
them the moment they change:

- **Is this the active generation?** The call carries the activation
  token binding its GRN, epoch and generation, and one for a generation
  that is not active is refused.
- **Is this lineage current?** A credential whose lineage a recovery
  replaced is refused, though its certificate has not expired.

The certificate's GRN still answers who is calling; these answer
whether that caller is still the one allowed to act.
