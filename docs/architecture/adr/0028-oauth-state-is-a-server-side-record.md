# ADR 0028: A login's state is a server-side record the callback spends

> Status: accepted
> Date: 2026-08-17

## Context

The OAuth callback compared the state in the query to the state in the
cookie and then expired both cookies. Expiring a cookie is an
instruction to the browser: it binds a browser that obeys it, and binds
nobody who captured the request. Nothing on the server recorded that a
state had been used, so a callback replayed with the same cookies was
accepted again and issued a second session.

Sessions had already been moved server-side for the same reason — a
cookie the server cannot revoke is not a control (ADR-0005) — while the
flow that issues them had not.

## Decision

A login records the state it issues, and the callback that completes
spends it. The record lives in `internal/authn` beside sessions, as a
row holding the state's hash and the moment it stops being live;
nothing else, because nothing else is decided from it.

- Its lifetime is the flow's: the same value that bounds the state
  cookie bounds the row, so the state and the record it is checked
  against expire together.
- Spending it is one delete, so two callbacks racing on one state leave
  one session between them.
- A callback that finds no live record is refused as unauthenticated,
  with a message of its own — a state never issued, a state whose flow
  ran out, and a state already spent are one answer, since the sender
  gains nothing from being told which.
- The state is spent where the session is about to exist, not where the
  request arrives. Everything that can fail without issuing anything
  runs first, so a refused attempt leaves the state live for the
  browser that owns it — the ordering an invitation already uses, where
  a refused membership leaves the invitation unburned (ADR-0006).
- Issuing deletes the records that have expired, so the table holds the
  logins in flight rather than every login ever started.

## Consequences

- Starting a login writes a row, on an endpoint no credential guards.
  The row is a hash and a timestamp, and each issue prunes, so the
  table is bounded by the logins started within one flow lifetime.
- A leaked database holds no state a callback could be built around,
  for the reason ADR-0005 stores only session token hashes.
- What garam refuses here is a state. The authorization code is refused
  by the provider, which must deny a second exchange of the same code
  (RFC 6749 §4.1.2), so a callback replayed against Google fails at the
  exchange before it reaches the record. The record is what holds when
  a provider does not — the dev provider (ADR-0010) accepts its
  constant code every time.
- A sender who substitutes a state of their own is refused for want of
  a record rather than by the cookie comparison, which they control
  both sides of.
- Recording only the states already spent, written after a callback
  succeeds, was rejected. It costs a write per sign-in instead of one
  per login started, but the marker's lifetime and the state's are then
  set apart from each other: the marker expires while the state — whose
  lifetime is only an instruction to the browser — is still accepted,
  which reopens the window the marker was added to close. Making the
  record the state itself is what keeps the two from coming apart.
