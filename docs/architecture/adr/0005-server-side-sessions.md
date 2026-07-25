# ADR 0005: Sessions are server-side rows keyed by token hash

> Status: accepted
> Date: 2026-07-25

## Context

After OAuth sign-in the API needs session state. Stateless signed
cookies cannot be revoked server-side; the spec includes a logout
endpoint that must actually end the session.

## Decision

Sessions are Postgres rows (`internal/authn`). The bearer token is a
random 256-bit value in an HttpOnly cookie; only its SHA-256 hash is
persisted, alongside the user GRN and expiry. Logout deletes the row.
Expired sessions read as absent.

## Consequences

- Logout and (later) admin revocation work by deleting rows.
- A database leak does not expose usable tokens — only hashes.
- Every authenticated request costs one indexed lookup; acceptable at
  MVP scale.
- The `SessionStore` interface leaves room for a cache or a different
  backend without touching handlers.
