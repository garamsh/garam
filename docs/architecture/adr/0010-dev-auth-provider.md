# ADR 0010: Development-only sign-in provider behind GARAM_DEV_AUTH

> Status: accepted
> Date: 2026-08-15

## Context

Local development and manual QA require real Google OAuth credentials,
or session rows seeded by hand (what `web/e2e/seed.ts` does). A
one-click local sign-in must not weaken production: no bypass of
session creation (ADR-0005), and no dead endpoints when the feature is
off. The web login page must offer the dev option only when the
backend actually serves it.

## Decision

- A second `authn.Provider` implementation, `dev`
  (`internal/authn/dev.go`), completes the authorization-code flow
  locally: `AuthCodeURL` points at its own configured callback URL with
  a constant code, and `Exchange` asserts a fixed identity
  (`grn:root:default:user:dev`). Everything after `Exchange` — user
  upsert, session row, cookie, revocation — is the shared code path;
  nothing downstream knows the provider exists.
- `serve api` registers the provider only with `GARAM_DEV_AUTH=true`.
  Unregistered, the generic `/auth/login/{provider}` and
  `/auth/callback/{provider}` paths answer 404 for `dev` like any
  unknown provider — no dev-specific routes exist to hide. The
  combination with `GARAM_COOKIE_SECURE=true` (the production-shaped
  default) refuses to start.
- The login page learns about the provider at runtime from a new
  public `GET /auth/providers` endpoint, not from a build-time
  variable: the backend flag stays the single source of truth, and a
  mismatched web build cannot render a button whose backend would 404.

## Consequences

- One click on `/login` creates a real user row and a real, revocable
  session; local setup no longer needs Google credentials.
- With the flag unset there is nothing to attack: no registered
  provider, no routes, no advertised UI. Enabling it also requires
  opting out of Secure cookies, which no production-shaped deployment
  does silently.
- All dev logins share one fixed identity; distinct developer
  identities are future work if they ever matter.
- `/auth/providers` is public and reveals which providers are enabled;
  acceptable, since provider names are not secret.
