# ADR 0133: `garam` is the one client CLI, built from `garam-cli/`, and carries every service's client commands

> Status: accepted
> Date: 2026-10-28

Drafted for #1709, which reserves this number. The direction is the
owner's of 2026-10-28, recorded on #1655 (comments 6467209029 and
6467499105) and on #1709 (comment 6467499390). garam is read at
`cd548db0be4d7d0010d872f71ededdcb83a873c2`; a claim below that names
no revision holds for it.

## Context

- **Two client CLIs signed in to one session.** `platform-cli/` built
  `garam`, and the `cowork` binary was a server and a client at once:
  `serve`, `migrate` and `probe` beside `login`, `logout`, `whoami`,
  `workspaces`, `pages`, `channels`, `messages`, `tasks`, `search` and
  `mcp`, the stdio bridge to its MCP endpoint (ADR-0127 point 7).
  Both kept the session in one credentials file (ADR-0120), the
  platform CLI through its own code and cowork through
  `lib/clisession`, a second copy of the file's format, its lock and
  the device grant (#1680).
- **ADR-0123's token file would have been built twice.** Every client
  that lets an agent run as itself reads `GARAM_TOKEN_FILE`, and each
  service with its own client is one more reader.
- **cowork's client did not call cowork's API.** Its commands called
  the MCP tools by name. ADR-0132 point 3 has a client unit reach its
  server only through an SDK generated from that server's published
  API, and ADR-0122 derives a command for every operation rather than
  writing one per operation.
- **cowork's API does not carry a page's content.** Its specification
  says so: a page is a Yjs document edited over a WebSocket, which
  OpenAPI does not describe. Reading a page as Markdown, creating one
  with its blocks, and searching page titles exist only as MCP tools
  (`read_page`, `create_page`, `search`), and #1709 puts new cowork
  operations out of scope.
- **ADR-0132 point 1 names a unit `<service>-<surface>`.** A client
  carrying every service's commands belongs to no one service.

## Decision

**`garam` is the one client CLI. It is built from `garam-cli/` and
carries the client commands of every service on the platform; a
service's own binary carries its server commands alone.**

1. **The command tree.** A service's commands sit under a noun named
   for it: `garam cowork <noun>... <verb>`, derived from
   `cowork/api/openapi.yaml` by the same `cligen` and grammar ADR-0122
   uses for the platform's, with `internal/cli/cowork.yaml` placing
   each operation as `commands.yaml` does. The platform's commands
   stay at the top level. A cowork command presents the platform
   session's bearer to the cowork that `--cowork-endpoint`, else
   `COWORK_ENDPOINT`, else `https://cowork.garam.sh` names, held to the
   same terms as the platform endpoint because the token crosses it.
   The workspace is `--org` or the saved organization, as for any
   command. `garam whoami`, `login`, `logout` and `orgs list` answer
   what cowork's `whoami`, `login`, `logout` and `workspaces` did.
2. **The bridge is `garam mcp cowork`.** `mcp` groups the stdio
   bridges, one per service's MCP endpoint, so the `cowork` noun holds
   only what cowork's API and tools answer, a later service's bridge
   is `garam mcp <service>`, and the command a local client is given
   says what it serves.
3. **cowork's API is reached through `lib/cowork-sdk-go`**, generated
   from `cowork/api/openapi.yaml` by oapi-codegen as
   `lib/platform-sdk-go` is from the platform's, and held to it by
   `check-gen`. Each derived command builds its request through the
   service's SDK.
4. **What the API does not carry goes through cowork's MCP tools.**
   `garam cowork pages read`, `garam cowork pages create` and
   `garam cowork search` call `read_page`, `create_page` and `search`,
   and `createPage` is placed as served by that hand-written command.
   MCP is the second surface cowork publishes (ADR-0127 point 7), and
   the bridge already makes `garam` its client. This is the one
   exception to ADR-0132 point 3's SDK rule, and it holds only for an
   operation cowork's specification does not declare.
5. **One package holds the session.** `garam-cli/internal/session`
   holds the credentials file's format, its lock, the device grant
   and the refresh, built on `lib/platform-sdk-go`. `lib/clisession`
   and the CLI's second copy are deleted. Every command, the bridge
   included, presents the bearer `Session.Bearer` answers, so
   ADR-0123's `GARAM_TOKEN_FILE` is read there and nowhere else when it
   is built. The file's location and format do not change.
6. **`cowork` serves, migrates and probes.** Its binary carries no
   client command and exits 2 for a command used wrongly and 1 for any
   other failure.
7. **The folder is `garam-cli/`.** The one client is named for the
   product, as its binary is. This is an exception to ADR-0132
   point 1's `<service>-<surface>` naming, for a client that spans
   services; every other executable unit keeps it.

## Consequences

- One install and one sign-in reach every service, and a new
  service's commands are a specification, a placing file and an SDK,
  with no new binary or session code.
- `garam-cli/` depends on every service whose commands it derives:
  a change to `cowork/api/openapi.yaml` regenerates
  `lib/cowork-sdk-go` and `garam-cli/internal/cli/cowork.gen.go` in
  the same pull request or fails `check-gen`, and `make affected`
  selects `garam-cli` for it. cowork's suites build `garam` to drive
  its client commands, as the platform's do.
- Commands change names: `cowork pages list` is `garam cowork pages
  list`, `cowork messages post` is `garam cowork channels messages
  post`, `cowork tasks show` is `garam cowork tasks get` with `garam
  cowork tasks comments list`, `cowork workspaces` is `garam orgs
  list`, and a local MCP client runs `garam mcp cowork`. A derived
  command takes the API's arguments, so `--assignee` names a GRN where
  the tool took `me`. A body property named `body`, as a cowork
  message's is, takes the `--body` flag ADR-0122 gives the whole body;
  that command sends its body by its properties alone.
- This supersedes ADR-0127 in part, on point 7's `cowork mcp` and on
  point 1's client commands in the `cowork` binary, and ADR-0132 in
  part, on point 1, where `platform-cli/` is the platform's client
  alone, and on point 2, where `lib/clisession` stays a library.
- Ruled out:
  - a `cowork-cli/` beside the platform's client, which is a client
    and a session per service;
  - dropping cowork's client, since a local MCP client still needs a
    stdio bridge;
  - adding cowork operations that carry a page's content, which #1709
    puts out of scope and which the specification's own statement
    about a page's content argues against;
  - leaving page content out of the CLI, which takes from `garam`
    what the `cowork` binary answered;
  - `garam cowork mcp` for the bridge, which mixes a bridge into the
    commands derived from cowork's API.

## Signs the decision was wrong

- The hand-written commands that call MCP tools grow past the three
  here, so cowork's API does not describe what its client uses and
  the specification, not the CLI, is what needs to change.
- A service's client needs a session of its own, or a credential the
  platform does not issue, so one session for every service was the
  wrong unit.
- Changes to a service routinely break `garam-cli/` through its
  derived commands, so the services and the one client do not change
  apart.
- A second client spanning services appears, so naming the one client
  for the product did not settle where such a client lives.
