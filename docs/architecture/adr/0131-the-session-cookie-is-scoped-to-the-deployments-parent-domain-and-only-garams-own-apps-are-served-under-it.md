# ADR 0131: The session cookie is scoped to the deployment's parent domain, and only garam's own apps are served under it

> Status: accepted
> Date: 2026-10-28

## Context

- **One app serves sign-in and the console today.** The console's
  `/api/*` rewrite (ADR-0009) puts the backend on the console's own
  origin, so the `garam_session` cookie the backend sets is host-only
  and same-origin.
- **ADR-0125 splits that app across two hosts.** `site/` serves the
  landing page and sign-in at `garam.sh`. `console/` serves the
  management screens at `console.garam.sh`. cowork's web app is served
  at `cowork.garam.sh`, and its backend already reads the platform's
  session cookie on whatever host the cookie reaches
  (`cowork/internal/api/credential.go`).
- **A person signs in once (#1660).** So either the cookie reaches
  every one of garam's hosts, or each host signs the person in again
  by a redirect through the site.
- **What a wider cookie changes is smaller than it looks.**
  - Every host under `garam.sh` is already same-site to every other,
    because a site is the registrable domain. `SameSite=Lax` therefore
    lets one subdomain's pages make requests to another, carrying its
    cookies, under either choice.
  - What the cookie's scope adds is which hosts *receive* the session.
  - ADR-0125 decision 5 already keeps content that users publish off
    `garam.sh` entirely, on a separately registered domain, "so that
    it never sits inside the session cookie's scope". cowork's
    artifacts are served from that origin (ADR-0127 decision 8).

## Decision

**The session cookie carries `Domain=` the deployment's parent domain,
read from `GARAM_COOKIE_DOMAIN`, so one sign-in reaches every one of
garam's hosts. Every host under that domain is an app garam ships, and
nothing a user publishes is served under it.**

1. **The attribute.** `GARAM_COOKIE_DOMAIN` names the domain, for
   example `garam.sh`. Every cookie the api server sets or clears
   carries it: the session, and the OAuth state and PKCE verifier
   cookies of a sign-in in flight. Left unset, every cookie stays
   host-only, which is what a single-host deployment uses. The local
   stack sets `garam.localhost` and serves the two apps at
   `garam.localhost` and `console.garam.localhost`, since `localhost`
   has no subdomains to share a cookie with.
   The other attributes are unchanged: `HttpOnly`, `Secure` unless
   `GARAM_COOKIE_SECURE` is false, `SameSite=Lax`, `Path=/`.
2. **Sign-in lives on the site.**
   - `site/` serves `/login` and `/register` and keeps the `/api/*`
     rewrite, so the OAuth callback and the password endpoints set the
     cookie on the site's host, scoped to the parent domain.
   - The return path stays a `/console/...` path, validated as today.
   - `site/` redirects `/console/:path*` to the same path on the
     console's origin. That one rule both finishes a sign-in and
     carries every old bookmark to the console's host. `/device`, the
     page `garam login` names relative to the host its `/api` was
     reached on (ADR-0120), is redirected the same way.
3. **The console sends a signed-out person to the site.** A visit
   without a session cookie, and a session the backend rejects, is
   sent to the site's `/login` with the console path as `next`. The
   console's own `/login` and `/register` redirect to the site's,
   carrying their query, and its `/` opens `/console`. Every other path
   stays where it is.
4. **What may sit under the parent domain.** Only an app in this
   repository, deployed by garam's chart, is served under
   `GARAM_COOKIE_DOMAIN`. That covers `site/`, `console/`, cowork's web
   app, and the later product app. A deployment that gives a host
   under it to anything else, a user's content or a third party's
   service, hands that host every session.

## Alternatives rejected

- **A host-only cookie per app, with a sign-in redirect through the
  site.** The console would send each signed-out visit to the site,
  which would mint a one-time code bound to the console's origin and
  redirect back. The console's backend route would then exchange the
  code for a session of its own. Each app would hold a separate
  session, so sign-out on one would leave the others signed in. Every
  app, cowork's included, would need the exchange and a route to
  receive it. And because the hosts are same-site, a host it keeps the
  cookie from can still send requests that carry it. The protection it
  buys over decision 4 is a host receiving the session that decision 4
  already forbids.
- **Serving every app from one host under different paths.** That
  undoes ADR-0125 decision 5's hosts, and it puts every app's pages
  in one origin, where a script in one reads every other's storage.

## Consequences

- Sign-in, sign-out and session expiry are one state across `site/`,
  `console/` and cowork's web app.
- A deployment sets `GARAM_COOKIE_DOMAIN` once it serves more than one
  host. The chart takes it as a value, and the console and the site
  take each other's origin as configuration.
- The rule on what may sit under the parent domain is checked by
  nobody. A reviewer reads it against every new host a pull request
  adds.
- Signing out on any host clears the cookie on all of them, because
  the clearing `Set-Cookie` carries the same `Domain=`. Where the
  `Set-Cookie` reaches a Next.js server rather than the browser — a
  Server Action's sign-in or sign-out — the app replays it with its
  `Domain=`, since a cookie planted host-only would be a second cookie
  of the same name.
- The return path crosses between the site and the console in the
  redirects of decisions 2 and 3, and it can hold an invitation token.
  Both origins are garam's under decision 4, so the rule that the token
  rides in no redirect to another origin (`web-app.md` §What it serves)
  now reads as no redirect off the parent domain.

## Signs the decision was wrong

- A host under the parent domain has to serve something garam does
  not ship, such as a status page or documentation from a hosted
  service, and decision 4 is waived for it.
- An app needs a session the others must not share, such as an
  elevated session for an administrative action, and the one cookie
  cannot express it.
- A vulnerability in one app is reported as a session theft in
  another, which the host-only alternative would have contained.
