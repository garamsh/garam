# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

## Structure

- **Responsibility documents** (`overview.md`, `domains.md`, `grn-model.md`, `authz.md`) — the single source of truth for how the system is shaped *now*. To know the current state, read only these.
- **`adr/`** — the record of individual decisions: direction taken, context, rejected alternatives.

## Rules

1. **Responsibility document skeleton**: current decisions (consolidated) / rationale summary with links to the relevant ADRs / open questions. Do not copy ADR content — synthesize the present state.
2. **ADRs are append-only.** Once merged, the body is frozen. Exceptions: updating the status field (`accepted` → `superseded by ADR-XXXX`), fixing typos or broken links, and appending a dated entry under `## Errata` when a decision stands but a fact supporting it was wrong — the erratum names what falsified the fact, and the original text stays intact. A changed decision means a new ADR that supersedes the old one — never an edit.
3. **Decisions land in pairs.** A PR that adds or supersedes an ADR must update the affected responsibility documents in the same PR. A PR with only one of the two is rejected — the final state must always live in the responsibility documents.
4. Keep this index current: every responsibility document and notable ADR is listed here.

## Index

Responsibility documents:

- [overview.md](./overview.md) — what garam is and is not; module list
- [domains.md](./domains.md) — the domain packages and their responsibilities
- [grn-model.md](./grn-model.md) — the GRN resource naming model
- [authz.md](./authz.md) — the authorization model
- [adapter.md](./adapter.md) — what the adapter garam ships must do, and what it rests on
- [deployment-contract.md](./deployment-contract.md) — the contract a cluster-owning repository is written against: ports, probe paths, configuration, key material arrival

ADRs:

- [0001](./adr/0001-grn-segments.md) — GRN segments identify owning resources
- [0002](./adr/0002-iam-style-policy-model.md) — IAM-style policy documents, native evaluator
- [0003](./adr/0003-single-cli-topology.md) — single cobra CLI, independent api/authz packages
- [0004](./adr/0004-openapi-first-api.md) — OpenAPI spec as source of truth
- [0005](./adr/0005-server-side-sessions.md) — server-side sessions
- [0006](./adr/0006-invitation-tokens.md) — invitation tokens
- [0007](./adr/0007-hybrid-statement-collection.md) — hybrid statement collection
- [0008](./adr/0008-identity-based-policy-attachments.md) — identity-based policy attachments
- [0009](./adr/0009-web-frontend.md) — Next.js web frontend in `web/`
- [0010](./adr/0010-dev-auth-provider.md) — development-only sign-in provider behind GARAM_DEV_AUTH
- [0011](./adr/0011-console-shell.md) — console sidebar shell over org sections
- [0012](./adr/0012-agent-operator-model.md) — agents provisioned by operators garam never dials
- [0013](./adr/0013-agent-messaging.md) — agent messaging through a garam API an adapter polls
- [0014](./adr/0014-agent-authentication.md) — agents authenticate with mTLS on a listener of their own
- [0015](./adr/0015-operator-identity.md) — operators authenticate as agents do; key material travels as files
- [0016](./adr/0016-ca-key-custody.md) — a certificate authority's key is sealed by a key the process holds
- [0017](./adr/0017-machine-process.md) — machine identities are served by a process of their own
- [0018](./adr/0018-delivery-acknowledgement.md) — a message is completed by the turn-end event that names it
- [0019](./adr/0019-machine-openapi-spec.md) — the machine listener carries an OpenAPI specification of its own
- [0020](./adr/0020-published-artifacts.md) — garam publishes images and a contract, not manifests
- [0021](./adr/0021-liveness-and-readiness.md) — liveness and readiness answer on a port that carries no credential
- [0022](./adr/0022-watching-an-agent.md) — an agent is watched only while someone is watching
- [0023](./adr/0023-web-probes.md) — the web app answers the same two probes as the rest
- [0024](./adr/0024-policy-decides-what-an-agent-sends.md) — a policy decides what an agent may send
- [0025](./adr/0025-engine-scope-is-a-caller-obligation.md) — an unattached policy's org scope is the caller's to hold
- [0026](./adr/0026-the-console-defines-an-agent.md) — the console defines an agent and an operator reads that definition
- [0027](./adr/0027-a-watch-crosses-processes-through-postgres.md) — a watch crosses between processes through Postgres
- [0028](./adr/0028-oauth-state-is-a-server-side-record.md) — a login's state is a server-side record the callback spends
- [0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md) — readiness reports what a replica cannot serve without, so not the policy listener
- [0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md) — the login write rate is bounded at the store
- [0031](./adr/0031-detaching-an-active-policys-last-attachment-is-refused.md) — detaching an active policy's last attachment is refused
- [0032](./adr/0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md) — garam ships the command that mints an operator's first certificate

## Rules for edits

- A PR that changes module boundaries updates the responsibility documents; a PR that changes a decision adds an ADR. Same PR, always.
