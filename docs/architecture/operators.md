# Operators

What an organization records about an operator, where that operator
stands in obtaining a certificate, and what it authenticates with: the
record that the organization has it and that its certificate
authenticates on (ADR-0045, ADR-0070), the one-time token registering
it mints and a member mints again where it has not enrolled (ADR-0064,
ADR-0067), and which certificate garam last issued it, together with
what it takes to replace that with the next (ADR-0046). Implemented by
`internal/operator`.

| Type | Responsibility |
|------|----------------|
| `Operator` | an organization's record that it has an operator under this GRN: who registered it and when, and where that operator stands in obtaining a certificate |
| `EnrollmentState` | where an operator stands: holding a certificate, holding a token nobody has spent, or holding neither |
| `Enrollment` | the one-time token an operator obtains its first certificate with: which operator it enrols, who minted it, and when it stops being spendable |
| `Store` | registers one with the token it enrols with and answers what it stored, mints that token again for one that has not enrolled, deletes one and everything the identifier held, answers one by GRN, and lists an organization's own |
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

A record mints nothing, and the token beside it grants no more than
permission to ask for one certificate, once. What the record does is
admit: the machine listener reads it per connection, so an operator
authenticates with the certificate it holds only while its organization
still holds the record, and withdrawing the record stops it (ADR-0070).
garam still never dials an operator, so nothing an operator says
reaches this domain and no state here is a report of one. The record is
read for three questions — whether this organization has the operator a
definition names, which the browser surface asks there; the list of
what the organization has registered, which the console shows and
`ListByOrg` exists for; and whether a presented certificate still names
an operator this organization holds, which the machine listener asks
through a seam of its own. The `agent_definitions` foreign key is the
copy of the definition's check no call path routes around, and it is
what refuses a delete as well.

That list also answers where each operator stands in obtaining a
certificate, derived from the two tables beside the record and stored in
neither: a certificate garam issued is `enrolled`, a token still inside
its window is `pending`, and neither is `expired`. The order is the
whole of it. A certificate outranks a token, because it is the outcome
the token exists to produce, so an operator holding one reads `enrolled`
however much of its token is left — a case that is reachable rather than
hypothetical, since `garam issue-certificate` records a certificate for
a GRN nobody registered and registering that GRN afterwards mints a
token beside it. What the three mean to a browser caller is
`api/openapi.yaml` §OperatorEnrollmentState, which is authoritative for
that (ADR-0059).

Registering therefore answers the record as stored rather than the one
it was handed: the state is read in the same transaction, so a GRN that
already holds a certificate answers `enrolled` on the call that minted
it a token it does not need.

`enrolled` says a certificate was obtained and not that the operator is
alive. garam records no expiry beside the row and asks the operator
nothing, so an operator that enrolled and then died reads `enrolled` and
that is correct. The timestamp beside the state is the current
certificate's issue time, which a renewal moves (ADR-0046): it answers
when garam last signed for the operator and never when the operator
enrolled. Recording the latter would be a history nothing else reads,
which is what `operator_certificates` declines by keeping the newest row
alone.

There is no update: a record carries nothing to change. There is a
delete, and it is the one write here that takes something away
(ADR-0070). It leaves the identifier holding nothing — the unspent
token goes by the foreign key that was declared for this route ahead of
it, and the record of the certificate garam last issued has no such key
and is removed by name in the same transaction. That second removal is
load bearing rather than tidy: left standing, the row would answer
`enrolled` against the next registration of the same identifier, and
the re-mint refusal below would then deny that operator the token it
needs. An operator an agent definition names is refused, and nothing is
deleted; the definitions move or go first.

The certificate half still reads no record above it. An operator renews
with what it holds, and the renewal rule is measured against the
lineage rather than against the registration. What reads the record is
the listener under both halves, before either is reached: a handshake
that finds no record carries no identity, so an operator without one
reaches neither the renewal nor anything else (ADR-0070). What crosses
between the two halves goes the other way still — the record half's two
queries join both tables under the certificate half to derive the state
above, in the direction a renewal never takes.

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
mint another.

Minting another is a member's to do, for an operator that holds no
certificate (ADR-0067). It grants nothing a registration does not:
where nothing was ever issued, it is what deleting the record and
registering the same identifier again would be, which is why it is
authorized on the action that registers rather than on one of its own.
That pair of calls is now one a member holding both actions can
actually make, where ADR-0067 argued from an act nobody could perform;
what the argument rested on is unchanged, because it is what the two
calls would grant and not whether they existed.
An operator holds at most one token whatever mints it — the schema
carries a unique key on the operator and the minting statement upserts
on it, so the newest token replaces what was there and one identity is
never reachable by two.

It is refused for an operator that already enrolled, and the refusal
says why. That operator holds a certificate, which is the outcome the
token exists to produce, so a second token would be a second credential
path to one identity — closer to issuing another credential for an
existing subject than to registering. Whether a member may do that is
undecided: it has no forcing case, and garam cannot tell a member
recovering from a compromise from the compromise itself. The message
the refusal carries is where that stands, so a person meeting it learns
a decision was taken rather than that something broke. Which half a
request falls in is the certificate-outranks-token order above, read
inside the same transaction as the write, by the derivation the
registration reads it by and not a second one.

Unknown, spent and expired are one refusal. A caller that spends nothing
presented no credential either way, and telling the three apart would
answer questions about tokens the caller does not hold. Only the hash
reaches the database, as it does for a session, so a row read out of a
dump enrols nothing.

The command ADR-0032 ships still mints a first certificate out of band,
and it is what recovers the operator that already holds one, for which
no token is minted again. An operator with no certificate and no token
is recovered by minting another instead. Every
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
certificate belonging to an operator the organization still wants — the
operator it was taken from renews, the copy loses the lineage, and it
expires. It withdraws nothing: a certificate that can no longer renew
keeps authenticating until it runs out, and a caller holding the one
garam has just replaced authenticates on it meanwhile. What ends a
theft rather than bounding it is deleting the operator, which stops
every certificate naming it at once (ADR-0070).

An operator with no record yet is the one case that comparison has
nothing to make, and it is not a refusal: the write that moves the
record inserts one where none exists, so the first renewal adopts the
lineage. That is what an operator whose certificate predates the record
needs, and it is why the fourth refusal bounds a copy only once a
lineage exists to lose.

The mint adopts whatever it issued, so re-running `garam
issue-certificate` takes the lineage from every certificate that already
names the operator, and so does an enrollment. That is the recovery from
an answer nobody received. It is not the revocation: it narrows what a
copy can do next and leaves it authenticating, where deleting the
operator refuses every copy from its next connection.

`CertificateStore` holds one row per operator and has no read: what the
rule needs is not the current certificate but whether the presented one
is still it, and the write answers that itself. Its table is read by the
record half, for whether a row is there and when it was written; that is
not a read on this interface, and nothing the record half answers names
which certificate the row holds — the serial is what decides a renewal
and stays on the machine listener. `EnrollmentStore` has no
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
also where a token is minted and where a record is deleted: registering
writes the record and the token in one transaction, so a registration
never exists without the token that enrols it; the same surface mints
another where the operator has not enrolled — a route no console page
reaches yet — and deletes one, which the console does reach. Deleting
is authorized on `operator:delete` rather than on the action that
registers, because registering grants nothing and this withdraws what
an operator runs on (ADR-0070). The record is read on the machine
listener too, through the seam that admits a certificate, which is the
one place either listener reads it to decide whether a caller is
admitted. The
agent's certificate is not
this domain's: an agent is admitted to its own by the claim its operator
made (ADR-0033) and renews on the route ADR-0040 gave it.
