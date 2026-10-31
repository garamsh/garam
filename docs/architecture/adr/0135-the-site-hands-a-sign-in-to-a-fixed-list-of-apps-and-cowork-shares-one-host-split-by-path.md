# ADR 0135: The site hands a sign-in to a fixed list of apps, and cowork's server and web app share one host split by path

> Status: accepted; supersedes ADR-0131 in part, on decision 2's
> return path
> Date: 2026-10-30

Drafted for #1715, which reserves this number. The decisions are the
PM's of 2026-10-28, recorded on #1715 (comment 6468737020 and the two
answers after it), inside the owner's direction on #1655. garam is read
at `f452452770d1269cacddeb9505f620ca2fe3b290`; a claim below that names
no revision holds for it.

## Context

- **ADR-0131 expected cowork's web app to take the handoff.** Its
  §Consequences says the app needs the same two routes, and its §Signs
  the decision was wrong names an app that cannot take the handoff as
  it is. cowork's web app could not.
- **The site knew one app.** `site-web/src/app/handoff/route.ts` sent
  every code to `GARAM_CONSOLE_URL`.
- **A return path was a console path.** ADR-0131 decision 2 keeps it
  to `/console/...`. `safeReturnPath` in `lib/platform-sdk-ts` and in
  `platform/internal/api/return_path.go` drop anything else, and
  `oauth_states.return_path` carries the same check
  (`platform/migrations/000045_oauth_state_return_path.up.sql`). A
  cowork path was dropped at every step of a sign-in.
- **A destination cannot come from the request.** A code is not tied
  to an app. If the site sent a code to an origin the request named, an
  attacker could draw a state, send a victim to the site with it,
  receive the victim's code on their own origin, and redeem it with
  their own state.
- **cowork's server reads the cookie.** The browser opens a page's sync
  WebSocket and a channel's live one directly to the server, and the
  session cookie is host-only to the host it was handed to. So the
  server has to answer on that host, and `https://cowork.garam.sh` is
  already where `garam` and the MCP endpoint reach it.

## Decision

**The site hands a sign-in to a fixed list of garam's apps, the console
and cowork. A request names the app by an identifier, and the site's
configuration holds each app's origin. A return path is a path on that
app. cowork's server and web app share `cowork.garam.sh`, split by path
at the gateway.**

1. **The list.** A handoff names its app in the `app` parameter,
   `console` or `cowork`, and a request naming none is the console's.
   Each identifier maps to an origin in the site's configuration,
   `GARAM_CONSOLE_URL` and the new `GARAM_COWORK_URL`, and both are
   required. A request naming an app off the list is refused. It is
   never read as another app, because that would hand a sign-in to an
   app the caller did not ask for.
2. **The return path is per app.** This replaces decision 2's "The
   return path stays a `/console/...` path". A return path is an
   absolute path on the chosen app's own origin, under the rule that
   already held: no scheme, host, `//`, backslash, control character or
   dot segment, and at most 2048 bytes. The console keeps its
   `/console/...` constraint, and cowork's paths are its own. Both
   copies of the rule take the app, and
   `platform/internal/api/testdata/return-path-vectors.json` covers
   both apps. Decision 2's other points stand.
3. **The app rides through a sign-in.**
   - The site's `/login` and its password sign-in carry `app`.
   - `POST /auth/login/{provider}` takes `app` in its form body and
     refuses one off the list.
   - For cowork, the destination held on `oauth_states` is the site's
     `/handoff?app=cowork&next=<path>`, and the column's check takes
     exactly that form (`platform/migrations/000065_*`). The full rule
     is applied on the write and again on the read. Holding no state,
     that address sends the visitor to cowork's `/auth/sign-in`, which
     starts the handoff.
   - No `app` column is added: it would change a store interface for a
     value the path already carries.
   - `/register` stays the console's. An invitation is the console's,
     so a cowork path does not travel there.
4. **The handoff endpoints do not change.** `POST /auth/handoff` and
   `POST /auth/handoff/redeem` are as ADR-0131 decision 3 and 4 state,
   since a code is not tied to an app.
5. **cowork's host is split by path at the gateway.** `/api/`, with the
   prefix stripped, and `/mcp` go to cowork's server, and everything
   else to cowork-web, so the handed-off cookie rides the sync and live
   WebSockets. The client's endpoint stays an origin, and `garam` calls
   the API at `<origin>/api` and MCP at `<origin>/mcp`.
   [deployment-contract.md](../deployment-contract.md) §cowork's host
   states the split, and `deploy/local/cowork-gateway.Caddyfile` mirrors
   it locally.

## Alternatives rejected

- **A destination named in the request, checked against an allow-list
  of origins.** It keeps an origin in a URL anyone can write, and the
  check becomes the one thing standing between a code and an attacker.
  An identifier leaves no such check to get wrong.
- **An `app` column on `oauth_states`.** See decision 3.
- **Serving the API through a Next.js rewrite in cowork-web.** Whether
  a standalone server proxies a WebSocket upgrade on a rewrite is
  unconfirmed, and a gateway path route carries upgrades natively.
- **`GARAM_COWORK_URL` optional on the site.** cowork is a built-in
  service every organization has (#1655), so a deployment without it
  is not a case to design for.

## Consequences

- A person signed in on the site reaches cowork's host with a session
  of its own, and no cookie sits on a parent domain. Decision 5 of
  ADR-0131 applies unchanged, so a sign-out on any host ends
  cowork-web's session on its next request to the platform. An open
  sync or live WebSocket makes no next request: it is checked once, at
  the upgrade, and stays open past the sign-out until it closes. #1721
  closes that window, and blocks the deployment.
- A third app is a new identifier, a new origin in the site's
  configuration and a new line in the rule, in both languages, and in
  the column's check.
- The site does not start without `GARAM_COWORK_URL`, so the chart
  requires `site.coworkUrl` although it deploys no cowork.
- `/api` and `/mcp` on cowork's host are not the web app's, and neither
  are `/auth`, `/healthz` and `/readyz`, so none of those can be an
  organization's slug on that host.
- The browser suite runs cowork's server on the app's host at a port
  of its own rather than through a gateway, since a host-only cookie
  ignores the port. It exercises the handoff across two hosts and not
  the path split.
- Deploying cowork is outside this repository's chart. Its workloads
  and routes land in `garamsh/aurora`'s manifests once a release
  publishes its images.

## Signs the decision was wrong

- An app needs a return path the shared rule cannot express, so the
  rule starts growing a branch per app.
- `oauth_states.return_path` comes to hold more than one site address
  per app, so the value the path carries is no longer one value and
  the rejected column was the better shape.
- A gateway in front of `cowork.garam.sh` cannot carry WebSocket
  upgrades on a path route, or cannot strip the prefix, so the split is
  a burden on every deployment.
- A person is reported signed in on one app and refused on another for
  longer than a request, which would mean a host-only session per app
  costs more than it protects.
