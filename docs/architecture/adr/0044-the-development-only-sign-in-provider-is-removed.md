# ADR 0044: The development-only sign-in provider is removed, and the Secure-cookie coupling goes with it

> Status: accepted
> Date: 2026-08-25

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

[ADR-0010](./0010-dev-auth-provider.md) added a second `authn.Provider`
implementation, `dev`, for one reason its own Context states: signing in
at all required real Google OAuth credentials, or session rows seeded by
hand. It was a way in where there was none.

[ADR-0041](./0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
gave garam a credential of its own — a username and a password verified
by Argon2id, issuing the same server-side session
([ADR-0005](./0005-server-side-sessions.md)) — and #506 built the
console pages that register and sign in with it. The reason ADR-0010
named is answered by a path that ships.

What the provider carried while it lived is a coupling. `serve api`
refused to start with `GARAM_DEV_AUTH=true` unless
`GARAM_COOKIE_SECURE=false`, which is wrong for any site served over
HTTPS: the flag pair makes a deployment choose between a way in and a
session cookie the browser will not send in the clear. `garamsh/gitops`
#149 is open on that pair.

## Decision

### The `dev` provider is removed, not disabled

`internal/authn/dev.go` and its test are deleted, along with the
`serve api` wiring that registered the provider, the `GARAM_DEV_AUTH`
and `GARAM_DEV_REDIRECT_URL` variables it read, the `Dev sign-in`
button on `/login`, and the values the local compose stack, the
minikube manifests and the chart's documentation carried for it. A
provider nothing constructs cannot be enabled by configuration, which
a default of `false` never guaranteed.

`authn.Provider` stays, with Google as its implementation. The
abstraction is not what ADR-0010 added — Google predates it — and
removing the second implementation does not make one a wrapper around
the other.

### The `GARAM_COOKIE_SECURE` precondition retires with it

The refusal to start on `GARAM_DEV_AUTH=true` with Secure cookies
existed only to keep the dev provider off a site served over TLS.
Nothing else reads the pair, so the check goes with the provider and
`GARAM_COOKIE_SECURE` returns to being one variable a deployment sets
on its own terms.

### ADR-0030's bound keeps its coverage through a test-only stub

`tests/e2e/login_test.go` drove `/auth/login/{provider}` through the
dev provider to prove the live-state bound
([ADR-0030](./0030-the-login-write-rate-is-bounded-at-the-store.md)):
`MaxLiveOAuthStates` redirects succeed, the next answers 429 and writes
nothing. That bound is a property nothing else covers, so the test
stays and takes a stub satisfying `authn.Provider`, declared in the e2e
package. Constructing the Google provider runs OIDC discovery against
its issuer at construction (`internal/authn/google.go`), which a test
may not reach; and the password path's own bound,
`MaxLivePasswordAttempts`, is a different bound with its own test, so
it does not stand in for this one.

## Consequences

- A deployment can no longer be configured into the state
  `garamsh/gitops` #149 describes. There is no flag pair left to get
  wrong.
- Local work and manual QA lose their way in. A stack brought up from
  `deploy/local/` or `deploy/local/minikube/` starts with an empty
  database: `/register` mints an account only from an invitation
  ([ADR-0006](./0006-invitation-tokens.md)), an invitation comes from
  an org admin, and the org's first `owner` is whoever created it while
  signed in. So the first account comes from real Google credentials,
  or from user and session rows written against the database directly,
  as `web/e2e/seed.ts` does for the browser suite. A bootstrap that
  needs neither is not decided here.
- One fixed identity, `grn:root:default:user:dev`, stops being created
  by anything. Rows an earlier local database already holds are
  ordinary user rows and are not migrated or removed.

## Rejected alternatives

- **Leave it, off by default.** It was already off by default. The
  hazard is not that it is on — it is that a deployment can turn it on,
  and that turning it on forces `GARAM_COOKIE_SECURE=false`. Neither
  survives the provider; neither is affected by the default.
- **Keep it for tests only.** A provider compiled into the shipped
  binary is shipped, whatever the package comment says. The two tests
  that needed a second provider take a stub declared in their own test
  package instead, which no build produces.
- **Seed a local account in `migrations/`.** The schema is one artifact
  and every deployment runs it, so a credential added for local work
  reaches production. An account for local work belongs to what deploys
  locally, not to the migrations every deployment shares.
