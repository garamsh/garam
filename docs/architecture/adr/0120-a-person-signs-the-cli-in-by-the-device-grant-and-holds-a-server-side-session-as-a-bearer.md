# ADR 0120: A person signs the CLI in by the device grant and holds a server-side session as a bearer

> Status: accepted; where the commands live (point 1) is superseded
> by
> [ADR-0132](./0132-a-root-folder-is-an-executable-unit-named-service-surface-and-code-libraries-sit-under-lib.md)
> Date: 2026-10-20

Drafted for #1616, for which the Garam PM reserved this number. The
direction is the owner's of 2026-10-20, recorded by the PM on #1616:
sign in from a terminal as `aws sso login` and `gh auth login` do, then
manage garam's objects from the shell. garam is read at
`72e936973db5f95757ddfb7b3a6bec2a306c95e5`; a claim below that names no
revision holds for it.

## Context

- **garam has no user-facing command line.** `garam` is the one binary
  every process runs ([ADR-0003](./0003-single-cli-topology.md)): `serve
  api`, `serve authz`, `serve machine`, `migrate` and `standalone`. A
  person reaches garam through the console alone.
- **A person's credential is a session.** A sign-in mints a
  server-side row keyed by its token's hash and hands the token over as
  the `garam_session` cookie
  ([ADR-0005](./0005-server-side-sessions.md)). Revocation is deleting
  the row. Every operation on the browser API is authorized on the user
  that row names.
- **Sign-in happens in a browser.** Google is a redirect provider
  ([ADR-0044](./0044-the-development-only-sign-in-provider-is-removed.md))
  and the password is the console's form
  ([ADR-0041](./0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
  A terminal can drive neither without asking for the password itself
  or listening on a local port for a redirect.
- **The browser API sits behind the console.** The console rewrites
  `/api/*` to `serve api` (`web/next.config.ts`), so the browser API
  answers requests whose public origin it is never told.
- **Every unauthenticated write is bounded at the store.** A login state
  ([ADR-0030](./0030-the-login-write-rate-is-bounded-at-the-store.md))
  and a password attempt (ADR-0041) each refuse past a count of live
  rows decided in the statement that writes, so the bound holds for
  every replica at once.

## Decision

**A person signs the CLI in by the OAuth 2.0 Device Authorization Grant
(RFC 8628), and the CLI holds a server-side session presented as a
bearer token.**

1. **The commands are in the `garam` binary.** User commands sit at the
   top level — `login`, `logout`, `whoami`, `orgs`, `agents`,
   `operators` — beside the server commands, which keep their names.
   garam keeps one binary and one image; a shorter name can later be a
   symlink. The CLI calls the browser API alone, through a client
   generated from `api/openapi.yaml`, and decides nothing the API does
   not.
2. **Login is the device grant.**
   - `POST /auth/device` answers a device code, a user code, their
     expiry and the polling interval. It carries no credential.
   - The person opens `/device` on the console, signs in there as they
     always do, and approves or denies the code
     (`POST /auth/device/approval`, on their session).
   - The CLI polls `POST /auth/device/token` and is answered RFC 8628's
     errors (`authorization_pending`, `slow_down`, `access_denied`,
     `expired_token`) until an approved code is redeemed, once, for a
     session.
   - `verification_uri` is answered as the path `/device`. The API
     cannot know the public origin the console serves it under, and
     the CLI joins the path to the endpoint it was pointed at, which is
     that origin.
3. **A CLI session is a session.** It is a row of `sessions`, of kind
   `cli`, naming the user who approved the code, with that user's
   authorization and the same revocation (ADR-0005). Two things set it
   apart:
   - **Its own lifetime and a refresh.** The access token lives one
     hour. A refresh token, valid thirty days from its issue, buys the
     next access token at the same token endpoint, and each refresh
     replaces both tokens, so a refresh token is spent once.
   - **It is presented as a bearer.** `Authorization: Bearer <token>`
     is accepted wherever the `garam_session` cookie is, and a request
     presenting one is read by it alone.

   A person lists their sessions and revokes any of them
   (`GET /me/sessions`, `DELETE /me/sessions/{session}`), and
   `garam logout` revokes the CLI's own.
4. **Every unauthenticated write of the grant is bounded at the store.**
   - Issuing a device code refuses once `MaxLiveDeviceAuthorizations`
     are live, decided in the statement that writes, as ADR-0030 bounds
     a login state.
   - A poll earlier than the code's interval answers `slow_down` and
     moves that code's interval five seconds later (RFC 8628 §3.5).
5. **The CLI keeps its tokens in one owner-only file.**
   `$XDG_CONFIG_HOME/garam/credentials`, or
   `~/.config/garam/credentials` by default, is written `0600` in a
   `0700` directory, keyed by endpoint, and never printed. The endpoint
   comes from `--endpoint` or `GARAM_ENDPOINT` and defaults to
   `https://garam.sh`. The organization comes from `--org`, or from the
   default `garam orgs use` saved.

## Consequences

- A person signs in from a terminal with nothing typed but a code. The
  CLI never sees a password or a provider's token.
- Revoking a session ends the CLI as it ends a browser, and the session
  list shows every CLI a person has signed in.
- The device grant can be phished: anyone who gets a person to approve
  a code they did not start signs a CLI in as that person. The `/device`
  page has to say what is being approved, and a code lives ten minutes.
- Guessing user codes buys nothing. Approving someone else's code signs
  *their* CLI in as the guesser, and every approval is made on a
  session that names who made it.
- `verification_uri` departs from RFC 8628's absolute URI. A client
  other than garam's own has to join the path to its endpoint.
- The browser API's request line already names the route pattern and
  never a header, so a bearer token reaches no log line.

## Signs the decision was wrong

- People keep CLIs signed in for months without using them, and the
  session list fills with refresh tokens nobody holds: the thirty days
  slide too far, and an absolute ceiling was wanted.
- A deployment serves the browser API on an origin the console does not
  share, and the joined `/device` path lands nowhere: the API needed a
  configured public origin after all.
- Approvals arrive for codes their approvers did not start: the device
  grant's phishing exposure outweighs not asking for a password, and a
  local-redirect login was the better trade.
