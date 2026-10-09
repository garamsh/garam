# ADR 0016: A certificate authority's key is sealed by a key the process holds

> Status: superseded by [ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
> Date: 2026-08-16

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

ADR-0014 gave every organization a certificate authority and left one
question open in its own consequences: "How that key is held is the
first question the implementation must answer, and this ADR does not
answer it." This answers it.

The answer was first written into `domains.md` instead, on the reasoning
that it resolved an open question rather than changing direction. That
was wrong twice over. This repository's own structure rule assigns
rejected alternatives to `adr/` and tells a responsibility document to
summarise rather than hold them, and ADRs are append-only — so the
moment the answer is chosen is the only moment it can be recorded
where a reader will look for it. Recording it late is not an option
this project leaves open.

A responsibility document may still carry the rationale for a decision
it describes; what it may not carry is the record of what was rejected.
That is the line this ADR restores.

## Decision

- **The private key is sealed before it reaches storage, under a
  key-encryption key the process reads at startup from a file only its
  owner can read.** Postgres holds ciphertext, so a database dump on
  its own opens nothing.
- **The sealed bytes name the key that sealed them.** An identifier
  derived from the key — not the key — travels ahead of the ciphertext
  and is bound as the cipher's additional data, so a process holding
  the wrong key is told which one it needs rather than being handed
  what looks like corruption. Without that identifier, re-wrapping
  under a new key later is impossible and the only remedy left is
  reissuing every certificate in every organization.
- **Sealing is a seam with one implementation.** No caller depends on
  the implementation, which is what keeps the stronger custody below
  reachable without touching the code that issues certificates.
- **Rotation is not built and not designed.** The identifier is what
  makes it possible later; nothing here performs it.

## Consequences

- The key-encryption key becomes as availability-critical as the
  database. Losing it loses every organization's certificate authority,
  and there is no recovery path — the certificates themselves would
  have to be reissued from new authorities.
- A process cannot construct a certificate authority without a sealer,
  and the sealer refuses a missing, group-readable, malformed or
  wrong-length key. Failing closed sits at the type level rather than
  at a boot-time check, which no future caller can route around.
- Custody is only as good as the file. The threat this defends against
  is a database dump, and the threat it does not defend against is a
  process or host compromise, where the key is in memory by design.
- Key rotation is unimplemented, so the identifier in the sealed bytes
  is presently a promise rather than a mechanism.

## Rejected alternatives

- **A hosted key management service.** Stronger custody, and the
  difference is not marginal: the key never enters garam's memory, so a
  host compromise does not yield it. Rejected because taking it would
  make issuing a certificate depend on a hosted service in a project
  that depends on none, and ADR-0012 turns on a user being able to
  bring their own operator — a deployment that must reach a particular
  vendor to start an agent is not one a user can bring. The seam is
  what keeps this reachable when that trade changes.
- **Storing the key in the clear.** Simplest, and it makes a database
  dump a complete compromise of every machine identity in every
  organization at once.
- **A key file per organization on disk.** No sealing to implement, and
  it does not survive the topology: ADR-0003 runs two processes and
  more than one instance answers, so shared storage would have to be
  introduced to hold what Postgres already holds.
- **Deriving the sealing key from a passphrase at startup.** Removes
  the key file, and replaces a file whose permissions can be checked
  with a secret that reaches the process through an environment
  ADR-0015 already refuses for key material.
