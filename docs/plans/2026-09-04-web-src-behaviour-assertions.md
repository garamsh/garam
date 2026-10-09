# What `web/src` asserts about garam, and whether the code agrees

A survey of the prose the web application shows a user, taken for #1003.
It changes no convention file, adds no check, and decides nothing: the
scope question it feeds is the PM's and the owner's.

Checked against `d748465`. Every path and line number below holds for
that commit and for nothing later, except the comment sweep under
§The strata outside the population, which names its own.

## Contents
- How a string was selected
- The control on the selector
- The count
- The assertions
- The strata outside the population
- What re-checking the clean rows found
- What this does not settle

## How a string was selected

A string is in the population when both hold.

- **It reaches a user.** It renders into the browser — page copy, a
  heading, a form hint, an empty state, a refusal a form shows. A code
  comment does not, and neither does an `Error` message no page
  renders.
- **It asserts something about garam's behaviour.** It states a general
  proposition about what garam does, does not do, requires, permits or
  refuses — one a reader could check against the code and find true or
  false, independent of the data on screen.

Four kinds of rendered string are therefore out, and each is out for a
reason worth naming, because each was tempting.

- **A label, heading or button.** `Register an operator`, `Members`,
  `Copy`. Nothing is asserted.
- **An instruction.** `Name the operator.`, `Give the organization a
  name.` Imperative mood, no truth conditions: no code could contradict
  it. The line is mood and truth-aptness rather than wording — `Every
  value needs a key.` reads like an instruction and is a declarative
  claim about what a definition requires, so it is in.
- **A report about the data on screen.** `This organization has no
  members.` states a fact about this organization, not about garam.
- **A report about one submission.** `The form did not arrive
  complete.`, `That username and password do not match an account.`
  These say what happened to this request. The bound behind them is a
  separate string and is in: `A password holds at least 15 characters.`

One boundary is drawn twice and stated here so the table is read the
same way both times. Where a sentence's subject is an operator's own
software rather than garam — what it builds, where it generates a key
— nothing in this repository can settle it, and the row says
unverifiable as written even where garam's own half of the sentence
holds.

The selector runs in three stages, and only the third is judgement. A
script pulls every string literal and JSX text node carrying three or
more words; a second stage drops class strings and import specifiers;
the two rules above then decide what is left. The mechanical stages are
deliberately over-wide, which is why the control below fires at the
third stage as well as the first.

## The control on the selector

A survey whose selector was never tested is not evidence, and the
failure #1003 came from is a miss — a false sentence that nothing
caught. So the control that matters is whether the selector fires on a
string known to qualify.

**The positive control is the sentence #1002 removed.** At `5873d77^`
the operators page read `garam has no revocation, so an operator whose
certificate has not expired keeps authenticating either way — which is
why there is no way to remove a record here.` It is known to qualify:
it is the instance #1003 is built on, and it is known false. Running
the same pipeline over `web/src` at that commit emits it at stage one,
at `register-operator-form.tsx:46-48`, and it survives stage two. Stage
three admits it: it renders, and it states a general proposition about
what garam does. The selector fires.

**The negative controls are three strings known not to qualify**, one
rejected mechanically and two by judgement, so that a stage passing
them is not mistaken for the whole selector working. All three line
numbers are at `d748465`.

| Control | Expected | Where it was rejected |
|---|---|---|
| `max-w-2xl text-sm text-muted-foreground` (`register-operator-form.tsx:48`) | out — a class string | stage two, as a class string; absent from the stage-two output |
| `Register an operator` (`register-operator-form.tsx:46`) | out — a heading | survives to stage three, rejected there |
| `Name the operator.` (`web/src/lib/operator.ts:33`) | out — an instruction | survives to stage three, rejected there |

Two of the three reach stage three, which is the point: the mechanical
net does not decide anything, and a control aimed only at it would have
tested nothing. The pair that separates the third stage is
`Name the operator.` against `Every value needs a key.` — surface
siblings, one imperative and one declarative, sorted opposite ways.

## The count

The denominator, so that nine is readable as nine out of eighty-five
rather than nine out of a query that matched little.

The two mechanical stages ran as a throwaway script, which this pull
request does not add, so 818 and 368 are not reproducible from this
file alone. They are here to bound the funnel, not to carry the
argument: what carries it is the 85, and every one of those is listed
below with a path and a line a reader can open.

| Stage | Strings |
|---|---|
| Stage one: literals and JSX text with three or more words | 818 |
| Stage two: after dropping class strings and import specifiers | 368 |
| Stage three: rendered, and asserting garam's behaviour | 85 |

The first two numbers count source lines and the third counts
assertions, so the funnel is not a subset chain. Of the 368 that
survive stage two, 36 are in the generated `web/src/types/api.ts` and
36 in the shadcn primitives under `web/src/components/ui/`, leaving 296
authored lines; stage three was applied by reading the files those
point into. A rendered paragraph split over several source lines is one
run there and several assertions here. That is the unit that matters:
the paragraph #1002 corrected was four sentences, of which one was
false.

Of the 85:

| Verdict | Strings |
|---|---|
| Verifiable | 71 |
| False | 9 |
| Unverifiable as written | 5 |

The nine false strings carry eight distinct claims. `page.tsx:149` is
the heading over `page.tsx:150` and says the same thing, and
`page.tsx:93` and `page.tsx:150` assert the same proposition about the
adapter at two places on one page.

Where the eight sit matters more than the count. Six are on pages a
visitor reads without signing in — four on the landing page, one on the
shell that carries `/login` and `/register`, and one on `/login`
itself. The other two are inside the console, and both sit where a
person is about to act: one in the confirmation that withdraws consent,
one in the paragraph above the register-operator button, which is the
position the sentence #1002 removed sat in.

| Claim | Where |
|---|---|
| `the adapter that would run beside it is not built` | landing page, at `:93` and again at `:150` |
| `or any way to watch one work` | landing page, agents section |
| `Sign in with the Google account your organization invited.` | landing page, above the console link |
| `Policy-based authorization on every request` | sign-in shell, on `/login` and `/register` |
| `In the console today` and the three sections it names | landing page, closing section |
| `Accounts are created by invitation, not by signup.` | the login page (#1028) |
| `every certificate the authority signed fails the handshake from then on` | above the register-operator button |
| `Messages between this organization and {other} stop at once.` | inside the withdraw-consent confirmation |

## The assertions

| Where | What it asserts | Verdict | Checked against |
|---|---|---|---|
| `web/src/app/(marketing)/_components/policy-attachments.tsx:49` | A policy attaches to an identity: a user, a group, or an agent. | verifiable | `migrations/000003_policy_attachments.up.sql:1-8`; `internal/api/policy.go:203-208` |
| `web/src/app/(marketing)/page.tsx:19` | An organization grows by invitation, never by open signup. A single-use, expiring token admits a user with a fixed org role — admin or member, never owner. | verifiable | `migrations/000001_init.up.sql:57-66` (`accepted_at`, `expires_at`, `CHECK (role IN ('admin','member'))`); `internal/org/queries.sql:127-130`; `internal/api/invitation.go:38-41,85-88` |
| `web/src/app/(marketing)/page.tsx:24` | Groups bundle people; a project holds a unit of work. Both live under one organization, carry their own members, and can hold roles scoped to that resource alone. | verifiable | `migrations/000001_init.up.sql:17-47`; `internal/api/statement.go:79-83,106-108` |
| `web/src/app/(marketing)/page.tsx:29` | IAM-style documents, stored per organization and versioned append-only. Attaching one to a user, a group, or an agent decides who it reaches, and a matching Deny anywhere in the set ends the decision. | verifiable | `migrations/000002_policies.up.sql:3,13-38`; `migrations/000003_policy_attachments.up.sql:1-8`; `internal/authz/evaluator.go:52-59`; `internal/authz/server.go:69-86` |
| `web/src/app/(marketing)/page.tsx:34` | Organizations, groups, projects, policies, users, and agents all carry a GRN, so one shape answers who a subject is and what a policy is written about. | verifiable | `grn` is the primary key of all six: `migrations/000001_init.up.sql:1,10,17,27`, `000002_policies.up.sql:1`, `000004_agents.up.sql:5`. Operator is a seventh GRN kind the sentence omits (`internal/operator/operator.go:40`); the sentence does not say "only these". |
| `web/src/app/(marketing)/page.tsx:47` | garam mints an agent's GRN inside an organization and a project and never changes it, so which operator runs the agent is a relation a reassignment moves rather than part of the identity. That operator provisions it from inside a cluster garam never dials into. | verifiable | Minted server-side `internal/agent/agent.go:179-185`; GRN is the PK `migrations/000004_agents.up.sql:6`; assignment is a separate table `:23-30`. Remark: the reassignment the sentence describes is store-only — `internal/agent/queries.sql:17-24` moves the operator, and no HTTP, CLI or console path calls it with a new one. |
| `web/src/app/(marketing)/page.tsx:52` | A message reaches an agent when an adapter beside it long-polls a queue held in Postgres, so any garam instance can answer any poll. | verifiable | `internal/message/service.go:149-179`; `internal/machineapi/message.go:191-206`; claim state is columns, not process memory: `migrations/000008_messages.up.sql:10-30` |
| `web/src/app/(marketing)/page.tsx:52` | Channels such as Slack are consumers of that API rather than part of it. | unverifiable-as-written | `grep -rn slack` over Go, SQL, YAML and `web/src` matches only this string. Absence is consistent with the claim and settles nothing; the tree exercises no channel either way. |
| `web/src/app/(marketing)/page.tsx:57` | An agent presents a client certificate whose SAN URI is its GRN, on a listener that takes no session cookie. It holds no user row and no org membership; that GRN is what lets it message every recipient in its own organization, a policy attached to it narrows that with a Deny, and no policy widens it past the organization (ADR-0024). | verifiable | `internal/ca/signing.go:73-97`; `internal/machine/service.go:81-93`; `internal/machine/listener.go:57-61`; `internal/machineapi/message.go:99-102,151-158`; `internal/authz/server.go:80-86` |
| `web/src/app/(marketing)/page.tsx:93` | garam holds the identities, the structure they belong to, and the policy documents that decide what each may do. The people are here today; an agent already has an identity, is a subject of the same policies, and exchanges messages on a listener of its own — the adapter that would run beside it is not built. | false | Every clause but the last holds (`migrations/000004_agents.up.sql:5`; `migrations/000003_policy_attachments.up.sql:1-8`; `internal/machineapi/message.go:123-142`; `internal/cli/machine.go:174-192`). The adapter is built. `internal/delivery/` is seven implementation files carrying a `Gateway` interface and an `HTTPGateway`, and its package comment says so outright: "It is what docs/architecture/adapter.md calls the adapter (ADR-0013, ADR-0018), named here for what it owns" (`internal/delivery/delivery.go:1-12`). `garam adapter` runs that loop beside one agent (`internal/cli/cli.go:6-8`; `internal/cli/delivery.go`), and `docs/architecture/overview.md:109-119` lists it under In scope. `internal/adapter` does not exist is a path check standing in for a capability check: the package is named `delivery`, "named here for what it owns", so a query on the name found nothing and nothing-found was read as not-built. What is unbuilt is the watching half alone (`overview.md:250-255`). |
| `web/src/app/(marketing)/page.tsx:108` | Sign in with the Google account your organization invited. | false | A password credential of garam's own always renders beside it (`web/src/app/(auth)/login/page.tsx:56`, `internal/api/password.go:100-110`, ADR-0041), and Google is registered only when both credentials are configured (`internal/cli/serve.go:196-206`; `deploy/charts/garam/values.yaml:59-61` defaults them empty). The login page states the conditional version at `web/src/app/(auth)/login/page.tsx:49-51`; this page did not follow. |
| `web/src/app/(marketing)/page.tsx:123` | The structure people are invited into, and the policy documents that decide what anything in it may reach. | verifiable | `migrations/000001_init.up.sql:57-66`; `internal/authz/server.go:69-86` |
| `web/src/app/(marketing)/page.tsx:149` | Reachable, not yet watched | false | The heading of the same claim as line 150. `GET /orgs/{org}/agents/{agent}/watch` is served: `internal/api/watch.go:32-100`, registered `api/server.gen.go:2483`, wired `internal/cli/serve.go:108-126`. |
| `web/src/app/(marketing)/page.tsx:150` | The listener, the certificate authority behind it, and the message queue are built. What is not is the adapter that would run beside the agent, or any way to watch one work. | false | Both halves are false. The adapter's delivery loop is built, on the evidence in the `page.tsx:93` row above; `internal/adapter` absent is a path check and not a capability check. The watch half: the stream is implemented (`internal/api/watch.go:32-100`), registered (`api/server.gen.go:2483`), wired in `serve api` (`internal/cli/serve.go:108-126`), crosses processes (`migrations/000011_agent_watches.up.sql:16-31`; `internal/watch/postgres.go:18-20,138`) and is covered end to end (`tests/integration/watch_test.go:44-110`). `docs/architecture/overview.md:133-136` lists it under In scope. `:250-255` says in its own words that "its delivery loop is built and the §Watching half of adapter.md is not", so what it puts out of scope is the console route and the adapter's watching half — not the adapter. |
| `web/src/app/(marketing)/page.tsx:174` | A GRN names every resource in the system. | unverifiable-as-written | The code defines no "resource". Seven GRN service types exist (`internal/org/org.go:32-34`, `internal/authz/policy.go:18`, `internal/user/user.go:19`, `internal/operator/operator.go:40`, `internal/agent/agent.go:23`), and seven persisted tables are keyed by something else — messages, invitations, sessions, agent watches, certificate authorities, message consents, policy versions. Under the narrow reading the sentence is circular; under the broad reading it is false. |
| `web/src/app/(marketing)/page.tsx:174` | Its organization and project segments name the resources that own it, so an agent is addressed inside an organization the way a group or a project is. | verifiable | `internal/grn/grn.go:7-14,42-47` |
| `web/src/app/(marketing)/page.tsx:187` | `default` is the project segment of an org-level resource. API paths address organizations, groups, projects, and policies by the last segment. | verifiable | `internal/grn/grn.go:36-37`; `internal/api/org.go:245-247`, `group.go:229-231`, `project.go:231`, `policy.go:254-256`; `api/openapi.yaml:274,411,598,1012` |
| `web/src/app/(marketing)/page.tsx:200` | The organizations you belong to, and for each one its groups, projects, and members with their roles. | false | The heading above it at `:197` reads `In the console today`, which makes the sentence an inventory; it names three of eight sections. `web/src/app/console/_components/navigation.ts:25-34` carries the list: overview, groups, projects, operators, agents, members, consents, policies. Operators, agents and consents all read live data and carry forms. The weakest of the seven false strings: each clause is individually true, and the sentence does not say "only". |
| `web/src/app/(auth)/layout.tsx:5` | Membership by invitation, with an org role | verifiable | `migrations/000001_init.up.sql:49-53,57-66`; `internal/api/invitation.go:90-97`; `internal/api/password.go:83-90`. Remark: an org's creator becomes owner without an invitation (`internal/api/org.go:43`). |
| `web/src/app/(auth)/layout.tsx:6` | Google sign-in, server-side sessions | unverifiable-as-written | Server-side sessions hold (`migrations/000001_init.up.sql:68-73`; `internal/api/auth.go:176-190`). Whether garam offers Google sign-in is a deployment fact the sentence does not name: `internal/cli/serve.go:196-206` registers the provider only when both credentials are set, and `deploy/charts/garam/values.yaml:59-61` defaults them empty. The bullet renders on `/login` and `/register` unconditionally while the button beside it is gated on `listProviders()` (`web/src/app/(auth)/login/page.tsx:41,57`). |
| `web/src/app/(auth)/layout.tsx:7` | Policy-based authorization on every request | false | There is no authorization middleware: the whole stack is `RecoverPanics`, `logRequests`, `otelhttp`, `MaxBytesHandler` (`internal/cli/serve.go:162-174`), and authorization is a per-handler `authorizeOn` call (`internal/api/org.go:194-235`). `/healthz`, `/readyz` (`internal/health/health.go:38-41`), `GET /metrics` (`internal/cli/serve.go:164`), every `/auth/*` route and `/me` reach a handler with no policy decision. A charitable reading — that authorization, where it happens, is policy-based rather than role-based — does hold (`internal/api/statement.go`). |
| `web/src/app/(auth)/layout.tsx:42` | `grn:<organization>:<project>:<service_type>:<identifier>` | verifiable | `internal/grn/grn.go:5,42-47` |
| `web/src/app/(auth)/login/page.tsx:50` | Use the account you registered, or the Google account your organization invited. | verifiable | Rendered only when `listProviders()` holds `google` (`:41`), which is the backend's registered set (`internal/api/auth.go:111-121`). |
| `web/src/app/(auth)/login/page.tsx:51` | Use the account you registered. | verifiable | The password form renders unconditionally (`:56`); `internal/api/password.go:100-110` |
| `web/src/app/(auth)/login/page.tsx:52` | Accounts are created by invitation, not by signup. | false | `internal/api/password.go:53-65` and `internal/api/invitation.go:68-97` hold for garam's own password accounts, which an invitation mints. "No open signup operation in `api/openapi.yaml`" is a check on the specification's operation names, and it does not reach the path that creates an account without one: `internal/api/auth.go:148-172` `upsertUser` "returns the user for the identity, creating it on first login", so completing OAuth writes a `users` row with no invitation consulted. The sentence is false as written. Filed as #1028. |
| `web/src/app/(auth)/register/page.tsx:40` | Registration takes an invitation: organizations grow by invitation, not by signup. The account this creates is garam's own, separate from any Google account you hold. | verifiable | `api/openapi.yaml:1475-1481`; `internal/api/password.go:17-23,53-65`; `internal/api/auth.go:150-153` |
| `web/src/features/auth/components/password-register-form.tsx:57` | The last part of the invitation link you were sent. | verifiable | The link is built as `/console/invitations/${token}` (`web/src/app/console/orgs/[org]/members/_components/invite-member-form.tsx:43`) |
| `web/src/features/auth/components/password-register-form.tsx:74` | Lowercase letters, digits and hyphens, at most 58 characters. It names the account and does not change. | verifiable | `internal/grn/grn.go:28,139-145`; `internal/api/password.go:23,222-224`; `UpdateUser` cannot move a `grn` (`internal/user/queries.sql:13-20`) |
| `web/src/features/auth/components/password-register-form.tsx:89` | At least 15 characters. | verifiable | `internal/authn/password.go:66,81` |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:80` | The identifier of the operator that is to run this agent. | verifiable | `internal/agent/definition.go:102-123,132-143`; `internal/api/agent.go:110-113` |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:81` | It reads the definition and builds the agent in its own cluster | unverifiable-as-written | garam serves the read (`internal/machineapi/definition.go:15-35`), but what an operator does with it is software this repository does not contain. Nothing here can make the sentence true or false. |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:82` | garam never dials it. | verifiable | No outbound client to an operator exists, which is what the sentence claims. The clause this row first gave for it was wrong on its own terms: of the two `http.Client` constructions at `internal/cli/delivery.go:117,137`, only `adapterClient` (`:137`) calls garam — `gatewayClient` (`:117`) "reaches the platform beside this adapter", which is the agent platform's gateway. `internal/cli/probe.go:37` makes a third outbound call through `http.DefaultClient`, which a query for `http.Client` constructions does not reach. None of the three dials an operator, so the verdict rests on the capability rather than on the query. |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:90` | What the operator constructs the agent from. garam stores the keys and the strings and interprets neither, so the keys are the operator's to choose. | verifiable | `internal/agent/definition.go:41-43,65-77` checks shape and never a name; values pass through at `internal/api/agent.go:218` and `internal/machineapi/definition.go:29`; storage is opaque `jsonb` (`migrations/000010_agent_definitions.up.sql:70-71`). Remark: "to choose" is bounded by the segment character rule. |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:131` | {bytes} of 4096 bytes | verifiable | `internal/agent/definition.go:18,43` (`valueMax = 4096`, bytes on both sides) |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:170` | {n} of 32 values — a definition carries no more. | verifiable | `internal/agent/definition.go:16,37` (`valuesMax = 32`) |
| `web/src/app/console/orgs/[org]/agents/_components/define-agent-form.tsx:192` | Defined. garam minted this identifier for the agent; the operator builds it and no agent answers under it until one does. | verifiable | `internal/agent/definition.go:103` → `internal/agent/agent.go:179-185`; the client cannot supply one — `AgentDefinitionCreate` carries `operatorGrn` and `values` and no identifier (`api/openapi.yaml:1939-1953`; the range this row first cited, `:1889-1903`, is `OperatorEnrollment`); only a definition row is written (`internal/agent/postgres_definition.go:17-19`, whose comment reads "It writes no agent row") |
| `web/src/app/console/orgs/[org]/consents/_components/consent-list.tsx:18` | Both halves stand, so messages cross between this organization and {other} in either direction. | verifiable | `internal/message/queries.sql:161-162` normalises with `least`/`greatest`; `migrations/000015_cross_org_consent.up.sql:19-20` stores one row per unordered pair |
| `web/src/app/console/orgs/[org]/consents/_components/consent-list.tsx:21` | This organization has granted its half. Messages stay refused until {other} grants its own. | verifiable | `internal/message/queries.sql:163-164`; `internal/machineapi/message.go:49-50` |
| `web/src/app/console/orgs/[org]/consents/_components/consent-list.tsx:23` | {other} has granted its half. Messages stay refused until this organization grants its own. | verifiable | Same evidence as the row above |
| `web/src/app/console/orgs/[org]/consents/_components/grant-consent-form.tsx:31` | Name the other organization by the identifier it is addressed by. Granting stands this organization's half of the pair and nothing more — messages cross once that organization has granted its own, and granting a half that already stands changes nothing. | verifiable | `internal/message/queries.sql:120-127` — the non-granting side's column is preserved by `coalesce`. Remark: a re-grant re-stamps the timestamp, which nothing the API exposes can see (`internal/api/consent.go:108-122`). |
| `web/src/app/console/orgs/[org]/consents/_components/grant-consent-form.tsx:66` | Granted. Messages now cross between this organization and {other}. | verifiable | `internal/message/consent.go:45`; `internal/api/consent.go:108-122` |
| `web/src/app/console/orgs/[org]/consents/_components/grant-consent-form.tsx:67` | Granted this organization's half for {other}. Messages stay refused until it grants its own. | verifiable | `internal/message/queries.sql:163-164` |
| `web/src/app/console/orgs/[org]/consents/_components/withdraw-consent-form.tsx:47` | Messages between this organization and {other} stop at once. | false | New posts do stop at once — the send path reads consent live (`internal/machineapi/message.go:110-116`; `internal/message/postgres_consent.go:48-54`). A message that already crossed does not: `ClaimMessage` has no consent predicate (`internal/message/queries.sql:33-49`), `Withdraw` deletes no messages (`internal/message/postgres_consent.go:33-42`), and the claim path never consults the consent store (`internal/machineapi/message.go:191-216`). ADR-0035 does not decide otherwise — its only mention of traffic already flowing is in a rejected alternative. |
| `web/src/app/console/orgs/[org]/consents/_components/withdraw-consent-form.tsx:47` | {other} keeps its own half, so granting again here would let them cross. | verifiable | `internal/message/queries.sql:137-142`; `tests/integration/postgres_test.go:4003-4008` |
| `web/src/app/console/orgs/[org]/consents/_components/withdraw-consent-form.tsx:48` | Nothing crosses today, because {other} has not granted its half, and withdrawing removes the relation. | verifiable | `internal/message/queries.sql:149-153`; the branch is reachable only when `stands` is false and `grantedHere` is true (`consent-list.tsx:63-69`) |
| `web/src/app/console/orgs/[org]/members/_components/invite-member-form.tsx:78` | Share this link with the person you invited. It works once and then expires. | verifiable | `internal/org/queries.sql:127-130` (`WHERE token = $1 AND accepted_at IS NULL`); `migrations/000001_init.up.sql:63`; `internal/api/invitation.go:85-88` |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:49` | Registering records that this organization has the operator and hands back a one-time token it enrols with. | verifiable | `internal/api/operator.go:44-56` — `operator.NewEnrollment` then `Register(o, enrollment)`, answered as `RegisteredOperator`; `internal/operator/queries.sql:32-37` |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:50` | The operator generates its own key pair in its own cluster | unverifiable-as-written | What an operator does with its own key is software this repository does not contain. Consistent with it: garam signs a certificate signing request and nothing else (`internal/ca/service.go:116`; `internal/machineapi/identity.go:34-37`), so no private key crosses. |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:51` | presents the token to get a certificate signed — no private key is ever shown here or held by garam. | verifiable | `internal/machineapi/identity.go:38-57` takes a token and a certificate signing request; `internal/operator/service.go:82-104` spends the token and signs; no key material is generated or returned anywhere on the path |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:52` | A record cannot be removed, so a mistyped identifier stays in the list | verifiable | `api/openapi.yaml:891,952` serves only `registerOperator` and `listOperators`; `internal/operator/queries.sql` holds no delete of `operators`; `internal/operator/store.go` offers none |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:53` | a control that removed one would look like revoking an operator and would revoke nothing, because a handshake is checked against the certificate and this organization’s certificate authority and never against this record | verifiable | `internal/machine/listener.go:54-56` runs `Service.Authenticate` inside the handshake; `internal/machine/service.go:46-57` checks the leaf against the org CA alone; `internal/machine` imports no `internal/operator` |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:57` | Short of expiry, what does stop an operator is deleting this organization | verifiable | No suspend or revoke exists; the certificate serial is read only on renewal (`internal/operator/queries.sql:22-27`); `internal/org/queries.sql:20-21` is a hard delete and `migrations/000005_certificate_authorities.up.sql:12` cascades |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:58` | that takes the certificate authority with it, every certificate the authority signed fails the handshake from then on, and garam does not bring the authority back. | false | The handshake no longer fails. `internal/machine/listener.go:55-61` returns nil when `Authenticate` fails, deliberately — refusing there would take the enrollment route down with every caller that has no certificate yet — so the connection completes anonymous and the refusal moves to each handler that asks for an identity. At `78f3e8d` this was true: `ClientAuth: tls.RequireAnyClientCert` and a `VerifyPeerCertificate` that returned the error. `b16a896` changed both, in the pull request that rewrote this paragraph. The reader’s conclusion still holds — deleting the organization does stop the operator — and the mechanism named does not. The cascade and the no-return half are unaffected (`migrations/000005_certificate_authorities.up.sql:12`; `internal/ca/service.go:151-176` cannot create one for an org that is gone). |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:131` | This is the only time this token is shown. garam keeps no copy and no page can retrieve it. | verifiable | `migrations/000023_operator_enrollment_tokens.up.sql:19-20` stores `token_hash` and no value; `internal/operator/enrollment.go:33-35`; no operation reads a token back (`api/openapi.yaml:891,952`) |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:132` | If you lose it, register the operator again under a different identifier, or have an administrator issue a certificate out of band. | verifiable | A second registration of one identifier is refused (`internal/operator/postgres.go:48`, `ErrOperatorAlreadyRegistered`), and no route re-mints a token for an existing record; `garam issue-certificate` is still a command (`internal/cli/cli.go:3`) |
| `web/src/app/console/orgs/[org]/operators/_components/register-operator-form.tsx:168` | Give it to the operator, which presents it with a certificate signing request to POST /enrollment on garam’s machine listener. It is spent by the first attempt and stops working at {time}. | verifiable | `api/machine.yaml:147,151`; `internal/operator/service.go:82-90` spends before it signs, so a refused request costs the token; `internal/operator/queries.sql:44-47` deletes on `expires_at > now()` |
| `web/src/components/api-unreachable.tsx:25` | The garam API is not answering, so this page cannot show {subject}. This is not a sign-in problem and not a permission problem: try again once the API is back. | verifiable | `web/src/lib/garam.ts:29-33` raises `GaramUnreachableError` only where the fetch itself failed; a refusal is `GaramApiError` and a 401 is `isUnauthenticated` |
| `web/src/app/not-found.tsx:14` | That page does not exist, or it is not yours to see. | verifiable | `web/src/features/orgs/api/orgs.ts:268-274` turns both 403 and 404 from an org-scoped read into `notFound()`; an unmatched URL reaches the same file (`stack-nextjs.md` §App Router file conventions) |
| `web/src/app/console/orgs/page.tsx:25` | The organizations you belong to. | verifiable | `internal/api/org.go:62` calls `ListOrgsByMember` with the caller's GRN, never the unfiltered `ListOrgs` (`internal/org/queries.sql:8-11`) |
| `web/src/app/console/orgs/new/page.tsx:15` | You become the organization's first owner. | verifiable | `internal/api/org.go:43` adds the creator as `org.RoleOwner` immediately after `CreateOrg` |
| `web/src/app/console/orgs/[org]/groups/page.tsx:25` | Bundles of members this organization grants access through. | verifiable | `migrations/000001_init.up.sql:17-24,37-41`; `internal/api/statement.go:79-83` |
| `web/src/app/console/orgs/[org]/projects/page.tsx:25` | The units of work this organization owns. | verifiable | `migrations/000001_init.up.sql:27-35` |
| `web/src/app/console/orgs/[org]/members/page.tsx:36` | The people who belong to this organization. | verifiable | `migrations/000001_init.up.sql:49-53`; `internal/org/queries.sql` `ListOrgMembers` |
| `web/src/app/console/invitations/[token]/page.tsx:19` | Accept the invitation to become a member. Invitations work once and expire. | verifiable | Same evidence as the row above |
| `web/src/app/console/orgs/[org]/policies/page.tsx:16` | The policy documents this organization authorizes against. | verifiable | `migrations/000002_policies.up.sql:3`; `internal/authz/server.go:69-86` |
| `web/src/app/console/orgs/[org]/policies/page.tsx:20` | Not readable here yet | verifiable | No file under `web/src` calls a policy read |
| `web/src/app/console/orgs/[org]/policies/page.tsx:21` | Policies are managed over the HTTP API; the console does not read them yet. | verifiable | `api/openapi.yaml:1012-1198` serves the policy operations; nothing under `web/src` calls one |
| `web/src/app/console/orgs/[org]/operators/page.tsx:35` | The operators this organization has registered. A record is what garam holds — an operator authenticates with the certificate it holds, and registering hands it a one-time token to obtain its first one with. | verifiable | `internal/machine/service.go:30-58` consults only the leaf and the org CA; `internal/api/operator.go:38-57` mints an enrollment and writes it with the record; `internal/operator/enrollment.go:20` sets the ten-minute window |
| `web/src/app/console/orgs/[org]/agents/page.tsx:41` | The agents this organization holds and the definitions it says they should be made from. garam learns an agent exists when an operator reports one. | verifiable | The only two creators of `agents` are `internal/agent/postgres_agent.go:18-28` and `postgres_definition.go:125-134`, both behind `operates()` (`internal/agent/service.go:170-175`); `api/openapi.yaml:732-733` serves only a read. "The only two writers" was wrong: `ReportAgentProvisioningState` and `ReportAgentRuntimeState` each `UPDATE agents` (`internal/agent/queries.sql:38-58`, ADR-0053). Both move an existing row's state and create none, so what this sentence asserts is unaffected. |
| `web/src/app/console/orgs/[org]/agents/page.tsx:54` | No operator has reported an agent to this organization. | verifiable | `internal/agent/queries.sql:8-12` reads the `agents` table alone; a defined-but-unclaimed agent has no row (`migrations/000010_agent_definitions.up.sql:39-43`) |
| `web/src/app/console/orgs/[org]/agents/page.tsx:63` | What this organization says its agents should be. A definition is a record: an operator reads it and builds the agent elsewhere. | verifiable | `internal/agent/queries.sql:60-64` is the only write and there is no update or delete; the operator reads it at `internal/machineapi/definition.go:15-35` |
| `web/src/app/console/orgs/[org]/consents/page.tsx:34` | The organizations this one exchanges messages with. A relation is a pair: a message crosses only while both organizations hold their half, and either side can withdraw its own. | verifiable | `internal/machineapi/message.go:99-101`; `internal/message/queries.sql:137-142,158-165` |
| `web/src/app/console/orgs/[org]/consents/page.tsx:42` | This organization holds no relation with another, so a message between one of its agents and an agent outside it is refused back to the sender. | verifiable | `internal/machineapi/message.go:49-52,70-80` discards the submitted message and queues a refusal addressed to the sender; `tests/integration/postgres_test.go:3951-3956` |
| `web/src/lib/agent-definition.ts:57` | A definition carries at most 32 values; this one carries {n}. | verifiable | `internal/agent/definition.go:16,37` |
| `web/src/lib/agent-definition.ts:65` | Every value needs a key. | verifiable | `internal/agent/definition.go:66` rejects the empty key; refused at `:39-41` |
| `web/src/lib/agent-definition.ts:69` | A key holds at most 64 characters; one here holds {n}. | verifiable | `internal/agent/definition.go:22,66` |
| `web/src/lib/agent-definition.ts:74` | The key "{k}" holds something other than lowercase letters, digits and hyphens. | verifiable | `internal/agent/definition.go:68-73` |
| `web/src/lib/agent-definition.ts:79` | The key "{k}" is used twice; a definition holds one value per key. | verifiable | `api/server.gen.go:178` (`map[string]string`); `migrations/000010_agent_definitions.up.sql:19,42`. Remark: the server refuses no repeat — `encoding/json` collapses it last-wins — so the refusal is the console's own and the state the sentence describes is the server's. |
| `web/src/lib/agent-definition.ts:85` | The value under "{k}" is {n} bytes; a value holds at most 4096. | verifiable | `internal/agent/definition.go:18,43` |
| `web/src/lib/operator.ts:37` | An operator identifier holds at most 64 characters; this one holds {n}. | verifiable | `internal/grn/grn.go:28,139`; reached from `internal/operator/operator.go:93-94` |
| `web/src/lib/operator.ts:42` | An operator identifier holds only lowercase letters, digits and hyphens. | verifiable | `internal/grn/grn.go:141-145` |
| `web/src/lib/password-credentials.ts:60` | A username holds at most 58 characters; this one holds {n}. | verifiable | 64 − 6: `internal/grn/grn.go:28,139` and `internal/api/password.go:23,222-224` (`localPrefix = "local-"`). Remark: no standalone 58 exists server-side, so the number is derived and will drift silently if either input moves. |
| `web/src/lib/password-credentials.ts:65` | A username holds only lowercase letters, digits and hyphens. | verifiable | `internal/grn/grn.go:141-145` via `internal/api/password.go:222-224` |
| `web/src/lib/password-credentials.ts:83` | A password holds at least 15 characters. | verifiable | `internal/authn/password.go:66,81` — runes on both sides |
| `web/src/lib/password-credentials.ts:87` | A password holds at most 128 bytes. | verifiable | `internal/authn/password.go:71,84` — bytes on both sides |

## The strata outside the population

What sits beside the population, with its size and how far it was
read, so that a reader can tell "measured and clean" from "not
measured".

**`web/src/types/api.ts` — 36 stage-two strings, not classified.** The
file is generated: `web/package.json:14` runs
`openapi-typescript ../api/openapi.yaml -o src/types/api.ts`, and
regenerating it changes nothing, which is what
`docs/convention/README.md` §Scope makes the test. Its prose is
`api/openapi.yaml`'s description fields copied through, so it is bound
where it is authored and not here. It carries assertions of exactly the
kind this survey counts — the consent, agent-state and invitation
paragraphs among them — and none was checked.

**`web/src/components/ui/` — 36 stage-two strings, read and empty.**
The shadcn primitives. The 36 are `import * as React from "react"`
lines, type unions the second stage did not catch, and two messages
addressed to a developer rather than a user
(`sidebar.tsx:49`, `sidebar.tsx:199`). None asserts anything about
garam.

**Comments — swept, and not finished.** A comment reaches no user, so
it is outside the population by the first rule. It makes the same kind
of claim and nothing binds it either: `code-comments.md` §When to
comment requires a comment to be corrected when *the code below it*
moves, which does not reach a comment asserting something about Go or
SQL in another directory. A second sweep was run over the same files
the population comes from, at `78f3e8d` — two commits back, and
`register-operator-form.tsx` is the one file in its scope that has
moved since. Its total is not comparable to the 85 above: every one of
the 85 was checked, and this sweep checked about half of what it
found.

| | Count |
|---|---|
| Comment-borne assertions found, across 85 files | 152 |
| Checked against code | 82 |
| Not reached | 70 |
| False among those checked | 7 |
| Unverifiable as written among those checked | 1 |

The seventy left unread skew toward claims about the console's own
structure, and all eight defects below were found among claims reaching
into Go, SQL or a specification, which is what the sweep prioritised.
So the rate in the unread remainder is unknown, not low.

| Where | What it asserts | Verdict | Checked against |
|---|---|---|---|
| `web/src/app/console/orgs/[org]/agents/page.tsx:22` | an agent is what an operator reported, a definition is what the organization decided, and nothing joins them (ADR-0033) | false | The claim writes the agent row under the definition's own `agentGRN` in one transaction (`internal/agent/postgres_definition.go:120-134`), so `agents.grn` and `agent_definitions.agent_grn` are the same identifier once a definition is claimed. ADR-0033 is the decision that joins them. |
| `web/src/app/console/orgs/[org]/members/page.tsx:23` | The form is rendered for all of them and the API refuses a caller that holds only the read — the same shape the operators and agents sections take. | false | `internal/org/role.go:112-114` puts `ActionAgentDefine` in `memberMachineActions`, so on the agents section every role that reads may also write. The agents page says so itself at `agents/page.tsx:26-28`. |
| `web/src/app/console/orgs/[org]/operators/page.tsx:24` | the API refuses a caller that holds only the read — the same shape the agents section takes | false | Same evidence. The operators half is right — `ActionOperatorRegister` is in `adminMachineActions` alone (`internal/org/role.go:105-107`) — and the comparison to agents is not. |
| `web/src/app/console/orgs/[org]/_components/resource-list.tsx:4` | A group or project: both carry a name and the caller's scoped roles. | false | The roles are the resource's own, granted to its members: `internal/org/group.go:20-22`; `api/openapi.yaml:1593-1598`. `internal/api/group.go:67` reads them without reference to the caller. |
| `web/src/app/console/page.tsx:4` | `/console` is where the OAuth callback and the proxy send a signed-in visitor | false | The callback half holds (`internal/api/auth.go:108`). The proxy runs only on `/console/:path*` (`web/src/proxy.ts:23`) and answers with `next()` or a redirect to `/login` (`:13-16`), so it sends nobody to `/console`. The login and register pages do (`web/src/app/(auth)/login/page.tsx:35-36`). |
| `web/src/app/console/orgs/[org]/agents/_components/agent-list.tsx:34` | `Updated` is when a report last landed | false | `updated_at` is also written when the row is created, by both writers (`internal/agent/postgres_agent.go:25`; `internal/agent/postgres_definition.go:125-132`). An agent the same comment block calls `pending` — no report yet — still shows a date. The clause beside it, `nothing expires one`, holds: `migrations/000013_expiry_sweeps.up.sql` sweeps sessions and invitations only. |
| `web/src/lib/garam.ts:70` | True when the error is the backend rejecting an absent session. | false | `isUnauthenticated` is `status === 401` (`:71-73`), which a present-but-expired or revoked session also draws. Three callers rely on that wider meaning and say so — `garam.ts:140-142`, `web/src/app/console/_components/console-sidebar.tsx:20-22`, `web/src/features/auth/api/get-session.ts:38-39`. |
| `web/src/app/console/orgs/[org]/consents/_components/consent-list.tsx:8` | A waiting relation is the state most of them are in | unverifiable as written | A claim about how rows are distributed in a live deployment. Nothing in `migrations/000015_cross_org_consent.up.sql` or `internal/message/queries.sql:119-173` makes one state more common than another. |

Two comments cite `ADR-0010`, whose status line reads
`superseded by ADR-0044` (`web/src/app/(auth)/login/page.tsx:30`,
`web/src/features/auth/api/list-providers.ts:8`). The substance they
cite it for survives — `/auth/providers` still exists
(`internal/api/auth.go:110-119`) — so this is a stale citation rather
than a false claim, and it is the kind a reader cannot tell from one
without opening the ADR.

**Backend prose the console renders verbatim — not counted.** Eleven
call sites return `error.message` from a `GaramApiError` into a Server
Action's error state, which a form renders inside `role="alert"` —
`web/src/features/orgs/api/register-operator.ts:70` and
`web/src/features/auth/api/sign-in-with-password.ts:67` among them. So
a sentence written in Go or in `api/openapi.yaml` reaches a console
reader unchanged. Those strings are outside `web/src` and outside what
#1003 asked for, and they are on the same surface as the ones inside
it.

**Not read at all.** `web/e2e/`, `web/src/testing/` and every
`__tests__` directory. A test's prose reaches no user.

One mechanism already exists on part of this ground and is worth the
PM's attention, because it is both a precedent and a demonstration of
the failure mode. `web/src/app/(marketing)/__tests__/claims.test.ts`
binds 15 landing-page claims to repository facts — a path that must or
must not exist, a migration that must name a table, an ADR that must be
`accepted` — and `make ci` runs it. It did not catch `page.tsx:150`,
because the fact it pins that claim to is the absence of the console
route `web/src/app/console/orgs/[org]/agents/[agent]` and of
`internal/adapter` (`claims.test.ts:91-104`), while the sentence a
reader meets says `any way to watch one work`. On the watch half the
harness holds the claim the author meant and the page states a wider
one. On the adapter half it holds nothing: `internal/adapter` absent is
the path check the rows above correct, so the check is green on a fact
that does not carry the claim, and `make ci` has been running it that
way (§What re-checking the clean rows found). No such harness exists
for the console pages at all.

## What re-checking the clean rows found

#1025 found the `internal/adapter` verdict wrong and asked what else in
this file rested on the same kind of evidence. The miss was in a row
marked `verifiable`, and the review that merged this file sampled three
of the seven `false` verdicts and no `verifiable` one, so the clean
rows are the ones nothing looked at twice.

**The population is every `verifiable` row in §The assertions, and all
73 were read rather than filtered.** A row is counted here when at
least one clause of its Checked against cell is a negative existential
— an assertion that nothing in the tree does X — such that the verdict
would change if such a thing turned out to exist. That is the class
exposed to the failure #1025 names: a query on a name finds nothing,
and nothing-found is read as the capability being absent. Fifteen of
the 73 are in that class.

**The control on the fifteen.** Parsing the tables in this file returns
73 `verifiable`, 7 `false` and 5 unverifiable in §The assertions and 8
rows in §The strata outside the population, which are the totals this
file stated for itself before the corrections above — so 73 is the
whole population and not what one query happened to match. Filtering
those 73 by negative vocabulary instead of reading them returned 17: it
missed two whose evidence opens on a negative the pattern did not carry
(`No suspend or revoke exists`, and `internal/machine` imports no
`internal/operator`) and admitted four whose absence sits in a Remark
rather than under the verdict. The regex is reported beside the reading
because the disagreement between them is the measure of how much
judgement the boundary takes, and a filtered population would have
reported a smaller number with nothing to say it was short.

**Each row was re-checked at `d748465`**, the commit this file is
pinned to, so a verdict that was wrong when it was written is not
confused with one that has drifted since.

| Row | What its evidence rested on | Outcome |
|---|---|---|
| `page.tsx:93` | `internal/adapter` does not exist | **Moved to `false`.** The row above carries the evidence. |
| `login/page.tsx:52` | no open signup operation in `api/openapi.yaml` | **Moved to `false`.** A check on the specification's operation names, which does not reach `upsertUser` (`internal/api/auth.go:148-172`). Filed as #1028. |
| `define-agent-form.tsx:82` | no outbound client to an operator exists | Verdict holds; **evidence was wrong**. `internal/cli/delivery.go:117` is `gatewayClient`, which reaches the agent platform rather than garam, and `internal/cli/probe.go:37` dials through `http.DefaultClient`, which a query for `http.Client` constructions does not reach. Corrected in the row. |
| `agents/page.tsx:41` | the only two writers of `agents` | Verdict holds; **evidence was wrong**. `agents` has four writers: the two named create rows, and `ReportAgentProvisioningState` and `ReportAgentRuntimeState` each update one (ADR-0053). Corrected in the row. |
| `define-agent-form.tsx:192` | the client cannot supply one | Verdict holds; **citation was wrong**. `api/openapi.yaml:1889-1903` is `OperatorEnrollment`; the schema meant is `AgentDefinitionCreate` at `:1939-1953`. Corrected in the row. |
| `password-register-form.tsx:74` | `UpdateUser` cannot move a `grn` | Holds. `internal/user/queries.sql` read whole: `CreateUser` is the only insert and `UpdateUser` sets no `grn`. |
| `register-operator-form.tsx:51` | no key material on the path | Holds, and the clause is scoped to the path rather than to the tree: enrollment generates and returns none, while renewal does return one — which the sentence does not reach. |
| `register-operator-form.tsx:52` | no delete of `operators` | Holds. `internal/operator/queries.sql` read whole, and `Store` offers `Register`, `Get` and `ListByOrg` and nothing else. A record does go when its organization does, by the `ON DELETE CASCADE` on `orgs` — which is the mechanism the row at `:57` describes. |
| `register-operator-form.tsx:53` | `internal/machine` imports no `internal/operator` | Holds. |
| `register-operator-form.tsx:57` | no suspend or revoke exists | Holds. |
| `register-operator-form.tsx:131` | no operation reads a token back | Holds. `MintOperatorEnrollment` writes `token_hash` alone and `SpendOperatorEnrollment` returns `operator_grn` alone. |
| `register-operator-form.tsx:132` | no route re-mints a token | Holds. `Register` is the only writer and refuses a second registration of one identifier. |
| `policies/page.tsx:20` | no file under `web/src` calls a policy read | Holds. No policy path is fetched anywhere under `web/src`. |
| `policies/page.tsx:21` | nothing under `web/src` calls one | Holds. Same evidence. |
| `agents/page.tsx:63` | the only write, and no update or delete | Holds. `LockAgentDefinition` is a `SELECT … FOR UPDATE` and writes nothing. |

So: fifteen rows exposed, two moved, three held on evidence that had to
be corrected under them, ten sound. **The count is the result whether
or not any row moved**, because what it says is how much of this file
rested on nothing-found — and a fifth of the clean rows did.

Four further rows carry a negative existential in a Remark rather than
under the verdict — `page.tsx:47`, `grant-consent-form.tsx:31`,
`agent-definition.ts:79` and `password-credentials.ts:60`. They are
named here so that a reader who counts them gets nineteen rather than
concluding the fifteen was drawn narrowly. None of the four rests its
verdict on the remark.

**This file contradicted itself, and that is worth more than either
half.** The `define-agent-form.tsx:82` row cites
`internal/cli/delivery.go` as `garam adapter` — in a row marked
`verifiable` — while `page.tsx:93` and `page.tsx:150` said the adapter
was not built. One evidence cell named the thing another two said did
not exist. Nothing reads across the rows of a table, which is why a
document is capable of holding both.

## What this does not settle

The scope question is not answered here, and this survey is capable of
supporting either answer, so both readings are stated.

**Toward binding it.** Eight false claims on one surface is not one
file that slipped. Seven of the nine strings are on pages a signed-out
visitor reads, and two sit where a person is about to act — the
position the sentence #1002 removed sat in.

Five of the eight drifted and three were false the day they were
written, and the split is what review reaches and what it does not.

| Claim | Written | Falsified by |
|---|---|---|
| `or any way to watch one work` (`page.tsx:149`, `:150`) | `a20035b`, 2026-08-17 | `7090dd8`, later the same day, which added `internal/api/watch.go` |
| `the adapter … is not built` (`page.tsx:93`, `:150`) | `a20035b`, 2026-08-17 | `40a0c32`, 2026-08-24, which added `internal/delivery` and the `garam adapter` command in one commit |
| `In the console today` (`page.tsx:200`) | `1f06749`, 2026-08-03 | `f779293` 2026-08-24, `1fc41f2` 2026-08-25, `96557e5` 2026-09-02 — the agents, operators and consents sections |
| `Sign in with the Google account…` (`page.tsx:108`) | `ffcef60`, 2026-08-03 | `2458d48`, 2026-08-25, which added password sign-in |
| `every certificate … fails the handshake from then on` (`register-operator-form.tsx:58`) | `5873d77` (#1002), 2026-09-04 | `b16a896` (#1005), the same day |
| `Policy-based authorization on every request` (`layout.tsx:7`) | `ffcef60`, 2026-08-03 | nothing — `/auth/login/{provider}` was served and `internal/api/auth.go` held no `authorize` call at that commit |
| `Messages … stop at once.` (`withdraw-consent-form.tsx:47`) | `96557e5`, 2026-09-02 | nothing — `ClaimMessage` has never carried a consent predicate |
| `Accounts are created by invitation, not by signup.` (`login/page.tsx:52`) | `cff3049`, 2026-08-26 | nothing — `upsertUser` has created a user on first login since `d0b2e4d`, 2026-07-25, the commit that added OAuth sign-in |

A reviewer can catch the three that were wrong on arrival. Four of the
five that drifted were true when they merged, and the pull request that
falsified each one touched neither the page nor anything a reviewer of
that page would open.

The fourth is the one to read closely. `b16a896` changed
`internal/machine/listener.go` so a certificate that does not
authenticate no longer fails the handshake, and the same pull request
rewrapped the paragraph carrying the sentence that says it does — so
the false line is in that diff, in a file the change already had open,
one day after #1002 corrected the sentence beside it and the day #1003
was filed about it. That is the case where "review catches it" was
tested directly.

**Toward leaving it.** 71 of 85 hold, several of them precisely, and
the whole cross-organization consent surface — ten assertions in front
of a destructive control — is accurate but for one clause. That is not
the profile of prose nobody checks. The two `false` calls that rest on
a reading rather than on a contradiction are named as such in the table
(`page.tsx:200`, and the mechanism-only drift at
`register-operator-form.tsx:58`, where the reader's conclusion still
holds), so a reviewer who reads them the other way can subtract them
without re-running anything.

**What the survey cannot say.** It finished one stratum. The comment
sweep beside it found eight defects in the 82 assertions it reached and
left 70 unread, and the generated file and the backend strings the
console renders were not read at all. All of them make the same kind of
claim on the same surface, so a decision resting on "the assertions are
otherwise accurate" rests on 85 rendered strings and not on `web/`.

**What it changes about the two readings above.** The comment stratum
does not settle the scope question either, because `documentation.md`
would not reach a comment even widened to cover rendered prose —
`code-comments.md` owns that territory, and `README.md` §Independence
forbids two files deciding one artifact. What it does show is that the
shape #1003 found is not confined to the surface #1003 named. Whether
that argues for widening one rule, for a second rule, or for neither,
is the decision this report does not make.
