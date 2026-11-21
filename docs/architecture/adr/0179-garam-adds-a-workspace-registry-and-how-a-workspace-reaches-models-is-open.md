# ADR 0179: garam adds a workspace registry and how a workspace reaches models is open

> Status: proposed
> Date: 2026-11-21

Append-only: once merged, the body below is not rewritten. A fact
later found wrong is corrected in an appended note, not edited out;
a revised decision is a new ADR that supersedes this one.

## Context

ADR-0126 decided an `ai-gateway` service, OpenRouter-routed, as the
only path from an agent to a model. The service is not yet built.

The owner decided (garamsh/discussions #6, comment 19791278,
2026-11-21) that garam's scope adds two things: a workspace
registry (a first-class resource for the environment a bot runs in)
and a model access layer. The workspace concept — Workspace,
WorkspaceClass, BotPool — has no garam resource today.

How a workspace reaches models has two proposed shapes, neither
decided: a garam model gateway issuing short-lived tokens, or a key
proxy inside garam-bot with tools over MCP. One invariant holds
under either: the workspace holds no real model keys.

## Decision

1. **garam adds a workspace registry.** A workspace is a named
   resource with:
   - Three declaration paths: a file, a Kubernetes CRD, or garam's
     dashboard and API.
   - WorkspaceClass / PVC semantics: a WorkspaceClass names the
     type and constraints; a Workspace references a class and is
     exclusive or shared-read.
   - One owner per object named by a `managed-by` annotation.
   - BotPool as a named collection of workspace slots.

2. **The workspace holds no real model keys.** What reaches a
   workspace for model access is a short-lived credential. The
   shape of that credential is an open question (see §3).

3. **How a workspace reaches models is an open question.** Two
   shapes are under consideration; neither is decided here:
   - A garam model gateway that issues short-lived tokens to the
     workspace. The workspace presents one on each model call.
     This is the direction ADR-0126 proposed, without committing
     to OpenRouter or any specific routing.
   - A key proxy built into garam-bot that holds the real key and
     exposes tools over MCP. The bot uses those tools rather than
     calling a provider API directly; the workspace still holds
     no key.
   Under either shape no real provider key is stored in or
   transmitted to the workspace. The choice is the subject of a
   later ADR.

4. **Two permission kinds.** People's permission (RBAC and garam
   IAM) governs who may create, read, update and delete workspace
   objects. The bot's permission (`spec.permissions`) governs what
   the bot running inside a workspace may do.

If this ADR is accepted, it supersedes ADR-0126 in part: on the
gateway as the only model access path and on its routing, key
storage and billing decisions. ADR-0126's specific shape (OpenRouter,
Jev, key-in-platform-store) is replaced by the open question above.
Until this ADR is accepted, ADR-0126 is the accepted record for
model access.

## Consequences

- The workspace registry is a new resource garam must provision,
  version, authorize and audit. Its implementation is P2 or later.
- Keeping model access open delays the choice of where keys live
  and how model calls are priced. A later ADR takes it up once one
  shape is implemented or the tradeoffs are measured.
- The two open shapes have different security profiles: the gateway
  shape centralizes key custody and audit in garam; the proxy shape
  delegates key management to garam-bot's deployment and may reduce
  garam's surface.
- BotPool and WorkspaceClass require their own GRN types under
  `iam/`, decided in a later pull request.

## Signs the decision was wrong

- The workspace abstraction duplicates Kubernetes primitives without
  adding what garam's authorization, audit and multi-tenancy bring,
  and operators prefer raw Kubernetes objects.
- Both model access shapes prove impractical and the invariant
  (no real key in the workspace) is relaxed.
- The three declaration paths diverge in what they can express, and
  a workspace defined by file cannot round-trip to the garam API
  without data loss.
