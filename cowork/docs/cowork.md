# cowork

Where people and agents write together in real time, message each
other, and keep the record of their work, with tools an AI can use at
its centre
([ADR-0127](../../docs/architecture/adr/0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)).

**Built: pages and real-time editing (#1663), messaging between
people (#1664), MCP and the client CLI (#1665), tasks (#1666), and
artifacts and the blob store (#1667).** Delivery to agents is decided
and unbuilt (§Delivery to agents), and so is an agent acting as itself
(ADR-0123). There is no image, and nothing deploys cowork.

## Contents
- What is built
- Ports
- What it asks of the platform
- Real-time editing
- Messaging
- Delivery to agents
- Tasks
- Artifacts
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
  healthcheck. It carries no client command: those are garam's
  (§The client CLI).
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
- **Tasks** (`internal/task`). Tasks with an assignee, a status, a
  due date, comments, and links to pages and messages, as a list and a
  board.
- **Artifacts** (`internal/artifact`, `internal/blob`,
  `internal/usercontent`). HTML pages and documents frozen from
  pages, each a sequence of immutable versions in the blob store, an
  HTML one served only from the user-content origin.
- **MCP** (`internal/mcp`). The tools an AI uses to read and edit
  pages, to read and post messages, to work tasks and to publish
  artifacts, as its caller.
- **The client CLI** is `garam`'s `cowork` commands and
  `garam mcp cowork`, a stdio bridge for local MCP clients, built in
  `garam-cli/` (§The client CLI).
- **The web app** (`cowork-web/`, at the root). Every block type is a
  plugin in its registry.
- **Not yet:** the Postgres hub several instances need, and the image.

## Ports

| Port | Package | Adapters |
|---|---|---|
| `page.Repository` | `internal/page` | `postgres/`, `memory/` |
| `collab.UpdateLog` | `internal/collab` | `postgres/` |
| `channel.Repository` | `internal/channel` | `postgres/`, `memory/` |
| `task.Repository` | `internal/task` | `postgres/`, `memory/` |
| `artifact.Repository` | `internal/artifact` | `postgres/`, `memory/` |
| `blob.Store` | `internal/blob` | `s3/` (RustFS, AWS S3, MinIO), `memory/` |
| `fanout.Hub` | `internal/fanout` | `memory/` (one instance) |
| `collab.DocEngine` | `internal/collab` | `goja/` |
| `platform.Platform` | `internal/platform` | `http/` |

`internal/cli` (`NewHandler`, `NewUserContentHandler`) is the one
place they are wired. The `AgentDelivery` port comes with delivery to
agents.

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
- `checkPermissions` for tasks: `cowork:task-read` and
  `cowork:task-write` on the organization and on a `cowork-task`; for
  artifacts, `cowork:artifact-read` and `cowork:artifact-write` on the
  organization and on a `cowork-artifact`.
- Not yet: `postMessage` to an agent's queue, under the
  organization's cowork principal (§Delivery to agents), and an agent
  on the browser API (ADR-0123). Until then a message's agent mentions
  are kept and answered as `undeliveredMentions`, and no agent acts in
  cowork as itself.

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

## Delivery to agents

**Unbuilt (#1664).**
[ADR-0129](../../docs/architecture/adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
decides how cowork reaches an agent's queue. Today it keeps the
mentions and delivers none.

- **One principal per organization**,
  `grn:<org>:default:service:cowork`, written by the platform with the
  organization. cowork derives it and installs nothing.
- **One deployment key.** cowork keeps its private half in an
  owner-only file and signs an assertion naming the organization's
  principal, which the platform's machine listener exchanges for a
  one-hour token.
- **What the token does.** `postMessage` to an agent of that
  organization, `getMessageStatus` for what it sent, `getIdentity`,
  and nothing else; nothing on the browser API, where cowork acts only
  as its caller.
- **`AgentDelivery`** posts each mention, and each direct message
  with an agent, under that principal, the body naming the author and
  the message's address in cowork. #1666 posts task-assignment notices
  the same way. The agent answers through the tools as itself.
- **Stopping one organization's cowork** is the deployment revoking
  its principal. The platform then refuses every `cowork:` action on
  that organization, so the workspace stops for everyone in it.

## Tasks

- **A task** has a title, a description, a status (`todo`, `doing`,
  `done`), an assignee, a person's or an agent's GRN, and a due date.
  Each change is the caller's `cowork:task-write` on the task, and a
  comment is the same act.
- **Links.** A task links pages and messages its writer may read: a
  page as `getPage` reads it, a message as reading its thread does.
  The task lists what it links, and `listTasks` with `page` or
  `message` lists the tasks that link one. A link is kept by ID and
  read when followed, so a reader who may not read its target sees
  that it is there and nothing of it.
- **Views.** `/{org}/tasks` lists them, and `?view=board` shows a
  column per status; a page and a message link to the tasks that link
  them, and a task made from either links it.
- **Agents.** An agent works a task through `list_tasks` (`assignee:
  me`) and `update_task`, which takes a comment with the change.
  Notifying an agent of an assignment waits on delivery to agents
  (#1664).

## Artifacts

- **Kinds.** An `html` artifact is a page of HTML, of at most 5 MiB. A
  `document` is a page's blocks, frozen as they stood when saved;
  saving again freezes the page again.
- **Versions.** Every save is a version of its own, numbered from 1,
  and none replaces another. A version's bytes are a blob under a key
  of its own, written once; the blob store refuses a second write to a
  key, so nothing kept is replaced even by a bug.
- **Where HTML is served** (ADR-0127 §8). Never from cowork's own
  origin. `viewArtifact` asks `cowork:artifact-read` as the viewer and
  answers an address on the user-content origin, signed for that
  version and good for ten minutes. The second listener `serve` starts
  there serves that version and nothing else. The web app loads it in
  an iframe sandboxed with `allow-scripts` alone, so the page runs in
  an opaque origin: no cookie, no storage, no reach into its parent.
  Its policy is `default-src 'none'`, with inline script and style,
  `data:` images and fonts, `connect-src 'none'` and `form-action
  'none'`, framed only by `COWORK_WEB_ORIGINS`. A script in it can
  call nothing, cowork's API included.
- **Embeds.** A page embeds an artifact as a block (`core/artifact`),
  and a message as one of its `artifacts`, each only one its author
  may read; each reader sees it as they may read it.
- **Agents** publish and revise through `create_artifact`,
  `update_artifact` and `read_artifact`, which reads an HTML version's
  source as data, never as a page.

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
  | `list_tasks` | tasks by status, assignee (`me` for the caller) or link | `cowork:task-read` |
  | `read_task` | a task and its comments | `cowork:task-read` |
  | `create_task` | a task, assigned and linked | `cowork:task-write` |
  | `update_task` | changes to a task, with a comment | `cowork:task-write` |
  | `comment_task` | a comment | `cowork:task-write` |

  | `list_artifacts` | the artifacts the caller may read | `cowork:artifact-read` |
  | `read_artifact` | a version: an html one's source, a document's Markdown | `cowork:artifact-read` |
  | `create_artifact` | an html page, or a page frozen as a document | `cowork:artifact-write` |
  | `update_artifact` | the next version | `cowork:artifact-write` |

  Every tool but `list_workspaces` takes the workspace as `org`.
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

cowork's client is `garam`, the one client CLI
([ADR-0133](../../docs/architecture/adr/0133-garam-is-the-one-client-cli-and-carries-every-services-commands-from-garam-cli.md)).
[cli.md](../../docs/architecture/cli.md) is authoritative for how it
signs in, what it keeps and the contract its commands keep.

- **The session is garam's.** `garam login` signs it in, and every
  cowork command presents its bearer.
- **Where it talks.** `--cowork-endpoint` or `COWORK_ENDPOINT` names
  cowork (default `https://cowork.garam.sh`), and `--endpoint` or
  `GARAM_ENDPOINT` the platform.
- **The workspace** is `--org`, or the one `garam orgs use` saved.
- **The commands.** `garam cowork ...` derives one per operation of
  `api/openapi.yaml`, through `lib/cowork-sdk-go`. `garam cowork pages
  read`, `pages create` and `search` call the tools of their names,
  since the API carries no page content.

## Connecting an AI client

Sign in first, with `garam login`, and name the workspace with `garam
orgs use <org>` or tell the AI which one `list_workspaces` answers.

- **Claude Code.** `claude mcp add cowork -- garam mcp cowork`.
- **Claude Desktop**, in `claude_desktop_config.json`:

  ```json
  {"mcpServers": {"cowork": {"command": "garam", "args": ["mcp", "cowork"]}}}
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
| `COWORK_BLOB_STORE` | `s3`, or `memory` for a development server whose artifacts go when it stops; unset keeps no artifacts |
| `COWORK_S3_ENDPOINT` | the store's URL; empty for AWS S3 itself |
| `COWORK_S3_REGION` | default `us-east-1` |
| `COWORK_S3_BUCKET` | the bucket (required for `s3`) |
| `COWORK_S3_ACCESS_KEY`, `COWORK_S3_SECRET_KEY` | the store's keys (required for `s3`) |
| `COWORK_S3_PATH_STYLE` | `true` to name the bucket in the path, as RustFS and MinIO expect |
| `COWORK_S3_CREATE_BUCKET` | `true` to create the bucket where it is missing |
| `COWORK_USERCONTENT_ORIGIN` | the user-content origin as browsers reach it; unset views no HTML artifact |
| `COWORK_USERCONTENT_ADDR` | where that origin listens, default `:8092` |
| `COWORK_USERCONTENT_SECRET` | the key its addresses are signed with, at least 32 bytes; the same on every instance |

Swapping the store is configuration alone. RustFS, as the deployment
runs it:

```sh
COWORK_BLOB_STORE=s3
COWORK_S3_ENDPOINT=http://rustfs:9000
COWORK_S3_BUCKET=cowork
COWORK_S3_PATH_STYLE=true
COWORK_S3_ACCESS_KEY=… COWORK_S3_SECRET_KEY=…
```

AWS S3 drops the endpoint and the path style and names its region
(`COWORK_S3_REGION=ap-northeast-2`); MinIO is RustFS's settings with
its own endpoint and keys.

`/healthz` answers while the process runs, and `/readyz` while its
database answers.

`make integration` runs `cowork/tests/integration` against its own
database, `cowork_test`, which `make db-up` creates, and the blob
store's contract against both adapters, the `s3` one over the RustFS
`make db-up` starts. It drives the
server `NewHandler` wires against a substitute platform, with Yjs
editors in their own engines over real WebSockets, and the `garam` it
builds from `garam-cli/` for the client commands.

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
- **The production user-content domain.** The owner chooses it, and
  registers it apart from garam's, so no cookie of garam's reaches it.
  `COWORK_USERCONTENT_ORIGIN` names it.
