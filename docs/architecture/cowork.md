# cowork

Where people and agents write together in real time, message each
other, and keep the record of their work, with tools an AI can use at
its centre (ADR-0127).

**None of this is built.** No `cowork/` folder exists. #1663 to #1667
build it in order, and each one's pull request updates this document.
The document moves to `cowork/docs/` with the service
([ADR-0125](./adr/0125-the-repository-is-a-monorepo-of-services-laid-out-by-responsibility.md)).

## Contents
- Current decisions
- Ports
- What it asks of the platform
- Rationale
- Open questions

## Current decisions

- **One binary.** `cowork serve` runs the API, the real-time endpoint
  and the MCP endpoint, and `cowork migrate` its migrations. The same
  binary is the client CLI, under ADR-0122's output and exit-code
  contract.
- **Pages** are Yjs documents edited in TipTap. The server relays and
  logs updates, compacts them into snapshots, and reads and writes
  documents itself through the Yjs reference implementation running
  in goja.
- **Messaging.** Channels, direct messages and threads. A message for
  an agent also reaches the agent's queue on the platform, and the
  agent answers through MCP.
- **Tasks** carry an assignee, a status, a due date, comments, and
  links to pages and messages.
- **Artifacts** are versioned HTML and frozen documents, served only
  from the user-content origin, into a sandboxed iframe.
- **MCP** over streamable HTTP at `/mcp`, and over stdio through
  `cowork mcp`. The tools are search, pages, messages, tasks and
  artifacts.
- **Plugins.** The web app has one registry for block types, views,
  commands and panels, and the built-in features use it too.
- **The console** links here and keeps cross-organization consent.

## Ports

| Port | Adapters |
|---|---|
| `Store` | `postgres/` |
| `BlobStore` | `s3/` (RustFS, AWS S3, MinIO by configuration), `memory/` |
| `Hub` | `memory/`, `postgres/` |
| `DocEngine` | `goja/` |
| `Platform` | `http/` |
| `AgentDelivery` | `machine/` |

## What it asks of the platform

- `me`, and `getIdentity` on the machine listener: who a caller is.
- `checkPermissions`: whether the caller may take a cowork action on a
  cowork resource. The platform answers `cowork:page-read` and
  `cowork:page-write` on the organization and on a `cowork-page`, and
  every role grants both (#1663).
- `postMessage`: delivery to an agent, under a service principal the
  platform issues in #1664.
- An agent principal on the browser API: #1633.

## Rationale

- Yjs because concurrent and offline edits must converge without
  loss, and its reference implementation runs both in the browser and,
  through goja, in a pure-Go binary (ADR-0127 §Context).
- The platform decides permissions so that people, agents and AI
  services share one model (ADR-0127, ADR-0002).
- Ports and adapters because that is the repository's shape already,
  and the storage swap the owner asked for is one adapter
  (`stack-go.md` §Service layout, ADR-0069).

## Open questions

- **OAuth for remote AI services.** Needs `platform/` as an
  authorization server. Not scheduled.
- **Loading plugins at runtime.** Plugins are bundled at build time
  until a later decision.
- **The production user-content domain.** The owner chooses it.
  `COWORK_USERCONTENT_ORIGIN` names it.
