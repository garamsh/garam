# ADR 0101: garam signs every message it delivers, and the agent verifies it alone

> Status: accepted
> Date: 2026-10-09

Drafted for #1252, which reserved this number and records the owner's
decision of 2026-10-09; the owner accepted it the same day (#1256).
Facts about garam hold at `a7148f8`. Facts about Sherlock hold at
`garamsh/sherlock@08c32c2` (`08c32c227d17acb850858189b3b3cad9a7a7d178`),
the head of its `dev` when read; Sherlock's half of this is
garamsh/sherlock#993.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives
- Errata

## Context

What an agent learns today about who sent it a message, and what that
rests on:

- **garam authenticates the poster.** The machine listener admits a
  caller by the client certificate whose SAN holds its GRN
  ([api-surface.md](../api-surface.md) §What admits a caller,
  [ADR-0014](./0014-agent-authentication.md)), and `Post` stores that
  GRN as the message's sender (`internal/message/service.go`).
- **The adapter says who sent it, and nothing checks the claim.** It
  posts a claimed message into the runtime over loopback as
  `garam-message.v1`, whose `sender` member is the GRN garam's claim
  carried (`internal/delivery/envelope.go`,
  [ADR-0084](./0084-the-adapter-activates-its-generation-and-reports-runtime-status-under-a-contract-garam-owns.md)
  §Delivery under the activation).
- **Sherlock takes the sender from the transport.** `callerID` reads
  the verified mTLS peer certificate's subject and answers the empty
  string over plain-HTTP loopback, and a message ID is scoped to that
  principal (Sherlock `internal/gateway/handler.go`, `callerID`;
  `docs/architecture/gateway.md`, Ingress). Over the loopback hop the
  adapter uses, every message arrives under the empty principal, and
  the `sender` inside the content is a string Sherlock does not verify.

So whatever reaches Sherlock's ingress is believed: any process on the
pod's loopback interface, or any path besides the adapter, can post
content naming any sender. Trust in a message is trust in garam's
transport. The owner's decision removes that: Sherlock is to be usable
outside garam, holds the credentials it judges messages by, and
decides for itself whether a message is for it. The owner chose signed
messages over transport authentication at the agent's endpoint, which
proves who connected and not what was sent (#1252).

Facts the design has to fit:

- **A message is handed out more than once.** Delivery is
  at-least-once and each hand-out is an attempt
  ([ADR-0018](./0018-delivery-acknowledgement.md)); the lease ceiling
  is five minutes and the attempts three by default
  (`internal/message/service.go`, `DefaultLeaseCeiling`,
  `DefaultAttempts`), and an unclaimed message may wait thirty days
  ([ADR-0034](./0034-a-delivered-message-keeps-its-key-and-drops-its-body.md)).
- **A redelivery must reach the runtime as the same bytes.** Sherlock
  answers a repeated ID with identical content as `queued` or
  `completed`, and other content under it with 409 (Sherlock
  `internal/gateway/handler.go`, `postMessage`; `gateway.md`), and
  [adapter.md](../adapter.md) §Stable identifiers and the outbox rests
  replay on that.
- **Sherlock's ingress refuses any member besides `id` and `content`**
  (Sherlock `internal/gateway/handler.go`, `postMessageRequest` decoded
  with `DisallowUnknownFields`), so a signature cannot ride that body
  without a Sherlock change.
- **garam stores one kind of message it authored itself**: the
  cross-organization refusal, written under the intended recipient's
  identity because garam holds none of its own (`crossingRefusal` in
  `internal/machineapi/message.go`;
  [ADR-0035](./0035-a-cross-organization-message-needs-both-organizations-consent.md)).
- **Sherlock reaches only destinations its operator configured**
  (Sherlock `docs/architecture/adr/0013-egress-is-tool-routed.md`), so
  a key set it fetches is a configured destination, and a deployment
  that cannot reach one has to be able to hand it the set as a file.
- **garam already seals private keys** under a key-encryption key the
  process reads from a file
  ([ADR-0016](./0016-ca-key-custody.md),
  [certificate-authorities.md](../certificate-authorities.md)).
- **No garam process dials an agent**
  ([ADR-0012](./0012-agent-operator-model.md),
  [ADR-0013](./0013-agent-messaging.md)), so what garam signs reaches
  the agent through whatever delivers it.

## Decision

### The format

**A message's signature is a JWS in compact serialization (RFC 7515)
whose payload is a JWT claims set (RFC 7519), signed with ES256
(RFC 7518, "Digital Signature with ECDSA").** It travels beside the
message's content, never inside it, and binds the content by digest. A
stock JOSE library verifies it with no code of garam's.

- **The protected header holds `alg`, `kid` and `typ` and nothing
  else.** `alg` is `ES256`; `kid` is the key's JWK thumbprint
  (RFC 7638); `typ` is `agent-message+jwt`, so a token of another kind
  from the same issuer cannot pass as a message (RFC 8725, "Use
  Explicit Typing"). No `jku`, `x5u` or `jwk`: a verifier never takes a
  key, or a place to fetch one, from the message it is judging.
- **ES256 and only ES256.** RFC 7518's table of `alg` values for JWS
  marks it Recommended+, and every mainstream JOSE library implements
  it. One algorithm means a verifier pins it rather than negotiating
  one.

### What is signed

The signed bytes are the JWS Signing Input of RFC 7515 — the encoded
header, a dot, the encoded payload — and the content enters them as a
digest of its raw octets. That is neither a canonical form of the
content nor a detached signature over it. The payload is:

| Claim | Value |
|---|---|
| `iss` | the deployment's issuer: the machine listener's external `https` origin in one spelling — a lowercase host, no empty port, no path and no trailing slash — which the deployment configures and keeps for its life |
| `sub` | the sender garam vouches for: the GRN its listener authenticated as the poster, or `iss` itself for a message garam authored |
| `aud` | the recipient's GRN, as one string |
| `org` | the recipient's organization, the GRN's organization segment ([grn-model.md](../grn-model.md)) |
| `message_id` | garam's identifier for the message, the one it is delivered under |
| `content_sha256` | SHA-256 over the content's octets, base64url without padding |
| `iat` | when the claim was taken, on the database's clock |
| `exp` | `iat` plus the lease ceiling, five minutes at the default: when the claim's ceiling ends |

- **The content's octets are the UTF-8 encoding of the content string
  as received**, after the transport's own decoding — not a JSON text
  quoting it. No canonical form is needed, because the octets are what
  the verifier holds; for garam the string is `garam-message.v1`, whose
  encoding is already the same on every redelivery
  (adapter.md §Stable identifiers and the outbox).
- **garam produces the content as well as the signature.** The claim
  answer carries both, and the adapter forwards each byte for byte. A
  renewal hands nothing out and carries neither.
  The content keeps the form ADR-0084 states; what moves is which side
  encodes it, so the digest is taken over the bytes that are delivered
  rather than over a second encoding of the same members.
- **`aud` is the GRN the runtime runs as.** The renderers garam knows
  give the runtime's agent ID that GRN (ADR-0084, on the Operator's
  sidecar), so a verifier compares it with its own as strings.
- **`org` is bound and not required to be checked.** `aud` already
  carries it; it is there for an accept policy a verifier may keep, so
  a verifier that knows its organization and not its GRN can still
  refuse another tenant's message.
- **No `jti`.** Every attempt of one message names the same
  `message_id` under a new signature, and a library's token-replay
  cache keyed on `jti` would refuse the second attempt of a message
  whose first was lost. The replay key is `message_id`, which the
  verifier's own idempotency already answers.
- **`sub` is what garam vouches for, not what the content says.** The
  refusal garam writes under the intended recipient's identity keeps
  that stored sender in its content (ADR-0035, unchanged), and is
  signed with `sub` equal to `iss`, because no caller posted it. A
  verifier deciding who sent a message reads `sub`, under `iss`.

### Attempts and relaying

- **garam signs at each hand-out, never at storage.** Every claim signs
  afresh with that claim's `iat` and `exp`. The key is opened before
  the claim is taken, so a key garam cannot open refuses the claim and
  spends none of the message's attempts. The content,
  `message_id`, `sub`, `aud`, `org` and digest are the same in every
  attempt, so the runtime's replay check on identifier and content
  holds unchanged, while no signature outlives the claim it was issued
  with. A message that waits thirty days carries no signature that
  waited with it.
- **Nothing between garam and the verifier renews a signature.** The
  adapter holds no key. A message whose post to the runtime outlasts
  `exp` is refused there and comes back on the next claim, signed
  again — the path a lapsed lease already takes.
- **A relay forwards; it does not re-sign.** Whatever carries the
  message — the adapter's long-poll and loopback post (ADR-0013), a
  webhook, a queue — passes the content and the signature unaltered,
  and the verifier's answer does not depend on which carried it.

### Keys

- **The machine process signs**, because it answers the claim
  ([ADR-0017](./0017-machine-process.md)). Its keys are stored in
  Postgres sealed under the key-encryption key, as a certificate
  authority's key is (ADR-0016), so every replica signs with the same
  key and a database dump opens none.
- **These keys sign messages and nothing else.** Anything else garam
  signs uses keys of its own.
- **Publication.** The machine listener serves the public half of every
  published key as a JWK Set (RFC 7517, "JWK Set Format"), one key per
  entry with `kid`, `kty` `EC`, `crv` `P-256`, `alg` `ES256` and `use`
  `sig`, at
  `GET /message-signing-keys`, as `application/jwk-set+json`, with
  `Cache-Control: max-age=3600` and no credential required — the
  footing `enrollOperator` already has (`security: []`,
  `api/machine.yaml`). Its URL is the issuer followed by that path. The
  issuer carries no trailing slash and is compared byte for byte, so
  `https://x/` and `https://x` are two issuers and not one. The
  document is complete in itself, so a deployment whose verifier cannot
  reach garam supplies the same document to it as a file.
- **Rotation with overlap.** A published key is in one of three
  states: *next*, published and signing nothing; *current*, the one
  key signing; *retired*, signing nothing and still published. A next
  key is published for the lead §What a verifier checks promises
  before it signs. A retired key stays published until its last
  signature has expired, plus the skew allowance, plus one cache
  lifetime.
- **With no key signing, the key created signs at once.** That is a
  deployment's first key, and the replacement for one retired at once:
  there is no earlier set either could be missing from, and nothing
  else can sign meanwhile.
- **A next key retired before it begins was never announced.** The
  current key goes on signing past the retired key's start, and the
  replacement is announced with the full lead; nothing signs at once
  for it.
- **Rotation decides on the clock as it stands once the lock is held.**
  Every replica that finds a key due takes one lock, reads the keys
  and the database's clock after it holds it, and a key it stores
  signs from that instant plus its lead, so replicas rotating at once
  store one key between them and no lead is shortened by the wait.
- **Who rotates.** garam announces the next key on its own once the
  current one has signed for 90 days less the lead, one at a time. It
  checks whenever it signs a claim or serves the set, so a deployment
  that does neither in that window announces it at the next one. A
  person retires a key at once with
  `garam retire-message-signing-key <kid>`, a sibling of `serve` as
  `garam issue-certificate` is; a key retired that way leaves the set
  immediately, which is how a compromised key is withdrawn.

### What a verifier checks

This is the contract garam promises: a verifier that performs these
checks accepts every message garam delivers to it, and refuses one that
is forged, altered, addressed elsewhere or expired.

1. **The issuer, before anything is fetched.** The verifier reads
   `iss` from the payload while it is still unverified, only to choose
   which set to use, comparing it byte for byte with the issuers it is
   configured for. It refuses an `iss` it is not configured to trust
   without fetching anything, and takes a set's location from its own
   configuration, never from the message.
2. **The header.** `alg` is `ES256`, `typ` is `agent-message+jwt`, and
   `kid` names a key in the set chosen by step 1. An unknown `kid`
   refetches that set, at most once a minute for each configured
   issuer, so a forged message can neither choose where the verifier
   fetches from nor make it fetch a trusted issuer's set any faster.
3. **The signature** verifies under that key, which binds `iss`: the
   key came from the set of the issuer that `iss` names.
4. **The audience.** `aud` equals the verifier's own identity — for an
   agent garam delivers to, its GRN — compared as strings.
5. **The time.** `exp` is later than now minus 60 seconds, and `iat`
   is earlier than now plus 60 seconds. The 60 seconds is the whole
   skew allowance garam's validity assumes.
6. **The content.** `content_sha256` equals the digest of the content
   received, and `message_id` equals the identifier the message
   arrived under.
7. **Replay and redelivery**, both judged by `message_id` within
   `(iss, sub)`, are two questions with two different records.
   - **A replayed signature** is the same signature presented again.
     It needs no record past its `exp` plus the skew allowance, because
     after that check 5 refuses it.
   - **A redelivered message** is the same `message_id` and content
     under a new, valid signature, and the verifier's own idempotency
     record answers it: same content as the redelivery it is, other
     content refused. This contract does not shorten that record.
     garam hands a message out again until it is completed or its
     attempts are spent, keeps an undelivered one thirty days from
     storage, and still hands one out past that until the sweep reaches
     it ([ADR-0034](./0034-a-delivered-message-keeps-its-key-and-drops-its-body.md),
     [messaging.md](../messaging.md)). A verifier that keeps a committed
     identifier for less than that whole window can run a message's
     turn twice.

The verifier caches a set for its `max-age`. While a refetch fails it
may go on with the set it holds for up to 24 hours more, and after that
accepts nothing from that issuer.

garam promises in return:

- `message_id` is unique across the issuer.
- Every attempt of one message carries the same content, `message_id`,
  `sub`, `aud` and `org`.
- `exp` never exceeds `iat` by more than the lease ceiling.
- **A key is published at least two hours — two cache lifetimes —
  before it signs anything,** except the two §Keys names that sign at
  once, which no set could hold in advance. That lead is the refresh
  deadline for a set supplied as a file: a renderer (step 4 of
  §Consequences) that replaces the file within two hours of the
  published set changing never leaves its verifier without the key a
  message is signed with. Shortening it is a change to this contract,
  not to garam's practice.
- A key leaves the set only once nothing it signed is still valid, or
  because it was retired at once.
- Nothing in the format names garam except the value of `iss`, so
  garam is one issuer among any a verifier trusts.

### Replies

**Whether an agent signs a reply it sends back through garam is
deferred, and this ADR does not sign one.** Signing it needs a key the
agent holds and garam can verify, which is machine identity, the
ground #1251's ADR-0100 is drafting. Until an ADR decides it, garam
knows a reply's sender by what authenticated the agent's
`POST /outbox-messages` (adapter.md §Stable identifiers and the outbox),
and when garam delivers that reply to its recipient it signs it as any
other message, `sub` being that sender.

### Order against ADR-0100

**Neither waits for the other.** This ADR moves no credential an agent
or operator authenticates to garam with, and ADR-0100 moves none a
verifier judges a message by. `sub` is the GRN garam authenticated
whichever mechanism authenticated it. Where the two meet is named
below.

## What this does not decide

- **Sherlock's internals** (garamsh/sherlock#993): where it configures
  issuers, how its replay state survives a restart, what status
  refuses what, whether verification is opt-in first, and how a
  verified `(iss, sub)` becomes the sender its gateway scopes
  identifiers to. The checks above are what it must be able to
  perform; how it performs them is its own.
- **Which TLS root a verifier trusts to fetch the set.** The machine
  listener presents a certificate under the garam server root
  ([ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)),
  which a verifier outside garam holds only if given it, and ADR-0100
  may change what that listener presents. A deployment that supplies
  the set as a file needs neither.
- **Whether keys ADR-0100 introduces are published beside these.** This
  set holds message keys only, and asks nothing of ADR-0100's.
- **An agent's own signature on what it sends** (§Replies).

## Consequences

The plan, in the order each step can land and the repository it lands
in:

1. **garam:** the key store, rotation and the retire command; the
   publication route; signing at each claim; the content and the
   signature in the claim answer. The answer only gains members, so an
   adapter of the adjacent release ignores them
   ([ADR-0079](./0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)).
2. **garam:** the adapter forwards the claim's content and signature
   byte for byte instead of encoding the content itself. It sends the
   signature in the member or header Sherlock's ingress names, and only
   to a runtime release that accepts it, and chooses no name of its
   own.
3. **garamsh/sherlock** (#993): the ingress takes the signature beside
   `content`, verifies it, and judges the sender by it; a deployment
   configures garam's issuer and its set. The name the signature
   travels under at that hop is Sherlock's ingress contract: #993
   states it in Sherlock's `docs/architecture/gateway.md`, and
   adapter.md §Foreign facts this rests on records it, with the
   release, when step 2 lands.
4. **garamsh/garam-agent-operator, and the standalone supervisor in
   garam** ([ADR-0085](./0085-a-standalone-supervisor-owns-each-generation-as-three-containers-on-a-rootless-docker-daemon.md)):
   render the issuer and the set's location, or the set itself, into
   the runtime's configuration. Nothing here changes the Operator's
   repository; this names what it must do.

Step 1 and the content half of step 2 ship before Sherlock is ready,
because nothing a runtime receives changes. The signature half of step
2 does not: Sherlock's ingress refuses a member it does not know
(Sherlock `internal/gateway/handler.go`, `postMessageRequest`), so the
adapter sends the signature only once step 3 has named where it goes
and a release accepts it. Step 3 is where an unsigned message starts
being refused.

- **Responsibility documents paired by the pull request that implements
  each step** (rule 3): [messaging.md](../messaging.md) for signing at
  each hand-out and the keys it signs with;
  adapter.md §The delivery loop and §Stable identifiers and the outbox,
  for content the adapter forwards and no longer encodes, and
  §Foreign facts this rests on for Sherlock's verification once a
  release carries it; api-surface.md §What admits a caller, for a third
  operation a caller with no certificate reaches;
  [deployment-contract.md](../deployment-contract.md) for the issuer a
  deployment sets and for the key-encryption key now sealing message
  keys as well.
- **ADRs this touches and does not supersede:** ADR-0013, whose
  transport is unchanged and no longer what trust in content rests on;
  ADR-0016, whose key-encryption key seals a second kind of key;
  ADR-0018, each attempt of which is signed afresh;
  [ADR-0024](./0024-policy-decides-what-an-agent-sends.md), since
  `internal/message` still mints no sender identity of its own and
  `iss` signs rather than sends; ADR-0035, whose refusal keeps its
  stored sender and is signed under `iss`; ADR-0040, whose listener
  serves the set; ADR-0079 and ADR-0084, whose content form is
  unchanged while its encoder moves to garam.
- **The test of which routes a caller with no certificate reaches**
  (`internal/machineapi/identity_test.go`) gains the publication route,
  and is renamed for it.
- **A claim garam cannot sign is not handed out.** It is refused as a
  server error before the queue is asked, and the message stays
  pending with its attempts unspent, so signing's availability is the
  claim's.
- **A key retired while a claim waits still signs that claim, and that
  window is accepted.** The key is opened before the claim's wait,
  which runs up to its 30 seconds, so a key retired at once during it
  signs the message the wait hands out. That key has already left the
  set, so a verifier refuses the hand-out: one attempt is spent, the
  lease lapses, and the redelivery is signed under the replacement. The
  cost is bounded by the wait and is a redelivery, not a loss.
- **Losing the key-encryption key also loses the message keys.** Unlike
  an authority, a message key is replaceable: retiring the key that no
  longer opens makes the next hand-out create one sealed under the key
  present, and a verifier fetches it on the unknown `kid`; one holding
  the set as a file is refused until its file is replaced.

## Signs the decision was wrong

- **Sherlock answers 409 to a redelivery garam made.** Two attempts
  then carried different content, so the encoder is not where this put
  it or is not deterministic.
- **Verifiers refuse on `exp` in ordinary operation.** The lease
  ceiling or the 60-second allowance is too narrow for the clocks
  agents run on.
- **A platform other than Sherlock cannot verify with its stock JOSE
  library.** ES256 or the explicit `typ` is less universal than this
  assumed.
- **Deployments that hold the set as a file stop accepting at each
  rotation.** Ninety days is then too short for a set nobody fetches,
  and a pull the deployment can make, or a longer-lived key, is owed.
- **A relay has to transform the content on its way.** A digest over
  the octets then breaks on a path this called independent, and a
  canonical form was needed after all.

## Rejected alternatives

- **HTTP Message Signatures (RFC 9421).** A signature covers the
  components of one HTTP message. A garam message crosses at least two
  — garam's claim answer and the adapter's post into the runtime — and
  a signature over the first does not verify on the second unless the
  adapter re-signs, which needs garam's key. Path independence is the
  requirement it fails, and fewer stock libraries implement it.
- **The content as the JWS payload, or the signature inside the
  content.** Either puts the signature in the bytes the runtime replays
  on, so a signature renewed per attempt turns each redelivery into new
  content, answered 409; the only way back is to sign once at storage.
- **Signing once at storage.** Validity would have to cover the thirty
  days a message may wait, and replay state as long, and garam would
  sign messages never delivered.
- **A detached JWS over the raw content (RFC 7797, `b64` false).** It
  binds the octets without a digest claim, but needs `b64` listed in
  `crit`, which many libraries refuse or do not implement; the digest
  claim gets the same binding from a plain verify.
- **COSE and CWT.** Compact, and less widely implemented in the
  languages agent platforms are written in than JOSE.
- **An HMAC under a secret shared with each agent.** garam would hand
  the agent its verification secret, so the credential is not the
  agent's own, and anyone able to verify could forge.
- **Transport authentication at the agent's endpoint.** The owner
  rejected it: it proves who connected and not what was sent, and a
  relay ends it.
- **`message_id` as `jti`.** A stock replay cache refuses the second
  attempt of one message.
- **A key location in the message (`jku`).** A verifier following it
  trusts whoever wrote the message.

## Errata

- 2026-10-10 — §Keys' "Its URL is the issuer followed by that path" was
  too strong when written. The issuer is an identity, compared byte for
  byte, and need not resolve: the dev cluster's
  `https://machine.dev.garam.sh` does not. A verifier fetches the set
  from its own `keys-url`, whose host must be the host it reaches the
  machine listener at — on a standalone host, the adapter's — because
  Sherlock verifies that host against the listener's certificate
  (#1269, #1287). The decision stands: garam serves the set at
  `/message-signing-keys` on its machine listener.
