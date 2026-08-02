# Authorization

Org-scoped permission decisions, served by an independent gRPC policy
engine (`gaon serve authz`, see [ADR-0003](./adr/0003-single-cli-topology.md)).
Policies are IAM-style documents (see
[ADR-0002](./adr/0002-iam-style-policy-model.md)).

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
| persisted policy statements | authz engine | from the org's cache |

The engine scopes a request to the org that owns the object: an org's
identifier is the organization segment of every resource beneath it
(ADR-0001). Persisted policies apply to their whole org — attaching a
policy to an individual subject is future work (ADR-0002).

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
belongs to. Policies stored on the user are not collected: an active
policy grants org-wide, and attaching one to a single subject is
future work (ADR-0007).

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
the Postgres `LISTEN`/`NOTIFY` channel `gaon_policy_change`, which a
schema trigger raises and the engine's listener consumes; if that
listener stops, `gaon serve authz` reports it and single-process
invalidation still holds.

Lifecycle is served by `PolicyService` in `proto/gaon/authz/v1`:
create, append a version, list policies and versions, activate,
archive, and roll back. Documents are validated at the write boundary, so a stored
document always parses.

## Components

| Component | Responsibility | Depends on |
|-----------|----------------|------------|
| `Statement` / `PolicyDocument` | IAM-style rule model (ADR-0002) | — |
| `Evaluator` | statement-set → `Decision` | — |
| `StatementEvaluator` | native in-process `Evaluator` | — |
| `Policy` / `PolicyVersion` | stored policy and its revisions | — |
| `Store` / `PostgresStore` | policy persistence | `migrations/` |
| `PolicyCache` | per-org parsed statement set | `Store` |
| `GRPCServer` | `Check` / `Explain` RPCs | `PolicyCache` |
| `PolicyGRPCServer` | policy lifecycle RPCs | `Store`, `PolicyCache` |

Role persistence (subject × role × scope) stays roadmap; roles remain
org data resolved by the api server.

The api server never imports this package; it calls the engine over
gRPC (`Check` / `Explain` in `proto/gaon/authz/v1`) and supplies the
role-derived statements it collected. The in-process `Evaluator`
remains for tests and local dev.

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
  deferred; identity-based only. Attaching a policy to a single
  subject is deferred too — an active policy grants org-wide.
- `Condition` is parsed but not evaluated in the MVP.
- The earlier Rego/OPA direction is superseded (ADR-0002); preserved
  WIP is reference only.
- Domain types do not satisfy `authz.Object`; the engine adapts a
  parsed GRN at the transport boundary.
