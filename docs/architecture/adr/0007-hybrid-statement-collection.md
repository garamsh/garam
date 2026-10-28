# ADR 0007: Hybrid statement collection

> Status: accepted; callers collecting role statements is superseded by
> [ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
> Date: 2026-08-02

## Context

[ADR-0002](./0002-iam-style-policy-model.md) makes policies org-managed
data stored in the engine. The P2 reading of it was that the api server
would ask `Check` with subject, object, and action only, moving all
statement collection engine-side.

Role-derived grants do not live in the engine. Org membership roles,
and the group and project roles resolved at check time, live in the
`org` domain's tables. Collecting them engine-side means either
duplicating role state into the authz database on every membership
change, or having the engine read another domain's tables — and
[ADR-0003](./0003-single-cli-topology.md) makes the engine
domain-free.

## Decision

The engine evaluates the union of two statement sources:

- **Persisted policies** — the engine owns their storage, versioning,
  cache, and lifecycle RPCs. It resolves them from the object's org.
- **Caller-supplied statements** — the api server keeps collecting
  role-derived grants and sends them on the `Check` / `Explain`
  request.

Deny-overrides applies across the union: either source alone can allow,
and a Deny from either source wins. `CheckRequest.statements` therefore
stays on the wire; the P2 slim-`Check` goal is relaxed, not deferred.

The engine learns exactly one fact about the domain model: an org's
identifier is the organization segment of every resource beneath it
(ADR-0001), which is how a request scopes to the policies that apply.

## Consequences

- Org tables stay the single source of truth for role state; there is
  no sync point between membership changes and the authz database.
- The `Check` contract is unchanged, so this lands without a migration
  for existing callers.
- Policy attachment to individual subjects is not needed yet: a
  persisted policy applies to its whole org. Identity-based attachment
  (ADR-0002) stays future work, and until it lands an active policy is
  an org-wide grant.
- The engine's evaluation input has two origins, so `Explain` must
  trace both to stay reproducible.
