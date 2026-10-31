# Organizations

The tenant hierarchy: the top-level tenant, what is grouped inside it,
and who belongs to one. Implemented by `platform/internal/org`.

**A `Group`'s and a `Project`'s `policies` are built in part.** Each
carries them as data and stores them, and nothing collects them into a
decision: a policy reaches a subject through an attachment the authz
engine owns (ADR-0008), not through this list.

**An organization's cowork principal is unbuilt (#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
has the platform write one, `grn:<org>:default:service:cowork`, in the
transaction that creates the organization, and a migration write one
for each organization that exists. Deleting the organization takes it
by the cascade below. No member manages it: a server command of the
deployment revokes and re-issues it, and while it is revoked every
`cowork:` action on the organization is refused
([machine-identity.md](./machine-identity.md) §cowork's principal).

**What a store writes and reads back is an exported field on the
aggregate**
([ADR-0069](./adr/0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md)),
which supersedes the accessor ADR-0051 chose. `Organization`, `Group`,
`Project`, `Membership` and `Invitation` carry exported fields, and the
store sits in `platform/internal/org/postgres/`, so `go list -deps` on
`platform/internal/org` reports no `jackc/pgx`. The rule that a Group or
Project member another org owns is refused stays in the domain as
`CheckMemberOrg`, which the store calls.

| Type | Responsibility |
|------|----------------|
| `Organization` | top-level tenant |
| `Group` | bundle of members addressed by GRN — a user or an agent (ADR-0014); the console calls it a team (ADR-0072) |
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

**An invitation is spent once, by a claim taken before anything it
admits is created** (#1381). The claim is a conditional update on the
invitation's row, matching only one that is neither spent nor expired,
so of two concurrent uses one matches and the other creates nothing.
Accepting is one transaction: the claim and the membership commit
together, and a membership the user already holds rolls the claim
back. Registering creates an account in stores outside this domain,
so its claim stands alone: it sets `accepted_at` and leaves
`accepted_by` empty, since that column references a user who does
not exist yet. That claimed state reads as spent everywhere, the
preview included. The user, the credential and the membership follow,
and `accepted_by` is written last. A failure on the way deletes the
user, which takes the credential and the membership with it, and then
releases the claim. The release matches only the claim's own
`accepted_at`, so it undoes no other holder's claim. A registration
that dies while holding a claim leaves the invitation spent with
nobody named, which fails closed: an admin issues a new one. One that
dies after the user was created also leaves an account, possibly with
a password, and no membership. That account reaches no organization,
and its username stays taken.

**A pending invitation is listed and revoked by an id that admits no
one**
([ADR-0136](./adr/0136-a-pending-invitation-is-listed-by-an-id-that-admits-no-one-and-revoking-deletes-it.md)).
The token is a bearer secret, so `listInvitations` answers each
invitation neither spent nor expired by its `id`, on `org:invite`, the
action that issues it. Revoking deletes the row, matching only an
unspent one of the organization in the path, so a token answers as
unknown afterwards and a revocation and a use never both take effect.
A spent invitation is refused as `invitation_accepted`.

**A group or project holds no member another organization owns.** An
org-owned GRN names its organization in the organization segment
(ADR-0001), so the store refuses an add whose member names an
organization other than the group's or project's, and stores nothing.
A root-owned GRN — a user's — belongs to no organization and is
admitted, the line the engine's attachment trigger draws (ADR-0008).
The check reads the two GRNs and no row, so no concurrent write can
slip past it. A remove is not refused: no such membership can be added
now, and one stored before the refusal existed stays removable.
`platform/api/openapi.yaml`'s `addGroupMember`, `addProjectMember`,
`removeGroupMember` and `removeProjectMember` descriptions state what a
caller is answered.

**Deleting an organization reaches past this domain, and the schema is
what performs it.** `DeleteOrg` issues one statement against `orgs`;
every row that goes does so through a foreign key declared `ON DELETE
CASCADE`, so what is taken is fixed by the migrations rather than by any
code path. Directly: this domain's groups, projects, memberships and
invitations, the policies the engine stores, the agents
`platform/internal/agent` owns, the operators
`platform/internal/operator` registers, and the certificate authority an
earlier release gave the organization, where it has one. Through those:
a group's and a project's members, a policy's versions and attachments,
an agent's assignment, and an operator's keys and the tokens exchanged
under them. Before the cascade, `platform/migrations/000047`'s trigger
on `orgs` holds the agents' execution rows and then the operators'
registrations, in the orders [agents.md](./agents.md) and
[machine-identity.md](./machine-identity.md) state.

Two of those reach further than one organization. A consent relation
is a pair, so deleting one organization removes the relation the other
is still a side of, without that organization acting (ADR-0035). And
the certificate authority is the one row nothing can put back: it
holds the authority's private key sealed under the key-encryption key,
and garam makes no authority again (ADR-0118,
[domains.md](./domains.md) §Shared code below the domains). What does
**not** go is the user accounts of the members, which the organization
does not own.

`getOrgDeletionPreview` is what states this to a caller before the
act, counted on the server rather than assembled from a list the
caller happens to hold;
[api-surface.md](./api-surface.md) §What decides the rest holds what
authorizes it.

This domain owns its action constants (`ActionOrgRead`,
`ActionGroupRead`, `ActionProjectRead`, `ActionPolicyCreate`, …) —
including the policy management actions, which the api server enforces
without importing the engine that stores policies, the consent
management actions (ADR-0035), which it enforces over the relation
`platform/internal/message` stores, and reading an organization's
agents, which it enforces without `platform/internal/agent` importing
this package back — and `Role.Actions` maps each role to the action
patterns it grants; `ActionsFor` does the same for the role names a
`Group` or `Project` carries as data. No role holds a wildcard over
agents, and none holds one over operators: ADR-0012 keeps provisioning
outside garam, so each agent action a role grants is granted by name and
a later one is a decision rather than an increment, and an operator's
three are named the same way — a wildcard would have granted deleting an
operator before anybody decided it (ADR-0045, ADR-0070). `agent:watch`
is granted by no role at all, which is that reason carried further —
ADR-0022 separates watching from reading so an organization may withhold
it, and a role naming it would settle that for every organization
instead. The authz evaluator decides. Groups and projects are addressed
under their organization's HTTP paths, and their GRNs carry the owning
org in the organization segment, so a group or project role is scoped to
that resource alone ([authz.md](./authz.md) §Statement collection).
