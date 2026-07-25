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
  `Evaluator`, `Manager`, `Checker`. (Implementations — Rego
  evaluator, in-memory stores, tests — are written but unlanded; they
  sit in the WIP backup and land under P1.)
- Mockery v3 setup (`.mockery.yaml`) generating Store mocks.
- Documentation under `docs/`.
- CI plumbing: `Makefile`, `.github/workflows/ci.yml`. Git hooks
  (lefthook) evaluated and not adopted.
- Foundation landed on `main` (PR #15); `develop` retired. Work now
  branches off `main`.

## Open

- **MVP** (roadmap, issue #12) — Google OAuth sign-in (`internal/authn`),
  invitation-based orgs, org-scoped roles, Postgres-backed stores, and
  the OpenAPI-first HTTP API. Not started. The `agent` domain is
  excluded from the MVP (still being planned).
- **authz landing** (P1) — the authz implementation WIP is preserved
  outside the repo (`~/gaon-authz-wip/`); it needs the file split
  required by `docs/convention/go.md` before landing.
- **Subject/Object adapters** (P1) — `User` and `Agent` lack
  `Type() string`; adapter functions or method additions needed before
  the `Checker` can be exercised on real domain types.
- **authz persistence & policy versioning** (P2) — no Postgres
  implementation yet.
- **Workflow core** (P3) — domain not created.

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

- Add `User` / `Agent` adapter functions for `authz.Subject` /
  `authz.Object`.