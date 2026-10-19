# Sign-in

How a person proves who they are, and the session that carries the
proof afterwards. Implemented by `internal/authn`.

| Type | Responsibility |
|------|----------------|
| `Provider` | OIDC authorization-code flow with PKCE; `google.go` implements it (ADR-0044 removed the development-only second implementation) |
| `Identity` | subject + nickname asserted by a provider |
| `Session` | a user's session, expiring: a browser's, or a CLI's of kind `cli` (ADR-0120) |
| `SessionStore` | persists sessions keyed by token hash; spends a CLI session's refresh token, and lists and revokes a user's sessions by id |
| `DeviceGrant` | the device code and user code one device sign-in is answered with (RFC 8628) |
| `DeviceStore` | persists device authorizations by their codes' hashes until an approved one is redeemed once for a CLI session; issuing refuses past `MaxLiveDeviceAuthorizations` (ADR-0120) |
| `OAuthStateStore` | persists the state a login issued, until the callback spends it or its lifetime runs out (ADR-0028); issuing refuses past `MaxLiveOAuthStates` live states (ADR-0030) |
| `Password` | a password as typed; renders as a redaction marker in every form, so it cannot reach a log line, a response, or a message (ADR-0041) |
| `Verifier` | the stored Argon2id form of a password, carrying the parameters it was computed under |
| `PasswordCredentialStore` | persists one verifier per user that has a password |
| `PasswordAttemptStore` | bounds the verifications live at once; recording refuses past `MaxLivePasswordAttempts` (ADR-0041) |

A second provider is a new file implementing `Provider`; call sites
depend only on the interface. Raw session tokens live in cookies, or
with the CLI that holds them; only SHA-256 hashes reach the database,
the state a login issues, a CLI's refresh token and both device grant
codes included.

A password is not a `Provider` and never becomes one: that interface is
an authorization-code flow, and satisfying it would mean inventing a
code. Password sign-in is its own endpoint, and it asserts no
`Identity` — an `Identity` is what a provider asserts, and this path
resolves the user directly. After it the path is the callback's: a
session row and a cookie, unchanged (ADR-0005, ADR-0041). What is
stored is an Argon2id
verifier carrying its own parameters, so raising them invalidates no
row: a sign-in against any other setting re-hashes on its way out.
Accounts minted this way take the reserved `local-` prefix in the user
identifier space, which a provider subject may not claim, so a password
account and a provider account can never resolve to one user row.

A wrong password and a username no credential belongs to are one
refusal, in message and in the work done: the second verifies against a
verifier derived from random bytes rather than returning early.

A callback completes once because the state it carries is spent from a
row, not because the response asked the browser to drop the cookie
holding it (ADR-0028). The state's own lifetime bounds the row, so a
record cannot outlive what it is checked against or lapse before it.
Issuing refuses once `MaxLiveOAuthStates` states are live, which bounds
the write rate the unauthenticated login endpoint can be driven at to
that many rows per flow lifetime, for every replica at once — the bound
is decided in the statement that writes, not held by any one process
(ADR-0030).

Tests outside this repository get their first signed-in user from
`tests/testprincipal`, which creates a fresh local database and writes
the user and its session through the stores above (ADR-0081). No
release image and no `garam` binary carries it, so it is no path into a
deployment. The test image published from `dev` carries it beside
`garam` and the migrations, as files a consumer copies out and runs
against its own local Postgres (ADR-0092).
`tests/testprincipal/README.md` is its contract and invocation, the
test image's included.

## The CLI's session

The CLI signs a person in by the device grant (RFC 8628) and holds a
session presented as a bearer (ADR-0120). `garam login` asks
`POST /auth/device` for a device code and a user code, and the person
approves the user code on the console's `/device` page, signed in there
as they always are (`POST /auth/device/approval`). The CLI polls
`POST /auth/device/token` meanwhile, answered RFC 8628's errors until
the approved code is redeemed. The redemption opens the session and
deletes the authorization in one transaction, so a code opens one
session.

- **Lifetimes.** A code lives `DeviceCodeTTL`, ten minutes, and is
  polled every `DevicePollInterval`, five seconds. A poll earlier than
  that answers `slow_down` and moves the code's interval
  `DeviceSlowDownStep` later. A lapsed row is kept an hour, so a late
  poll reads `expired_token` rather than an unknown code.
- **The session.** It is a row of `sessions` of kind `cli`, naming the
  approving user and carrying the name the CLI sent (`client_name`). Its
  access token lives `CLIAccessTTL`, an hour, and its refresh token
  `CLIRefreshTTL`, thirty days. A refresh at the same token endpoint
  replaces both in one statement, so a refresh token is spent once. The
  prune that every browser sign-in runs spares a CLI row whose refresh
  token is live.
- **Presentation.** `Authorization: Bearer` is accepted wherever the
  `garam_session` cookie is, and a request carrying an `Authorization`
  header is read by it alone. A malformed header or a dead token is
  refused even beside a live cookie. `POST /auth/logout` revokes the
  session either presents, and expires the cookie only when the cookie
  presented it.
- **Listing and revoking.** `GET /me/sessions` lists a person's live
  sessions of both kinds by an id that is not the token's hash, marking
  the one the request came on. `DELETE /me/sessions/{session}` revokes
  one, and answers 404 for an id the person holds no session by.
- **Bounds.** Issuing refuses once `MaxLiveDeviceAuthorizations`, 1024,
  are live, decided in the statement that writes, as ADR-0030 bounds a
  login state.

The `/device` page is the console's
([web-app.md](./web-app.md) §What it serves). No read answers a pending
code's client name, so the page shows the code the person entered, to
compare with their terminal, and whom approving signs the CLI in as
(ADR-0120 §Consequences).
