# Operators

What an organization records about an operator, and what that operator
authenticates with: the record that the organization has it (ADR-0045),
the one-time token registering it mints (ADR-0064), and which
certificate garam last issued it, together with what it takes to replace
that with the next (ADR-0046). Implemented by `internal/operator`.

| Type | Responsibility |
|------|----------------|
| `Operator` | an organization's record that it has an operator under this GRN: who registered it and when |
| `Enrollment` | the one-time token an operator obtains its first certificate with: which operator it enrols, who minted it, and when it stops being spendable |
| `Store` | registers one with the token it enrols with, answers one by GRN, and lists an organization's own |
| `EnrollmentStore` | spends a token, answering the operator it enrols |
| `Credential` | the certificate a connection authenticated with — the identity it names, which certificate it is, and how long it is good for |
| `CertificateRequest` | a certificate signing request as it arrived, holding the public key an enrolling operator generated |
| `Certificate` | what an operator authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `IssuedCertificate` | a `Certificate` garam generated the key pair for, carrying the private key it returns once |
| `Current` | the certificate garam last issued an operator: the record a renewal is measured against |
| `CertificateStore` | records what garam last issued, and moves that record to the successor only where the presented certificate is still it |
| `Service` | signs an enrolling operator's request, mints a certificate out of band and adopts it, and renews the one an operator presents |
| `Issuer` | signs a certificate for a subject GRN, over a key it generates or over one a request carries, and hands back the garam server root beside it |

The identifier is the caller's rather than garam's, which is what makes
this domain unlike every other org resource. An operator's GRN is
already the subject of a certificate wherever `issue-certificate` has
run (ADR-0032), so a minted identifier would name something other than
the operator that exists. What the domain applies is the GRN segment
rule, and the schema applies it again where nothing routes around it.

A record grants nothing and reports nothing, and the token minted beside
it grants no more: it is permission to ask for one certificate, once. No
listener reads a record to admit a caller — an operator authenticates
with the certificate it holds (ADR-0015), and garam never dials one, so
there is no state here for anybody to report. What the record is read
for is the question a definition asks — whether this organization has
the operator it names — which the browser surface asks there, and the
list of what the organization has registered, which the console shows
and `ListByOrg` exists for. The `agent_definitions` foreign key is the
copy of the definition's check no call path routes around.

There is no update and no delete. A record carries nothing to change,
and removing one would stop nothing an operator can already do, which is
a route ADR-0045 declines to offer until revocation is decided. What
becomes of an unspent token when a delete does arrive is settled ahead
of it: the row references the registration, so the token goes with the
record rather than outliving the GRN.

The certificate half reads no record above it. An operator renews with
what it holds, and whether its organization registered it changes
nothing about that.

An operator's first certificate is obtained by enrolling (ADR-0064).
Registering mints a token, the operator generates its key pair where the
certificate will be used, and this domain spends the token and signs the
request. **The subject is the token's and never the request's**, so a
request naming another identity obtains a certificate for the operator
the token named or nothing at all, and what comes back carries no
private key because none was generated. Enrolling adopts the lineage
exactly as the out-of-band mint does, so an enrolled operator renews on
the same rule as any other.

Three things bound what a token is worth. It stops being spendable ten
minutes after it is minted. Spending it removes it, in the statement
that reads it, so two callers presenting one token leave one
certificate. And the attempt spends it whether or not the signing
succeeds, so a caller cannot hold one open across malformed requests —
what a refused enrollment costs is the token, and the recovery is to
register again.

Unknown, spent and expired are one refusal. A caller that spends nothing
presented no credential either way, and telling the three apart would
answer questions about tokens the caller does not hold. Only the hash
reaches the database, as it does for a session, so a row read out of a
dump enrols nothing.

The command ADR-0032 ships still mints a first certificate out of band,
and it is what recovers an operator with no token left. Every
certificate after the first is issued here, over the connection the
certificate being replaced authenticated, so neither bootstrap is a
standing obligation.

Four things refuse a renewal, and each is a decision rather than a
guard. A certificate whose subject is not an operator is refused before
any window is measured: what this domain renews is an operator's
certificate. A certificate that has expired renews nothing: it
authenticates nothing, so no identity reaches this route with it, and
the rule states the same boundary as the empty end of its window — what
follows expiry is a token to enrol with, or a mint out of band. A
certificate with more than a third of its validity left is refused as
too early, measured off the certificate rather than off a configured
number, so the window holds whatever lifetime signed it. And
only the certificate garam last issued an operator may renew it: garam
records that certificate and moves the record to the successor in the
statement that checks it, so every other copy of that identity stops
being renewable at once. That last one is what bounds a stolen
certificate where there is no revocation — the operator it was taken
from renews, the copy loses the lineage, and it expires. It revokes
nothing: a certificate that can no longer renew keeps authenticating
until it runs out.

An operator with no record yet is the one case that comparison has
nothing to make, and it is not a refusal: the write that moves the
record inserts one where none exists, so the first renewal adopts the
lineage. That is what an operator whose certificate predates the record
needs, and it is why the fourth refusal bounds a copy only once a
lineage exists to lose.

The mint adopts whatever it issued, so re-running `garam
issue-certificate` takes the lineage from every certificate that already
names the operator, and so does an enrollment. That is the recovery from
an answer nobody received, and the closest thing to a revocation garam
has.

`CertificateStore` holds one row per operator and has no read: what the
rule needs is not the current certificate but whether the presented one
is still it, and the write answers that itself. `EnrollmentStore` has no
read for the same shape of reason: nothing can answer a token without
also spending it, which is what makes single use the store's rather than
a check a caller performs. Signing happens through the `Issuer` seam the
composition root satisfies from `internal/ca`, as the agents domain's
does, so this one never holds key material; the successor is signed
before the record moves, and a caller that lost the lineage receives
nothing. The certificate half's HTTP surface is the
machine listener — the renewal a certificate authenticates, and the
enrollment reached without one — and `internal/cli` is its other caller,
`garam issue-certificate`. The record half's surface is the
browser-facing listener, on the organization's own paths, and that is
also where a token is minted: registering writes the record and the
token in one transaction, so a registration never exists without the
token that enrols it. The
agent's certificate is not
this domain's: an agent is admitted to its own by the claim its operator
made (ADR-0033) and renews on the route ADR-0040 gave it.
