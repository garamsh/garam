# ADR 0125: The repository is a monorepo of services laid out by responsibility

> Status: accepted
> Date: 2026-10-27

Drafted for #1656, which reserves this number. The direction is the
owner's of 2026-10-27, recorded on #1655 with its reasons and the
alternatives rejected. garam is read at
`5b596c07962d557b9cced6b867c279fd9371833b`; a claim below that names
no revision holds for it.

## Context

- **garam becomes an agent-based collaboration platform** (#1655).
  Beside what it holds today — identity, organizations, authorization
  and the control side of agents — it will carry a collaborative
  document service, a product-management service and an AI model
  gateway. Each has its own dependencies, scaling and release cadence.
- **The repository has one shape for one service.**
  - One Go module, `github.com/garamsh/garam`, with 62 packages
    (`go list ./... | wc -l`), and one binary, `cmd/garam`
    (ADR-0003).
  - One Next.js app in `web/` that serves the landing page, sign-in
    and the console behind a same-origin `/api/*` rewrite (ADR-0009).
  - One gate, `make ci`, over the whole tree.
- **The agent side stays outside.** The runtime (`garamsh/sherlock`)
  and the cluster manager (`garamsh/garam-agent-operator`) are public
  repositories that may be open-sourced, while this one may become
  private, and visibility is per repository. BYO operator stays a
  requirement, so ADR-0012's model holds.
- **What the shape must survive.** Services will be added without a
  plan for each one. A change should touch one folder, a folder
  should have one owner, and CI should read the folder.

## Decision

**The repository is a monorepo of services. Each top-level folder is
one responsibility, a Go module or web package sits inside the folder
of the service that owns it, and services meet only through published
contracts.**

1. **Top-level folders.**

   | Folder | Holds |
   |---|---|
   | `platform/` | identity, organizations, authorization, agent control, and the machine listener: today's garam, binary included |
   | `console/` | every management screen, served at `console.garam.sh` |
   | `site/` | the landing page and sign-in, served at `garam.sh` |
   | `design/` | the design system both web apps import |
   | `tooling/` | lint and type presets, and CI scripts |
   | `deploy/` | the chart and the local compose stack |
   | `docs/convention/` | the rules for the whole repository |

   There is no folder for code without an owner (`shared/`, `libs/`,
   `common/`). A thing several services use belongs to the service
   that defines it, and is published from there: `grn` and `errorx`
   become `platform/sdk/`, a module of their own, when a second module
   first needs them.
2. **A service's folder is created by its first pull request.**
   `ai-gateway/`, `cowork/` and `product/` are named here, and none
   exists until work on it starts.
3. **Workspaces are committed at the root.** `go.work` names every Go
   module and `pnpm-workspace.yaml` every web package. The root holds
   no Go package.
4. **Boundaries.** Each is stated in `structure.md` §Services as a
   rule a reviewer checks a diff against:
   - a service owns its database, and no table is read or written by
     another service;
   - a service calls another through that service's published API
     specification or `platform/sdk/`, never its `internal/` or its
     database;
   - dependencies point toward `platform/`, and none points back;
   - services authenticate to one another with tokens `platform/`
     issues, never by network position;
   - `console/` calls only the services' public management APIs;
   - management endpoints and runtime endpoints are separate
     deployments.
5. **Domains.** `garam.sh` (site), `console.garam.sh`,
   `cowork.garam.sh`, `product.garam.sh`, `agents.garam.sh` (the
   machine listener) and `ai.garam.sh` (the model gateway). Content
   that users publish is served from a separately registered domain,
   so that it never sits inside the session cookie's scope.
6. **What earlier ADRs keep.**
   - ADR-0003: one binary for the platform's processes. Its path
     becomes `platform/cmd/garam`, and other services have binaries
     of their own.
   - ADR-0009: each web app reaches its backend through a same-origin
     `/api/*` rewrite. Its location (`web/`) is superseded: the app
     becomes `console/` and `site/`.
   - ADR-0042, unchanged: the chart describes a deployment, and it
     stays at `deploy/charts/garam/`. A new service adds what it
     deploys to the chart, or a chart of its own, in its first pull
     request.

None of this is built. #1659 moves today's tree, #1660 splits the web
app, and #1661 makes CI run per affected module.

## Consequences

- A service is added, built, tested and deployed through its own
  folder. Ownership is the folder, and per-module CI (#1661) reads it.
- The move (#1659) rewrites the module path in 448 Go files and the
  path citations in the documents, except inside ADR bodies, which
  README rule 2 freezes. Work in flight on those paths waits for the
  move or rebases after it.
- `docs/convention/` assumes one module and one gate. #1657 proposes
  the changes, and the move waits for the PM's decision on them.
- Splitting the web app changes the session cookie's scope, which is
  #1660's to decide and record.
- A service cannot reach into another's tables, so a feature that
  spans services needs an API or an event first. That is the price of
  the boundary.
- Ruled out: a layout by kind (`services/`, `libs/`, `web/`), a
  catch-all shared folder, and merging the agent-side repositories
  into this one.

## Signs the decision was wrong

- Pull requests routinely change several service folders together,
  showing that the responsibilities were drawn in the wrong place.
- `platform/sdk/` changes in most pull requests, so per-module CI
  selects almost everything every time.
- `console/` cannot be built without importing a service's internal
  types, showing that management cannot be separated from the
  services it manages.
- One team's work regularly waits on another folder's owner. That
  would point to one service split in two.
