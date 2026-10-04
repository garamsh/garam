# Agents

Autonomous identity, provisioned outside garam (ADR-0012). Implemented
by `internal/agent`.

| Type | Responsibility |
|------|----------------|
| `Agent` | actor owned by an org; carries the state the operator reports about its pod and the state it reports about itself, separately |
| `Definition` | what an organization says an agent should be: which operator runs it and with what values (ADR-0026), and not what it is built from — it names no image. It names its subject where an `Agent` names itself, and carries none of the state an `Agent` carries, so a reader cannot take one for the other; it writes no `agents` row until the operator it names claims it (ADR-0033) |
| `Template` | a starting point an organization keeps for defining agents: a name, a description and values a definition copies once (ADR-0075). It names no operator and no agent, and no definition records the template it came from, so editing or deleting one reaches no agent |
| `Name` | what an organization calls a defined agent or a template, separate from the GRN: trimmed, one to 100 characters, and not unique |
| `Assignment` | which operator runs an agent, and the epoch that binding holds |
| `Certificate` | what an agent authenticates with and the garam server root it verifies garam by, as this domain hands them back |
| `Service` | creates an agent for the calling operator, issues the certificate of one assigned to it or renews an agent's own, answers an operator with the definitions naming it so it can claim one (ADR-0033), and records the state each party reports (ADR-0053) |
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
organization. What that operator reports about the agent's pod rests
on the same comparison (ADR-0053), so it writes state exactly where it
obtains a certificate and nowhere wider. A caller that is not an
operator is refused, except on the two routes an agent reaches as
itself: renewing its own certificate over the connection that
certificate authenticated, which grants it nothing it does not already
hold (ADR-0040), and reporting its own runtime state. Both are bounded
by the identity the connection proved rather than by an assignment, and
the renewal is the one route the assignment comparison does not stand
on — the connection is what authorizes it, and it reaches the issuer
before the comparison is made. An expired certificate has no such
connection, so the operator route is what recovers one.

A definition's operator and values are written once. Its name is the
organization's word for the agent and changes alone: renaming touches
no GRN, operator, value, assignment, membership or attachment, and no
operator is told (ADR-0075). A definition created without a name, and
every one written before names existed, is called by its identifier.

`Store` answers `Service` and, org by org, the read the browser-facing
listener serves people: an organization's agents and the assignments
holding them, each a batch read rather than one per agent. An agent no
operator holds contributes no assignment, so the two lists are not one
length. Both aggregates are rehydrated inside this package — their
fields are unexported and no exported constructor restores a stored
row's state or timestamps — which is why the store that reads them back
lives here rather than below. A `Template` is a data carrier with
exported fields (ADR-0069), and a `Definition`'s name is an exported
field beside its accessors; the stores stay beside the others until
#1083 moves this domain's store out of the package.

Signing happens through the `Issuer` seam the composition root
satisfies from `internal/ca`, so the two domains still cooperate
through `grn.GRN` alone and this one never holds key material. The
HTTP surfaces are the machine listener — where the operator reports the
pod and the agent reports itself, each on a route of its own and
neither reaching the other's fact (ADR-0053) — and the browser-facing
one for the organization's members, which reads the agents and writes
only definitions, their names and templates: provisioning is an
operator's, from outside garam, and a definition writes no `agents`
row until the operator it names claims it (ADR-0033). The queue a
message reaches an agent through is the messaging domain.

What an agent is built from is the operator's. A `Definition` names no
image, and `values` is not a way around that: garam stores the keys and
the strings and interprets neither (`api/machine.yaml` §Definition), so
a value that happened to name an image would be one garam still never
reads — which leaves the shape of that payload where ADR-0026 left it,
undecided, rather than deciding it here. The images an agent's Pod is
built from and the size of the volume it keeps state on are named by
flags that Garam Agent Operator's `cmd/main.go` declares
([`garamsh/garam-agent-operator`](https://github.com/garamsh/garam-agent-operator)),
which is authoritative for what every agent that operator constructs
is built from. Read at
[`2d795f5`](https://github.com/garamsh/garam-agent-operator/blob/2d795f5/cmd/main.go),
when the repository was named `gagent-operator`, they were
`--agent-image`, `--agent-copy-image`, `--agent-tools-image` and
`--agent-storage-size`; that list is the reading at that commit and not
the operator's current set.

The boundary is drawn by what each side can check rather than by what
either owns. An image reference is a fact about a registry some cluster
can reach — a mirror, an air-gapped copy, the pull secret that admits
it, the network policy in front of it — and garam observes none of
that: ADR-0012 keeps it from dialing into a cluster, so a digest
entered here would be a claim garam has no way to test and whose
failure, at deploy time, it would never see. The operator does not test
one either; it takes the reference as given and leaves it to the API
server to refuse on the object, which is the first party in the chain
that can. garam is on no part of that chain. Sherlock
([`garamsh/sherlock`](https://github.com/garamsh/sherlock)) stated the
same boundary from the agent's own side, for the tool set it loads, at
`internal/config/config.go:79-82` in
[`eeea6a9`](https://github.com/garamsh/sherlock/blob/eeea6a9/internal/config/config.go#L79-L82),
read when the repository was named `gagent`.

Nothing in garam requires a version or a capability of an agent today.
Such a requirement is the one thing on this boundary garam could
properly make, because what its own machine contract needs is a fact
garam holds rather than one about somebody's registry. ADR-0079 accepts
one: each interservice contract works with the adjacent supported
release of its peer in both upgrade orders, and unsupported semantics
are refused by name. None of it is built, so garam still publishes no
release of that contract and no compatibility window (ADR-0049).

## Separated execution

**Nothing in this section is built.** It is the accepted target of
ADR-0077 and ADR-0078, and everything above describes what runs until a
GRN cuts over. Product implementation is tracked in #1157.

- **Desired runtime values and templates leave garam.** The Operator
  control service is their one writable managed source and the cluster
  controller pulls from it; a standalone host keeps its own.
  `Definition` values and `Template` are what move, per GRN, with no
  dual write and no new workload or volume. The GRN, the `Name` and the
  assignment stay here, and the console still copies a template once
  (ADR-0077).
- **A managed agent is created by request, not by claim.** The control
  service asks for one under a delegation and a stable request
  identifier; registration and the first assignment are one
  transaction, and a retry answers the same GRN. Today they are two
  writes, as `Service.Create` in `internal/agent/service.go` says.
- **A generation is what an assignment does not fence.** Beside the GRN
  and the epoch, garam records the active process generation, which a
  lifecycle authority activates after proving it owns the writer; the
  execution credential cannot activate one. The machine listener checks
  it per request ([machine-identity.md](./machine-identity.md)
  §Generation and lineage) (ADR-0078).
- **Applied configuration is the runtime's report**, at its GRN, epoch,
  generation and desired revision, kept apart from what the Operator
  rendered or provisioned. The Operator control service holds it; garam
  only answers whether that epoch and generation are current, and a
  report activates nothing. A missing or stale report is unknown, never
  success (ADR-0078).
- **Execution authority is a scoped, expiring delegation plus a
  high-trust permission**, both checked fresh before a pending revision
  activates (ADR-0078).
- **A lost credential is recovered for the same GRN**, replacing its
  lineage and fencing the old one, rather than by recreating the agent
  (ADR-0078).
