# ADR 0138: A caller of the policy service presents a short-lived signed token, and owners write entities through a sequenced RPC

> Status: accepted
> Date: 2026-10-31

Drafted for #1689, which reserves this number and holds the rulings
this records. It adds to
[ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
§§7, 8 and 10 and supersedes none of them. garam is read at
`7a3dd27e4d54b937cd0422daec1fb56c0d625356`; a claim below that names
no revision holds for it.

## Context

- **ADR-0130 §10 needs a token nothing issues.** `policy/` verifies
  the tokens the platform issues "against keys it is configured with",
  and calls no service. Every machine token the platform issues is
  opaque: `platform/internal/operator/token.go` mints random bytes, and
  the token is stored by hash and read on every request. No key can
  verify one, and no signer of a token exists in `platform/`, `lib/` or
  `cowork/`. `jose` there parses keys and verifies client assertions.
- **ADR-0129's principal does not reach it.** That token's audience is
  the machine listener alone, and it is unbuilt (#1664).
- **ADR-0130 §7 names a mechanism and no surface.** Owners write through
  a transactional outbox, and §10 says the feeds reach the service "as
  writes its owners make". `structure.md` lets no service write
  another's table, and `garam.policy.v1` holds only `Evaluate` and
  `Evaluations`, so such a write had nowhere to arrive. A feed also
  redelivers and reorders, and nothing said what that does.

## Decision

**A caller presents a short-lived JWT signed by the platform, with the
audience `policy`, verified offline against a configured JWKS file.
An owner writes through `ApplyEntityChanges`, whose per-entity sequence
makes a replayed or reordered delivery a no-op.**

1. **The token.** A compact JWS under ADR-0102's profile. Its protected
   header is `alg`, `kid` and `typ` and nothing else, with `alg` RS256
   and `typ` `JWT`, so an unknown `crit` or a `jku` refuses the token.
   `iss` is the configured issuer, `aud` is the one string `policy` and
   never an array, `sub` names someone, and `iat` and `exp` are integer
   dates. `nbf` is optional, checked, and no later than `exp`. A
   duplicate member refuses the token.
2. **Its lifetime.** `0 < exp - iat <= 300` seconds, checked apart from
   skew. Dates allow ADR-0102's 30 seconds of skew, and an `iat` ahead
   of that is refused, or a token could state a short lifetime and
   outlive it.
3. **The keys.** One issuer and one JWKS file, named by configuration.
   Each key is `kty` RSA, `use` sig and `alg` RS256, names a `kid`,
   which is how a token finds it, and has a modulus of 2048 to 4096
   bits. A member the profile does not name, a private one above all,
   refuses the file. It holds one or two keys, ADR-0129's rotation: add
   the successor, move the issuer, remove the old one. A file breaking
   this stops the service at start.
4. **Keys load at start,** so a rotation is a rollout. A compromised
   key is removed by rolling the service.
5. **`ApplyEntityChanges`.** A batch of upserts and deletes, each
   carrying the owner's sequence, monotonic per entity, and fed by the
   owner's transactional outbox. A change at or below the stored
   sequence changes nothing. A deleted entity keeps its sequence, so an
   earlier write delivered late does not revive it. An entity's type
   must belong to the caller (ADR-0130 §8).
6. **An entity states `org`, `members` and its parents and nothing
   else,** which is ADR-0130's rule that entities carry containment and
   membership. Each is validated against the schema on write.
7. **Until a caller can be shown to own a type, none supplies an
   entity:** `Resource.properties` is refused, and so is a `context`
   holding a field, the schema declaring none.
8. **A membership ends when its group does.** A slice drops every
   parent with no live row, whatever an owner's outbox delivers next,
   because Cedar honours a parent that has no entity.
9. **Every write names its organization.** A policy's GRN sits under
   its organization, and a link or guardrail naming another
   organization's entity is refused. A user is nobody's entity: it may
   belong to several organizations, and tenant isolation decides each.
10. **`Evaluations` never answers nothing.** An absent or empty list is
    the one question the request's defaults state, as in AuthZEN 1.0
    §7.1, since a caller folding answers reads none as nothing denied.
    A question at fault is denied in its own answer with the fault as
    its reason, and the rest are answered. A store failure fails the
    call. A request holds at most 1000 evaluations.
11. **`policy migrate` applies the migrations and exits, and `policy
    serve` applies none,** as `deployment-contract.md` §The migrate job
    states it for the platform. A dirty database refuses the run.

The issuer lands in slice 4 of #1689, and the RPC in slice 3. Slice 2a
builds the verifier against a key its tests generate, and the write as
a library call with item 5's semantics.

## Consequences

- Revoking a service's token lags by up to five minutes plus skew. A
  caller is a service, not a person, and every decision still reads its
  subject from the store, so a revoked person loses authority as soon
  as the store says so.
- `platform/` gains a signer and a key it holds, which it does not have
  today, and that key is a credential to every decision the service
  makes for a caller.
- A verifier needs no call and no table, so `policy/` stays below the
  platform and answers while the platform is down.
- A compromise of a key is contained by a rollout rather than by a file
  write, which is slower than ADR-0129's read per exchange.
- A guardrail is at most 4 KiB and 12 levels deep, measured against
  the stack a request runs on, since an overflow aborts the process
  rather than failing the request.
- Every owner keeps a per-entity counter, and the store keeps a row for
  each deleted entity.
- One caller identity authenticates, and the service does not yet
  decide what that caller may ask, so nothing in slice 2a separates one
  service from another. That is owed to slice 3 with per-type
  ownership.
- A transport carrying the bearer token is plaintext until slice 2b
  gives the server a certificate. It lands before anything calls the
  service, ADR-0125 ruling out network position.

## Signs the decision was wrong

- A key has to be withdrawn faster than a rollout, so reading the file
  per request is proposed.
- A second issuer is needed, which item 3's one file cannot state.
- Five minutes costs an issuer enough tokens to be measured, or a
  revocation lag of that length is what an incident report blames.
- Owners cannot keep a monotonic per-entity sequence, because the same
  entity is written from two places.
- Tombstones outgrow the live rows.

## Alternatives rejected

- **Introspecting the platform's opaque tokens.** It revokes at once,
  and it reverses ADR-0130 §10: `policy/` would call a service, and
  decisions would stop when the platform does.
- **Reading the platform's token table.** `structure.md` forbids it.
- **The owners writing `policy/`'s tables directly.** The same rule.
- **`policy/` consuming each owner's outbox.** That is a call from
  `policy/` to a service, which ADR-0130 §10 rules out.
- **A last-writer-wins upsert with no sequence.** A redelivered older
  change would undo a newer one, and a membership would come back after
  it was removed.
- **Failing the whole `Evaluations` call on one bad entry.** One
  malformed object would deny a caller the whole page it was filtering.
- **Reading the JWKS file on every verification.** It would match
  ADR-0129's "no restart" and parse keys on the hot path of every
  decision, for a rotation that happens rarely.
- **`ES256` or `EdDSA`.** ADR-0102's profile and the keys the deployment
  already handles are RSA, and a second key type buys nothing here.
