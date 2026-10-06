# ADR 0091: A recovered credential names its chain, and garam stores it with the recovery

> Status: accepted
> Date: 2026-10-07

Drafted for #1222, which reserved this number. The scope is the PM's
decision there (option (a)), taken under the owner's delegation, and
the PM's answer in that issue's dispatch on how a retry is answered.

## Context

[ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
decided that a certificate garam hands back carries the authority
that signed it and the garam server root the holder verifies garam
by. It decided that for the first certificate.
[ADR-0078](./0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)
later decided recovery, and its route on `execution-fence.v1`
answered the certificate alone. Every other certificate answer in
`api/machine.yaml` names both values.

The recovery signs through the same request signer and the
organization's one authority as initial issuance, so garam held both
values and dropped them. Garam Agent Operator's manager half of
recovery (#300) relies on the observed behaviour instead: it keeps the
first certificate's issuer and server root and verifies a recovered
leaf against them, which holds only while nothing replaces either.

A retry of a recovery under the same request identifier is answered
from the stored recovery, and `agent_recoveries` stored only the
certificate. Recoveries recorded before this decision hold no chain,
and nothing records what they were signed under.

## Decision

**A recovery's answer names the issuer that signed the recovered
certificate and the garam server root the holder trusts, and garam
stores both with the recovery so a retry answers them again.**

- **The fields are additive.** `RecoveredCredential` requires
  `issuerPem` and `serverRootPem`. A consumer that ignores them is
  unaffected, so `execution-fence.v1` keeps its version and no other
  field changes.
- **What is answered is what the signer produced.** Migration 000036
  adds both to `agent_recoveries`; every new recovery writes them,
  which a `NOT VALID` check enforces, and its first answer and every
  retry read them from there rather than from the authority later.
  000036 rewrites no row.
- **A recovery recorded before 000036 is answered with the chain the
  signer signs under now.** It is the one case read separately, read
  without signing and without creating an authority. It equals the
  chain the certificate was signed under while neither the
  organization's authority nor the machine listener's configured
  server root has been replaced since that recovery. Replacing either
  ends it: such a retry would then name a chain its certificate does
  not verify against, or a server root garam no longer presents.

## Consequences

- A holder verifies a recovered credential from the answer alone, and
  Garam Agent Operator need not carry a chain over from the first
  certificate.
- Replacing an organization's authority, or the server root, is free
  of this decision for every recovery recorded after 000036. Whatever
  first decides either replacement owes an answer for the recoveries
  recorded before it, whose retries would otherwise answer the new
  chain.
- Rolling back below 000036 drops the stored chains; a recovery
  recorded meanwhile is then answered as a pre-000036 one.

## Rejected alternatives

- **Stating the chain in prose only (option (b)).** It leaves the
  answer resting on nothing replacing an authority, which nothing
  guarantees.
- **Reading the chain from the authority on every answer.** A retry
  would answer a chain the signer did not produce whenever either
  value had been replaced.
- **Backfilling pre-000036 recoveries.** Nothing records what they were
  signed under, so a backfill writes the same current chain the
  fallback answers and presents it as recorded.
- **Refusing a pre-000036 retry.** It fails a holder that lost its
  answer, while the current chain is right under the condition above.
