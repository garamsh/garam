# ADR 0002: IAM-style policy documents with a native evaluator

> Status: accepted
> Date: 2026-07-25

## Context

Policies are managed per org, must be versioned and auditable, and are
enforced on every org-scoped request. GRN already plays the ARN role,
which makes the AWS IAM policy model a natural fit. An earlier
direction compiled Rego (OPA) sources per org; that WIP is preserved
but unlanded. IAM-style statement matching is simpler, deterministic,
and has no runtime dependency.

## Decision

- A policy is an IAM-style document: `Version` plus a `Statement`
  list of `{ Sid, Effect, Action[], Resource[], Condition? }`.
- `Resource` entries are GRN patterns (`grn:acme:*`); `Action` entries
  are `<service_type>:<verb>` strings with wildcard support
  (`org:*`, `*`).
- Evaluation order: explicit Deny → otherwise any Allow → default
  Deny. Deterministic and explainable per statement.
- The evaluator is native Go in `internal/authz`; no OPA in the hot
  path. Rego is reconsidered only if `Condition` expressiveness
  outgrows the model.
- Policies are stored in Postgres: `policies` (metadata, status,
  default version pointer) + append-only `policy_versions`. Rollback
  moves the pointer.
- Identity-based attachment first (policies attach to users/groups).
  Resource-based policies are deferred.

## Consequences

- The policy engine has no external dependency in the request path.
- The preserved Rego WIP (`~/garam-authz-wip/`) becomes reference
  material, not a landing candidate in its current shape.
- The policy document JSON schema must be documented in
  `docs/architecture/authz.md` and validated at write time.
- Per-org evaluation cache holds parsed statement sets, invalidated
  on policy change (see ADR-0003 for the service split).
