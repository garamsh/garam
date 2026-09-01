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

The `Store` interface covers CRUD on each plus membership and cascade
delete on `Organization`. Group and project listing is scoped to one
organization; group listing is scoped in both directions — the groups
of an org, and the groups of the org a member belongs to, which a check
sends to the engine to resolve group-attached policies (ADR-0008). A
project is listed by its organization alone. Orgs grow by invitation and
acceptance, not open signup.
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
