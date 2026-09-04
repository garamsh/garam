# Organizations

The tenant hierarchy: the top-level tenant, what is grouped inside it,
and who belongs to one. Implemented by `internal/org`.

**A `Group`'s and a `Project`'s `policies` are built in part.** Each
carries them as data, exposed by the aggregate (ADR-0051) and stored,
and nothing collects them into a decision: a policy reaches a subject
through an attachment the authz engine owns (ADR-0008), not through
this list.

| Type | Responsibility |
|------|----------------|
| `Organization` | top-level tenant |
| `Group` | bundle of members addressed by GRN — a user or an agent (ADR-0014) |
| `Project` | container for a unit of work |
| `Membership` | user in an org; carries a `Role` (`owner`, `admin`, `member`) |
| `Invitation` | single-use, expiring bearer token that admits a user into an org with a fixed role |

The `Store` interface covers CRUD on each plus membership, cascade
delete on `Organization`, and a count of what that delete would take
from this domain. Group and project listing is scoped to one
organization; group listing is scoped in both directions — the groups
of an org, and the groups of the org a member belongs to, which a check
sends to the engine to resolve group-attached policies (ADR-0008). A
project is listed by its organization alone. Orgs grow by invitation and
acceptance, not open signup.

**Deleting an organization reaches past this domain, and the schema is
what performs it.** `DeleteOrg` issues one statement against `orgs`;
every row that goes does so through a foreign key declared `ON DELETE
CASCADE`, so what is taken is fixed by the migrations rather than by
any code path. Directly: this domain's groups, projects, memberships
and invitations, the policies the engine stores, the agents and agent
definitions `internal/agent` owns, the operators `internal/operator`
registers, and the organization's certificate authority. Through
those: a group's and a project's members, a policy's versions and
attachments, an agent's assignment, and an operator's unspent
enrollment tokens.

Two of those reach further than one organization. A consent relation
is a pair, so deleting one organization removes the relation the other
is still a side of, without that organization acting (ADR-0035). And
the certificate authority is the one row nothing can put back —
[certificate-authorities.md](./certificate-authorities.md) holds why.
What does **not** go is a certificate an operator already holds
(ADR-0045 errata), and the user accounts of the members, which the
organization does not own.

`getOrgDeletionPreview` is what states this to a caller before the
act, counted on the server rather than assembled from a list the
caller happens to hold; [api-surface.md](./api-surface.md) §What
decides the rest holds what authorizes it.

This domain owns its action constants (`ActionOrgRead`,
`ActionGroupRead`, `ActionProjectRead`, `ActionPolicyCreate`, …) —
including the policy management actions, which the api server enforces
without importing the engine that stores policies, the consent
management actions (ADR-0035), which it enforces over the relation
`internal/message` stores, and reading an
organization's agents, which it enforces without `internal/agent`
importing this package back — and `Role.Actions`
maps each role to the action patterns it grants; `ActionsFor` does the same for
the role names a `Group` or `Project` carries as data. No role holds a
wildcard over agents, and none holds one over operators: ADR-0012 keeps
provisioning outside garam, so each agent action a role grants is granted
by name and a later one is a decision rather than an increment, and
ADR-0045 leaves an operator's enrolment and its revocation undecided,
which is more of the same.
`agent:watch` is granted by no role at all, which is that reason carried
further — ADR-0022 separates watching from reading so an organization may
withhold it, and a role naming it would settle that for every
organization instead. The authz evaluator decides. Groups and projects are addressed under their
organization's HTTP paths, and their GRNs carry the owning org in the
organization segment, so a group or project role is scoped to that
resource alone ([authz.md](./authz.md) §Statement collection).
