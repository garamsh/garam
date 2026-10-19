# ADR 0104: Message signing keys may be held by an OpenBao Transit provider, and the file key-encryption key stays the default

> Status: accepted
> Date: 2026-10-18

Drafted for #1476, which reserves this number and records the owner's
direction of 2026-10-18: an optional OpenBao (or Vault) Transit
provider, with the file key-encryption key kept as the default. Facts
about garam hold at `57fcdd227b19d57b0f613c89a4bf5b7511475f4d`. Facts
about OpenBao hold for the image
`openbao/openbao:2.7.1@sha256:6d2b93856e3fcf7b18ad855a0b51eaba474dc8b79cf554379ea32034797d2acf`,
the digest of its multi-platform index, run in dev mode; the commands
and their output are in this decision's pull request.

Accepted by the Garam PM on 2026-10-18 under the owner's delegation,
recorded in the review on #1479.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives
- Errata

## Context

What the key-encryption key does today:

- **It seals two kinds of private key.** Every organization's
  certificate authority (`internal/ca/sealer.go`,
  [ADR-0016](./0016-ca-key-custody.md)) and every key garam signs
  messages with (`internal/message/signing.go`,
  [ADR-0101](./0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)).
  Both go through one AES-256-GCM sealer built from the file
  `GARAM_CA_KEY_FILE` names (`internal/cli/machine.go`,
  `machineKeyMaterial`), and `garam serve machine` refuses to start
  without it.
- **A message key is opened into garam's memory to sign.**
  `keyring.Signer` opens the sealed private half, and `Sign` signs in
  process. Postgres holds the sealed bytes; the KEK file, the database
  and the process's memory each hold everything needed to sign.
- **The authorities are retiring and the message keys are not.**
  [ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
  removes every certificate authority at step G4 of
  `docs/plans/2026-10-09-machine-identity-migration.md`, and that plan
  keeps the KEK past G4 because message signing still reads it.
- **The seams are `ca.Sealer` and `message.Sealer`**, both `Seal` and
  `Open` over a binding. A provider behind `message.Sealer` hands the
  private half back to garam on every `Open`, so it can move where the
  wrapping key lives and cannot keep the private key out of garam.
- **ADR-0016 rejected a hosted KMS** because a deployment would have to
  reach a particular vendor to start an agent. A self-hosted OpenBao or
  Vault is no vendor's service, and an optional provider leaves a
  deployment that sets nothing where it is.

What Transit does, measured against the image named above:

- **`sign` on an `ecdsa-p256` key with `hash_algorithm` `sha2-256` and
  `marshaling_algorithm` `jws`** answers `vault:v<n>:` followed by 86
  base64url characters. Appended to the JWS signing input it was given,
  that is a compact JWS which go-jose v4.1.4, accepting ES256 alone,
  verifies under that version's `public_key`, and refuses under another
  version's.
- **`key_version` chooses which version signs.** Setting the key's
  `min_encryption_version` above it makes that `sign` fail with HTTP
  500.
- **`rotate` answers the new `latest_version` and every version's
  `public_key`**, so the caller that rotated learns the public half of
  the version it made from the same answer.
- **`associated_data` on `aes256-gcm96` is enforced.** Ciphertext made
  under one value refuses to decrypt under another, and under none,
  with HTTP 400 `cipher: message authentication failed`; ciphertext
  made under none refuses under one.
- **`rewrap` refuses that ciphertext**, with or without
  `associated_data`, with the same error. Ciphertext bound to a row
  cannot be moved to a newer key version without the caller decrypting
  and encrypting it again.
- **An ACL policy of three paths is enough to sign and rotate.** With
  `read` on `transit/keys/<key>` and `update` on
  `transit/keys/<key>/rotate` and `transit/sign/<key>`, a token reads,
  rotates and signs; it is refused (403) the key's configuration,
  creating another key, exporting, deleting, and any other key.

## Decision

### The seam is custody of a signing key, not sealing

**`message.Sealer` stops being the seam a message key is held through.
The keyring takes a key custody instead: what creates a key and signs
with it.** A custody creates a P-256 key and answers its public half and
what the row must record to sign with it later; given a row, it signs a
JWS signing input under that key. The private half is never something a
custody hands back.

- **Two custodies.** The file custody is today's code moved behind the
  seam: it generates the key in process, seals the private half with
  the KEK sealer bound to the row, and opens it to sign. The Transit
  custody holds the private half in OpenBao and signs there. It is
  `internal/message/transit/`, an implementation package named for the
  technology it implements against, as `internal/ca/postgres/` is
  ([structure.md](../structure.md)).
- **`message.Sealer` survives as what the file custody seals with**, and
  `ca.Sealer` still satisfies it. Nothing in `internal/ca` changes.
- **`internal/cli` decides which custody new keys go to**, as the
  composition root decides every implementation
  ([structure.md](../structure.md) §Where the composition root lives).
- **With no provider configured, everything is today's**: the file
  custody, the same rows, the same bytes in them, the same errors.

### A row records its custody

**Every message key row records which custody holds its private half.**
A file row keeps its sealed private key. A Transit row records the
mount, the key name and the key version, and no private material. Rows
written before this are file rows. The migration only adds; no row is
rewritten and none is deleted.

A process signs with a row only in the custody that row names. A row
of a custody the process is not configured with does not sign there.

### Keys under Transit

- **One Transit key, one garam key per version.** The deployment names
  one `ecdsa-p256` key. garam makes a new signing key by `rotate`, takes
  the new version's public half from that answer, and stores a row for
  that version. It signs with `key_version` set to the row's version,
  so a version made later, by garam or anyone else, never signs in its
  place.
- **`kid` stays the RFC 7638 thumbprint**, computed by garam from the
  public half it stored, exactly as for a file key. Transit's version
  number is never the `kid` and appears in nothing a verifier reads.
- **The published set is derived from garam's rows, as now.** A
  version Transit holds without a row is not published and never signs.
  The set is read from Postgres alone, so serving it never calls
  OpenBao.
- **Rotation keeps ADR-0101's schedule and lock.** garam announces the
  next key two hours before it signs, after 90 days less that lead, one
  at a time, under the lock every replica takes. `rotate` is called
  under that lock. A transaction that fails after its `rotate` leaves a
  version with no row: unpublished, never used, and harmless.
- **Retiring is the row, as now.** `garam retire-message-signing-key`
  withdraws the row; the version stays in Transit and garam never signs
  with it again. The command opens nothing, so it needs no OpenBao
  configuration.
- **A compromise under Transit is a stolen OpenBao token, not a stolen
  key.** The private half cannot leave Transit (`exportable` is false,
  and garam's policy has no export path), so what a thief holds is the
  right to sign, under every version the policy covers, the replacement
  included. Revoking that token and its auth role in OpenBao is what
  withdraws it; retiring a `kid` alone does not.

### Signing is part of the claim

**The signature is made inside the claim's transaction, so a claim that
cannot be signed rolls back and spends none of the message's
attempts.** ADR-0101 promised that by opening the key before the claim;
a signature Transit makes after the claim can fail where a local one
could not. One path serves both custodies: the claim takes the signer,
and the row is claimed, signed and committed together.

- The key that signs is still chosen before the claim's wait, as now,
  so rotation and the choice of key stay outside the transaction.
- A call to Transit is bounded by a timeout well inside the lease. It
  holds the claimed row for at most that long, and for a fenced claim
  the execution lock the fence takes too
  ([ADR-0080](./0080-an-agents-execution-row-is-the-one-lock-its-writes-and-replacements-share.md)).

### When OpenBao cannot be reached

**garam fails closed on the hand-out alone.**

- **A claim is refused as a server error** with nothing claimed and no
  attempt spent, as a key that would not open is refused now.
- **The published set is served**, because it is read from the rows.
- **A due announcement waits.** The current key goes on signing, and the
  next read that finds the key due tries again. Only where no key
  signs — a deployment's first, or after a retirement at once — does a
  failed `rotate` leave nothing to sign with, and claims are refused
  until it succeeds.
- **Readiness does not report it.** Every replica reaches the same
  OpenBao, so readiness failing takes the whole machine listener out
  of its Service and routes no claim anywhere better, while renewals,
  completions, tokens and the set would still have been served
  ([ADR-0029](./0029-readiness-reports-what-a-replica-cannot-serve-without.md)).
- **Start does not reach it.** `garam serve machine` checks the
  configuration's shape at start and calls OpenBao only when it signs
  or rotates.

### Configuration and authentication

**The provider is on when `GARAM_TRANSIT_ADDR` is set, and off
otherwise.** Only `garam serve machine` reads it.

| Variable | What it carries |
|---|---|
| `GARAM_TRANSIT_ADDR` | OpenBao's `https` base URL; any other scheme is refused |
| `GARAM_TRANSIT_CA_FILE` | the PEM root that verifies OpenBao's certificate; required, with no fallback to the system roots |
| `GARAM_TRANSIT_TOKEN_FILE` | an owner-only file holding the token garam presents |
| `GARAM_TRANSIT_MOUNT` | the Transit mount; `transit` when unset |
| `GARAM_TRANSIT_SIGNING_KEY` | the name of the `ecdsa-p256` key messages are signed under |

- **All or nothing.** With `GARAM_TRANSIT_ADDR` set, the CA file, the
  token file and the key name are required; any `GARAM_TRANSIT_*` set
  without it is refused. A start that could never sign does not happen.
- **garam authenticates with a token it reads from a file, and logs in
  with nothing.** The file is read through `internal/keyfile` on every
  call, so it is owner-only and owned by the process's UID
  ([ADR-0015](./0015-operator-identity.md)), and a replaced token takes
  effect on the next call. Whatever writes it — OpenBao Agent's
  auto-auth with the Kubernetes method in a cluster, any method on a
  standalone host — is the deployment's, and so are login and renewal.
  The file may appear after garam starts; until it does, a claim is
  refused as when OpenBao is unreachable.
- **No credential comes through the environment.** The variables above
  carry a location, a name and two paths.
- **The token's value is never logged**, nor is any part of a Transit
  answer but the version and the public half.

### Certificate authorities stay on the file key-encryption key

**`ca.Sealer` keeps its one implementation, and no authority is sealed
through Transit.** Transit `encrypt` with `associated_data` would hold
the binding — that was measured, not assumed — and it is still not
taken, for three reasons:

- **The authorities go at G4.** Moving them means a second sealed
  format told apart from the first, a re-sealing command with its dry
  run, and a KEK that is still required until every row has moved —
  all to be deleted with the authorities.
- **`rewrap` does not reach row-bound ciphertext.** Rotating the Transit
  key would leave each authority on its old version unless garam
  decrypted and encrypted it again, so the mechanism that makes Transit
  rotation cheap is not available to it.
- **It would not keep the key out of garam.** An authority's private
  key is opened into memory at every issuance under either sealer.
  What Transit would move is the KEK out of the pod, and the KEK stays
  there for the authorities until G4 whichever is chosen.

So the KEK stays a deployment secret until G4, and is no longer a
permanent one: after G4, a deployment whose signing keys are in
Transit has no reader of it left, and G4 decides whether `serve
machine` then starts without it. A deployment with no provider keeps
it for its message keys, as the plan says.

### Moving a deployment onto Transit, and off it

**Message keys move by rotation, never by re-sealing**, so no command
re-seals anything and no sealed row is touched.

- **On.** A process configured with Transit whose signing key is a
  file row, with no key announced, treats the next key as due at once:
  it announces a Transit key with the full two-hour lead while the file
  key goes on signing, opened with the KEK the authorities still need.
  A file key already announced begins as announced, and the Transit key
  follows it the same way, so one key is announced at a time as
  ADR-0101 requires. The file key then retires on ADR-0101's schedule
  and stays published while what it signed can be valid. No verifier,
  including one that holds the set as a file, meets a key it was not
  given two hours ahead.
- **Off.** A process without Transit cannot sign with a Transit row.
  That is the case ADR-0101 already names for a key that does not open:
  claims are refused as server errors, and
  `garam retire-message-signing-key` on that key makes the next
  hand-out create a file key that signs at once. A deployment leaving
  Transit without that outage keeps Transit configured until it has
  done so on its own schedule.

### What a deployment owes

The OpenBao side is the deployment's, and garam creates nothing there.

- **An OpenBao reachable over `https`** under a certificate the root in
  `GARAM_TRANSIT_CA_FILE` verifies.
- **A Transit mount and one `ecdsa-p256` key**, created by the
  deployment, not exportable, and never deleted while a row names it.
  Its `min_encryption_version` is never raised past a version garam's
  set holds as signing or next: that version would stop signing.
- **An ACL policy of exactly the three paths measured above** for that
  key, and nothing else.
- **An auth role binding garam's machine workload identity** — the
  account the chart names
  ([ADR-0043](./0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md))
  — to that policy, and something that writes and renews the token file
  as the process's UID.

No `garamsh/aurora` change is made under this decision; the PM requests
what it needs once the implementation has merged (#1476).

## What this does not decide

- **OpenBao namespaces.** No variable names one; a deployment that needs
  one is a later decision.
- **A Transit sealer for the certificate authorities**, should G4 not
  land. The measurement that would make it sound is recorded above.
- **Removing `GARAM_CA_KEY_FILE` from `garam serve machine`.** That is
  G4's, on the authorities' retirement.
- **Any other key garam might sign with.** This set holds message keys
  only, as ADR-0101 decided.
- **Which renderer writes the token**, OpenBao Agent or another.

## Consequences

- **With a provider, no message-signing private key reaches the
  database or garam's memory**, and a database dump together with the
  KEK opens no key that signs.
- **Signing's availability is OpenBao's**, for the hand-out only, and a
  claim refused for it costs no attempt.
- **Each hand-out costs one call to OpenBao**, inside the claim's
  transaction.
- **`message.Signer` takes a context** and can fail on the network,
  where ADR-0101's could fail only on encoding or randomness.
- **The implementation**, in its own pull request after this is
  accepted: the custody seam and both custodies; the row's custody
  columns, added by migration; signing inside the claim; the
  configuration in `internal/cli`; the chart's values for the
  provider, with no secret in them; and integration tests against an
  OpenBao container pinned by digest that prove a Transit-signed
  hand-out verifies under the published set and under go-jose,
  rotation, retirement, the move onto Transit with its lead, and the
  unavailable case.
- **Responsibility documents paired here** (rule 3), each saying the
  mechanism is unbuilt (rule 9): [messaging.md](../messaging.md) for
  where the signing keys are held;
  [deployment-contract.md](../deployment-contract.md) for what a
  deployment supplies and what the KEK still seals;
  [certificate-authorities.md](https://github.com/garamsh/garam/blob/c551f13ef50301340a755bee6350c2d446287965/docs/architecture/certificate-authorities.md) for the
  authorities staying on the file KEK; and
  [overview.md](../overview.md) for the decision's rationale.
- **ADR-0101 is superseded in part**: §Keys' "Its keys are stored in
  Postgres sealed under the key-encryption key", and §Consequences'
  "Losing the key-encryption key also loses the message keys", for a
  deployment that configures a provider; and the key opened before the
  claim as how no attempt is spent, now signing inside the claim. Its
  format, schedule, publication and verifier contract stand.
- **ADR-0016 and ADR-0100 are unchanged.** ADR-0016 is already
  superseded whole by ADR-0100, and its reason for refusing a hosted
  KMS — one vendor every deployment must reach — is not what this
  adopts. ADR-0100 retires the authorities and their KEK use as it
  said; this decides only what reads the KEK after them.

## Signs the decision was wrong

- **Claims are refused for OpenBao often enough to show in delivery.**
  A per-call dependency was then the wrong trade, and a key held in
  memory per replica, unwrapped once by Transit, was owed.
- **The claim's transaction, held across the call, shows in lock waits
  on the execution row.**
- **A deployment needs OpenBao to create or configure keys for garam**,
  so the three-path policy was too narrow for real operation.
- **G4 slips far enough that the authorities' KEK outlives the reasons
  above**, which would make the Transit sealer this declined worth its
  migration.

## Rejected alternatives

- **The file KEK only** (the issue's option 1). It leaves the private
  half openable from a dump plus the KEK, and the KEK a permanent
  deployment secret.
- **A deployment-supplied signing key file, or a cloud KMS signer, and
  no KEK** (option 2). A key file rotates only by a person replacing it,
  which ADR-0101's automatic overlap cannot schedule; a cloud KMS is
  the vendor dependency ADR-0016 refused.
- **A KEK scoped to message keys, with rotation built** (option 3). It
  builds rotation for a wrapping key the private half is still opened
  under, which is the exposure this removes.
- **Transit `encrypt` behind `message.Sealer`.** It keeps the seam, and
  every `Open` returns the private half to garam, so the key is in
  memory exactly as now.
- **`datakey` envelope encryption.** The same: the data key is in
  garam's memory.
- **Signing after the claim and releasing it on failure.** The release
  is a second write that can fail too, and when it does the attempt is
  spent.
- **Kubernetes auth implemented in garam.** It needs login and lease
  renewal in garam, and a second method for a standalone host; the token
  file serves both with none.
- **One Transit key per garam key.** It needs a policy that creates and
  deletes keys, where one key's versions need three paths.
- **OpenBao reachability in readiness**, for the reason in
  §When OpenBao cannot be reached.

## Errata

- 2026-10-18 — the implementation reads the public half from
  `rotate`'s answer and never calls `read` on the key; the policy garam
  needs is those two paths. §Context's three-path measurement and
  §What a deployment owes' "exactly the three paths measured above"
  were wrong about `read` being needed, as #1482 showed; the decision
  stands, and a deployment grants `update` on
  `<mount>/keys/<key>/rotate` and `<mount>/sign/<key>` alone.
