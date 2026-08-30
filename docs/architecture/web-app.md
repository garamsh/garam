# Web App

What the application a person uses garam through is responsible for, the one seam it reaches the backend through, and what it does not decide.

## Contents
- What it is
- What it serves
- How it reaches garam
- What it does not decide
- Its own toolchain and gate
- Not established
- Decisions this rests on

## What it is

`web/` implements it: a Next.js App Router application, TypeScript
strict, Tailwind and shadcn/ui
([ADR-0009](./adr/0009-web-frontend.md)). It is the one part of this
repository that is not Go, it carries a toolchain and a gate of its
own, and it ships as one of the two images a version tag publishes
([ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)).

It stays its own image for what each of the two holds while it runs.
This one's final stage is the Node base the build ran on
(`deploy/local/Dockerfile.web`): a shell, `wget`, and the runtime that
executes the `server.js` and the `node_modules` subset copied into it —
the local stack reaches this application's probe through that shell and
the garam processes' by running the shipped binary
(`deploy/local/docker-compose.yml`). The other image is `FROM scratch`
holding five copied paths (`deploy/local/Dockerfile`): certificates, one
account and no root, `migrations/`, and the `garam` binary that
`migrate`, `issue-certificate` and every serve subcommand run from.
Merging the two gives each process what the other holds — a shell and an
interpreter inside the image whose processes read the key-encryption key
that seals every organization's certificate authority
([deployment-contract.md](./deployment-contract.md) §What the binary
enforces about key material), the schema migrations and that binary
inside the image whose process renders a browser's input — and that is
what a proposal to build and pull one image instead of two has to
answer. [ADR-0020](./adr/0020-published-artifacts.md) §Rejected
alternatives refused a single image on that ground, and
[ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
§Decision leaves the two standing where it supersedes that ADR.

**This document is authoritative for what the application is
responsible for and where its boundary falls.** Four artifacts already
own things a reader might come here for, and none of them is restated:

- `api/openapi.yaml` is the source of truth for every operation the
  application calls
  ([ADR-0004](./adr/0004-openapi-first-api.md)), and `web/src/types/api.ts`
  is generated from it rather than written
  ([ADR-0019](./adr/0019-machine-openapi-spec.md)).
- [api-surface.md](./api-surface.md) describes the browser-facing
  listener as a **surface**: what admits a caller to it and what
  decides what that caller may do. This document describes the
  **client** of that surface. Where both could answer a question about
  admission or authorization, that one answers it.
- `deploy/charts/garam/README.md` is authoritative for what a
  deployment sets — `web.apiUrl` included, and for what happens when it
  is absent at runtime
  ([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)).
- `docs/convention/stack-nextjs.md` governs how the application is laid
  out inside `web/src/`: which folder holds what, and which tier may
  import which. Nothing here restates a layout rule, and a question
  about where a file belongs is that file's, not this one's.

What is left is this document's: which routes exist and what each one
is for, the single seam the application reaches garam through, and the
decisions it does not make.

## What it serves

Three route groups and two probes. A Next.js route group contributes no
URL segment, so `(marketing)` and `(auth)` are not in any path.

| Path | For | Reached by |
|---|---|---|
| `/` | the public landing page | anyone |
| `/login` | sign in — a password form, and a provider button only where the backend answers with that provider; a visitor already holding a session is sent on to the console | anyone |
| `/register` | turn an invitation into a password account ([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)) | anyone holding an invitation token |
| `/console/*` | the signed-in application | a visitor carrying a session cookie |
| `/healthz`, `/readyz` | the two probes, spelled as every other process spells them ([ADR-0023](./adr/0023-web-probes.md)) | anything that can open the port |
| `/api/*` | the same-origin rewrite onto the browser-facing listener ([ADR-0009](./adr/0009-web-frontend.md)) | the browser |

The sign-in page asks `/auth/providers` at runtime and renders a
provider button only for a name it answers with, so a build cannot
offer a door the backend would answer 404 for
(`web/src/app/(auth)/login/page.tsx`, reading through
`web/src/features/auth/api/list-providers.ts`). A read that fails
yields no names, so the password form stays the way in. Which ADR holds
that decision is open (§Not established).

The console's structure is `Organizations → organization → section`,
carried by a collapsible sidebar shell over every `/console/*` route
([ADR-0011](./adr/0011-console-shell.md)). Its org-scoped sections are
declared in one place, `web/src/app/console/_components/navigation.ts`,
which is what the sidebar, the breadcrumb and the active marking all
read; adding one is an entry there and a route folder.

What a section can do differs, and the difference is the state of the
API behind it rather than of the console:

| Section | What the console does with it |
|---|---|
| Overview, Groups, Projects, Members | reads |
| Operators | reads, and registers one ([ADR-0045](./adr/0045-an-organization-registers-an-operator.md)) |
| Agents | reads definitions and defines one ([ADR-0026](./adr/0026-the-console-defines-an-agent.md)); it does not read the agents the browser API answers |
| Policies | neither: the section holds its place and says policies are managed over the HTTP API |

Organizations are created, an invitation is issued from the Members
section and accepted at `/console/invitations/{token}` — which is
itself behind sign-in, because accepting one joins an existing account
to an organization.

No organization, group, project or user is addressed by its GRN
anywhere in the interface. The console shows names and addresses
resources by the identifier segment the API's own paths take
([ADR-0011](./adr/0011-console-shell.md),
[ADR-0001](./adr/0001-grn-segments.md)).

## How it reaches garam

**One module.** `web/src/lib/garam.ts` holds the only call to the
backend anywhere under `web/src/`; every read and every Server Action
goes through it. That is what makes the boundary checkable rather than
asserted: one file to read to know everything the application asks for.

It is reached over two paths, and only one of them is the rewrite:

- **The browser** reaches the backend under `/api/*` on this origin, so
  the `garam_session` cookie stays same-origin and no CORS layer is
  needed (ADR-0009). This is the path the OAuth round trip and the
  provider sign-in link take.
- **A server-side render** calls the configured address directly,
  because it is not a browser and has no origin to keep. The rewrite
  exists for the browser, not because it is how the application reads
  data.

The address is server-side only and never reaches a browser. It is
declared once, as a typed schema in `web/src/env.ts`, parsed at import
so a missing value fails at start rather than inside the first request
that needs it. That file is also the authority `make chart` reads the
application's environment names from
([ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)),
so adding a variable here is a change the chart has to answer for.

Sign-in state is read from the backend's `/me` and never inferred from
the cookie (ADR-0009).

## What it does not decide

The boundary is as much of the responsibility as the contents.

- **No authorization decision.** `web/src/proxy.ts` sends a request to
  `/console/*` carrying no session cookie to `/login`, and that is a
  fast path rather than a gate: a cookie can be expired or revoked, so
  the console verifies the session against `/me`, and a 401 from any
  read sends the visitor to `/login` regardless. Every org-scoped
  decision belongs to the policy engine
  ([ADR-0002](./adr/0002-iam-style-policy-model.md),
  [authz.md](./authz.md)), and a form the API will refuse is allowed to
  render and be refused.
- **Mints no identity and holds no credential.** No GRN, no session, no
  certificate, no invitation token originates here. The session is a
  server-side row and the cookie carries an opaque token whose hash
  alone is stored, so there is nothing here to read or forge
  ([ADR-0005](./adr/0005-server-side-sessions.md)); an invitation is
  the token itself, and possession is the whole of the claim
  ([ADR-0006](./adr/0006-invitation-tokens.md)).
- **Serves no secret's value.** No surface does, and
  [ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)
  names the console when it says so.
- **Owns no schema.** `web/src/types/api.ts` is generated from
  `api/openapi.yaml`, so a change to what an operation carries starts
  in the specification and arrives here (ADR-0004, ADR-0019).
- **Its probes read nothing.** Neither answer reads the API, the
  environment or the filesystem, so a backend outage takes no replica
  out of rotation
  ([ADR-0023](./adr/0023-web-probes.md)). Reporting an outage is the
  reading surfaces' job and not the probes': `/login`, `/register` and
  every console section that reads render a notice in place of what
  they could not fetch.
- **Reaches no listener but one.** The machine listener is a separate
  process on separate credentials
  ([ADR-0017](./adr/0017-machine-process.md)); nothing here holds key
  material or speaks to it, and bringing the application up locally
  needs no certificate.

## Its own toolchain and gate

`make web-ci` is the gate, and it is not part of `make ci` — the two
run side by side. In order: `web-format`, `web-lint` (which also
typechecks), `web-test`, `web-build`, `web-check-gen`.

Two things are outside it. `make web-e2e` runs the Playwright specs in
`web/e2e/` against a real built application, a real backend and a real
Postgres, so it needs infrastructure the gate does not start. `make
web-install` is a prerequisite rather than a check.

Tests are in two locations by decision, not by accident: Vitest owns
the tests colocated under `web/src/`, Playwright owns `web/e2e/`, and
each config names the other's territory so neither collects the other's
files (ADR-0009, `docs/convention/stack-nextjs.md` §12).

## Not established

- **Whether a responsibility document is owed per domain or per index
  entry.** `#605` records that the line defining what rule 5 binds
  differs from the template garam adopted it from, and that the two
  readings point at different files. This document is owed under
  either, so nothing here turns on the answer, and nothing here
  presumes one.
- **The browser end of watching an agent.** garam's half is built and
  [ADR-0022](./adr/0022-watching-an-agent.md) puts a browser at one end
  of the stream, reading it as it arrives and seeing only from when it
  opened. No console route opens one, so the view that ADR describes
  does not exist yet, and where it would live is undecided.
- **Reading policies.** The section exists and says it cannot. Whether
  the console reads them, and whether it ever writes one, is not
  recorded either way.
- **Which ADR holds the sign-in page's provider discovery.** The
  behaviour is live and is stated under §What it serves.
  [ADR-0010](./adr/0010-dev-auth-provider.md) decided it, and
  [ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  superseded that ADR to remove the second provider without carrying
  the discovery decision either way — so what the application does
  rests on a decision whose ADR is superseded.
  `web/src/features/auth/api/list-providers.ts` and
  `web/src/app/(auth)/login/page.tsx` both cite ADR-0010 for it.
- **What the application promises a visitor across versions.** Nothing
  records it, and [api-surface.md](./api-surface.md) §Not decided
  records the same absence for the API it calls. One released image is
  bound to one API address, because the rewrite target is baked in at
  build time and nothing can check the two agree
  ([ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)).

## Decisions this rests on

The decisions the state above is made of. An ADR that changes what the
application serves, what it reaches, or what it may not decide belongs
on this list, which is the quickest test of whether a change is one
this document owes an update.

Three decide the application itself:

- [ADR-0009](./adr/0009-web-frontend.md) — a Next.js application in
  `web/` in this repository, reaching the API through a same-origin
  `/api/*` rewrite, with three test layers
- [ADR-0011](./adr/0011-console-shell.md) — the console is a sidebar
  shell over `Organizations → organization → section`, and prints no
  GRN
- [ADR-0023](./adr/0023-web-probes.md) — the application answers the
  same two probes as every other process, as route handlers, reading
  nothing

The rest bear on it without being about it:

- [ADR-0001](./adr/0001-grn-segments.md) — the identifier segment is
  what the API's paths take, which is what the console shows instead of
  a GRN
- [ADR-0002](./adr/0002-iam-style-policy-model.md) — the engine decides
  every org-scoped request, so the interface decides none
- [ADR-0004](./adr/0004-openapi-first-api.md) — `api/openapi.yaml` is
  the source of truth the client's types are generated from
- [ADR-0005](./adr/0005-server-side-sessions.md) — a session is a row
  and the cookie carries only a token naming it
- [ADR-0006](./adr/0006-invitation-tokens.md) — an invitation is a
  single-use token, and possession is the claim
- [ADR-0017](./adr/0017-machine-process.md) — the machine listener is a
  separate process this application never reaches
- [ADR-0019](./adr/0019-machine-openapi-spec.md) — one specification
  per listener, so the generated client carries no operation it could
  not call
- [ADR-0021](./adr/0021-liveness-and-readiness.md) — what the two probe
  paths mean, which ADR-0023 imports wholesale
- [ADR-0022](./adr/0022-watching-an-agent.md) — a watch is opened and
  closed by a browser, and the view is best effort rather than a record
- [ADR-0026](./adr/0026-the-console-defines-an-agent.md) — the console
  defines an agent, and a definition is not a running thing
- [ADR-0028](./adr/0028-oauth-state-is-a-server-side-record.md) — the
  callback a login lands on refuses a replayed state as unauthenticated
- [ADR-0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md)
  — starting a login is refused once the live-state bound is reached,
  which the login page has to render
- [ADR-0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md)
  — defined, claimed and running are distinct states, and conflating
  them in an interface is a defect
- [ADR-0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md)
  — no surface serves a secret's value, the console named among them
- [ADR-0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  — the application is one of the two published images, and its API
  address is baked in at build time
- [ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
  — a password is garam's own credential, `/register` is invitation
  gated, and one refusal covers a wrong password and an unknown name
- [ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
  — the chart describes the deployment, and reads this application's
  environment names from `web/src/env.ts`
- [ADR-0043](./adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)
  — the web workload carries an identity of its own and reads no
  credential
- [ADR-0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  — the development-only sign-in button is gone, and local work has no
  way in but real credentials or the e2e seed
- [ADR-0045](./adr/0045-an-organization-registers-an-operator.md) — an
  organization registers an operator, which is the Operators section
