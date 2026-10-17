# ADR 0030: The login write rate is bounded at the store

> Status: accepted
> Date: 2026-08-18

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

ADR-0028 made a login's state a server-side record, and with it made
`Login` a handler that writes: one row in `oauth_states` per request,
on an endpoint that is unauthenticated by definition. The record's
lifetime and the prune on every issue bound the table's steady-state
size to the logins started within one flow lifetime. What nothing
bounded was the write rate itself: an unauthenticated caller could
drive one database write per request, as fast as the requests arrive
(#339).

Two facts about the deployment decide where a bound can stand.

- **The endpoint is served by however many replicas run.** A bound a
  process holds in memory multiplies by the replica count, which the
  code does not know; the database is the one place every replica's
  writes meet.
- **An unauthenticated request owes no trustworthy identity.** A
  bound keyed on client address reads `X-Forwarded-For` behind a
  proxy, and a caller who sets that header themselves gets one budget
  per value they invent — no bound at all. Keying on the peer address
  instead ties the answer to where the proxy sits and gives every
  NATted office one budget between them.

## Decision

Issuing a state refuses once `MaxLiveOAuthStates` (1024) states are
live. The refusal is decided in the statement that writes:
`IssueOAuthState` inserts only while the live count is under the
bound, and the store answers `ErrOAuthStateRateLimited`
(`KindResourceExhausted`, which the HTTP surface maps to 429) when it
inserted nothing. Because a state lives at most one flow lifetime, the
bound on live rows is a bound on the write rate: at most
`MaxLiveOAuthStates` attacker-driven writes per flow lifetime, for the
whole deployment rather than per process.

- The count and the insert are one statement, so the check and the
  write cannot come apart between processes.
- The prune ADR-0028 gave the statement still runs on a refused
  issue, and spending a state frees a slot, so a full table drains:
  shedding load cannot wedge it.
- The bound is this handler's alone, because this handler is the
  unauthenticated surface's only unconditional write. `ListProviders`,
  the probes, and `/metrics` write nothing; the callback writes only
  after the provider vouches for the code, which an unauthenticated
  caller cannot drive; logout deletes by a session token, which the
  caller must already hold — and holding one makes them authenticated.

## Consequences

- The write rate an unauthenticated caller can cause on
  `/auth/login/{provider}` is bounded at 1024 rows per flow lifetime
  (ten minutes), however fast the requests arrive and however many
  replicas serve them.
- A caller who fills the table refuses legitimate logins with it for
  up to one flow lifetime. That is the trade a shared bound makes:
  under active attack the endpoint sheds everyone rather than the
  database absorbing the writes. Recovery needs no operator — spent
  and lapsed states free slots as they go.
- The shape generalizes to any future unauthenticated write path:
  bound the live rows where the write happens, in the statement that
  writes. Extending it to the authenticated API is not done here; the
  authenticated surface is a different question (#339 scopes it out).

## Rejected alternatives

- **A per-client-address token bucket.** There is no address an
  unauthenticated caller cannot choose: the forwarded header is
  caller-controlled, and the peer address punishes every client
  behind one NAT while sparing a caller with many sources. The bucket
  map is also itself an unauthenticated write per request — one in
  memory instead of in the database.
- **A process-wide token bucket.** The bound would multiply by the
  replica count, a fact the code does not hold, and would shed the
  whole deployment's login rate onto whichever replica the load
  balancer favours.
- **Dropping the server-side record.** That record is the fix for the
  replayed callback (#279, ADR-0028); reverting it reopens the replay.

## Errata

- 2026-08-27 — §Decision's "The bound is this handler's alone, because
  this handler is the unauthenticated surface's only unconditional
  write" was true when written and is false since #499, which merged
  [ADR-0041](./0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
  together with the endpoints it decides. `api/openapi.yaml` carries
  `security: []` on five of its operations today, two of them writes
  by verb: `POST /auth/password/register` and `POST
  /auth/password/login`. Password login is the second unconditional
  write — `internal/api/password.go` records an attempt before a
  credential is looked up and before anything is hashed — so this
  handler is no longer the only one. Registration does not make a
  third: it writes only once the invitation is known live, and a live
  token is what an unauthenticated caller does not hold (ADR-0006).
  ADR-0041
  §"The rate bound is extended, because ADR-0030 does not reach here"
  quotes this sentence and gives the attempt a bound of its own,
  `MaxLivePasswordAttempts`, decided in the statement that
  writes — the shape §Consequences generalizes, applied to the write
  ADR-0041 adds. The decision here is untouched: the login write rate
  is bounded at the store, and `MaxLiveOAuthStates` bounds
  `/auth/login/{provider}`. Whether one bound belongs to an
  unauthenticated surface holding two unconditional writes is not
  decided by this entry.
