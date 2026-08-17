# Authorization

Org-scoped permission decisions, served by an independent gRPC policy
engine (`garam serve authz`, see [ADR-0003](./adr/0003-single-cli-topology.md)).
Policies are IAM-style documents (see
[ADR-0002](./adr/0002-iam-style-policy-model.md)).

## Contents
- Policy document
- Evaluation
- Statement collection
- Policy persistence
- Managing policies over HTTP
- Policy attachment
- Components
- Roles vs policies
- Explain
- Constraints

## Policy document

```json
{
  "Version": "2026-07-25",
  "Statement": [
    {
      "Sid": "AllowOrgRead",
      "Effect": "Allow",
      "Action": ["org:read", "project:list"],
      "Resource": ["grn:acme:*"]
    }
  ]
}
```

- `Effect`: `Allow` | `Deny`.
- `Action`: `<service_type>:<verb>` strings; `*` and `?` wildcards may
  appear anywhere and repeat (`org:*`, `*`). Constants are owned by
  the defining domain (`internal/org`, `internal/user`, …). The engine
  matches strings only.
- `Resource`: GRN patterns with the same wildcard rules
  (`grn:acme:*` scopes to an org).
- `Condition`: reserved; not evaluated in the MVP.

## Evaluation

```
Evaluator.Evaluate(context, Context)
  1. any matching Deny statement?          → DecisionDeny
  2. else any matching Allow statement?    → DecisionAllow
  3. else                                  → DecisionDeny (default)
```

The engine matches strings only. Matching = action pattern ∩ resource
pattern both match.

## Statement collection

Evaluation input has two origins and the engine unions them
([ADR-0007](./adr/0007-hybrid-statement-collection.md)). Deny-overrides
applies across the union: either origin alone can allow, and a Deny
from either wins.

| Origin | Owner | Resolved |
|--------|-------|----------|
| role-derived statements | api server | per request, from org tables |
| identity-derived statements | machine listener | per request, from the caller's own GRN |
| persisted policy statements | authz engine | from the org's cache |

The engine scopes a request to the org that owns the object: an org's
identifier is the organization segment of every resource beneath it
(ADR-0001). Which of that org's policies apply then depends on their
attachments (§Policy attachment).

### Role-derived statements

The api server checks each request against the resource it targets —
the org, or the group or project addressed by the path — and collects
the statements that apply to that resource:

| Origin | Action patterns | Resource patterns |
|--------|-----------------|-------------------|
| org membership role | `Role.Actions()` | the org GRN and `grn:<org>:*` |
| subject's direct roles | `org.ActionsFor(user.Roles())` | the org GRN and `grn:<org>:*` |
| target group's roles, when the subject is a member | `org.ActionsFor(group.Roles())` | the group GRN |
| target project's roles, when the subject is a member | `org.ActionsFor(project.Roles())` | the project GRN |

Group and project roles reuse the org role vocabulary (`owner`,
`admin`, `member`), are assigned through the `roles` field of the
group and project create and patch endpoints, and are narrowed by
resource: a group `admin`
administers that group and nothing else, while an org `admin`
administers everything the org owns. Unknown role names grant nothing.
Org membership stays the precondition for every check — neither a
group grant nor a direct role admits a non-member of the org.

Roles stored on the user itself are direct grants and carry the org
role's scope, so a direct role applies in every org the subject
belongs to. Policies are not collected here at all — the engine owns
them and resolves them from the subject and the groups the request
carries (§Policy attachment).

### Identity-derived statements

A machine identity holds no roles and belongs to no org, so the machine
listener collects what its GRN alone grants it and supplies that in the
same slot ([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)).

| Origin | Action patterns | Resource patterns |
|--------|-----------------|-------------------|
| the caller's own GRN | `message:send` | `grn:<caller org>:*` |

That is why an agent nobody has written a policy for goes on sending
inside its organization. A policy narrows it with a Deny, as a Deny
narrows a role's grant.

What keeps an agent out of another organization is not that pattern.
The listener refuses a recipient of another organization before it
asks, which is how it meets the obligation §Policy attachment states;
the organization segment in the pattern is defence in depth behind it.

The listener sends the sender's groups beside that statement. Group
membership is org data and this surface holds no store, so it reads
them over a seam of its own, which is what makes a policy attached to a
group decide what its members send (§Policy attachment). Membership it
cannot read refuses the message rather than deciding it without: an
unread group and an empty one are the same answer otherwise, and the
difference is every Deny attached to one.

## Policy persistence

Policies are org-managed data in Postgres (ADR-0002): `policies`
carries the metadata, status, and default-version pointer;
`policy_versions` is append-only, enforced by a schema trigger. A
stored version is never rewritten — activation and rollback move the
pointer, and versions disappear only with the policy that owns them.

| State | Meaning |
|-------|---------|
| `draft` | stored, evaluated by nothing |
| `active` | contributes its default version's statements to checks |
| `archived` | retained, no longer evaluated |

The engine parses each org's active documents once and caches the
resulting statement set. Writes made through the engine drop the org's
entry directly. Writes made by any other process announce the org on
the Postgres `LISTEN`/`NOTIFY` channel `garam_policy_change`, which a
schema trigger raises and the engine's listener consumes.

Losing that connection is ordinary rather than exceptional — a Postgres
failover breaks it by design — so the listener reopens one, backing off
between attempts so a database that is down is not retried into an
outage of its own. A notification raised while it was down is not
repeated, so a reopened connection drops every cached org: what the
engine read before the break is what it cannot know is current. Only
shutting the process down ends the listener, and `garam serve authz`
reports that.

A reconnecting listener does not take the engine out of rotation. The
engine's readiness reports the database and nothing else, because
readiness reports what a replica cannot serve without and one that is
reloading its cache still answers
([ADR-0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md)).
A decision may therefore be made from statements no notification has
invalidated yet; if that ever has to be refused, the refusal belongs to
the check, which can name the request in doubt, and not to a probe,
which cannot.

Lifecycle is served by `PolicyService` in `proto/garam/authz/v1`:
create, append a version, list policies and versions, activate,
archive, roll back, attach, detach, and list attachments. Documents are
validated at the write boundary, so a stored document always parses.

## Managing policies over HTTP

Org admins reach that lifecycle under `/orgs/{org}/policies`
(`api/openapi.yaml`). The api server passes each call through the
`PolicyManager` seam — the gRPC client to `PolicyService` — so it never
imports the engine and never parses a document: a document that does
not parse comes back as `invalid_argument` and leaves as 400.

Policy management is authorized like any other action. The
`policy:create`, `policy:read`, `policy:update`, `policy:attach`, and
`policy:detach` constants live in `internal/org`, because the api
server enforces them and may not import the engine that stores
policies. `owner` and `admin` grant `policy:*`; `member` grants none,
so reading a policy is an admin action. The addressed policy's GRN is
the object of the check, and its organization segment is the org in the
path, so a policy of another org is not addressable.

## Policy attachment

An active policy is attached to subjects through `policy_attachments`
([ADR-0008](./adr/0008-identity-based-policy-attachments.md)). The
engine matches subject strings and validates no service type, so an
agent GRN is a subject on the same terms as a user
([ADR-0014](./adr/0014-agent-authentication.md)) — it needs no model
change, only a caller that can authenticate as one.

| Attachment | Applies to |
|------------|------------|
| none | every subject the caller asks about (see below) |
| a user GRN | that user |
| an agent GRN | that agent |
| a group GRN | the members the request reports for that subject |

A check therefore evaluates four sources: the org's unattached active
policies, the policies attached to the subject, the policies attached
to a group the subject belongs to, and the statements the calling
server collected. Deny-overrides applies across all four.

**An unattached policy's scope is the caller's obligation, not the
engine's** ([ADR-0025](./adr/0025-engine-scope-is-a-caller-obligation.md)).
The engine loads the policies of the organization owning the **object**
and applies every unattached one to whatever subject it was handed; it
holds no membership data and cannot tell whether that subject belongs
to that organization. A caller must therefore establish that the
subject belongs to the object's organization before asking, and refuse
before asking if it cannot. `internal/api` establishes it by fetching
the member whose role it needs; the machine listener establishes it by
comparing organizations outright, because a machine identity is never an
org member and there is no membership row to miss on. The groups a
caller sends are subjects too, and the same obligation covers them: a
group belongs to the org that names it in its GRN.

The row above reads "every subject the caller asks about" for that
reason. A caller that hands the engine a subject from another
organization gets that organization's unattached policies applied to
it, which is an organization boundary decided by the caller and not by
the policy.

Detaching an active policy's last attachment does not retire it, it
widens it back to the org — which for a Deny turns one subject's
restriction into every subject's, and for an Allow is the shortest
route to handing that grant to every subject a caller asks about.
Archiving is what takes a policy out of every check.

An attached policy decides what an agent sends
([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)), inside
the sending agent's own org and nowhere else. Every attachment that
decides an agent's message is therefore a Deny: an Allow widens nothing
the identity-derived grant does not already cover, and it buys no
recipient outside the org, which the listener refuses before a policy
is read.

Each caller resolves the subject's groups from the org tables and sends
them as `subject_groups` on `Check` / `Explain`; the engine matches
those GRNs against attachments without knowing what a group is. An
attachment stays inside the policy's org, enforced by a schema trigger;
a root-owned identity such as a user cannot name an org in its GRN, so
org membership stays the api server's precondition. An agent names its
org in its own GRN, so the trigger binds it directly and it is never an
org member (ADR-0014) — a group holds it as a member all the same, and
that membership is what a policy attached to the group decides through.
Attachment writes announce the org on `garam_policy_change` like any
other policy write.

## Components

| Component | Responsibility | Depends on |
|-----------|----------------|------------|
| `Statement` / `PolicyDocument` | IAM-style rule model (ADR-0002) | — |
| `Evaluator` | statement-set → `Decision` | — |
| `StatementEvaluator` | native in-process `Evaluator` | — |
| `Policy` / `PolicyVersion` | stored policy and its revisions | — |
| `PolicyAttachment` | binds a policy to a subject (ADR-0008) | — |
| `Store` / `PostgresStore` | policy and attachment persistence | `migrations/` |
| `PolicyCache` | per-org parsed policies and their attachments | `Store` |
| `GRPCServer` | `Check` / `Explain` RPCs | `PolicyCache` |
| `PolicyGRPCServer` | policy lifecycle RPCs | `Store`, `PolicyCache` |

Role persistence (subject × role × scope) stays roadmap; roles remain
org data resolved by the api server.

The api server never imports this package; it calls the engine over
gRPC (`Check` / `Explain` in `proto/garam/authz/v1`) and supplies the
role-derived statements it collected. The machine listener reaches the
same engine the same way, over a seam of its own, and supplies the
identity-derived ones. The in-process `Evaluator` remains for tests and
local dev.

## Roles vs policies

| Concept | Storage | Update cadence | Where it runs |
|---------|---------|----------------|---------------|
| Role | Postgres, `org` tables | frequent | api server collects the statements a role grants |
| Policy | Postgres, engine tables | rare; audited | parsed, cached per org, evaluated per request |

Roles are data. Policies are rules. Keep them on separate update
channels.

## Explain

The `Explain` RPC reports which statements matched and which produced
the final decision, across both origins. A denied request must be
reproducible after the fact.

## Constraints

- Resource-based policies (attaching a policy to a resource) are
  deferred; identity-based only (ADR-0008).
- `Condition` is parsed but not evaluated in the MVP.
- The earlier Rego/OPA direction is superseded (ADR-0002); preserved
  WIP is reference only.
- Domain types do not satisfy `authz.Object`; the engine adapts a
  parsed GRN at the transport boundary.
