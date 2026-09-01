# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

## Structure

- **Responsibility documents** (one `.md` per domain or concern, e.g. `memory.md`, `gateway.md`) — the single source of truth for how the system is shaped *now*. To know the current state, read only these.
- **`adr/`** — the record of individual decisions: direction taken, context, rejected alternatives.

## Rules

1. **Responsibility document skeleton**: current decisions (consolidated) / rationale summary with links to the relevant ADRs / open questions. Do not copy ADR content — synthesize the present state.
2. **ADRs are append-only.** Once merged, the body is frozen. Exceptions: updating the status field (`accepted` → `superseded by ADR-XXXX`), fixing typos or broken links, and appending a dated entry under `## Errata` when a decision stands but a fact supporting it was wrong — the erratum names what falsified the fact, or where the fact was wrong when written, what it was wrong about, and the original text stays intact. A changed decision means a new ADR that supersedes the old one — never an edit.
3. **Decisions land in pairs.** A PR that adds or supersedes an ADR must update the affected responsibility documents in the same PR. A PR with only one of the two is rejected — the final state must always live in the responsibility documents.
4. Keep this index current: every responsibility document and every ADR is listed here. `structures/` is not — bootstrap deletes it, and an entry for a file that is about to go leaves the index wrong. An ADR covers a decision that changes a project rule — a settled choice that does not affect the rules does not earn one.
5. **Every domain or concern has a responsibility document.** The PR that introduces one adds its document; the PR that removes one deletes it. A system with no code yet has no more than `structure.md` (rule 6), and that is the correct state.
6. **Structural rules live in `structure.md`.** The shape chosen at bootstrap imposes rules — what the unit is, what may reference what, when a new one is earned — and they belong in the responsibility document for that concern, where a reviewer cites one by line and a correction is an ordinary edit. Not the ADR: rule 2 freezes that body, so superseding the choice of shape would be the price of fixing a rule's wording.
7. **A point the structural rules leave to the project is settled in an ADR** — the one recording the shape where the question is met at bootstrap, a later one where it is met later. Which side is taken decides what counts as a defect, so it is a decision under rule 4, and the ADR still holds the record after bootstrap. Nothing obliges a project to settle a question it has not met: until then the point waits under `structure.md`'s open questions, where it binds nothing, and moves into that document's current decisions when the project takes a side.
8. **Structural rules rank between the convention tiers.** Where `structure.md` and a convention file decide the same case differently, the stack-specific file that applies governs; `structure.md` governs against the tiers below it in `docs/convention/README.md` §Precedence — the stack-neutral files, then that index itself. A stack file carries the shape its framework imposes and the project cannot restate; the shape chosen at bootstrap is this project's own refinement of what a stack-neutral file states for every project. A project with no `structure.md` meets no such conflict, and this rule asks nothing of it.
9. **A responsibility document says a mechanism is unbuilt before it describes it.** A section describing a mechanism nothing implements, or one built in part, opens with that fact; underneath the description it reaches a reader who has already believed the mechanism is there. [adapter.md](./adapter.md) §Readiness and §Subscriptions open that way. A document with no `##` heading is one section: the fact opens its body, below the line naming what the document governs, as [messaging.md](./messaging.md) does. An ADR is outside this: a decision is unbuilt when it is taken, and rule 2 freezes the body afterwards.

## What nothing detects

Rule 1 requires a responsibility document's current decisions to be consolidated and synthesized. A decision restated at many sites in one document is neither, and rule 1 has said so since 981c537, which predates every pull request named below. Nothing detects a document that has accumulated those sites, and nothing detects one of them being left behind when the decision changes. The requirement is stated; the detection is what is absent.

Rule 3 is not that detection, and it is not what let the sites accumulate. It pairs an ADR with the responsibility documents it affects, and its unit is the file: it is satisfied once the affected document is in the pull request. Of the four cases #558's investigation read, rule 3 reached one and was breached there: #393 added ADR-0035 and paired [domains.md](./domains.md) without opening [overview.md](./overview.md). In the other three it did what it says — the affected document was opened and moved, or no ADR landed for it to trigger on. What stayed stale in those three was a further site of a claim the document was already carrying at several, which is rule 1's ground and not rule 3's.

An author who has moved one site cannot find the rest by searching for the decision they just made, and that is a fact about the sites rather than about the search. The ones left behind are the ones that do not name the new decision, so the search returns precisely the set that already moved. `git show 4b1bef1:docs/architecture/overview.md | grep -oiE '0035|consent'` returns nothing — 4b1bef1 is the revision before #556 corrected that document — while `grep -oE 'ADR-[0-9]{4}'` over the same input returns 76: the search ran, over a file citing ADRs throughout, and the stale statement of the cross-organization refusal named neither the number nor the decision's word.

Searching for the superseded decision instead is a genuine improvement over searching for the new one, and it is still a search. Two careful searches of `docs/architecture/overview.md` at 7dd0682 for the removed development-only sign-in provider disagreed in opposite directions inside one count: one missed line 268, which reads `dev-only local`, and one counted line 106, an §In scope bullet about org structure that may not carry the claim at all. Between those two searches over that document the unit being counted was not fixed; that is what the disagreement shows, and it is not evidence that no unit could be fixed. What holds either way is rule 1's ground: a document that does not accumulate the sites has nothing to enumerate.

No pipeline stands behind this either: `make ci` reads no file under `docs/`, so no run of it passes or fails on anything this section describes. What this repository's workflows do run is §Operation.

[api-surface.md](./api-surface.md) §What nothing checks records the same shape for a different artifact, the prose inside the two API specifications. That half is stated there and not here.

## Operation

What this repository runs against itself. `docs/convention/ci.md` decides how a check is named and secured; this section records where one runs.

- `.github/workflows/ci.yml` triggers on `pull_request` and on nothing else; `.github/workflows/release.yml` triggers on a `v*` tag (ADR-0039).
- **The after-merge run is not taken.** A `push: branches: [main]` trigger ran on `ci.yml` until #811. Both triggers read the merge result, so what that one alone covered is a `main` that moved between the pull request's run and the merge.
- **Nothing mechanical covers that gap; the role that merges does.** The obligation is stated once, in `AGENTS.md` §Roles; this bullet records that the gap is covered and is not that statement. The mechanical replacement is a required status check with *branches must be up to date*, which is a repository setting and so the account owner's.

## Index

Responsibility documents:

- [structure.md](./structure.md) — the shape garam is partitioned into, what may reference what, and when a new boundary is earned
- [overview.md](./overview.md) — what garam is and is not; module list
- [domains.md](./domains.md) — garam's domains named by subject matter, and what else under `internal/` is not one
- [organizations.md](./organizations.md) — the tenant hierarchy: the top-level tenant, what is grouped inside it, and who belongs to one
- [users.md](./users.md) — human identity
- [sign-in.md](./sign-in.md) — how a person proves who they are, and the session that carries the proof afterwards
- [certificate-authorities.md](./certificate-authorities.md) — the certificate authority an organization signs machine identities with
- [operators.md](./operators.md) — what an organization records about an operator, and what that operator authenticates with
- [agents.md](./agents.md) — autonomous identity, provisioned outside garam
- [machine-identity.md](./machine-identity.md) — who is calling on the listener a machine identity authenticates on
- [messaging.md](./messaging.md) — the durable queue a message reaches an agent through, and the consent that lets one cross an organization boundary
- [grn-model.md](./grn-model.md) — the GRN resource naming model
- [authz.md](./authz.md) — the authorization policy domain: the rules that decide what a subject may do, and the engine that decides
- [api-surface.md](./api-surface.md) — the listeners garam serves on, what admits a caller to each, and what each one's specification is authoritative for
- [adapter.md](./adapter.md) — what the adapter garam ships must do, and what it rests on
- [deployment-contract.md](./deployment-contract.md) — what a deployment must plan for that no manifest carries: readiness, termination, migration failure, key-material enforcement, and what is not established
- [web-app.md](./web-app.md) — what the application a person uses garam through is responsible for, the one seam it reaches the backend through, and what it does not decide

ADRs:

- [0001](./adr/0001-grn-segments.md) — GRN segments identify owning resources
- [0002](./adr/0002-iam-style-policy-model.md) — IAM-style policy documents, native evaluator
- [0003](./adr/0003-single-cli-topology.md) — single cobra CLI, independent api/authz packages
- [0004](./adr/0004-openapi-first-api.md) — OpenAPI spec as source of truth
- [0005](./adr/0005-server-side-sessions.md) — server-side sessions
- [0006](./adr/0006-invitation-tokens.md) — invitation tokens
- [0007](./adr/0007-hybrid-statement-collection.md) — hybrid statement collection
- [0008](./adr/0008-identity-based-policy-attachments.md) — identity-based policy attachments
- [0009](./adr/0009-web-frontend.md) — Next.js web frontend in `web/`
- [0010](./adr/0010-dev-auth-provider.md) — development-only sign-in provider behind GARAM_DEV_AUTH; superseded by 0044
- [0011](./adr/0011-console-shell.md) — console sidebar shell over org sections
- [0012](./adr/0012-agent-operator-model.md) — agents provisioned by operators garam never dials
- [0013](./adr/0013-agent-messaging.md) — agent messaging through a garam API an adapter polls
- [0014](./adr/0014-agent-authentication.md) — agents authenticate with mTLS on a listener of their own
- [0015](./adr/0015-operator-identity.md) — operators authenticate as agents do; key material travels as files
- [0016](./adr/0016-ca-key-custody.md) — a certificate authority's key is sealed by a key the process holds
- [0017](./adr/0017-machine-process.md) — machine identities are served by a process of their own
- [0018](./adr/0018-delivery-acknowledgement.md) — a message is completed by the turn-end event that names it
- [0019](./adr/0019-machine-openapi-spec.md) — the machine listener carries an OpenAPI specification of its own
- [0020](./adr/0020-published-artifacts.md) — garam publishes images and a contract, not manifests; superseded by 0042
- [0021](./adr/0021-liveness-and-readiness.md) — liveness and readiness answer on a port that carries no credential
- [0022](./adr/0022-watching-an-agent.md) — an agent is watched only while someone is watching
- [0023](./adr/0023-web-probes.md) — the web app answers the same two probes as the rest
- [0024](./adr/0024-policy-decides-what-an-agent-sends.md) — a policy decides what an agent may send
- [0025](./adr/0025-engine-scope-is-a-caller-obligation.md) — an unattached policy's org scope is the caller's to hold
- [0026](./adr/0026-the-console-defines-an-agent.md) — the console defines an agent and an operator reads that definition
- [0027](./adr/0027-a-watch-crosses-processes-through-postgres.md) — a watch crosses between processes through Postgres
- [0028](./adr/0028-oauth-state-is-a-server-side-record.md) — a login's state is a server-side record the callback spends
- [0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md) — readiness reports what a replica cannot serve without, so not the policy listener
- [0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md) — the login write rate is bounded at the store
- [0031](./adr/0031-detaching-an-active-policys-last-attachment-is-refused.md) — detaching an active policy's last attachment is refused
- [0032](./adr/0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md) — garam ships the command that mints an operator's first certificate
- [0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md) — an operator claims the agent a definition mints
- [0034](./adr/0034-a-delivered-message-keeps-its-key-and-drops-its-body.md) — a delivered message keeps its key and drops its body
- [0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md) — a cross-organization message needs both organizations' consent
- [0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md) — a secret in garam is a reference, not a value
- [0037](./adr/0037-the-machine-listener-presents-a-per-organization-certificate.md) — the machine listener presents a per-organization certificate (superseded by 0040)
- [0038](./adr/0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md) — the engine learns a subject's memberships and enforces its own scope
- [0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md) — a version tag publishes both images to the account's registry
- [0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md) — the machine listener presents a pinned garam server certificate, and certificates rotate without a restart
- [0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md) — a password is garam's own credential, verified by Argon2id and minted by an invitation
- [0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md) — a chart at `deploy/charts/garam/` describes the deployment, superseding ADR-0020
- [0043](./adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md) — a workload's account is named by a value the deployment sets, and the chart creates none by default
- [0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md) — the development-only sign-in provider is removed, superseding ADR-0010
- [0045](./adr/0045-an-organization-registers-an-operator.md) — an organization registers an operator, and a definition may name no other
- [0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md) — an operator renews its certificate by presenting the one it holds, and only the newest one renews
- [0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md) — garam is partitioned by domain, and a boundary reaches a sibling through the surface that sibling declares
- [0048](./adr/0048-garams-domains-are-named-by-subject-matter.md) — garam's domains are named by subject matter, and the Go package list is not the domain list
- [0049](./adr/0049-neither-published-specification-promises-anything-across-versions.md) — neither published specification promises anything across versions
- [0050](./adr/0050-a-specification-states-its-listeners-5xx-once-and-declares-none-per-operation.md) — a specification states its listener's 5xx once and declares none per operation
- [0051](./adr/0051-an-aggregate-exposes-what-it-persists.md) — an aggregate exposes what it persists
- [0052](./adr/0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md) — a concern is owned by the boundary whose subject matter it is, not by the one it was extracted from
- [0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md) — an operator and an agent each report state on the machine listener, fenced by the assignment's epoch
- [0054](./adr/0054-the-concrete-types-rule-binds-every-boundary-not-only-a-domain.md) — the concrete-types rule binds every boundary, not only a domain

## Rules for edits

- A PR that changes module boundaries updates the responsibility documents; a PR that changes a decision adds an ADR. Same PR, always.
