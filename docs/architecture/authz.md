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

The caller collects the subject's statements (via roles/groups and
direct attachments) and hands them to the engine; the engine matches
strings only. Matching = action pattern ∩ resource pattern both match.

> Planned (roadmap P2, issue #56): the per-org statement set will be
> parsed once and cached in the engine, with policy writes
> invalidating the org's cache (Postgres `LISTEN`/`NOTIFY`). Today the
> api server builds the statement set per request from org roles.

## Components

| Component | Responsibility | Depends on |
|-----------|----------------|------------|
| `Statement` / `PolicyDocument` | IAM-style rule model (ADR-0002) | — |
| `Evaluator` | statement-set → `Decision` | — |
| `StatementEvaluator` | native in-process `Evaluator` | — |
| `GRPCServer` | `Check` / `Explain` RPCs | `proto/gaon/authz/v1` |

Policy persistence (`PolicyDocument` + append-only versions) and role
persistence (subject × role × scope) are roadmap; the engine today
evaluates caller-supplied statements only.

The api server never imports this package; it calls the engine over
gRPC (`Check` / `Explain` in `proto/gaon/authz/v1`) and supplies the
statements collected for the subject. The in-process `Evaluator`
remains for tests and local dev.

## Roles vs policies

| Concept | Storage | Update cadence | Where it runs |
|---------|---------|----------------|---------------|
| Role | Postgres (planned) | frequent | collects the policies that apply to a subject |
| Policy | Postgres (planned, roadmap P2) | rare; audited | parsed, cached per org, evaluated per request |

Roles are data. Policies are rules. Keep them on separate update
channels.

## Explain

The `Explain` RPC reports which statements matched and which produced
the final decision. A denied request must be reproducible after the
fact.

## Constraints

- Resource-based policies (attaching a policy to a resource) are
  deferred; identity-based only.
- `Condition` is parsed but not evaluated in the MVP.
- The earlier Rego/OPA direction is superseded (ADR-0002); preserved
  WIP is reference only.
- Domain types do not satisfy `authz.Object`; the engine adapts a
  parsed GRN at the transport boundary.
