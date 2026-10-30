# CLI

What the one client CLI, the `garam` binary `garam-cli/` builds, is
responsible for: how a person signs one in, how an agent runs it, what
it keeps between runs, what it calls, and what it does not decide. The
server binaries' commands are [overview.md](./overview.md)'s module
list, [deployment-contract.md](./deployment-contract.md)'s and
[cowork.md](../../cowork/docs/cowork.md)'s.

## Contents
- What it is
- The commands it derives
- Signing in
- Running as an agent
- What it keeps
- What it calls
- What it does not decide
- Not established
- Decisions this rests on

## What it is

`garam-cli/internal/cli` implements it, and
`garam-cli/cmd/garam` builds it into a client binary apart from the
server's `platform/cmd/garam`
([ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md),
[ADR-0132](./adr/0132-a-root-folder-is-an-executable-unit-named-service-surface-and-code-libraries-sit-under-lib.md)).
Both binaries are named `garam`; the client carries no server command
and the server no user command. It is the one client of every service
on the platform: cowork's client commands are its too, and the
`cowork` binary carries none
([ADR-0133](./adr/0133-garam-is-the-one-client-cli-and-carries-every-services-commands-from-garam-cli.md)).
It imports no service's `internal/`. The user commands sit at the top
level:

| Command | What it does |
|---|---|
| `garam login [--no-browser]` | signs the CLI in by the device grant |
| `garam logout` | revokes the CLI's session and forgets its tokens |
| `garam whoami` | names the user the session is for |
| `garam orgs use <org>` | saves the organization later commands act in |
| `garam orgs ...`, `groups ...`, `projects ...`, `members list`, `invitations ...` | organizations, their teams (the API's groups), projects, members and invitations, derived (§The commands it derives) |
| `garam operators ...` | the organization's operators, their keys, delegation and control endpoint, derived (§The commands it derives) |
| `garam agents list`, `garam agents get <agent>` | the organization's agents, with each one's name |
| `garam agents rename <agent> <name>`, `--clear` | names an agent, or clears its name ([ADR-0119](./adr/0119-a-managed-agent-carries-a-display-name-on-its-own-record-set-at-creation-and-renamed-on-agent-rename.md)) |
| `garam cowork ...` | cowork's pages, channels and their messages, tasks and artifacts, derived (§The commands it derives) |
| `garam cowork pages read <page>`, `pages create`, `search <query>` | a page's content as Markdown, a page created with its paragraphs, and pages and messages found, through cowork's MCP tools (§What it calls) |
| `garam mcp cowork` | serves cowork's tools to a local MCP client over stdio, as the session |

Every one of them takes `--endpoint`, `--org` and `--output`
(`-o table`, the default, or `-o json`, which writes the API's own
objects). `cowork` and `mcp cowork` also take `--cowork-endpoint`.

## The commands it derives

Every browser API operation becomes a command derived from
`platform/api/openapi.yaml` ([ADR-0122](./adr/0122-the-cli-derives-a-command-for-every-browser-api-operation-and-writes-a-secret-to-a-file-alone.md)), and every operation of
`cowork/api/openapi.yaml` one under `garam cowork` (ADR-0133).
`garam-cli/internal/cli/cligen` writes
`garam-cli/internal/cli/commands.gen.go` from the platform's
specification and `garam-cli/internal/cli/commands.yaml`, and
`garam-cli/internal/cli/cowork.gen.go` from cowork's and
`garam-cli/internal/cli/cowork.yaml`, which `make -C garam-cli gen`
runs and `make -C garam-cli check-gen` holds current.

- **Grammar.** `garam <noun>... <verb> [arguments]`:
  - the nouns are the path's static segments;
  - the verb is the operationId's first word;
  - the path parameters are the arguments, and an argument beginning
    with `-` follows `--`;
  - a path that ends in its own verb names it once
    (`invitations accept <token>`);
  - query parameters and body properties are kebab-case flags, an
    object one as JSON or `@file`, and `--body` sends the whole body,
    except where a body property is itself named `body`, as a cowork
    message's is: the flag is that property's.
  - `commands.yaml` renames the few whose derived name reads badly.
- **Sessions.** An operation the specification lets a caller reach
  with none (`security: []`, as `getInvitation` is) sends no session,
  so it runs signed out.
- **Output.** `-o json` is the API's own answer. A table shows the
  answer's scalar properties in declared order, and an answer with none
  prints as JSON.
- **Exit codes.** 0 success, 2 usage, 3 not signed in, 4 permission
  denied, 5 not found, 6 conflict, 7 invalid argument, 8 unavailable,
  and 1 for anything else. The server binary ends with 1 on any
  failure.
- **Secrets.** An answer `commands.yaml` lists a secret field of
  (`issueOperatorKey`, `createInvitation`, `mintOperationAuthority`) is
  written whole to the file `--out` names. The file is created at mode
  0600, never over an existing one, before the request is sent, and
  removed if garam refuses or cannot be reached. The command prints the
  answer without the secret and names the file on stderr; without
  `--out`, or with a file already there, it sends nothing.
- **Coverage.** Each of the platform's 83 operations and cowork's 28
  is derived, served by a hand-written command, excluded with its
  reason, or pending, and in exactly one of those. `cligen` refuses
  anything else, and so does
  `TestAPIOperations_PlaceEveryOperationOfTheSpecification`.

## Signing in

`garam login` requests a device code, prints the user code and the page
it is approved on, opens that page unless `--no-browser` is passed, and
polls until the code is decided or lapses. It honours `slow_down` and
the interval the answer carries, and keeps polling through a failed
request until the code's expiry, as RFC 8628 §3.5 has a client do. The
page is answered as the path `/device` and joined to the endpoint the
command talks to; the console serves it
([web-app.md](./web-app.md) §What it serves). What the server does
with each request is [sign-in.md](./sign-in.md) §The CLI's session.

A command presents the access token as a bearer. Where it lapses within
a minute, the command spends the refresh token for a new pair first,
and a refused refresh answers "not signed in; run garam login".
`garam logout` revokes the session on the server and drops the tokens
from the file. A session already revoked elsewhere is treated as signed
out.

## Running as an agent

**Unbuilt.**
[ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
lets an agent run the CLI as itself, with no sign-in and no browser
step. Today every command presents a person's session. When it is
built it is read where the session's bearer is answered,
`garam-cli/internal/session`, so cowork's commands and its bridge take
it with the platform's (ADR-0133).

- **`GARAM_TOKEN_FILE`** names a file holding the agent's browser
  token ([machine-identity.md](./machine-identity.md) §Browser tokens).
  Where it is set, every command reads that file, presents the token
  as a bearer and sends nothing from the credentials file.
- **Read per command, never written.** The CLI refreshes nothing; the
  agent's assignee replaces the file atomically before the token
  lapses. A file anyone but its owner can read is refused, as the
  credentials file is.
- **No session commands.** `garam login` and `garam logout` refuse
  while it is set, and a refused token exits 3, not signed in.
- **The API still decides.** The organization comes from `--org` as
  for anyone. garam refuses any but the agent's own, as it refuses the
  operations ADR-0123 keeps from an agent whatever its policies say.

## What it keeps

`garam-cli/internal/session` holds all of it: the file's format, its
lock, the device grant and the refresh, and the bearer every command
presents (ADR-0133).

- **The file.** `$XDG_CONFIG_HOME/garam/credentials`, or
  `~/.config/garam/credentials` where that is unset or relative. It
  holds, per endpoint, the session's tokens and their expiries, and the
  organization `garam orgs use` saved.
- **Its permissions.** It is written `0600` in a `0700` directory,
  replaced whole through a file created beside it, and refused when
  anyone but its owner can read it.
- **Concurrent commands.** A read-modify-write holds an exclusive lock
  on `credentials.lock` beside it, so two commands refreshing at once
  spend the refresh token once and the second reads the pair the first
  wrote.
- **Printing.** No command prints a token.
- **Logout keeps the org.** `garam logout` keeps the saved organization.

## What it calls

- **The browser API.** The CLI calls the browser API through
  `lib/platform-sdk-go`, the client and types oapi-codegen generates
  from `platform/api/openapi.yaml`, for every operation
  (`make -C lib/platform-sdk-go gen`; `check-gen` holds it current).
- **cowork's API**, through `lib/cowork-sdk-go`, generated from
  `cowork/api/openapi.yaml` on the same terms, as the platform session
  the command holds. cowork is the origin `--cowork-endpoint` names,
  else `COWORK_ENDPOINT`, else `https://cowork.garam.sh`, on the
  endpoint's terms below, since the session's tokens cross it too. The
  API is at `<origin>/api` (ADR-0135).
- **cowork's MCP tools** for what its API does not carry, a page's
  content: `pages read`, `pages create` and `search` call `read_page`,
  `create_page` and `search` at cowork's `/mcp`, and `mcp cowork`
  relays a local client's messages there. A tool's refusal exits by
  the kind its `_meta` names
  ([cowork.md](../../cowork/docs/cowork.md) §MCP).
- **The endpoint.** It comes from `--endpoint`, else `GARAM_ENDPOINT`,
  else `https://garam.sh`, and is an origin. It is `https`, or `http` to
  a loopback address alone, because a session's tokens cross it. The
  API is reached under it at `/api`, behind the console's rewrite
  ([web-app.md](./web-app.md) §How it reaches garam).
- **The organization.** It comes from `--org`, else the saved one, and a
  command that needs one refuses rather than guessing. `garam orgs use`
  reads the organization before saving it, so what is saved is one the
  session reaches.

## What it does not decide

Authorization is the API's: a command sends what the person asked for
and reports what the API answered. A refusal reaches the person as the
API's message under its kind.

## Not established

- **Most resource groups.** The operators and orgs groups are derived.
  Every other operation `commands.yaml` lists under `pending` has no
  command yet, and is enabled a resource group at a time (#1632), until
  the list is empty.
- **Other platforms.** The file lock is `flock(2)`, which builds where
  `syscall.Flock` does. No release publishes the binary for a platform
  without it.

## Decisions this rests on

- [ADR-0003](./adr/0003-single-cli-topology.md) — one binary carries
  every server command; the client's is its own (ADR-0132)
- [ADR-0005](./adr/0005-server-side-sessions.md) — a session is a row
  revoked by deleting it
- [ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md)
  — the CLI signs in by the device grant, holds a session as a bearer,
  and keeps it in one owner-only file
- [ADR-0122](./adr/0122-the-cli-derives-a-command-for-every-browser-api-operation-and-writes-a-secret-to-a-file-alone.md)
  — every browser API operation is a command derived from the
  specification, and a secret is written to a file alone
- [ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
  — an agent runs the CLI from a token file its assignee keeps
  current; unbuilt
- [ADR-0132](./adr/0132-a-root-folder-is-an-executable-unit-named-service-surface-and-code-libraries-sit-under-lib.md)
  — the user commands are a client binary of their own, calling the
  platform through a generated SDK
- [ADR-0133](./adr/0133-garam-is-the-one-client-cli-and-carries-every-services-commands-from-garam-cli.md)
  — `garam`, built from `garam-cli/`, is the one client of every
  service, with cowork's commands under `garam cowork`, its bridge as
  `garam mcp cowork`, and one package for the session
