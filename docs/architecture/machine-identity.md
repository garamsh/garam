# Machine Identity

Who is calling on the listener a machine identity authenticates on
(ADR-0014, ADR-0015). Implemented by `internal/machine`.

| Type | Responsibility |
|------|----------------|
| `Identity` | the GRN a verified client certificate names, and the certificate's own serial and dates — which is what a caller asking to replace it is answered by (ADR-0046) |
| `Service` | recovers that GRN and verifies the certificate against the organization's authority |
| `TrustRoot` | supplies the authority a presented certificate has to chain to |
| `ServerCertificate` | supplies the certificate the listener presents, asked per handshake so a replaced one needs no restart (ADR-0040) |

An agent and an operator are the same kind of caller here and the
listener does not branch on which. What the certificate asserts is the
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
authority's certificate and never its key. `ServerCertificate` is the
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
