# Domains

Per-domain responsibilities and boundaries.

## Common shape

```
internal/<domain>/
├── <domain>.go     # aggregate
├── service.go      # (optional) Service interface + impl
├── store.go        # Store interface
├── postgres.go | memory.go | ...   # impl
├── *_test.go
└── mocks/          # mockery v3 output
```

Domains cooperate through `grn.GRN`, never through each other's
concrete types.

## `internal/grn`

`GRN` value type with `New`, `Parse`, `Validate`, `String`, `IsZero`.
Depends on `errorx` only.

## `internal/errorx`

`Kind` enum aligned with gRPC status codes, `Error` interface,
`New` / `Wrap` / `IsKind` / `KindOf`. No domain dependencies.

## `internal/org`

Top of the tenant hierarchy.

| Type | Responsibility |
|------|----------------|
| `Organization` | top-level tenant |
| `Group` | bundle of users/agents |
| `Project` | container for a unit of work |

The `Store` interface covers CRUD on each plus membership and cascade
delete on `Organization`.

## `internal/user`

Human identity.

| Field | Meaning |
|-------|---------|
| `grn.GRN` | user identifier (`service_type = "user"`) |
| `nickname` | display name, not enforced unique |
| `organizations` | orgs the user belongs to |
| `roles` | directly assigned roles |
| `policies` | directly assigned policies |

A user can belong to multiple orgs. Roles and policies on the user are
direct grants; group-derived grants resolve at check time.

## `internal/agent`

Autonomous identity. Same shape as `user` with two differences:
agency (delegated principal, can act outside its own org) and narrower
default role/policy set (least-privilege).

Most likely domain to grow audit, rate-limit, and action-logging
responsibilities.

## Store implementation status

| Domain | Interface | postgres | memory | other |
|--------|-----------|----------|--------|-------|
| org | ✅ (`store.go`) | ❌ | not planned | — |
| user | ✅ (`store.go`) | ❌ | not planned | — |
| agent | ✅ (`store.go`) | ❌ | not planned | — |
| authz.RoleStore | ✅ (`role.go`) | ❌ | ✅ (`NewMemoryRoleStore`) | — |
| authz.PolicyStore | ✅ (`policy_store.go`) | ❌ | ✅ (`NewMemoryPolicyStore`) | — |
| authz.Manager | ✅ (`manager.go`) | ❌ | ✅ (`NewMemoryManager`) | — |

The `authz` package keeps in-memory implementations as test doubles
(testing, dev). Domain stores go straight to Postgres — no domain
`memory.go` files are planned. See
[`../plans/roadmap.md`](../plans/roadmap.md).

## Known gap

Domain types (`User`, `Agent`, …) do not yet satisfy `authz.Subject`
or `authz.Object`. Either add the missing methods (`Type() string`)
or expose adapter functions (`user.AsSubject(*User) authz.Subject`).
Tracked in [`../plans/current-state.md`](../plans/current-state.md).

## Future domains

| Domain | Location (planned) | Responsibility |
|--------|---------------------|----------------|
| workflow | `internal/workflow` | workflow definition and execution |
| doc | `internal/doc` | permissioned document |
| audit | `internal/audit` | decision and change events |
| token | `internal/token` | session token issuance and exchange |