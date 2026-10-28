# ADR 0127: cowork is a service where people and agents co-edit, message and work through tools an AI can use

> Status: accepted; ruling out a participant list is superseded by
> [ADR-0128](./0128-a-cowork-channels-participants-narrow-what-the-platform-allows-and-grant-nothing.md);
> the client commands in the `cowork` binary (point 1) and
> `cowork mcp` (point 7) are superseded by
> [ADR-0133](./0133-garam-is-the-one-client-cli-and-carries-every-services-commands-from-garam-cli.md)
> Date: 2026-10-27

Drafted for #1662, which reserves this number. The direction is the
owner's of 2026-10-27, recorded on #1662 and #1655. garam is read at
`5b596c07962d557b9cced6b867c279fd9371833b`; a claim below that names
no revision holds for it.

## Context

- **What the owner asked for.** cowork is built like Notion. Its core
  values come first, and the rest of Notion's features follow:
  - real-time co-editing;
  - tools an AI can use (MCP) as the heart of the product;
  - messaging, so that people and agents collaborate;
  - the record of work: tasks, and the history of all of the above.

  It also needs:
  - artifacts (HTML and documents) beside Markdown;
  - a single server binary, and a client CLI;
  - an MCP server for services such as Claude and ChatGPT;
  - a web app that features plug into;
  - storage that RustFS or S3 can back, by configuration alone;
  - a console that only links to it.
- **Where it sits.** ADR-0125 makes it a service folder, `cowork/`,
  with its own module, binary and database, at `cowork.garam.sh`. It
  reaches `platform/` only through `platform/`'s published API.
  Content users publish is served from a separately registered domain.
- **What `platform/` offers today.**
  - `me` answers who a session belongs to.
  - `checkPermissions` answers allow or deny for the caller, an
    action and a resource. It answers only from a fixed table of
    actions and resource kinds
    (`internal/api/permission_check.go:19-85`). It refuses a kind it
    does not know with 400, and answers an action it does not define
    with `deny`.
  - On the machine listener, `getIdentity` answers who a machine token
    belongs to, and `postMessage` places a message on an agent's
    queue. The adapter beside the agent claims it from there
    (ADR-0013, `adapter.md`).
  - An agent cannot yet call the browser API as itself. That is #1633
    (ADR-0123).
- **The shape to follow.** The repository already has ports and
  adapters, under other names:
  - a domain declares the interfaces it depends on
    (`stack-go.md` §Service layout);
  - each technology's implementation sits outside the domain, in a
    package named for the technology (ADR-0069);
  - one composition site wires them (`structure.md` §Where the
    composition root lives).
- **The real-time constraint.** Concurrent editing that never loses
  an edit, including edits made offline, is what CRDTs are for. Yjs
  is the reference CRDT for rich-text editors (ProseMirror / TipTap
  bindings), and its canonical implementation is JavaScript. The
  server must also read and write documents itself: for MCP edits by
  agents, for search, and for exports. The platform's images build
  with `CGO_ENABLED=0` (`deploy/local/Dockerfile:34`), so a binding
  that needs cgo would break the single static binary.
- **What goja costs, measured.** `yjs` 13.6.33, bundled by esbuild to
  ES2015 (256 KB), runs in goja once `crypto.getRandomValues` is
  supplied from Go's `crypto/rand`. On the session's container,
  shared with a test run:
  - the bundle loads in 40 to 58 ms;
  - appending a paragraph to a document kept open in the engine
    costs about 12 ms, and to one decoded afresh from its state
    each time, about 70 ms;
  - encoding the state of a 1,200-paragraph document costs 143 ms.

## Decision

**cowork is one Go binary that serves pages co-edited through Yjs,
messages, tasks and artifacts to a pluggable web app, a CLI and an MCP
server. It asks `platform/` who a caller is and what the caller may do,
and keeps everything else in its own database and blob store, behind
ports.**

1. **One binary, server and client.**
   - `cowork serve` runs the HTTP API, the real-time WebSocket
     endpoint and the MCP endpoint.
   - `cowork migrate` applies its migrations.
   - The rest are client commands: `login`, `pages`, `messages`,
     `tasks`, `artifacts`, and `mcp`, a stdio bridge for local MCP
     clients.
   - The client follows ADR-0122's contract: `-o json`, its exit codes,
     and secrets written to files alone.
2. **Ports and adapters**, in the repository's existing shape. Its
   ports and their first adapters:

   | Port | First adapters |
   |---|---|
   | `Store` | `postgres/` |
   | `BlobStore` | `s3/`, which serves RustFS, AWS S3 and MinIO by endpoint, credentials and bucket alone; `memory/` for tests |
   | `Hub`, which fans live updates out to connected clients | `memory/` for one instance, `postgres/` (`LISTEN`/`NOTIFY`) for several |
   | `DocEngine`, which reads and writes a Yjs document on the server | `goja/` |
   | `Platform`, which asks who a caller is and what it may do | `http/` |
   | `AgentDelivery`, which reaches an agent's runtime | `machine/` |

   Nothing outside an adapter imports a driver or an SDK.
3. **Real time: Yjs, with the server as a relay that also understands
   documents.**
   - Each page is one Yjs document. The web editor is TipTap
     (ProseMirror) with its Yjs binding.
   - The server speaks the Yjs sync and awareness protocol over
     WebSocket. It relays updates between clients through the `Hub`,
     and keeps every update in an append-only log that it compacts
     into snapshots.
   - Where the server must read or write a document itself (MCP
     edits, text for search, Markdown and HTML export), `DocEngine`
     runs the Yjs reference implementation, bundled to ES2015, inside
     goja, a JavaScript engine written in Go. That keeps the binary
     pure Go and the merge rules identical to the clients'.
   - An MCP edit becomes a Yjs update like any other, so open editors
     merge it live.
4. **Data model.**
   - A workspace belongs to one platform organization.
   - It holds a page tree. A page's content is a document whose
     top-level nodes are blocks.
   - Channels (public or private), direct messages, and threads under
     any message.
   - Tasks, with an assignee (a person's or an agent's GRN), a status,
     a due date, comments, and links to pages and messages.
   - Artifacts, each a sequence of immutable versions.
   - Every object has a GRN under the organization (ADR-0001):
     `cowork-page`, `cowork-channel`, `cowork-task`, `cowork-artifact`.
5. **Authorization is the platform's.**
   - cowork's actions (`cowork:page-read`, `cowork:page-write`,
     `cowork:channel-read`, `cowork:channel-post`, `cowork:task-write`,
     `cowork:artifact-write`, and the rest the milestones name) and
     its resource kinds join the table `checkPermissions` answers
     from.
   - The organization roles' statements grant them to members by
     default (ADR-0002), and policies narrow or widen them like any
     other action.
   - cowork asks `checkPermissions` with the caller's own credential
     before every read and write, batched per request. #1663 carries
     the platform change.
   - An agent is decided the same way, once #1633 lets it present
     itself to the browser API. Until then, no agent acts in cowork
     as itself.
6. **Messaging reaches agents through the platform's queue.**
   - A message in cowork that mentions an agent, or that is posted in
     a direct message with one, is also posted to that agent's queue
     through `AgentDelivery`, under a service identity that
     `platform/` issues to cowork.
   - The agent answers through cowork's MCP tools, as itself, and the
     answer lands in the thread.
   - cowork is the record of the conversation. The queue stays the
     transport to runtimes and keeps ADR-0013's guarantees.
   - The service identity is a new kind of machine principal, built in
     #1664.
7. **MCP is how an AI works in cowork.**
   - The endpoint speaks MCP's streamable HTTP transport, at
     `cowork.garam.sh/mcp`. The CLI's `cowork mcp` bridges stdio for
     local clients.
   - The tools: `search`, `read_page`, `create_page`, `update_page`,
     `list_channels`, `read_messages`, `post_message`, `create_task`,
     `update_task`, `create_artifact`, `update_artifact`.
   - A caller authenticates with the bearer `platform/` issues: a
     person's CLI session (ADR-0120), or an agent's token. OAuth for
     remote AI services that require it needs `platform/` to act as an
     authorization server. That is a later issue, and #1665 ships
     without it.
8. **Artifacts are served from the user-content origin.**
   - An HTML artifact is served only from the origin
     `COWORK_USERCONTENT_ORIGIN` names. That is a separately registered
     domain in production, which the owner chooses, and another port
     on localhost in development.
   - It is served into a sandboxed iframe, under a
     Content-Security-Policy that allows no request to cowork's own
     origin.
   - A document artifact is a page exported and frozen as one version.
   - Versions are immutable blobs in the `BlobStore`.
9. **Plugins.**
   - The web app has one registry. A plugin registers block types
     (TipTap extensions), views, commands and side panels.
   - Every built-in feature registers through it, the same way a
     plugin outside the core package would.
   - Plugins are bundled at build time from a manifest. Loading them
     at runtime is not decided here.
10. **The console** links to cowork in its navigation. Cross-
    organization message consent stays there, since it is an
    authorization setting. No console page moves, because none
    carries collaboration today.
11. **Milestones.**
    - #1663: the binary, pages and real-time editing.
    - #1664: messaging.
    - #1665: MCP and the CLI.
    - #1666: tasks and the console link.
    - #1667: artifacts and the blob store.

None of this is built. [cowork.md](../cowork.md) describes it, and
each milestone's pull request updates that document.

## Consequences

- People, agents and AI services act through one permission model,
  the platform's, and every act is recorded under the actor's own GRN.
- Server-side document work goes through a JavaScript engine. It is
  slower than native code, and is kept off the relay's hot path: the
  relay forwards updates without decoding them.
- `checkPermissions` gains a call per request. A slow platform slows
  cowork, and cowork fails closed when the platform is unreachable.
- `platform/` changes in three places: the permission table (#1663),
  a service principal (#1664), and later an OAuth authorization
  server.
- An agent cannot use cowork until #1633 lands.
- Ruled out:
  - operational transform, which needs a central transform of every
    operation and handles offline edits worse;
  - a Node sidecar for Yjs, a second runtime beside the binary;
  - Automerge and other cgo bindings, which break the static build;
  - cowork keeping its own access lists, a second permission model
    beside the platform's.

## Signs the decision was wrong

- Edits are lost or reordered under concurrent or offline editing, or
  an MCP edit fails to merge into an open editor.
- goja runs the Yjs bundle too slowly for MCP edits on large pages, or
  diverges from the browser's results on the same updates.
- `checkPermissions` latency dominates cowork's response times.
- Agents cannot complete their tasks through the MCP tools alone, and
  reach for an API the tools do not cover.
- A plugin needs to reach past the registry into core code.
