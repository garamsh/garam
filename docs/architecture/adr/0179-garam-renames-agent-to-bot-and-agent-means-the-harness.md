# ADR 0179: garam renames "agent" to "bot" and "agent" means the harness a bot uses

> Status: accepted
> Date: 2026-11-21

Append-only: once merged, the body below is not rewritten. A fact
later found wrong is corrected in an appended note, not edited out;
a revised decision is a new ADR that supersedes this one.

## Context

The existing term "agent" covers both what garam tracks — an
autonomous identity an operator manages — and what that resource
runs inside: the harness (Claude Code, Codex, and future
self-agents). As garam adds a workspace registry and further
machine identity concepts the vocabulary collapses: a sentence
about an agent's workspace, or an agent running an agent, names
two different things.

The owner decided (garamsh/discussions #6, comment 19791278,
2026-11-21) that the resource garam names and manages is a "bot"
and that "agent" means the harness a bot uses.

## Decision

garam renames the resource it currently calls "agent" to "bot" in
all new API resource names, the console, IAM GRN types, and
documentation. "Agent" in garam's vocabulary means the harness a
bot uses: Claude Code, Codex, and future self-agents.

The rename applies to:

- **API resource names.** `createAgent` → `createBot`,
  `listAgents` → `listBots`, `iam:agent.*` actions →
  `iam:bot.*`, and each other route and operation that names
  the resource.
- **The console.** Every label that shows "Agent" shows "Bot".
- **IAM GRN types.** `grn:<org>/iam/agent/<id>` →
  `grn:<org>/iam/bot/<id>`.
- **Documentation.** This repository's documents.

No existing code, database schema, or API changes in this pull
request. A later migration carries those; until it lands,
existing code continues to say "agent" and GRNs keep their
current form. All accepted ADRs that use "agent" for the renamed
resource keep their text; each will read "bot" once migration
completes.

## Consequences

- New code and documentation use "bot" for the managed resource
  and "agent" for the harness; existing code says "agent" for
  both until migration.
- Every accepted ADR that names the managed resource as
  "agent" — ADR-0012 and each subsequent one through the
  record — describes a resource whose wire name will be "bot"
  after migration. None of those decisions changes.
- The GRN `grn:<org>/iam/agent/<id>` issued before migration
  identifies a "bot" after migration. The rename moves the type
  segment; what the GRN names is unchanged.

## Signs the decision was wrong

- Teams outside garam consistently use "agent" for the managed
  resource and find "bot" narrower than what they deploy there,
  so the rename produces a term that does not match how the
  platform is described elsewhere.
- The code migration turns out to be too disruptive, and existing
  code stays on "agent" indefinitely, making the vocabulary split
  between documentation and code permanent rather than temporary.
