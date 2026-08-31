# ADR 0038: The engine learns a subject's memberships and enforces its own scope

> Status: accepted
> Date: 2026-08-19

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

[ADR-0025](./0025-engine-scope-is-a-caller-obligation.md) settled the
contract half — a caller may only ask the engine about a subject
belonging to the organization that owns the object — and deferred the
engine-membership half as the durable alternative. [ADR-0035](./0035-a-cross-organization-message-needs-both-organizations-consent.md)
closed the second window the deferral left open: cross-organization
messaging stands on a consent relation, and the API server is the one
that asks the engine. With #245 settled, the durable fix is the next
step [ADR-0025](./0025-engine-scope-is-a-caller-obligation.md)'s decision
explicitly held open.

The engine cannot hold the boundary today without a relationship it
holds in its own hand — and the only one it holds is the one
[ADR-0007](./0007-hybrid-statement-collection.md) gave it: subject_groups.
The membership lives in the org tables the API server reads for
role-derived statements, and the engine is deliberately kept
domain-free ([ADR-0003](./0003-single-cli-topology.md)). A subject's
organization membership is the same kind of fact — and the engine
cannot reach it.

## Decision

**The engine receives a subject's organization membership alongside its
group membership, and applies the org scope itself.**

- The eval input gains `subject_org` (the GRN of the organization the
  subject belongs to, when the subject is an org member), the same
  shape `subject_groups` already has.
- The engine refuses the call when the object's organization is not the
  subject's organization and `subject_org` is unset — the boundary the
  obligation demands, evaluated where the obligation lives.
- The API server resolves `subject_org` the way it resolves
  `subject_groups`: from the org tables it already reads. The wiring is
  the same one, one more field.

The scope check moves into the engine; the engine no longer depends
on the caller to hold the boundary on its behalf.

## Consequences

- A caller that cannot establish membership refuses before asking the
  engine — the API server's precondition for every cross-organization
  path stays; an unauthenticated path that reaches the engine refuses
  there instead. ADR-0025's obligation moves, and the engine becomes
  the place it is enforced.
- The engine's unit test `TestOrgIsolation_OnlyAttachmentKeepsGrantsInsideAnOrg`
  ([#296](./../adr/0008-identity-based-policy-attachments.md)'s follow-up)
  is now a property the engine tests in isolation, rather than the
  cross-package fixture the API relied on to catch a second instance of
  #265. The fixture was added because the engine did not hold the
  boundary; the boundary is now where it belongs.
- A new EvalInput field is the only schema change. The wire format
  carries it, the API server populates it, the engine reads it.
  `docs/architecture/authz.md` records it; [ADR-0025](./0025-engine-scope-is-a-caller-obligation.md)'s
  body becomes frozen with a status pointer here.

## Rejected alternatives

- **Keep the engine free and rely on the API server's precondition.**
  The current state. #265 is the evidence it is not enough: one
  unattached Allow in the recipient's organization admitted every sender
  anywhere because the caller could not hold the boundary, and a
  second caller (`machineapi/message.go`) was written without knowing
  it had to. The rule is enforceable, and the engine is the place.
- **Give the engine a connection to the org tables.** Couples the
  engine to the same boundary [ADR-0007](./0007-hybrid-statement-collection.md)
  was drawn to keep. The membership is the API server's fact; sending
  it across is what the current `subject_groups` flow already does.

## Errata

- 2026-08-31 — §Consequences' "The engine's unit test
  `TestOrgIsolation_OnlyAttachmentKeepsGrantsInsideAnOrg` … is now a
  property the engine tests in isolation" names a function that no
  longer exists. It was true when written and is false since
  `1d67d5c` (#411), the pull request that implemented this ADR: that
  commit deleted the function from `tests/e2e/postgres_test.go`
  nineteen minutes after `01395db` (#409) merged this ADR, and added
  `TestGRPCServer_RefusesForeignSubjectWhenSubjectOrgUnset` to
  `internal/authz/server_test.go` in its place. What the bullet
  asserts is what that commit did — the property is one the engine
  tests in isolation, in the engine's own package, under that name —
  so what is stale is the name and not the claim. The decision is
  untouched.
