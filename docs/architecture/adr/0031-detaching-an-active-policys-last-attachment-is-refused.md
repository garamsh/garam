# ADR 0031: Detaching an active policy's last attachment is refused

> Status: accepted — archiving, as the remedy the refusal names and as
> a state whose last attachment detaches freely, is superseded by
> [ADR-0117](./0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md): deleting the policy is the remedy
> Date: 2026-08-18

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0008](./0008-identity-based-policy-attachments.md) gave an active
policy with no attachment an org-wide reach, for continuity: that is
the behavior [ADR-0007](./0007-hybrid-statement-collection.md) shipped,
before attachment existed. Detaching was assumed to be attach's
symmetric inverse, so removing a policy's last attachment returned it
to that state.

Two things made that assumption unsafe. ADR-0024 let a policy decide
what an agent may send on the machine surface, so a policy's reach
stopped being theoretical. And under deny-overrides the widening
inverts the operator's intent (#259): detaching a Deny from the one
subject it was written for — "stop restricting this agent" — applies
that Deny to every subject the caller asks about. The act of loosening
one subject tightened everyone else's, silently.

ADR-0008's continuity reason is about the *state*: a policy created
and activated without attachments must keep meaning org-wide. Nothing
in it argues the *detach transition* must be a path into that state.

## Decision

Detaching an **active** policy's last attachment is refused with
`failed_precondition` (HTTP 409 on the browser API). The refusal's
message names the deliberate acts: archive the policy to retire it, or
attach the next subject before detaching this one.

- The org-wide state itself is unchanged: an active policy created
  without attachments still applies org-wide. Entering that state
  stays possible; it is never a side effect of removing an attachment.
- A draft or archived policy's last attachment detaches freely —
  nothing evaluates either.
- The check runs where the write happens, in a transaction holding the
  policy's row lock, so two concurrent detaches of a policy's last two
  attachments serialize and the second is the one refused. A refused
  detach writes nothing and emits no change notification.

## Consequences

- The transition that inverted operator intent no longer exists; both
  remedies are acts whose names say what they do.
- `DetachPolicy`'s contract — proto, OpenAPI, and the generated
  clients — states the refusal, so a caller learns it before meeting
  it.
- The engine's evaluation model is untouched: attachment still
  narrows, its absence still means org-wide. Only a lifecycle
  transition is guarded.

## Rejected alternatives

- **Effect-dependent rules** (refuse only for a Deny). The model has
  no "a Deny policy": `Effect` is per-statement and a document may mix
  both (ADR-0002), so the rule would invent a concept the model does
  not have — and would leave Allow widening in place, which hands a
  grant to every subject a caller asks about.
- **Archive instead of refusing.** One RPC would change two things —
  the attachment set and the status — and a detach-then-reattach
  sequence would retire the policy in between. That exchanges a loud
  surprise for a silent one; a refusal is reversible and keeps each
  RPC doing one thing.
- **Document the widening and keep it.** The behaviour was documented
  from the start and surprised anyway, because the surprise is in the
  gap between intent and effect, not in anyone's ignorance of the
  rule.
