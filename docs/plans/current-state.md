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
- Mockery v3 setup generating **importable** mocks (PR #62).
- Documentation under `docs/`.
- CI plumbing: `Makefile` with working format/check-gen gates (PR #71),
  `.github/workflows/ci.yml`. Git hooks (lefthook) not adopted.
- Foundation landed on `main` (PR #15); `develop` retired. Work now
  branches off `main`.
- **MVP** (issue #12) — Google OIDC sign-in, invitation-based orgs,
  org roles enforced through the `authz.Evaluator` seam, OpenAPI-first
  API, sqlc-backed Postgres stores, cobra CLI (`serve api` /
  `serve authz` / `migrate`).
- **authz gRPC transport** (ADR-0003 stage 2) — `gaon serve authz`
  runs the engine as a separate gRPC process; the api server never
  imports `internal/authz`.
- **Group/project structure** (#54, #63) — CRUD + membership over the
  OpenAPI API, org-scoped listing, spec-first.
- **Scoped permission evaluation** (#55, #72, #78) — group and project
  roles resolve at check time via `scopedStatements`; roles are
  assignable over HTTP; deny-overrides holds across scopes.
- **User direct roles** (#75, #82) — collected at check time
  (org-wide scope; per-org direct roles need role persistence, future).
- **authz policy persistence** (P2, #56, #79) — `policies` +
  append-only `policy_versions` (trigger-enforced), `PolicyService`
  lifecycle RPCs, per-org `PolicyCache` with `LISTEN`/`NOTIFY`
  invalidation, hybrid statement collection (ADR-0007). e2e 10/10
  against Postgres 17.

## Open

- **Dead code cleanup** (#76) — in flight.
- **Policy attachment to subjects** — an active policy grants org-wide
  until identity-based attachment lands (ADR-0002, ADR-0007).
- **Per-org direct roles** — direct user roles currently apply in
  every org the subject belongs to; scoping them needs subject × role
  × scope persistence.
- **Workflow core** (P3) — domain not created.

## Known corrections applied

- Module path renamed `garam` → `gaon`; all imports updated.
- Rego direction superseded by IAM-style documents (ADR-0002); the
  legacy authz interfaces were removed (#28, #40, #47, #59).
- e2e moved to `tests/e2e/` with a root-relative migrations path
  (#53, #58, #64, #70).

## Backlog

Pulled from `// TODO` lines and review notes. Each item gets its own
PLAN.md when picked up.

- Nothing open beyond the issues above.
