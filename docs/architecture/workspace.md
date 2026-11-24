# Workspace registry

**None of this is built.** A workspace is a named environment a bot
runs in; this document describes the direction taken in
[ADR-0180](./adr/0180-garam-adds-a-workspace-registry-and-how-a-workspace-reaches-models-is-open.md)
(proposed). ADR-0180's decisions apply only if that ADR is accepted,
except its decision 3, how a workspace reaches models, which
[ADR-0190](./adr/0190-ai-gateway-is-an-interface-deployable-at-garam-or-in-the-organizations-own-environment-and-a-session-has-one-brain.md)
decides.

## Proposed decisions (ADR-0180)

- **A workspace has three declaration paths:** a file, a Kubernetes
  CRD, or garam's dashboard and API.
- **WorkspaceClass / PVC semantics.** A WorkspaceClass names the
  type and constraints a workspace of that class is built on. A
  Workspace references a class and is exclusive or shared-read.
- **BotPool** is a declarative-spec object; its semantics are
  defined by garam-bot's specification (garamsh/garam-bot#1193).
- **One owner per object**, named by a `managed-by` annotation.
- **The workspace holds no real model keys.** What reaches a
  workspace for model access is a short-lived credential.
- **Two permission kinds.** People's permission (RBAC and garam IAM)
  governs who may create, read, update and delete workspace objects.
  The bot's permission (`spec.permissions`) governs what the bot
  running inside a workspace may do.

## Decided (ADR-0189, ADR-0190)

- **A workspace reaches models through ai-gateway**
  ([ai-gateway.md](./ai-gateway.md)), with a token bound to the
  gateway's audience that a bot garam declared obtains by exchanging
  its bot token. The gateway runs at `ai.garam.sh` or in the
  organization's environment, where it also does the key proxy's job:
  the provider key stays in the gateway.
- **A bot's `spec.permissions` become Cedar policies**, decided by the
  policy service for what reaches garam and by the place's own
  evaluator for the rest, and translated the same way for a bot run
  without garam
  ([ADR-0189](./adr/0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)
  decision 4).

## Open questions

- **GRN types for WorkspaceClass and BotPool.** These need types
  under `iam/`; decided in a later pull request. A Workspace is
  `iam/workspace`
  ([ADR-0189](./adr/0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)
  decision 3).
- **Billing and audit at a gateway in an organization's
  environment.** What it records stays there unless interface 5
  carries it ([ai-gateway.md](./ai-gateway.md) §Open questions).
