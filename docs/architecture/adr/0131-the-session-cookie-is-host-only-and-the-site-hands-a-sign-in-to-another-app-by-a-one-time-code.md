# ADR 0131: The session cookie is host-only, and the site hands a sign-in to another app by a one-time code

> Status: accepted; decision 2's return path is superseded by
> [ADR-0135](./0135-the-site-hands-a-sign-in-to-a-fixed-list-of-apps-and-cowork-shares-one-host-split-by-path.md)
> Date: 2026-10-28

## Context

- **One app serves sign-in and the console today.** The console's
  `/api/*` rewrite (ADR-0009) puts the backend on the console's own
  origin, so the `garam_session` cookie the backend sets is host-only
  and same-origin.
- **ADR-0125 splits that app across two hosts.** `site/` serves the
  landing page and sign-in at `garam.sh`. `console/` serves the
  management screens at `console.garam.sh`. cowork's web app is served
  at `cowork.garam.sh`.
- **A person signs in once (#1660).** So either the cookie reaches
  every one of garam's hosts, or each host is handed the sign-in by a
  redirect through the site.
- **The owner decided the second (#1655).** The cookie stays host-only
  and each app reaches the sign-in by redirect.
- **Hosts under one registrable domain are same-site.** `SameSite=Lax`
  does not tell `console.garam.sh` from `garam.sh`. So nothing about a
  cookie's scope keeps one host's pages from sending requests to
  another; what the scope decides is which hosts *receive* the session.

## Decision

**The session cookie is host-only to the app that set it. A person
signs in on the site, and another of garam's apps is handed a session
of its own by a one-time code the site issues and the app redeems on
its own host, bound to a state the app holds.**

1. **The cookie.** Every cookie the api server sets carries no
   `Domain=`, as before this split: the session, and a sign-in's OAuth
   state and PKCE verifier. `HttpOnly`, `Secure` unless
   `GARAM_COOKIE_SECURE` is false, `SameSite=Lax`, `Path=/`. The local
   stack serves the two apps at `garam.localhost:3001` and
   `console.garam.localhost:3000`, since a cookie ignores the port and
   two apps on one host would share it.
2. **Sign-in lives on the site.**
   - `site/` serves `/login` and `/register` and keeps the `/api/*`
     rewrite, so the OAuth callback and the password endpoints set the
     cookie on the site's host.
   - The return path stays a `/console/...` path, validated as today.
   - `site/` redirects `/console/:path*` to the same path on the
     console's origin. That one rule both finishes a sign-in and
     carries every old bookmark to the console's host. `/device`, the
     page `garam login` names relative to the host its `/api` was
     reached on (ADR-0120), is redirected the same way.
3. **The handoff.** An app without a session sends the visitor through
   four steps, each a `303` sent with `referrer-policy: no-referrer`:
   1. The app's `/auth/sign-in?next=` draws a state of 32 random bytes,
      holds it in the `garam_handoff_state` cookie, scoped to the app's
      `/auth/handoff` and living ten minutes, and redirects to the
      site's `/handoff?next=&state=`.
   2. The site's `/handoff` sends a visitor with no session to its own
      `/login?next=`, whose sign-in returns to the console path and so
      starts the handoff again. With a session, it asks
      `POST /auth/handoff {state}` for a code and redirects to the
      app's `/auth/handoff?code=&next=`.
   3. The app's `/auth/handoff` drops the state cookie and redeems the
      code with it through `POST /auth/handoff/redeem`. The backend
      answers a new session's cookie, which the app replays to the
      browser on its own host, and the visitor lands on `next`.
   4. A refused redemption sends the visitor to the site's
      `/login?next=`.
4. **What a code is.** The backend stores a code and its state as
   hashes, with the session that issued it. A code lives one minute
   and redeems once: the statement that spends it creates the session,
   so two redemptions racing buy one. It redeems only with the state it
   was issued for, so a code an attacker obtained meets a browser that
   never held that state. Only a browser's session issues one: a
   bearer is a CLI's, and a CLI that could buy a browser session would
   hold more than ADR-0120 gave it.
5. **One sign-in, many sessions.** The session a redemption creates
   records the sign-in it was handed from and expires when that one
   does. Signing out or revoking any session of the family ends the
   sign-in on every host: the delete removes the sign-in, and the
   handed-off sessions go with it.
6. **The console sends a signed-out person to the handoff.** A visit
   without a session cookie, and a session the backend rejects, is
   sent to the console's `/auth/sign-in` with the console path as
   `next`. The console's own `/login` and `/register` redirect to the
   site's, carrying their query, and its `/` opens `/console`. Signing
   out lands on the site's `/login`.

## Alternatives rejected

- **A session cookie scoped to the deployment's parent domain.** One
  `Domain=garam.sh` cookie would reach every host with no handoff. But
  every host under the domain would then receive every session, so the
  deployment would have to promise that nothing it does not ship, a
  hosted status page or a third party's service, is ever served under
  it, and nothing checks that promise. A defect in one app would expose
  the sessions of all of them. The owner chose the redirect (#1655).
- **Serving every app from one host under different paths.** That
  undoes ADR-0125 decision 5's hosts, and it puts every app's pages
  in one origin, where a script in one reads every other's storage.

## Consequences

- Each app holds a session of its own. A sign-in on the site reaches
  the console without a second sign-in, at the cost of three redirects
  the first time the console is opened.
- Sign-out and revocation still end the sign-in everywhere (decision
  5), but an app that already holds a cookie learns it on its next
  request to the backend, not at once.
- cowork's web app needs the same two routes before it reads a session
  on its own host; its backend reads the platform's cookie wherever the
  cookie reaches (`cowork/internal/api/credential.go`), which is now
  only the host it was handed to.
- `@garam/platform-client/handoff` holds the paths, the cookie name
  and the state's shape, so every app redeems the same way.
- The return path crosses between the site and the console in the
  redirects of decisions 2, 3 and 6, and it can hold an invitation
  token. Both origins are garam's, and every one of those redirects is
  sent with no referrer (`web-app.md` §What it serves).
- A deployment sets nothing for the cookie. The console and the site
  take each other's origin as configuration.

## Signs the decision was wrong

- The redirects are measured as a cost a person notices on opening the
  console, or one an app cannot afford, such as a native client that
  cannot hold a state cookie.
- Signed-out sessions are reported as still working on another host
  for longer than a request, which decision 5 means to prevent.
- An app beyond the console needs the handoff and cannot take
  `@garam/platform-client/handoff` as it is.
