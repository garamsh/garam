# ADR 0132: A root folder is an executable unit named `<service>-<surface>`, and code libraries sit under `lib/`

> Status: accepted
> Date: 2026-10-28

Drafted for #1700, which reserves this number. The direction is the
owner's of 2026-10-28, recorded on #1655 (comment 6462762003). garam
is read at `401060bce36e8ce2c00fd82d13d155b88152ba59`; a claim below
that names no revision holds for it.

## Context

- **ADR-0125 named the root folders one by one.** `console/`,
  `site/` and `design/` each got a row, and cowork's web app grew
  inside the service, at `cowork/web/`. A name said which service a
  folder served only where the service is the platform's and the
  reader knew it.
- **One service has several surfaces, and more are coming.** cowork
  already has a server and a web app. An Android or Flutter client is
  planned, and the platform has a server, a console and a client CLI.
- **Two folders hold code another unit links, and neither is a
  service.** `design/` is the design system both web apps import, and
  `platform/client/` (`@garam/platform-client`) is the client of the
  platform's API that they call it through. `platform/sdk/` holds
  `errorx` and `grn`, which both Go modules import, and `clisession`,
  a CLI's sign-in to the platform, which only cowork's CLI imports.
- **The client CLI is a binary inside the server's module.**
  `platform/cmd/garam` builds one binary that serves (`serve`) and is
  also the client a person runs (`login`, the generated API
  commands). `platform/internal/cli` holds both.
- **ADR-0125 ruled out `libs/` as a catch-all.** `style.md` has since
  stated the line the owner draws (#1691). A folder grouping packages is
  not a catch-all when each package in it is named for its own
  responsibility.

## Decision

**A top-level folder is one of three things: a service's own tree, an
executable unit named `<service>-<surface>`, or a supporting
responsibility. A code library that more than one unit links sits under
`lib/`, in a folder named for what it is.**

1. **Executable units.** A surface a person or a machine runs — a web
   app, a client binary, a mobile app — is a folder at the root named
   for its service and its surface:

   | Folder | Was | Surface |
   |---|---|---|
   | `platform-web/` | `console/` | the console, at `console.garam.sh` |
   | `site-web/` | `site/` | the landing page and sign-in, at `garam.sh` |
   | `cowork-web/` | `cowork/web/` | cowork's web app |
   | `platform-cli/` | part of `platform/cmd/garam` | the client CLI |

   A service's server stays in the service's own folder, its binary
   under `cmd/` as `stack-go.md` lays a module out: `platform/` and
   `platform/cmd/garam`, `cowork/` and `cowork/cmd/cowork`. The site is
   named for the platform's service, because sign-in is the
   platform's, and keeps `site` as its own name.
2. **`lib/` holds code libraries only.** Each package in it is named
   for its responsibility, and none builds a binary:

   | Folder | Was |
   |---|---|
   | `lib/design/` | `design/` |
   | `lib/platform-sdk-ts/` | `platform/client/` |
   | `lib/platform-sdk-go/` | `platform/api/client.gen.go` |
   | `lib/grn/` | `platform/sdk/grn` |
   | `lib/errorx/` | `platform/sdk/errorx` |
   | `lib/clisession/` | `platform/sdk/clisession` |

   `lib/clisession` stays a library rather than moving into
   `platform-cli/`, because cowork's CLI signs in to the platform
   through it, and a unit does not import another unit's code.
3. **An SDK is generated from its service's published API.**
   `lib/platform-sdk-ts` and `lib/platform-sdk-go` are generated from
   `platform/api/openapi.yaml`, by openapi-typescript and
   oapi-codegen, and `check-gen` holds them to it. Each hand-writes
   only what generation cannot express. A client unit reaches a server
   only through its service's SDK: `platform-cli/` calls the platform
   through `lib/platform-sdk-go` and never imports `platform/internal`.
4. **What ADR-0125 keeps.** Every boundary in its point 4 stands, with
   `lib/` in the place `platform/sdk/` held: a service is reached
   through its published API specification or the libraries in
   `lib/`. Services' own folders are still created by their first pull
   request, and the root still holds no Go package.

#1700 builds this in two pull requests. The first moves the web
packages (`platform-web/`, `site-web/`, `cowork-web/`, `lib/design/`,
`lib/platform-sdk-ts/`). The second moves the Go libraries and splits
`platform-cli/` out of `platform/cmd/garam`.

## Consequences

- A folder's name says which service it belongs to and what runs from
  it. A new surface is a new root folder whose name is decided, and
  the service's own folder holds only its server.
- `lib/` is the one place to look for code shared between units. A
  package there that only one unit imports belongs to that unit.
- The SDKs follow the specification by construction. A change to
  `openapi.yaml` changes them in the same pull request or fails
  `check-gen`.
- Splitting the CLI from the server supersedes ADR-0120 in part, on
  point 1: the user commands leave the `garam` binary for
  `platform-cli/`'s, and keep their names and the generated client.
  The platform's server keeps ADR-0003's one binary for its processes.
- This supersedes ADR-0125 in part, on the table of top-level folders
  and on `platform/sdk/` as the home of what services share.
- Every path citation outside ADR bodies moves with the folders, and
  the pnpm importer and `tooling/ci/modules` names change with them.
  Work in flight on those paths rebases after the move.
- Ruled out: a root by kind (`apps/`, `services/`); keeping a web app
  inside its service's folder; a `pkg/` beside `lib/`; and putting a
  binary under `lib/`.

## Signs the decision was wrong

- Root folders multiply into single-file surfaces, or a pull request
  routinely changes a service and all of its `<service>-<surface>`
  folders together, so the surface is not the unit that changes.
- `lib/` gains packages one unit imports, or a package named for
  nothing in particular, which is the catch-all ADR-0125 ruled out.
- A generated SDK needs more hand-written wrapping than generated
  code, so the specification does not describe what the clients use.
- `platform-cli/` keeps reaching for the server's types that the SDK
  does not carry, so the client and the server were one unit after all.
