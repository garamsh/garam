# Workspace registry

**None of this is built.** A workspace is a named environment a bot
runs in; this document describes the direction taken in
[ADR-0179](./adr/0179-garam-adds-a-workspace-registry-and-how-a-workspace-reaches-models-is-open.md)
(proposed). ADR-0179's decisions apply only if that ADR is accepted.

## Current decisions

- **A workspace has three declaration paths:** a file, a Kubernetes
  CRD, or garam's dashboard and API.
- **WorkspaceClass / PVC semantics.** A WorkspaceClass names the
  type and constraints a workspace of that class is built on. A
  Workspace references a class and is exclusive or shared-read.
- **BotPool** is a named collection of workspace slots.
- **One owner per object**, named by a `managed-by` annotation.
- **The workspace holds no real model keys.** What reaches a
  workspace for model access is a short-lived credential. How that
  credential is issued is an open question.
- **Two permission kinds.** People's permission (RBAC and garam IAM)
  governs who may create, read, update and delete workspace objects.
  The bot's permission (`spec.permissions`) governs what the bot
  running inside a workspace may do.

## Open questions

- **How a workspace reaches models.** Two shapes are under
  consideration (ADR-0179 §Decision 3):
  - A garam model gateway issuing short-lived tokens, one per model
    call.
  - A key proxy in garam-bot exposing tools over MCP; the bot uses
    those tools rather than calling a provider directly.
  Under either shape the workspace holds no real provider key. A
  later ADR decides which shape is taken.
- **GRN types for WorkspaceClass and BotPool.** These need types
  under `iam/`; decided in a later pull request.
- **Billing and audit.** How model call cost is attributed and
  recorded depends on the model access shape chosen above.
