# Current State

> Last updated: see git log for the file's history.

## Done

- Module name: `github.com/garamsh/gaon`.
- GRN value type with `New` / `Parse` / `Validate` / `String` /
  `IsZero` and tests. Organization segment fixed to `garam`.
- `errorx` package with gRPC-aligned kinds, `New` / `Wrap` / `IsKind`
  / `KindOf`, and tests.
- Domain interfaces declared: `org.Store`, `user.Store`,
  `agent.Store`, plus aggregate types `Organization`, `Group`,
  `Project`, `User`, `Agent`.
- `authz` interfaces: `Subject`, `Object`, `RoleStore`, `PolicyStore`,
  `Evaluator`, `Manager`, `Checker`. Rego `Evaluator` with
  `data.gaon.authz.allow` query.
- `authz` in-memory implementations: `MemoryRoleStore`,
  `MemoryPolicyStore`, `MemoryManager`, `RegoEvaluator`.
- `authz_test.go` exercising every interface and the `Checker` path.
- Mockery v3 setup (`.mockery.yaml`) generating Store mocks.
- Documentation under `docs/`.

## Open

- **Domain memory implementations** (P0) — interfaces declared, no
  in-memory `Store` impls for `org` / `user` / `agent` yet, so
  end-to-end domain tests are blocked.
- **Subject/Object adapters** (P1) — `User` and `Agent` lack
  `Type() string`; adapter functions or method additions needed before
  the `Checker` can be exercised on real domain types.
- **Persistent stores** (P2) — no Postgres implementation yet.
- **Transport** (P3) — no HTTP / gRPC layer.
- **Workflow core** (P4) — domain not created.
- **CI / hooks** (P5) — no `.github/workflows/`, no `lefthook.yml`,
  no `Makefile`.

## Known corrections applied

- Module path renamed `garam` → `gaon`; all imports updated.
- Rego package renamed `garam.authz` → `gaon.authz`; comments and
  query path updated.
- GRN organization segment restored to `garam` after a mistaken
  rename to `gaon`.
- `internal/authz/rego.go` doc comment path fixed
  (`testdata/policy.rego` → `policies/authz.rego`).

## Backlog

Pulled from `// TODO` lines and review notes. Each item gets its own
PLAN.md when picked up.

- Add domain memory impls + tests.
- Add `User` / `Agent` adapter functions for `authz.Subject` /
  `authz.Object`.
- Decide on HTTP router (chi vs stdlib) at P3 time.
- Add `Makefile`, `lefthook.yml`, `.github/workflows/ci.yml`.