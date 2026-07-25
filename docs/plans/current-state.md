# Current State

> Last updated: see git log for the file's history.

## Done

- Module name: `github.com/garamsh/gaon`.
- GRN value type with `New` / `Parse` / `Validate` / `String` /
  `IsZero` and tests. Org/project segments identify owning resources (ADR-0001).
- `errorx` package with gRPC-aligned kinds, `New` / `Wrap` / `IsKind`
  / `KindOf`, and tests.
- Domain interfaces declared: `org.Store`, `user.Store`,
  `agent.Store`, plus aggregate types `Organization`, `Group`,
  `Project`, `User`, `Agent`.
- `authz` engine: IAM-style `Statement` model and native
  `StatementEvaluator` (ADR-0002), served over gRPC (ADR-0003).
  Legacy interfaces (`Subject`, `Object`, `Checker`, `Manager`,
  `RoleStore`, `PolicyStore`) remain but are unused — removal tracked
  in issue #28.
- Mockery v3 setup (`.mockery.yaml`) generating Store mocks.
- Documentation under `docs/`.
- CI plumbing: `Makefile`, `.github/workflows/ci.yml`. Git hooks
  (lefthook) evaluated and not adopted.
- Foundation landed on `main` (PR #15); `develop` retired. Work now
  branches off `main`.
- **MVP** (roadmap, issue #12) — Google OIDC sign-in via
  `internal/authn` (provider abstraction + server-side sessions),
  invitation-based orgs with roles (`owner` / `admin` / `member`)
  enforced through the `authz.Evaluator` seam (in-process native
  evaluator, ADR-0003 stage 1), OpenAPI-first API (`api/openapi.yaml` +
  generated server), sqlc-backed Postgres stores for `user`, `org`,
  and `authn` sessions, migrations, cobra CLI (`serve api` /
  `serve authz` / `migrate`), and e2e store round-trips against real
  Postgres. `make check-gen` keeps generated code (oapi-codegen, sqlc,
  mockery) honest in CI. The `agent` domain stayed untouched.

- **authz gRPC transport** (ADR-0003 stage 2, issue #22) — `gaon serve
  authz` runs the engine as a separate gRPC process (`Check` /
  `Explain` in `proto/gaon/authz/v1`, buf codegen checked by
  `make check-gen`); the api server authorizes through the gRPC client
  behind its own `Evaluator` seam and no longer imports
  `internal/authz`. Statement patterns gained full wildcard semantics
  (`*` and `?` anywhere in the pattern).

## Open

- **Dead authz code removal** (#28) — legacy interfaces and their
  mocks have no implementations or references.
- **Membership duplication + org.Patch cleanup** (#23).
- **authz persistence & policy versioning** (P2) — policies are
  caller-supplied today; Postgres `policies` + append-only
  `policy_versions` (ADR-0002) not started.
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

- Configurable dev-DB port for parallel worktrees (#33).