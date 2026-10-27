# cowork

Where people and agents write together in real time, message each
other, and keep the record of their work, with tools an AI can use at
its centre
([ADR-0127](../../docs/architecture/adr/0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)).

**Built so far: pages and real-time editing (#1663), messaging
between people (#1664), and MCP and the client CLI (#1665).**
Delivery to agents waits on the owner's decision on cowork's principal
(#1655), and an agent acting as itself on #1633. Tasks and the
console's link (#1666), and artifacts and the blob store (#1667), are
not built, there is no image, and nothing deploys cowork. Each
milestone's pull request updates this document.

## Contents
- What is built
- Ports
- What it asks of the platform
- Real-time editing
- Messaging
- MCP
- The client CLI
- Connecting an AI client
- Running it
- Rationale
- Open questions

## What is built

- **One binary**, `cowork` (`cmd/cowork`). `cowork serve` runs the
  HTTP API, the sync and live endpoints, and the MCP endpoint;
  `cowork migrate` applies `migrations/`; `cowork probe <url>` is the
  healthcheck. The rest are client commands, `cowork mcp` among them.
- **The page tree** (`internal/page`, `api/openapi.yaml`). A page sits
  under at most one parent and is ordered among its siblings; deleting
  one deletes the pages beneath it and every page's content.
- **Real-time editing** (`internal/collab`). Each page is a Yjs
  document, synced over a WebSocket in y-websocket's protocol, kept as
  an update log compacted into a snapshot.
- **Messaging** (`internal/channel`). Public and private channels,
  direct messages, threads, mentions of people and agents, links to
  pages, full-text search, and each channel's messages live over a
  WebSocket.
- **MCP** (`internal/mcp`). The tools an AI uses to read and edit
  pages and to read and post messages, as its caller.
- **The client CLI** (`internal/cli`). The tools as commands, and
  `cowork mcp`, a stdio bridge for local MCP clients.
- **The web app** (`web/`). Every block type is a plugin in its
  registry.
- **Not yet:** the Postgres hub several instances need, and the image.

## Ports

| Port | Package | Adapters |
|---|---|---|
| `page.Repository` | `internal/page` | `postgres/`, `memory/` |
| `collab.UpdateLog` | `internal/collab` | `postgres/` |
| `channel.Repository` | `internal/channel` | `postgres/`, `memory/` |
| `fanout.Hub` | `internal/fanout` | `memory/` (one instance) |
| `collab.DocEngine` | `internal/collab` | `goja/` |
| `platform.Platform` | `internal/platform` | `http/` |

`internal/cli` (`NewHandler`) is the one place they are wired. The
`BlobStore` and `AgentDelivery` ports come with the milestones that
need them.

## What it asks of the platform

- `me`: who presented a credential. cowork passes the platform's
  session cookie or a CLI session's bearer as presented, and reads a
  bearer alone where there is one.
- `checkPermissions`, as that same caller: `cowork:page-read` and
  `cowork:page-write`, on the organization for its page tree and on a
  `cowork-page` for one page. Every role grants both (#1663).
- cowork answers 503 whenever the platform cannot be reached, and
  decides nothing alone.
- `checkPermissions` for messaging: `cowork:channel-read` and
  `cowork:channel-post` on the organization and on a
  `cowork-channel` (ADR-0128).
- Not yet: `postMessage` to an agent's queue, under a principal the
  owner has yet to decide (#1655), and an agent principal on the
  browser API (#1633). Until then a message's agent mentions are kept
  and answered as `undeliveredMentions`, and no agent acts in cowork
  as itself.

## Real-time editing

- **The sync endpoint**, `GET /orgs/{org}/pages/{page}/sync`. The
  upgrade is answered as reading the page is: 401, 403 or 404 before
  any WebSocket. A browser may open it only from cowork's own host or
  a host `COWORK_WEB_ORIGINS` names, because the session cookie rides
  along.
- **Writes** are `cowork:page-write` on the page, asked once at the
  upgrade. A connection without it reads live and is answered
  y-websocket's permission-denied message for any edit, which is
  neither kept nor relayed. A policy change reaches an open connection
  when it reconnects.
- **What the server does with the protocol.** It sends its state
  vector on connect, answers a client's state vector with what the
  client lacks, and keeps and relays every update it accepts. An
  update that does not decode is refused before it is kept. Awareness
  (cursors, presence) is relayed and never kept.
- **Storage.** Updates are appended in order (`page_updates`). After
  200 follow a page's snapshot, the next update folds them into a new
  one (`page_snapshots`). A load reads the snapshot and the updates
  after it in one transaction, so a compaction between the two reads
  loses nothing.
- **The engine.** `cowork/engine` bundles Yjs into
  `internal/collab/goja/engine.gen.js`, which `make gen` rebuilds and
  `make check-gen` holds to its source. The relay's path runs only
  Yjs's update functions, which decode no document. Reading a page's
  blocks, and the edits the server makes for MCP, decode one.

## Messaging

- **Who reads what** (ADR-0128). The platform's allow first, then,
  for a private channel or a direct message, the participant list,
  which only narrows. A private channel's participants add others; a
  direct message's are fixed when it starts; leaving takes the caller
  off.
- **Threads.** A reply names the message that starts its thread, and
  a reply to a reply joins the same thread. A channel lists the
  messages that start threads, each with its reply count.
- **Links.** A message links pages its author may read, and a page
  lists the messages that link it, in channels the reader may read. A
  page links a message by its address, as it links anything.
- **Search** is Postgres full-text search over message bodies, in its
  language-neutral configuration, within the channels the caller may
  read.
- **Live.** `GET /orgs/{org}/channels/{channel}/live` carries each
  message posted there as one text frame, the API's `Message`. Reading
  is decided at the upgrade, as for a page's sync.

## MCP

- **The endpoint**, `POST /mcp`, in MCP's streamable HTTP transport,
  stateless: every request is identified on its own, and answers as
  JSON.
- **It takes a bearer**, a person's CLI session (ADR-0120), and not
  the session cookie, so a page on another site cannot call the tools
  as the browser's visitor. A request without one is answered 401
  before any tool runs.
- **The tools.**

  | Tool | What it does | Asks |
  |---|---|---|
  | `list_workspaces` | the organizations the caller belongs to | `listOrgs` |
  | `list_pages` | the page tree the caller may read | `cowork:page-read` |
  | `search` | page titles and messages | `cowork:page-read`, `cowork:channel-read` |
  | `read_page` | a page as Markdown and as numbered blocks | `cowork:page-read` |
  | `create_page` | a page, with its blocks | `cowork:page-write` |
  | `update_page` | a new title, and edits to blocks by index | `cowork:page-write` |
  | `list_channels` | the channels and direct messages the caller may read | `cowork:channel-read` |
  | `read_messages` | a channel's messages | `cowork:channel-read` |
  | `post_message` | a message or a reply | `cowork:channel-post` |

  Every tool but `list_workspaces` takes the workspace as `org`. The
  task and artifact tools come with #1666 and #1667.
- **Each asks what the HTTP API asks**, through the same services, so
  the platform decides a tool call exactly as the call it mirrors.
- **An edit is a client's.** `update_page` opens the page in the
  server's engine, makes each edit, and keeps and relays its update as
  it would a browser's, so everyone with the page open sees it as it
  is made.
- **A refusal is the tool's error**, holding none of the resource.
  Its `_meta` names the error's kind under `garam.sh/kind`, which the
  CLI exits by. A failure of cowork's own names no cause; the cause is
  logged.

## The client CLI

- **The session is garam's CLI session.** `cowork login` signs in by
  the platform's device grant and keeps the session in garam's
  credentials file (`$XDG_CONFIG_HOME/garam/credentials`, mode 0600),
  so `garam login` and `cowork login` sign the same session in, and
  `cowork logout` signs both out. `platform/sdk/clisession` holds the
  file's format, its lock, and the grant.
- **Where it talks.** `--endpoint` or `COWORK_ENDPOINT` names cowork
  (default `https://cowork.garam.sh`), and `--platform` or
  `GARAM_ENDPOINT` the platform (default `https://garam.sh`). http is
  refused to any host but loopback.
- **The workspace** is `--org`, or the one `garam orgs use` saved.
- **The commands.** `login`, `logout`, `whoami`, `workspaces`, `pages
  list|read|create`, `channels`, `messages list|post`, `search` and
  `mcp`. Each but the first three calls the tool of its name, so the
  CLI does what an AI client may, as the same caller.
- **ADR-0122's contract.** `-o json` prints what the tool answered,
  messages go to stderr, and a command exits 3 not signed in, 4
  refused, 5 not found, 6 a conflict, 7 an invalid argument, 8
  unavailable, 2 used wrongly, and 1 otherwise.

## Connecting an AI client

Sign in first, with `cowork login`, and name the workspace with `garam
orgs use <org>` or tell the AI which one `list_workspaces` answers.

- **Claude Code.** `claude mcp add cowork -- cowork mcp`.
- **Claude Desktop**, in `claude_desktop_config.json`:

  ```json
  {"mcpServers": {"cowork": {"command": "cowork", "args": ["mcp"]}}}
  ```

- **A client that takes a remote server and a header.** The endpoint
  is `https://cowork.garam.sh/mcp` with `Authorization: Bearer
  <token>`. A session's access token lives an hour, so the stdio
  bridge, which refreshes it, is the one to use where the client can
  run a command.
- **Claude's and ChatGPT's remote connectors.** Both sign in to a
  remote server by OAuth, which needs the platform as an authorization
  server. Neither connects until that is built (Open questions).

## Running it

| Variable | Meaning |
|---|---|
| `COWORK_DATABASE_URL` | cowork's own Postgres database (required) |
| `COWORK_PLATFORM_URL` | the root of the platform's browser API (required) |
| `COWORK_LISTEN_ADDR` | default `:8090` |
| `COWORK_WEB_ORIGINS` | comma-separated host patterns allowed to open the sync WebSocket |
| `COWORK_MIGRATIONS_DIR` | default `migrations` |

`/healthz` answers while the process runs, and `/readyz` while its
database answers.

`make integration` runs `cowork/tests/integration` against its own
database, `cowork_test`, which `make db-up` creates. It drives the
server `NewHandler` wires against a substitute platform, with Yjs
editors in their own engines over real WebSockets.

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

- **The session cookie in production.** The platform's cookie is
  host-only, so it reaches cowork on `localhost` at any port, but not
  `cowork.garam.sh` from `console.garam.sh`. Scoping it to the parent
  domain is #1660's.
- **OAuth for remote AI services.** Claude's and ChatGPT's remote
  connectors sign in by OAuth, which needs the platform as an
  authorization server. Not scheduled.
- **Loading plugins at runtime.** Plugins are bundled at build time
  until a later decision.
- **The production user-content domain.** The owner chooses it.
  `COWORK_USERCONTENT_ORIGIN` names it.
