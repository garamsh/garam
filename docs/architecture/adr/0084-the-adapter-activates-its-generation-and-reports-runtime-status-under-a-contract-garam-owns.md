# ADR 0084: The adapter activates its generation and reports runtime status under a contract garam owns

> Status: proposed
> Date: 2026-10-05

Drafted for #1186. The Operator PM recorded on
`garamsh/garam-agent-operator#218` (2026-10-04T19:18Z) that garam owns
the adapter-facing activation and runtime-status contract, shared by
managed and standalone, and that the Operator implements it in its
control service. The inputs are that issue's proposal of 16:30Z, the
garam PM's review of 17:18Z, and the #1169 decisions of 2026-10-04 at
16:36 and 16:47. Facts about Sherlock hold at `v0.1.0`
(`44aaa555105f9834244eaac4ad18b0ed75dbf0ff`).

## Context

ADR-0078 lets a lifecycle authority activate a runtime generation and
fences every protected call by the token that activation issued. The
authority is the Operator control service when managed and the local
supervisor when standalone; neither is the runtime, and garam's
`activateAgent` refuses an agent's own certificate. The adapter is what
must obtain the token, because it makes the protected calls, and what
must carry the runtime's status outward, because Sherlock dials
nothing. So there is a hop between the adapter and the authority that
no specification defines, and two parties waiting on its shape.

Sherlock `v0.1.0` answers `GET /agents/{agent}/status` on loopback with
`contract_version` 1, `agent_id`, `assignment_epoch`, `generation` (a
128-bit start nonce as 32 lowercase hex), `config_revision` (an opaque
string), `state` (`serving` or `draining`) and `started_at`
(`internal/gateway/handler.go`). `activateAgent` answers a replay of
the same `requestId` with the same activation, a new token and a higher
`tokenVersion`, and admits only the last token issued.

## Decision

**The contract is `agent-execution.v1`.** Every request and every
answer of both routes below, refusals included, carries
`Garam-Contract-Version: agent-execution.v1`. A request with another
value is refused with 400 naming the supported one; an answer without
it is an unsupported peer to the adapter. There is no negotiation, and
a change of meaning is a new value. Bodies are JSON with camelCase
members and refusals are `{kind, message}` with a snake_case `kind`.

**Activation: `POST /v1/agents/{grn}/activations`.**

- The request carries `Authorization: Garam-Placement <token>`, the
  placement token read afresh from its file on each attempt, and
  `{requestId, epoch, generation, configRevision}`, all strings. The
  last three are what the runtime's status reported, unchanged.
- **The adapter owns `requestId`.** It is a UUID, version 8 (RFC 9562),
  whose 128 bits are the first 16 bytes of SHA-256 over six fields with
  the version and variant bits then set. Each field is written as its
  length in bytes, a 4-byte big-endian integer, and then its UTF-8
  bytes: `agent-execution.v1`, the GRN, the epoch, the generation, the
  revision, and the lowercase hex SHA-256 of the placement token. The
  same intent therefore has one identifier across adapter restarts and
  certificate renewal, and a new generation, revision, epoch or
  placement is new intent. The token never appears in it.
- The answer, 201 the first time and 200 for a replay, is
  `{activationId, tokenVersion, token, grn, epoch, generation,
  configRevision}`. **The adapter verifies the binding**: `grn`,
  `epoch`, `generation` and `configRevision` equal what it sent,
  `activationId` and `token` are not empty and `tokenVersion` is at
  least 1. A mismatch is a wrong binding and its token is discarded.
- **The highest `tokenVersion` wins.** The adapter keeps the token of
  the highest version for its activation and discards a lower or equal
  one, whatever order answers arrive in. Another `activationId` for the
  same `requestId` is a wrong binding. The token lives in memory and
  never enters a log, a file or an error.
- **One attempt at a time.** A replay is that same request unchanged:
  control preserves the original garam request identity, replacement
  anchor and binding, rechecks placement, credential and authority on
  every attempt, forwards the leaf verified on the current request
  rather than the first one, and never creates a new activation to
  recover an unknown outcome.
- Refusals: `400 invalid_request`, `401 unauthenticated`,
  `403 placement_not_current`, `403 credential_fenced`,
  `403 not_authorized`, `409 request_reused`, `409 epoch_superseded`,
  `409 activation_superseded` and `503 undecided`.

**Runtime status: `POST /v1/agents/{grn}/runtime-status`.**

- No placement token: the agent's leaf and the activated binding decide
  acceptance. The body is `{activationId, grn, epoch, generation,
  configRevision, state, startedAt, observedAt}`, the first from the
  activation and the next six from the runtime's status unchanged, with
  `observedAt` the adapter's read time as evidence only.
- `204` accepts it, and an identical retry is `204` too. Refusals:
  `400 invalid_request`, `401 unauthenticated`,
  `403 credential_fenced`, `409 generation_not_current` and
  `503 undecided`.
- **A report never activates or supersedes anything.** An empty
  `configRevision` is unknown, never evidence of the expected revision,
  and a missing, stale or refused report leaves the revision unknown or
  pending at control.

**What the adapter does with each answer.**

- **Nothing protected before activation.** The adapter reads the
  runtime's status, requires `contract_version` 1, its own GRN as
  `agent_id` and a 32-hex generation, and activates. It claims, renews,
  completes and forwards only while it holds a token, and every one of
  those calls carries `Authorization: Garam-Execution <token>`.
- **Undecided is retried; refused is not.** A 503, a timeout or a
  failed connection retries the same request with bounded exponential
  backoff. Every 4xx, a wrong binding and an unsupported peer are
  definite: the same request is not sent again, protected work stops,
  the outbox is kept and the refusal is logged by kind. The adapter
  acts again only when an input changes — a new generation, epoch or
  revision from the runtime, or another placement token — which is a
  new request.
- **A definite fence from garam stops protected work too.** garam's 403
  on any protected call, and its 409 on a claim, where no hold exists
  to have lapsed, mean the token or the credential is not current. The
  adapter neither recovers the credential nor contests the activation:
  recovery is the manager's.
- **Temporary unavailability stops nothing local.** The adapter never
  stops the runtime; garam or control being unreachable delays claims,
  forwards and reports, which retry.
- **Status is reported after activation and on each change** of what
  the runtime reports. A 503 retries the latest observation rather than
  queueing old ones; a 409 stops reporting for that generation.

**Settings.** The adapter reads `GARAM_ADAPTER_CONTROL_URL`,
`GARAM_ADAPTER_CONTROL_ROOT_FILE` (the root control's serving
certificate chains to) and `GARAM_ADAPTER_PLACEMENT_TOKEN_FILE`, as the
garam PM accepted them, and `GARAM_ADAPTER_OUTBOX_DIR`. One GRN,
`GARAM_ADAPTER_AGENT`, names the agent on both hops, so
`GARAM_ADAPTER_GATEWAY_AGENT` is removed: the runtime is started with
that GRN as its agent identifier.

**Delivery under the activation**, as the #1169 decisions of 12:41,
16:26, 17:04, 17:16, 17:30, 17:46, 18:07 and 18:37 fixed it:

- The adapter posts a claimed message to the runtime under garam's
  message identifier, never the lease, with content
  `{"contract":"garam-message.v1","sender":…,"body":…}` in that order,
  encoded by Go's `encoding/json` default, the sender being the one
  garam's claim verified.
- It forwards an outbox entry through `POST /outbox-messages` under
  `durable-delivery.v1` (ADR-0083), with the entry's identifier as the
  idempotency key and its `created_at` as `createdAt`, and removes the
  entry only after an answer naming a stored message with the expected
  sender, recipient and key. It reads an entry only as a bounded
  regular file inside the outbox, never through a link. There is no
  fallback to `POST /messages`, and no entry is deleted, re-keyed or
  re-stamped for being old or of unknown age.

## What this does not decide

- How a standalone supervisor authenticates the adapter and issues its
  placement token. The routes, bodies, answers and ordering above are
  the same there; the transport is #1180's.
- The tested window with Sherlock's adjacent release (ADR-0079). This
  binds `v0.1.0` and claims no other.

## Consequences

- The Operator implements both routes in its control service citing
  the commit that merges this; Operator's own
  `Control-Contract-Version` proposal is replaced.
- An adapter deployment must now name a control endpoint, a placement
  token and an outbox: there is no unfenced mode once the agent is cut
  over, and an adapter that cannot activate claims nothing.
- A restarted adapter replays its own activation and receives a newer
  token, which fences the token any earlier process of it held.
- Every change to a body, a refusal or the identifier's derivation is a
  new contract value.

## Rejected alternatives

- **Control mints the request identifier.** A restarted adapter would
  then present new intent and a lost answer could not be replayed.
- **Sherlock's status object verbatim inside the report.** It ties the
  report's shape to the runtime's contract version; the fields control
  checks are named instead.
- **An integer revision.** Sherlock defines it as a string, and parsing
  it would let the adapter invent an order the runtime does not have.
- **Caching the first activation's answer.** It would hand out a token
  a replay had already superseded.
