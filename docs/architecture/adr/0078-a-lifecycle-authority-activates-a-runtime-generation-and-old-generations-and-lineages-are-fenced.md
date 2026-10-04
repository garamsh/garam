# ADR 0078: A lifecycle authority activates a runtime generation, and old generations and lineages are fenced

> Status: accepted
> Date: 2026-10-04

Decided by the PM under the owner's explicit delegation of 2026-10-04,
recorded in #1155 (D4 to D7). Facts about Sherlock hold at `07aa5c4`.

## Context

An agent carries a GRN that never changes and an assignment epoch that
every reassignment raises (ADR-0012). The epoch fences state reports
(ADR-0053) and the operator's certificate route (ADR-0015). Nothing
fences a process of the same assignment: a restarted or copied runtime
holding the same certificate claims, renews and completes messages,
because `Claim` in `internal/message/service.go` compares the caller
with the recipient and nothing else. An agent renews its own
certificate over the connection it holds (ADR-0040), and its
certificate is revoked only by its lifetime (ADR-0014).

Configuration has no applied evidence either. Sherlock reads its
configuration at startup and queues ingress in memory, so a rendered
file or a Ready Pod says nothing about what the running process
applied. A local SQLite lock keeps two processes on one host apart; it
cannot fence a copied remote volume, and StatefulSet replacement or a
`ReadWriteOnce` claim alone are not cross-host proof.

The authority to change execution is broader than it should be in the
unmerged #1132, which grants configuration to ordinary members, and an
authorization cache can be stale while it reconnects.

## Decision

**Four identifiers stay distinct**: the GRN, the assignment epoch, the
process generation and the desired configuration revision. garam
records the active generation of each GRN at its epoch.

- **A lifecycle authority activates a generation, after proving it owns
  the writer.** Managed, that is the Operator control service under its
  delegation (ADR-0077); standalone, it is the local supervisor's
  narrowly scoped activation credential. Either is separate from the
  runtime's execution credential, which cannot activate a replacement.
  Activation yields a token binding the GRN, epoch and generation.
- **The token is checked per request, not per handshake.** Claim,
  renew, complete and every state-changing agent call are refused for
  a generation that is not active or a credential lineage that is not
  current, including on a connection that was already open.
- **Authority is delegated, scoped and fresh.** An administrator issues
  an expiring delegation scoped to an organization, operator and
  action. Editing execution settings or a deployment profile needs a
  separate high-trust permission; an ordinary creator may only use an
  authorized immutable profile. Current authorization is checked when a
  mutation is accepted and again before a pending revision activates,
  and unknown freshness refuses the new privileged action. The Operator
  still enforces host safety: image digests and secret references are
  deployer-approved, never member-editable.
- **Applied configuration is the runtime's report.** A change is
  validated, the runtime drains and restarts, and the runtime reports
  the GRN, epoch, generation, desired revision, outcome and report
  time. The adapter beside it carries that report outbound to the
  Operator control service, which holds it; garam holds no runtime
  evidence and answers only whether the epoch and generation are
  current. A report never activates a generation, and a generation is
  one process start rather than a Pod. Rendered and provisioned states
  are the Operator's separate report. An older generation or revision
  cannot overwrite newer evidence, and a missing or stale report reads
  as unknown or pending, never as success. No hot reload.
- **Uncertain storage ownership blocks activation.** Same-storage
  restart keeps memory. A move to other storage needs quiescence, a
  consistent backup of the database and outbox, fencing of the old
  writer and a verified restore before the new generation activates.
- **A lost or expired credential is recovered for the same GRN.**
  Explicit, scoped reauthorization replaces the credential lineage and
  fences the old one; the agent is not recreated.

## What this does not decide

- Operator enrollment. ADR-0064's token and ADR-0067's refusal to
  re-mint for an enrolled operator stand; this is an agent's runtime
  lineage.
- The token's encoding and the routes carrying it. The machine
  specification is authoritative for them once built.

## Consequences

- **None of this is built.** Today a message call is fenced by the
  calling identity alone, and an agent's certificate is revoked only by
  expiry.
- ADR-0014's revocation-by-lifetime is superseded for an agent's
  lineage, as ADR-0070 superseded it for an operator.
- ADR-0053's agent route, fenced by the identity the connection proved,
  is superseded in part: the active generation fences it too. Its epoch
  fence stands.
- ADR-0040's self-renewal stands inside a current lineage and renews
  nothing for a fenced one.
- A restart costs no certificate: the token, not a new issuance, is
  what a generation changes.

## Rejected alternatives

- **Reusing the epoch as the generation.** It misses a restart under
  the same assignment.
- **A new certificate per generation.** It ties churn in issuance to
  every restart.
- **Offline capability tokens.** They survive a revocation until they
  expire.
- **Membership as configuration authority.** Too broad for settings
  that choose what runs on a host.
- **Recreating the agent to recover a credential.** It loses identity
  relations and can detach memory.
- **Pod Ready, a rendered file, or a hot reload as applied state.** The
  first two prove nothing about the process; the third needs atomic
  model, tool and instruction changes Sherlock does not have.
