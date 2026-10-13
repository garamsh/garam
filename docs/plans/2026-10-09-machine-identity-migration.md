# Machine Identity Migration

The order Garam, the Operator and Sherlock adopt machine authentication
and common agent enrollment, and the evidence required before removal.

Garam facts checked at `3beb1569b804f6461b2e956601e2a0377b81152e`.
Foreign facts below are the PMs' compatibility replies, not a new source
inspection: Operator at
`4d2cbc6ca37668b912c4c0f0ae211c11c90bf5f3` on
[garam#1353](https://github.com/garamsh/garam/issues/1353#issuecomment-6146268863),
and Sherlock at `2281328366515c1c67a72bdf978c2266b842cdec`, forwarded
by the Garam PM on 2026-10-13. Claims about those projects hold for
those revisions alone.

## Contents
- The shape of the move
- What is built
- The implementation sequence
- Contracts and compatibility
- How a running operator moves over
- Direct attachment gate
- Common enrollment acceptance
- Authentication acceptance
- What is paired where
- What would stop the plan

## The shape of the move

**Asymmetric Operator exchange and common enrollment are unbuilt.**
[ADR-0102](../architecture/adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
is proposed for #1353 following the
[owner's approved direction](https://github.com/garamsh/garam/issues/1353#issuecomment-6146292449).
The accepted ADR-0100's G1 implementation remains available: opaque
shared-secret exchange and token transport alongside certificates.
On acceptance ADR-0102 supersedes its Operator long-lived credential
issuance/exchange and separate target enrollment model, not its
short-lived tokens, server TLS, audiences or fences. ADR-0101's signed
messages and Garam's signing-private-key custody remain separate.

Garam adds support first and keeps the existing cohorts. The Operator
then cuts over both binaries and rendered agent credentials in one
coordinated release with one authentication mode. Operator ADR-0068
forbids development compatibility paths: no dual-auth switch or silent
fallback is introduced there. Both upgrade orders must be measured
before any adjacent-release window is advertised; an unsupported new
client against old Garam refuses by name and changes no state.

Common enrollment admits an identity and placement without
provisioning. A direct, independently running Sherlock needs a scoped
connector/lifecycle authority binding, not an Operator, Pod or the
Docker three-container runner. Garam's direct implementation is
unbuilt. Sherlock owns incoming message authentication and configured
issuer trust. The PM retains per-caller authorization above Sherlock
(Option A of
[proposal #1067](https://github.com/garamsh/sherlock/issues/1067));
runtime-local authorization needs a concrete gap and an owner decision,
not completion of Operator exchange.

## What is built

The checked Garam revision contains all four G1 steps:

| Step | Current implementation and evidence |
|---|---|
| G1a | Operator shared-secret issuance/re-issue, one-hour exchange and per-request token reads: `internal/operator/token.go`, `internal/machineapi/identity.go`, migration 000038 |
| G1b | Assignee-minted agent tokens, current epoch/lineage checks and `durable-delivery.v2`: `internal/agent/postgres_access_token.go`, migration 000039 |
| G1c | Token audience introspection, operation/execution/cutover/managed v2 values and audience foreign keys: `internal/machineapi/contract.go`, migrations 000040/000041 |
| G1d | Supervisor credential and token exchange, `standalone-enrollment.v2` and one-agent confinement: `internal/machineapi/standalone.go`, `internal/agent/supervisor_credential.go`, migration 000042 |

The adapter and standalone CLI remain certificate clients
(`internal/cli/delivery.go`, `internal/cli/standalone_host.go`).
G2 #1352 is undispatched and must follow ADR-0102 rather than complete
the superseded shared-secret Operator target by assumption.

The managed and standalone mechanisms still differ:

- `managed-enrollment.v1` creates a managed identity and signs its first
  credential; v2 creation opens the lineage with no certificate.
- `standalone-enrollment.v1` spends an approval bound to two public-key
  fingerprints; v2 spends a keyless approval and answers a supervisor
  credential. An identical v2 spending retry re-issues the credential
  under the approval lock and voids its former tokens.
- These distinct bindings remain enforced. Common admission does not
  make a v1 approval spendable under v2 or rewrite old replay answers.

## The implementation sequence

Only #1353 and #1352 identify current Garam work below. Rows naming
future issues are instructions for the owning PM to scope and dispatch,
not invented tracker numbers or claims implementation has begun.

| Order | Owner and issue | Work and gate |
|---|---|---|
| 1 | Garam #1353 | Pair proposed ADR-0102 with affected documents and this plan; PM accepts the profile and custody direction before code |
| 2 | Garam, new asymmetric identity implementation issue | Issue RSA PEM once, store public key only, register key incarnation/state, add `machine-token.v2` on `/oauth/token`, shared replay store, key/token revocation and explicit Bearer alias; preserve every G1 and certificate test |
| 3 | Garam, new common enrollment implementation issue | Publish `agent-enrollment.v1` wire, approval/admission/refusal forms and public idempotent result; use existing execution lock and scoped authority model for both placements |
| 4 | Garam #1352 (G2) | Adapter/supervisor token transport, atomic per-request files, v2 control headers; implement direct connector binding without requiring runtime reprovisioning, or split that work into a PM-owned follow-up issue |
| 5 | Operator PM, new coordinated O1 issues | Review shared contract first, then scope both binaries' PEM exchange, controller introspection, audience/epoch proofs, common managed admission and rendered short-lived token refresh; one cutover release, no compatibility switch |
| 6 | Sherlock PM, local proposal/implementation issues | Proposal #1067 compares authenticated origin and channel admission while retaining authorization above Sherlock; Option B needs owner decision; no Operator or Garam private keys |
| 7 | Owning PMs, joint acceptance issues | Record exact revisions/digests and both upgrade orders, managed/direct message+reply+restart, replay/rotation/revocation failures and all TLS trust consumers |
| 8 | Deployment owners | Adopt only after joint evidence and owner gates; gitops#14 and infra#331 remain. This documentation task deploys nothing |
| 9 | Garam, later G3/G4 issues | Remove old paths only after inventory and seven-day observation gates; no removal or destructive migration is authorized by #1353 |

Garam additive support can land while the current Operator continues
using certificates. O1 requires that support running at its actual
endpoint, not merely merged or released. Existing Operator #348 gates
remain as its PM's compatibility reply states; this plan changes no
other repository's issue or architecture decision.

Sherlock's message verification already exists according to its PM's
reply (ADR-0026 and `signature.go` at the revision above), while its
ADR-0014 places per-caller authorization above Sherlock. Its proposal
#1067
compares concrete admission cases before changing that boundary;
no local ACL implementation is required for already-built JWS trust,
and no SimpleX integration or terminal attach is included.

## Contracts and compatibility

ADR-0102 §Assertion exchange profile and §Supersession and compatibility
are authoritative for the new profile and retained contracts. Current
wire authority remains `api/machine.yaml`, not this plan.

| Value | Migration treatment |
|---|---|
| `machine-token.v1` | Preserve existing operator/supervisor exchange and agent mint |
| `machine-token.v2` | Add Operator client credentials/private_key_jwt exchange; normal OAuth response and bounded replay-safe RS256 assertions |
| `agent-enrollment.v1` | Add common approval/admission/delivery semantics, with authority/placement differences |
| `managed-enrollment.v1`, `.v2` | Keep current managed cohorts until an explicitly tested replacement |
| `standalone-enrollment.v1`, `.v2` | Keep current direct-host cohorts and approval/retry distinctions |
| `operation-authority.v1`, `.v2` | Keep controller/operation proofs and audience restrictions |
| `execution-fence.v1`, `.v2` | Keep lifecycle and per-request generation/lineage fences |
| `agent-cutover.v1`, `.v2` | Keep staged legacy cutover and archival definitions |
| `agent-execution.v1`, `.v2` | Preserve explicit control transport selection; G2 builds token mode |
| `durable-delivery.v1`, `.v2` | Keep stable outbox keys and seven-day admission budget |
| `garam-message.v1` | Keep signed content/digest and redelivery identity unchanged |

No tested adjacent-release window exists at the checked Garam revision.
Joint acceptance must name the oldest supported peer releases and the
next ones, execute both orders and record unsupported-profile refusal.
Garam carrying old/new cohorts does not require a new Operator release
to understand both. A rollback to an old Operator can use its old
cohort only while Garam still serves it and its credentials remain
valid; revocation is never undone to make rollback look successful.

G3's observation includes registered-but-idle callers, standalone
supervisors, hosted services, legacy agents and audience tokens. Require
inventory evidence of each cohort's successful new-mode exchange,
refresh and restart plus seven days of no old-authenticated requests;
`garam_machine_requests_total` alone cannot detect an idle operator or
distinguish shared-secret exchange from an asymmetric one. The new
implementation must add exchange/key-cohort evidence without logging
assertions, bearer values or private bytes. Removal gates stay open
until that evidence exists; they are not calendar deadlines.

G3 and G4 remain separate releases: a destructive table drop cannot
precede retiring readers. Message signing continues to use sealed keys,
so G4 never drops the key-encryption key or message key storage merely
because the certificate authorities go.

## How a running operator moves over

After Garam's additive contracts are deployed at the endpoint:

1. An authorized person issues a key for each existing controller and
   hosted service registration separately; each keeps its current GRN.
   Initial new-key setup preserves the old credential cohort during
   rollout. The person installs private PEM and the pinned server root
   as owner-only files; Garam stores public verification material alone.
2. Declare one external/admin writer for each stable key. Retire the
   Operator's old renewal writer in its local ADR-0008 pair before
   cutover. The owned PEM copy is replaced atomically and reloaded for
   every exchange; `kid` is derived from the same snapshot. Raw Secret
   projection and `fsGroup` alone do not establish owner-UID/0600.
3. O1 starts only with those files installed. Both binaries exchange
   fresh RS256 assertions at Garam for one-hour opaque tokens. The
   manager obtains a separate audience token for the hosted service;
   that service introspects it and obtains a fresh agent-bound proof
   before every lifecycle/release/status/placement/credential decision,
   after long polling too, comparing the revision's epoch.
4. Managed agents obtain scoped short-lived tokens under their current
   epoch/lineage. The Operator keeps both Garam/control token copies
   current, atomically, and the adapter reads them per request.
   Placement retains its own life and digest; refresh is not new intent.
5. Preserve `server-root.pem` for adapter TLS and Sherlock's
   `keys-ca-file`; move mounts and consumers atomically if needed.
   Message `iss` still matches configured issuer trust byte for byte;
   it is not the token endpoint's audience or the fetch URL.
6. Prove the new release's refresh/restart and key rotation before
   withdrawing old operator credentials. Removing an old API credential
   alone does not recover an agent lineage; a suspected agent-token
   compromise requires separate authorized recovery.

The assertion lives at most 60 seconds with 30 seconds of skew; the
access token one hour; planned key overlap at most 24 hours. Access
refresh is jittered between two thirds and three quarters of token life,
leaving at least a quarter for propagation. Agent lifetime retains its
20-minute to 24-hour bounds and one-hour default. Two audiences remain
separate and ordinary refresh does not void an unexpired predecessor.

A definite authentication refusal pauses protected work until key or
configuration changes; an unavailable endpoint retries with backoff
and a fresh assertion. Lost exchange answers use a new `jti`, not an
assertion replay. Lost PEM delivery revokes that issuance and issues
anew; no download is read back from storage.

## Direct attachment gate

**Unbuilt and blocked on an owner architecture decision.** The
current standalone runner can observe recorded Docker container IDs,
nonces and termination. An independently running Sherlock's status
cannot supply that proof, and forcing it into the three-container
runner would contradict the approved separation from provisioning.

ADR-0102 §Direct attachment proof remains an implementation gate names
the options presented to the PM: authenticated local attachment with
an exclusive host lease, or a scoped host authority that can own and
observe the existing process. Neither is selected. The comparison must
include crash/restart, stale binding/lease, disconnected authority,
unobserved predecessor and replacement: expiry or lineage recovery
alone proves no local writer has stopped. Direct identity admission
must not activate or bypass the execution fence before this gate closes.
Operator private-key exchange can proceed independently. The PM owns
bringing the remaining choice to the owner and scoping the evidence.

## Common enrollment acceptance

Run these against both managed creation and attachment to an already
running direct Sherlock, with actual authority and runtime processes:

| Case | Required observation |
|---|---|
| First admission | One GRN, current assignment/epoch and lineage, fenced mode and no activation; no workload provisioning implied |
| Identical retry and simultaneous admission | Same public result and one identity; no extra writer, epoch or lineage |
| Changed binding under request ID | Conflict, with no identity or placement mutation |
| Expired/withdrawn/other-tenant approval | Refused, no credential or activation; same-cohort valid approval is the control |
| Unknown authority or unavailable policy/store | No successful admission; unavailable is distinct from positive authorization |
| Replacement at expected epoch | Same GRN, raised epoch/new lineage; stale expected assignment refuses |
| Old generation or lineage on open connection | Protected calls refuse; a current generation's control call succeeds |
| Replacement racing an admitted write | Execution lock orders them; no replaced-generation write commits after replacement |
| Direct runtime attachment | Runtime/memory stays in place; one-agent authority cannot enroll or access another agent and requires no Operator/Pod/Docker runner |
| Writer ownership unknown or lost | No new activation or protected calls; no assertion an unobserved writer exited |
| Lost agent token delivery | Assignee re-mints in current lineage, no enrollment replay or agent self-mint |
| Legacy standalone v2 replay | Existing credential re-issue/void behavior persists separately from common public retry |
| Message, reply and restart | Stable signed identifiers/content, durable outbox and memory kept; no long-lived private key in runtime/adapter/workspace |

Enrollment success proves no message admission. Independently verify
configured issuer, authenticated Origin, altered/wrong-audience/expired
signature refusal and ordinary redelivery. Garam enrollment does not
grant
command authority. Proposal #1067 retains current authorization above
Sherlock; any future runtime-local authorization requires an owner
choice and its own acceptance before claiming that behavior.

## Authentication acceptance

Use independent stock OAuth/JOSE client/server test vectors and a real
multi-replica replay store. Every refusal case includes a valid control
that would fail if the intended check were removed.

| Case | Required observation |
|---|---|
| Standard exchange | Form client credentials request plus issued RSA PEM succeeds with RS256; 200 standard token response and no cache |
| Identity/key substitution | Wrong client_id/iss/sub, cross-org or another registration's kid refuses |
| Algorithm/header confusion | none/MAC/ES256, wrong key type, duplicate claims or remote key headers refuse; message JWT cannot exchange |
| Audience and time | Wrong endpoint spelling/array, future iat/nbf, expired exp and lifetime over 60 seconds refuse at specified boundaries |
| Replay, concurrency and restart | Same client_id+jti succeeds at most once across replicas and after restart; different kid/signature does not bypass it |
| Lost exchange response | Fresh assertion succeeds inside live-token bounds; old jti refuses without exposing bearer readback |
| Replay/key/store outage | 503 and no token; no cached positive or shared-secret fallback |
| Planned rotation | Successor exchange succeeds before old key withdrawal; both work inside overlap, old key/tokens refuse after withdrawal |
| Compromise or lost PEM | Immediate replacement; old tokens refuse on an open connection, including raced exchanges |
| Delete/re-register same operator_id | Old key, assertion and tokens cannot regain authentication through the new record |
| API alias and versions | Bearer and Garam-Token give equal confinement/fencing; certificate+token or multiple credentials refuse; old v1/v2 controls remain unchanged |
| Control audience | Manager token shown to control proves current controller/agent only by fresh introspection; never authorizes Garam API use there |
| Custody and rate bounds | Public-only persistence and redacted logs; no private key on agents; documented exchange saturation returns 429 with Retry-After |

## What is paired where

- #1353 pairs ADR-0102 and the index with machine identity, operators,
  agents, adapter, messaging, API surface, deployment contract and this
  existing plan. ADR-0100 body remains frozen; its partial status moves
  only when the PM accepts the successor. ADR-0101 remains accepted.
- Every Garam implementation pairs the affected responsibility sections
  with the current wire/schema and tests. Replace unbuilt descriptions
  with observed state only when that implementation lands.
- Operator and Sherlock PMs own their local issue/ADR/document pairs;
  this plan edits neither repository and assigns no fictional issues.
- G3/G4 update all documents affected by actual removal, including
  certificate authorities, organizations and the architecture index.

## What would stop the plan

- The PM declines or changes ADR-0102's proposed protocol;
  implementation
  follows the accepted record, not this draft by inertia.
- Operator/Sherlock shared-contract review finds a conflicting local
  rule. Present the conflict to the owning PM before implementation.
- A stock client needs a proprietary assertion format, replay succeeds,
  revocation waits for token expiry, or key material reaches a runtime.
- Common admission requires reprovisioning an independently running
  runtime or cannot establish scoped writer ownership and fencing.
- Any known or idle caller lacks measured new-mode compatibility; G3
  remains blocked, with its registration named in inventory.
- The key-encryption key still has a reader, including message signing;
  keep it and its storage rather than removing them under G4.
