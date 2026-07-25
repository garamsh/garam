# Authorization

Two layers: **role-based enrichment** and **Rego evaluation**. Roles
live in data, policies live in code-shaped rules.

```
Checker.Allow(...)
  1. load subject's roles (RoleStore)
  2. keep only object-scoped roles
  3. delegate to Evaluator.Evaluate(Context)
  4. return DecisionAllow | DecisionDeny
```

## Components

| Component | Responsibility | Depends on |
|-----------|----------------|------------|
| `RoleStore` | subject × role × scope persistence | `grn.GRN` |
| `PolicyStore` | named Rego source storage | string id |
| `Manager` | grant/revoke lifecycle + audit log | `RoleStore` |
| `Checker` | role enrich + evaluate | `RoleStore`, `Evaluator` |
| `Evaluator` (Rego) | `Context` → `Decision` | OPA `rego` |

## Interfaces

```go
type Subject interface {
    GRN() grn.GRN
    Type() string
    Roles() []string
    Policies() []string
}

type Object interface {
    GRN() grn.GRN
    Type() string
}

type Decision int  // DecisionAllow | DecisionDeny

type Context struct {
    Subject Subject
    Object  Object
    Action  string             // "doc:read", "doc:write", ...
    Env     map[string]string  // request context (IP, time, ...)
}

type Evaluator interface {
    Evaluate(ctx context.Context, c Context) Decision
}
```

## Rego policy

- Package: `gaon.authz`
- Query: `data.gaon.authz.allow`
- Input: a projection of `Context`

  ```rego
  package gaon.authz

  default allow := false

  allow if {
      some role in input.subject.roles
      role == "admin"
  }
  ```

- Policies live in `internal/authz/policies/<name>.rego`. Starter:
  [`authz.rego`](../../internal/authz/policies/authz.rego).

The package is `gaon.authz`, not `garam.authz`. See
[grn-model.md §5](./grn-model.md#rego-package-is-gaonauthz) for the
split between data origin and code home.

## Roles vs policies

| Concept | Storage | Update cadence | Where it runs |
|---------|---------|----------------|---------------|
| Role | `RoleStore` (subject × role × scope) | frequent (during a workflow) | enrichment of the subject before evaluation |
| Policy | `PolicyStore` (Rego source) | rare; audited | compiled and cached, queried per request |

Roles are data. Policies are rules. They look similar in Rego but use
different update channels — keep them separate.

## Explain

`Manager.Explain(subject, object, action)` walks the grant/revoke
changelog for the subject-object pair, emits one `DecisionTrace` per
change, and reports whether any grant produced the allow. The point
is reproducibility: a denied request must come with a follow-up
explanation.

## Current usage (in-memory)

```go
ev, _ := authz.NewRegoEvaluator(ctx, defaultPolicySource)
roles := authz.NewMemoryRoleStore()
mgr  := authz.NewMemoryManager(roles)
chk  := authz.NewChecker(roles, ev)

ok, err := chk.Allow(ctx, subject, object, "doc:read")
```

## Constraints

- `Evaluator` today is Rego only. Other implementations (in-process
  rule tables, hard-coded) plug into the same interface when added.
- Policy cache is in-process. Multi-instance deployments will need a
  shared cache or a policy reload signal.
- Domain types do not satisfy `authz.Subject` / `authz.Object` yet —
  adapters are planned.