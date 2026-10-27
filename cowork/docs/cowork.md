# cowork

Where people and agents write together in real time, message each
other, and keep the record of their work, with tools an AI can use at
its centre
([ADR-0127](../../docs/architecture/adr/0127-cowork-is-a-service-where-people-and-agents-co-edit-message-and-work-through-tools-an-ai-can-use.md)).

**Built so far: the server's pages and real-time editing (#1663).**
The binary serves the page tree and each page's sync; there is no web
app yet, no image, and nothing deploys it. Messaging (#1664), MCP and
the client CLI (#1665), tasks and the console's link (#1666), and
artifacts and the blob store (#1667) are not built. Each milestone's
pull request updates this document.

## Contents
- What is built
- Ports
- What it asks of the platform
- Real-time editing
- Running it
- Rationale
- Open questions

## What is built

- **One binary**, `cowork` (`cmd/cowork`). `cowork serve` runs the
  HTTP API and the sync endpoint, `cowork migrate` applies
  `migrations/`, and `cowork probe <url>` is the healthcheck. The
  client commands and `cowork mcp` come with #1665.
- **The page tree** (`internal/page`, `api/openapi.yaml`). A page sits
  under at most one parent and is ordered among its siblings; deleting
  one deletes the pages beneath it and every page's content.
- **Real-time editing** (`internal/collab`). Each page is a Yjs
  document, synced over a WebSocket in y-websocket's protocol, kept as
  an update log compacted into a snapshot.
- **Not yet:** the web app and its plugin registry, the Postgres hub
  several instances need, and the image.

## Ports

| Port | Package | Adapters |
|---|---|---|
| `page.Repository` | `internal/page` | `postgres/`, `memory/` |
| `collab.UpdateLog` | `internal/collab` | `postgres/` |
| `collab.Hub` | `internal/collab` | `memory/` (one instance) |
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
- Not yet: `postMessage` to an agent's queue (#1664), and an agent
  principal on the browser API (#1633). Until #1633, no agent acts in
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
  blocks, and the edits the server will make for MCP (#1665), decode
  one.

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
- **OAuth for remote AI services.** Needs the platform as an
  authorization server. Not scheduled.
- **Loading plugins at runtime.** Plugins are bundled at build time
  until a later decision.
- **The production user-content domain.** The owner chooses it.
  `COWORK_USERCONTENT_ORIGIN` names it.
