# Architecture

What the system is made of and why it is shaped this way. Workers read this before touching code; the PM keeps it honest.

## Contents

- [overview.md](./overview.md) — what gaon is and is not; module list
- [domains.md](./domains.md) — the domain packages and their responsibilities
- [grn-model.md](./grn-model.md) — the GRN resource naming model
- [authz.md](./authz.md) — the authorization model
- [adr/](./adr/) — architecture decision records, one file per decision

## Rules

- Describe structure with lists of paths and short sentences. No diagrams.
- Record *why* in ADRs, *what* in the overview. Never record *how* — that is the code's job.
- A PR that changes module boundaries updates `overview.md`; a PR that changes a decision adds an ADR. Same PR, always.
