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
Checker.Allow(subject, object, action)
  1. collect statements: subject's attached policies (via roles/groups)
     + policies attached to the subject directly
  2. any matching Deny statement?          → DecisionDeny
  3. else any matching Allow statement?    → DecisionAllow
  4. else                                  → DecisionDeny (default)
```

Matching = action pattern ∩ resource pattern both match. The per-org
statement set is parsed once and cached in the engine; policy writes
invalidate the org's cache (Postgres `LISTEN`/`NOTIFY`).

## Components

| Component | Responsibility | Depends on |
|-----------|----------------|------------|
| `PolicyStore` | policy documents + append-only versions | Postgres |
| `RoleStore` | subject × role × scope persistence | `grn.GRN` |
| `Evaluator` | statement-set → `Decision` (native Go) | — |
| `Manager` | grant/revoke/attach lifecycle + audit log | stores |
| `Checker` | collect + evaluate | `RoleStore`, `Evaluator` |

The api server never imports this package; it calls the engine over
gRPC (`Check` / `Explain` in `proto/gaon/authz/v1`) and supplies the
statements collected for the subject. The in-process `Evaluator`
remains for tests and local dev.

## Roles vs policies

| Concept | Storage | Update cadence | Where it runs |
|---------|---------|----------------|---------------|
| Role | `RoleStore` (subject × role × scope) | frequent | collects the policies that apply to a subject |
| Policy | `PolicyStore` (document + versions) | rare; audited | parsed, cached per org, evaluated per request |

Roles are data. Policies are rules. Keep them on separate update
channels.

## Explain

`Manager.Explain(subject, object, action)` reports which statements
matched and which produced the final decision. A denied request must
be reproducible after the fact.

## Constraints

- Resource-based policies (attaching a policy to a resource) are
  deferred; identity-based only.
- `Condition` is parsed but not evaluated in the MVP.
- The earlier Rego/OPA direction is superseded (ADR-0002); preserved
  WIP is reference only.
- Domain types do not satisfy `authz.Subject` / `authz.Object` yet —
  adapters are planned.
