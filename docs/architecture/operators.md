# Operators

What an organization records about an operator, and what that operator
authenticates with: the record that the organization has it (ADR-0045),
and which certificate garam last issued it, together with what it takes
to replace that with the next (ADR-0046). Implemented by
`internal/operator`.

| Type | Responsibility |
|------|----------------|
| `Operator` | an organization's record that it has an operator under this GRN: who registered it and when |
| `Store` | registers one, answers one by GRN, and lists an organization's own |
| `Credential` | the certificate a connection authenticated with — the identity it names, which certificate it is, and how long it is good for |
| `Certificate` | what an operator authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Current` | the certificate garam last issued an operator: the record a renewal is measured against |
| `CertificateStore` | records what garam last issued, and moves that record to the successor only where the presented certificate is still it |
| `Service` | mints an operator's certificate out of band and adopts it, and renews the one an operator presents |
| `Issuer` | signs a certificate for a subject GRN and hands back the garam server root beside it |

The identifier is the caller's rather than garam's, which is what makes
this domain unlike every other org resource. An operator's GRN is
already the subject of a certificate wherever `issue-certificate` has
run (ADR-0032), so a minted identifier would name something other than
the operator that exists. What the domain applies is the GRN segment
rule, and the schema applies it again where nothing routes around it.

A record grants nothing and reports nothing. No listener reads one to
admit a caller: an operator authenticates with the certificate it holds
(ADR-0015), and garam never dials one, so there is no state here for
anybody to report. What the record is read for is the question a
definition asks — whether this organization has the operator it names —
which the browser surface asks there, and the list of what the
organization has registered, which the console shows and `ListByOrg`
exists for. The `agent_definitions` foreign key is the copy of the
definition's check no call path routes around.

There is no update and no delete. A record carries nothing to change,
and removing one would stop nothing an operator can already do, which is
a route ADR-0045 declines to offer until revocation is decided.

The certificate half reads no record above it. An operator renews with
what it holds, and whether its organization registered it changes
nothing about that.

An operator's first certificate arrives out of band (ADR-0015,
ADR-0032). Every one after it is issued here, over the connection the
certificate being replaced authenticated, so the out-of-band mint is a
bootstrap rather than a standing obligation.

Four things refuse a renewal, and each is a decision rather than a
guard. A certificate whose subject is not an operator is refused before
any window is measured: what this domain renews is an operator's
certificate. A certificate that has expired renews nothing: the listener
refuses it a handshake earlier, and the rule states the same boundary as
the empty end of its window, so what follows expiry is a mint out of
band. A certificate with more than a third of its validity left is
refused as too early, measured off the certificate rather than off a
configured number, so the window holds whatever lifetime signed it. And
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
names the operator. That is the recovery from an answer nobody received,
and the closest thing to a revocation garam has.

`CertificateStore` holds one row per operator and has no read: what the
rule needs is not the current certificate but whether the presented one
is still it, and the write answers that itself. Signing happens through
the `Issuer` seam the composition root satisfies from `internal/ca`, as
the agents domain's does, so this one never holds key material; the
successor is signed before the record moves, and a caller that lost the
lineage receives nothing. The certificate half's HTTP surface is the
machine listener, and `internal/cli` is its other caller — `garam
issue-certificate`, the one bootstrap ADR-0015 leaves out of band. The
record half's surface is the browser-facing listener, on the
organization's own paths, which is the third caller this domain has. The
agent's certificate is not
this domain's: an agent is admitted to its own by the claim its operator
made (ADR-0033) and renews on the route ADR-0040 gave it.
