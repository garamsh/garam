# ADR 0041: A password is garam's own credential, verified by Argon2id and minted by an invitation

> Status: accepted
> Date: 2026-08-25

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives
- Errata

## Context

`internal/authn` offers two sign-in providers and neither takes a
password: Google, and the development-only provider behind
`GARAM_DEV_AUTH` ([ADR-0010](./0010-dev-auth-provider.md)). Both run
the OIDC authorization-code flow, so both end at an `Identity` some
third party asserted. Nothing in `migrations/` holds a credential —
there is no column and no table.

The owner has decided a person must be able to register and sign in
with a username and a password, alongside Google. What that costs is
what this ADR settles.

Three decisions already standing constrain it. Sessions are
server-side rows keyed by token hash
([ADR-0005](./0005-server-side-sessions.md)), so a password
authenticates and does not become a session mechanism of its own.
Organizations grow by invitation and acceptance rather than open
signup ([ADR-0006](./0006-invitation-tokens.md)). And the
unauthenticated login endpoint's write rate is bounded in the
statement that writes
([ADR-0030](./0030-the-login-write-rate-is-bounded-at-the-store.md)),
which fixes the shape any new unauthenticated cost is bounded in.

## Decision

### Argon2id verifies a password, and the verifier carries its own parameters

Argon2id, at 19 MiB of memory (`m=19456`), two iterations (`t=2`), one
degree of parallelism (`p=1`), a 128-bit salt and a 256-bit tag.

The parameters are not a remembered default. The OWASP Password
Storage Cheat Sheet, read on 2026-08-25, names Argon2id the first
choice and states the setting directly: "Use Argon2id with a minimum
configuration of 19 MiB of memory, an iteration count of 2, and 1
degree of parallelism." The same page lists five settings — from
`m=47104, t=1` to `m=7168, t=5` — and says they "provide an equal
level of defense, and the only difference is a trade off between CPU
and RAM usage." The salt and tag sizes come from RFC 9106
(September 2021) §4, which specifies a 128-bit salt and a 256-bit tag
for both of its recommended options.

`p=1` is taken over RFC 9106 §4's `p=4` deliberately. That RFC prices
a single hash in isolation; this process verifies concurrent logins on
shared cores, where lanes bought for one request are taken from
another. OWASP's list already prices that trade, and the setting
chosen sits on it.

The stored verifier is a PHC string that carries the parameters it was
made with, so verification reads its cost from the value it is
checking rather than from the current policy. Raising the policy
therefore invalidates nothing: an old verifier keeps verifying under
its own parameters, and a sign-in that succeeds against parameters
other than the current policy re-hashes the password at the current
policy before it returns. This is the upgrade path OWASP names on the same
page — "wait until the user next authenticates, then re-hash their
password with the new work factor." A parameter change is a constant
in `internal/authn` and a deploy; it is not a migration, not a reset,
and not a second column. A scheme that could not be strengthened later
would be the wrong scheme, and this is what makes strengthening
possible.

### The verifier lives in a table of its own

`password_credentials` holds one row per user that has a password,
keyed by and referencing the user it authenticates.

Not a column on `users`. That table is read on every authenticated
request to resolve the session's principal, and a column would carry
the verifier into memory on every one of them and into the aggregate
every handler already holds. A table of its own keeps the verifier on
the single read path that verifies it. Most users have no password, so
the column would also be the only nullable one on the table, and
withdrawing password sign-in would be an update where it should be a
delete.

**This is not the secret of
[ADR-0036](./0036-a-secret-in-garam-is-a-reference-not-a-value.md).**
That ADR governs references to values other people's stores hold, and
it holds because a value garam stores is a value garam's backup can
leak. A password verifier is neither of those things: it is garam's
own authentication material rather than a tenant's, and it is a
one-way function of a password rather than the password. So ADR-0036
does not forbid this row, and this row licenses nothing about storing
a secret's value. A reader who conflates the two reaches the wrong
conclusion in both directions, which is why the boundary is written
here rather than left to be inferred.

### A password is not a `Provider`

`Provider` runs an authorization-code flow: a redirect out, a code
back, an exchange. A password has no redirect, no code, and no third
party. Satisfying that interface would mean inventing an authorization
code — which is what the development provider does, and the weakness
that made a server-side state record necessary
([ADR-0028](./0028-oauth-state-is-a-server-side-record.md) states it:
that provider "accepts its constant code every time").

So password sign-in is an endpoint of its own rather than a provider.
It asserts the same `Identity` the interface produces, and everything
after that is the path a callback already runs — resolve the user,
create the session row, set the cookie. ADR-0005 is untouched.
`/auth/providers` keeps answering the redirect providers only: it
exists so the login page does not render a button whose backend would
404, and password sign-in is served unconditionally, so there is
nothing for it to advertise.

### The two routes never meet on one user row

A user's GRN identifier is the subject the provider asserted, and that
space is flat — whoever presents a subject is handed the row that
subject names. A password account whose identifier could equal a
Google subject would be adopted by that Google user's next sign-in,
and the reverse, which is an account takeover in either direction.

So garam mints password identifiers under a reserved prefix: the
account for username `alice` is `grn:root:default:user:local-alice`.
No provider subject may claim that prefix, and a subject that does is
refused where the provider's identity is turned into a GRN. The
collision becomes unrepresentable rather than unlikely, and it rests
on a rule this repository enforces rather than on the format of
anybody else's subject.

The answer to what happens when one person arrives by both routes is
therefore that they hold two accounts with two GRNs. Linking them is
not built, not prepared for, and would be a decision of its own.

### A password account is created only by an invitation

Registration presents an invitation token, a username, and a password;
it creates the user, the credential, and the membership the invitation
names. Accepting an invitation while already signed in is unchanged.

Open registration is refused. garam verifies no email address, has no
anti-abuse machinery, and has no organization to attribute a self-made
account to; an endpoint minting a user row for anyone who asks is an
unauthenticated write of exactly the shape ADR-0030 was written for.
ADR-0006 decided that organizations grow by invitation, and an account
that exists in no organization has nothing to do here. The invitation
is already an unguessable single-use possession proof, so it is the
authorization to register.

**Nothing is seeded.** garam ships no default password, no bootstrap
account, and no fixture credential. The first sign-in in a deployment
is Google's, as it is today; locally it is ADR-0010's provider. Any
value of that shape would be a credential committed to this
repository, and there is none.

### ADR-0010 survives

The development provider is not made redundant by a password. Signing
in with one needs an account, which needs an invitation, which needs
an organization, which needs somebody already signed in — the password
path cannot bootstrap itself, so it does not replace the one-click
local sign-in ADR-0010 exists to give. Making it bootstrap locally
would mean shipping a seeded account and a known password, which is
worse than the flag ADR-0010 already hides behind. ADR-0010 keeps its
`accepted` status and is not superseded.

### The rate bound is extended, because ADR-0030 does not reach here

ADR-0030 bounds `/auth/login/{provider}` and says why it bounds
nothing else: that handler "is the unauthenticated surface's only
unconditional write." Password login writes nothing until a password
verifies, so on that test it needs no bound at all. But it spends what
a write costs and more — one Argon2id verification is 19 MiB and a
deliberately expensive computation, and the unknown-username path
spends it too. An unauthenticated caller who can drive that per
request holds a better lever than one row per request, so the absence
of a write is not the absence of a cost.

The bound is therefore the same shape applied to the new cost.
`password_attempts` records an attempt before any verifier is
computed, and the statement that inserts writes nothing once
`MaxLivePasswordAttempts` (1024) attempts are live; past the bound the
endpoint answers `resource_exhausted` and computes no hash — a request
carrying the right password is shed as readily as one carrying a
guess. A verification that succeeds deletes its own attempt, so only failures
accumulate against the bound and ordinary traffic drains itself — the
property a spent state already gives ADR-0030.

The trade is ADR-0030's and is taken knowingly: the bound belongs to
the deployment, so a caller who fills it refuses everyone's password
logins for up to one attempt lifetime. Keying per username or per
client address was rejected there because the caller chooses both, and
that reasoning holds here without change.

Registration takes no attempt slot: the invitation already gates it, so
nothing is hashed for a caller who does not hold a live token, and a
token is a 192-bit secret nobody guesses (ADR-0006). The bound is the
login endpoint's, where there is no such gate.

### A refusal names nothing

A wrong password and an unknown username are one answer: one error,
one kind, one fixed message, and no field saying which of the two it
was. A helpful message here publishes the account list, which is the
defect this rule exists to prevent.

They also do the same work. With no credential row the handler
verifies the presented password against a verifier derived once from
random bytes, so the unknown-username path spends a computation rather
than returning early. That verifier is not a
credential and is not a seeded password: no password produces it, and
nothing accepts it.

A password is never logged, never returned by any endpoint, and never
placed in an error message. That is a property of the type rather than
a rule reviewers enforce: the password is carried in a distinct type
whose rendering — as a string, as a formatted value, and as a log
attribute — is a redaction marker, so a password that reaches a log
line or a message renders nothing.

One residual is named rather than hidden. A credential still carrying
parameters weaker than the current policy verifies faster than the
current-policy verifier the unknown-username path uses, which
distinguishes an existing account from an unknown one for a caller who
can measure the difference over a network. Re-hashing on sign-in
converges the population onto one setting, so the window is the time
until each such account next signs in.

## Consequences

- A person holding an invitation can register and sign in without a
  Google account. Logout and revocation are unchanged, because the
  session a password produces is the session a callback produces
  (ADR-0005).
- garam stores authentication material of its own for the first time.
  A leaked database exposes verifiers, which is what Argon2id's cost
  is for, and exposes no password and no usable session token.
- Strengthening the parameters costs a constant and a deploy. Nothing
  is invalidated and nobody is asked to reset, because each verifier
  carries the parameters it was made with.
- A person with a Google account and a password account holds two user
  rows. Every membership, role, and policy is attached to one of them,
  and the other has none of it.
- Registration writes the user, the credential, the membership and the
  spent invitation as four statements rather than one, because no store
  here exposes a transaction. A failure between the first two leaves a
  user row nobody can sign in as and a username nobody can
  re-register — a transaction is the right fix and this ADR does not
  build one.
- The console has no password form. The endpoints exist and no browser
  UI reaches them, which is deliberate and left to a separate issue.
- Under attack on the password endpoint the whole deployment sheds
  password logins for up to one attempt lifetime. Google sign-in is
  unaffected: it is bounded separately and by its own table.
- garam now has a second identifier space rule to keep — `local-` is
  reserved in user identifiers — and a provider that ever asserted
  such a subject would be refused rather than silently adopted.

## Rejected alternatives

- **bcrypt.** OWASP, read on 2026-08-25, states it "should only be
  used for password storage in legacy systems where Argon2 and scrypt
  are not available." Nothing here is legacy, and it caps the password
  at 72 bytes with no memory cost at all — the cost Argon2id exists to
  impose.
- **scrypt.** The same page names it the fallback "if Argon2id is not
  available," with a cost parameter of 2^17, a block size of 8 and a
  parallelization parameter of 1. Argon2id is available, so the first
  recommendation is taken; nothing here is a reason to differ from it.
- **A column on `users`.** It puts the verifier on every authenticated
  request's read path to save a table, and makes withdrawing a
  password an update to a nullable column rather than a delete.
- **A `Provider` implementation.** It would require inventing an
  authorization code for a flow that has none, reproducing the
  weakness ADR-0028 had to cover, and it would put a password form
  behind `/auth/providers` where every entry is a redirect.
- **Open registration.** An unauthenticated endpoint that mints user
  rows, with no email verification and no organization behind it,
  against an ADR that already decided organizations do not grow that
  way.
- **A seeded account with a known password**, to bootstrap a
  deployment or to let the password path replace ADR-0010. It is a
  credential in the repository, and one that every deployment would
  share.
- **Account lockout after a number of failures.** It hands anyone who
  knows a username the power to lock its owner out, and it bounds
  nothing on the unknown-username path, which costs a full
  verification and has no account to lock.
- **Per-username or per-client-address attempt bounds.** The caller
  chooses the username and can choose the forwarded address, so each
  buys one budget per value invented — ADR-0030's reasoning, which
  this path does not change.

## Errata

- 2026-08-26 — §Decision's "A password is not a `Provider`" says the
  endpoint "asserts the same `Identity` the interface produces". It
  asserts none, and did not when this ADR merged: `Identity` is what a
  `Provider` returns from `Exchange` (`internal/authn/provider.go`,
  `internal/authn/google.go`), while `internal/api/password.go`
  resolves the user itself, `verifyPassword` returning a `*user.User`
  that `LoginPassword` hands straight to `startSession`. #499 merged
  that file and this ADR together, and the file read that way then.
  `docs/architecture/domains.md` carries the correct version today —
  "Password sign-in is its own endpoint, and it asserts no `Identity`
  — an `Identity` is what a provider asserts, and this path resolves
  the user directly" — so a responsibility document and this ADR
  contradict each other in the live repository, and the responsibility
  document is the half to believe. The decision stands, and the
  sentence overstates it rather than weakening it: what a password
  sign-in shares with a callback begins one step later than written,
  at the user row, and ADR-0005 is untouched either way.
- 2026-08-26 — §Decision's "A refusal names nothing" says the
  unknown-username path "spends a computation rather than returning
  early". One kind of unknown username returns early: a string that
  cannot name an account never reaches the credential lookup, and
  `internal/api/password.go` refuses it before an attempt is recorded
  and before anything is hashed — a carve-out that file names in
  place, with its reason, and has named since #499. The claim the
  argument rests on is unaffected and correct: refusing there leaks
  nothing about which accounts exist, because what it separates is a
  well-formed username from a malformed one, and the API specification
  states that rule already. What is wrong is the reach — "the
  unknown-username path" where the sentence holds of the well-formed
  half of it, which is the half a verification is spent on.
- 2026-08-26 — §Consequences' "The console has no password form. The
  endpoints exist and no browser UI reaches them, which is deliberate
  and left to a separate issue" was true when written and is false
  since #506, which built those pages two hours after #499 merged this
  ADR: `web/src/app/(auth)/login/page.tsx` renders `PasswordSignInForm`
  and `web/src/app/(auth)/register/page.tsx` renders
  `PasswordRegisterForm`.
  [ADR-0044](./0044-the-development-only-sign-in-provider-is-removed.md)
  §Context records the same thing. The tail was answered rather than
  overtaken: the separate issue was raised as #505 an hour after this
  ADR merged, and #506 closed it. The decision is unchanged — what
  those pages reach is the credential this ADR specifies, and
  `docs/architecture/web-app.md` describes the console as it now is.
