# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

## Structure

- **Responsibility documents** (`overview.md`, `domains.md`, `grn-model.md`, `authz.md`) — the single source of truth for how the system is shaped *now*. To know the current state, read only these.
- **`adr/`** — the record of individual decisions: direction taken, context, rejected alternatives.

## Rules

1. **Responsibility document skeleton**: current decisions (consolidated) / rationale summary with links to the relevant ADRs / open questions. Do not copy ADR content — synthesize the present state.
2. **ADRs are append-only.** Once merged, the body is frozen. Exceptions: updating the status field (`accepted` → `superseded by ADR-XXXX`), and fixing typos or broken links. A changed decision means a new ADR that supersedes the old one — never an edit.
3. **Decisions land in pairs.** A PR that adds or supersedes an ADR must update the affected responsibility documents in the same PR. A PR with only one of the two is rejected — the final state must always live in the responsibility documents.
4. Keep this index current: every responsibility document and notable ADR is listed here.

## Index

Responsibility documents:

- [overview.md](./overview.md) — what gaon is and is not; module list
- [domains.md](./domains.md) — the domain packages and their responsibilities
- [grn-model.md](./grn-model.md) — the GRN resource naming model
- [authz.md](./authz.md) — the authorization model

ADRs:

- [0001](./adr/0001-grn-segments.md) — GRN segments identify owning resources
- [0002](./adr/0002-iam-style-policy-model.md) — IAM-style policy documents, native evaluator
- [0003](./adr/0003-single-cli-topology.md) — single cobra CLI, independent api/authz packages
- [0004](./adr/0004-openapi-first-api.md) — OpenAPI spec as source of truth
- [0005](./adr/0005-server-side-sessions.md) — server-side sessions
- [0006](./adr/0006-invitation-tokens.md) — invitation tokens
- [0007](./adr/0007-hybrid-statement-collection.md) — hybrid statement collection

## Rules for edits

- A PR that changes module boundaries updates the responsibility documents; a PR that changes a decision adds an ADR. Same PR, always.
