# ADR 0003: Invitations are token-keyed, not GRN-addressed

> Status: accepted
> Date: 2026-07-25

## Context

"One identifier system across every resource (GRN)" suggests
invitations should be GRN-addressed. But the GRN service_type
catalogue (`docs/convention/naming.md`, PM-owned) has no `invitation`
entry, and an invitation is not a resource clients address — it is a
capability: a single-use secret passed out of band.

## Decision

An `org.Invitation` is keyed by its token: a 192-bit random secret,
hex-encoded so it satisfies the GRN identifier alphabet should a
future catalogue entry make invitations GRN-addressed. The token
carries the org, role, expiry, and single-use semantics; accepting
means presenting the token while signed in.

## Consequences

- No convention change was required from the worker side; if the PM
  adds `invitation` to the service_type catalogue, tokens already fit
  the identifier rules.
- Tokens are unguessable, so possession is authorization to accept —
  matching the invite-link mental model.
- Revoking a pending invitation means deleting or expiring the row;
  there is no stable public identifier to reference it by.
