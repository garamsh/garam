# Sign-in

How a person proves who they are, and the session that carries the
proof afterwards. Implemented by `internal/authn`.

| Type | Responsibility |
|------|----------------|
| `Provider` | OIDC authorization-code flow with PKCE; `google.go` implements it (ADR-0044 removed the development-only second implementation) |
| `Identity` | subject + nickname asserted by a provider |
| `Session` | browser session for a user, expiring |
| `SessionStore` | persists sessions keyed by token hash |
| `OAuthStateStore` | persists the state a login issued, until the callback spends it or its lifetime runs out (ADR-0028); issuing refuses past `MaxLiveOAuthStates` live states (ADR-0030) |
| `Password` | a password as typed; renders as a redaction marker in every form, so it cannot reach a log line, a response, or a message (ADR-0041) |
| `Verifier` | the stored Argon2id form of a password, carrying the parameters it was computed under |
| `PasswordCredentialStore` | persists one verifier per user that has a password |
| `PasswordAttemptStore` | bounds the verifications live at once; recording refuses past `MaxLivePasswordAttempts` (ADR-0041) |

A second provider is a new file implementing `Provider`; call sites
depend only on the interface. Raw session tokens live in cookies; only
SHA-256 hashes reach the database, the state a login issues included.

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
binary or image Garam ships carries it, so it is no path into a
deployment; `tests/testprincipal/README.md` is its contract and
invocation.
