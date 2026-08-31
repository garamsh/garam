# Agents

Autonomous identity, provisioned outside garam (ADR-0012). Implemented
by `internal/agent`.

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Definition` | what an organization says an agent should be: which operator runs it and with what values (ADR-0026). It names its subject where an `Agent` names itself, and carries none of the state an `Agent` carries, so a reader cannot take one for the other; it writes no `agents` row until the operator it names claims it (ADR-0033) |
| `Assignment` | which operator runs an agent, and the epoch that binding holds |
| `Certificate` | what an agent authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Service` | creates an agent for the calling operator, issues the certificate of one assigned to it or renews an agent's own, and answers an operator with the definitions naming it so it can claim one (ADR-0033) |
| `Issuer` | signs a certificate for a subject GRN and hands back the garam server root beside it |

garam mints an agent's GRN and never changes it, so which operator runs
the agent is a relation a reassignment moves rather than part of the
identity. That GRN must name an agent of the organization that owns it:
ADR-0014 rests tenancy on its organization segment, so the domain
refuses a pairing that disagrees rather than trusting its callers, and
the schema refuses it again where nothing routes around it. An
assignment stays inside that organization and names an operator
(ADR-0015), which the schema alone decides. A definition names one the
organization has registered as well, which the browser surface decides
against the operators domain and the schema decides again (ADR-0045);
this domain reads no record of its own and addresses an operator by GRN. Every assignment raises the
epoch, and a state report is accepted only at the current one — the
operator a reassignment replaced reports nothing about the agent it
lost. An agent is never an organization member (ADR-0014), so nothing
here references `users`.

`Service` is what an operator does to the agents it runs. It creates
one in the operator's own organization, garam minting the identifier,
and it answers with the certificate of an agent assigned to that
operator at the current epoch and of no other — the whole of the
authorization on the operator route, which ADR-0015 states is not an
optimisation and may not be skipped, so no operator reaches an issuer
without it. Comparing the operator on the row is the epoch fence as
well, and it is the tenancy check too, since a GRN carries its
organization. A caller that is not an operator is refused, with one
exception: an agent asking for its own certificate is renewing over the
connection that certificate authenticated, which grants it nothing it
does not already hold (ADR-0040). That branch is the one route the
assignment comparison does not stand on — the connection is what
authorizes it, and it reaches the issuer before the comparison is made.
An expired certificate has no such connection, so the operator route is
what recovers one.

`Store` answers `Service` and, org by org, the read the browser-facing
listener serves people: an organization's agents and the assignments
holding them, each a batch read rather than one per agent. An agent no
operator holds contributes no assignment, so the two lists are not one
length. Both aggregates are rehydrated inside this package — their
fields are unexported and no exported constructor restores a stored
row's state or timestamps — which is why the store that reads them back
lives here rather than below.

Signing happens through the `Issuer` seam the composition root
satisfies from `internal/ca`, so the two domains still cooperate
through `grn.GRN` alone and this one never holds key material. The
HTTP surfaces are the machine listener for the operator and the
browser-facing one for the organization's members, which reads the
agents and writes only definitions — provisioning is an operator's, from
outside garam, and a definition writes no `agents` row until the
operator it names claims it (ADR-0033). The queue a message reaches an
agent through is the messaging domain.
