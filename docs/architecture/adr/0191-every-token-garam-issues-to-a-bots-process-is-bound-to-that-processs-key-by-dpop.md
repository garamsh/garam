# ADR 0191: Every token garam issues to a bot's process is bound to that process's key by DPoP

> Status: accepted
> Date: 2026-11-28

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2078, for which the Garam PM reserved this number. The
owner's direction is `garamsh/discussions` discussion 6, comment
19863377: a bot's token is bound to its process's key, so a stolen
token is useless. The design is garam-bot's
`docs/architecture/bot-system.md` §Authentication at
`cb7c5b676c9549170168cf241057b0c1d01df29a` and garam-bot ADR 0037.

## Context

[ADR-0189](./0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)
decision 2 has a bot's place's connector exchange its own token for
the bot's, by RFC 8693, at the machine listener's token endpoint
(#2049). The token it answers, `garam_bt_`, is a bearer token: whoever
holds it can use it for its hour, wherever it was copied to. A bot runs
where garam controls nothing: a cluster, a person's PC, a naru host.

A token can be bound to a key in two ways. mTLS binding (RFC 8705)
needs the TLS connection to end at the party that checks the token,
and a place does not control where TLS ends between it and garam. A
DPoP proof (RFC 9449) is a JWT signed per request and carried in a
header, so it passes any proxy.

## Decision

1. **Every token garam issues to a bot's process is a DPoP token
   (RFC 9449).** It is answered with `token_type` `DPoP`, and garam
   keeps the RFC 7638 thumbprint of the key it is bound to as the
   token's `cnf.jkt`. Today that is the bot token the exchange issues.
   The model and tool tokens garam is to issue to bots, ai-gateway's
   (ADR-0190) and those of the apps garam brokers, are bound the same
   way when they are built (#2027). A token garam does not issue, such
   as a credential a tool names by reference, is outside this.
2. **The key is the bot's process's own.** The process makes an ES256
   key when it starts, and the key never leaves it: not to a file, a
   Secret, the connector or a workspace. A new process makes a new
   key and needs a new exchange. The connector sends, beside its own
   token as the `actor_token`, a proof the bot's process signed for
   the token endpoint, so the connector never holds the bot's key.
3. **A proof is a compact JWS in the `DPoP` header, one per request.**
   Its header is `typ` `dpop+jwt`, `alg` `ES256` and `jwk` the public
   key, with no private member. Its claims are:
   - `htm`, the request's method;
   - `htu`, the request's URL without its query and fragment;
   - `iat`, within 60 seconds of garam's clock either way;
   - `jti`, unique;
   - `nonce`, at the token endpoint;
   - `ath`, the base64url SHA-256 of the access token, wherever a
     token is sent with it.
4. **A proof is used once.** Each `jti` is spent for its key in
   Postgres for twice the `iat` window, so a replay reaching another
   replica, or arriving after a restart, is refused. It is spent only
   once the rest of the request authenticated, at the token endpoint
   once the actor token resolved, so a caller garam does not know
   writes no `jti`.
5. **The token endpoint always requires a server nonce** (RFC 9449
   §8). Time is cut into five-minute periods, and each has one nonce:
   the first replica to answer in a period makes it at random and
   keeps it in Postgres, and every replica answers it and accepts it
   in its period and the next. A proof without a nonce, or with one
   that is not the current or the previous period's, is answered 400
   `use_dpop_nonce` with the current one in `DPoP-Nonce`, and the
   client retries with it. Every answer of the bot exchange carries
   the current nonce. Postgres holds at most two nonces, however many
   requests arrive, and no key is configured for them.
6. **Every resource server garam runs verifies the proof and the
   binding** before it acts on a bound token:
   - the token arrives as `Authorization: DPoP <token>`, and one
     presented as `Bearer` is refused;
   - the proof verifies as decision 3 states, with `ath` matching the
     token;
   - the proof's key's thumbprint is the token's `cnf.jkt`;
   - its `jti` is spent as decision 4 states.

   No garam route accepts a bot token yet. This decision binds the
   first one that does, and the verification it calls is built with
   this one.

## Consequences

- A copied bot token is useless without the bot process's key, and a
  copied proof is useless after its one use, or past its minute.
- The exchange costs a connector one more round trip: the first
  request of a process learns a nonce.
- Postgres holds a row per spent proof for two minutes and at most
  two nonces, and the next write sweeps what expired. A request that
  authenticates nothing writes no row but, at most, the period's
  nonce.
- garam-bot#1207 has to carry a proof from the bot's process to its
  connector at each kind of place, which this decision does not
  build.
- A bound token never enters a workspace, since a harness cannot sign
  with the bot process's key.
- **Deploy order.** Once the migration that binds the token runs and
  a binary that requires the proof serves the exchange, a connector
  that exchanges without a proof is refused `invalid_dpop_proof`, and
  every bot token minted before is deleted. No garam route accepts a
  bot token yet, so the exchange is the only thing that stops
  working, until a connector carrying garam-bot#1207's proof is
  deployed. An environment where anything depends on the exchange
  succeeding deploys that connector first.

## Signs the decision was wrong

- A place cannot get a proof from its bot's process to its connector
  in time, so the binding has to be relaxed for that place.
- A proxy between a bot and garam rewrites the URL a proof names, so
  `htu` fails for a request that was not tampered with.
- The nonce round trip or the per-proof write becomes the token
  endpoint's bottleneck.
