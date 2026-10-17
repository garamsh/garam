# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

## Structure

- **Responsibility documents** (one `.md` per domain or concern, e.g. `memory.md`, `gateway.md`) — the single source of truth for how the system is shaped *now*. To know the current state, read only these: what one of them does not hold itself, rule 1 requires it to route to.
- **`adr/`** — the record of individual decisions: direction taken, context, rejected alternatives.
- **This file** — the index, and not one of those documents: §Index
  names every file that is one, and the current state stated here is
  this repository's own rather than the system's — where its checks run
  (§Operation) and what its record has no detection for
  (§What nothing detects).

## Rules

1. **Responsibility document skeleton**: current decisions
   (consolidated) / rationale summary with links to the relevant ADRs /
   open questions. Do not copy ADR content — synthesize the present
   state. **Current state a working artifact carries and enforces stays
   in that artifact, and the document that owns the concern routes to
   it** — naming the artifact, the section of it, and what that section
   is authoritative for. That route is what lets §Structure promise the
   current state to a reader of these documents alone: either the state
   is in these documents, or the route to it is. A boundary saying only
   what a document will not restate is not one, because a reader meets
   the refusal and learns neither that the state exists nor where to
   reach it — which is how the chart version rule came to sit in no
   responsibility document at all
   ([ADR-0058](./adr/0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)).
   [web-app.md](./web-app.md) and [api-surface.md](./api-surface.md)
   route that way to `deploy/charts/garam/README.md`, and
   [deployment-contract.md](./deployment-contract.md) §What it is names
   `deploy/charts/garam/README.md` §Pinning a version.
2. **ADRs are append-only.** Once merged, the body is frozen.
   Exceptions: updating the status field (`accepted` → `superseded by
   ADR-XXXX`), fixing typos or broken links, and appending a dated entry
   under `## Errata` when a decision stands but a fact supporting it was
   wrong — the erratum names what falsified the fact, or where the fact
   was wrong when written, what it was wrong about, and the original
   text stays intact. A changed decision means a new ADR that supersedes
   the old one — never an edit.
3. **Decisions land in pairs.** A PR that adds or supersedes an ADR
   must update the affected responsibility documents in the same PR. A
   PR with only one of the two is rejected — the final state lives
   outside the ADR, in the document a reader of the present state
   opens. **Where the decision is a rule of this index, that document
   is this index**
   ([ADR-0062](./adr/0062-an-adr-deciding-a-rule-of-the-index-pairs-with-the-index.md)).
   The rules this index carries are written in it and in no
   responsibility document, so such an ADR affects none and still lands
   its final state in the same PR, in the rule it changes: the pairing
   is performed against the index rather than waived, which is what
   holds this apart from the exemption below. ADR-0060 and ADR-0061
   each landed in rule 4 that way. An ADR whose decision is anything
   else is outside this line and owes the responsibility documents it
   affects.
   **One ADR pairs with nothing: the one that confirms a decision
   already in the record.** It settles which reading of an existing ADR
   was right, leaves that ADR `accepted` with its decision standing
   unqualified, and moves no current state — so nothing a
   responsibility document says becomes wrong or incomplete when it
   merges, and there is no affected document to update. For that ADR
   the final state stays where the confirmed decision already put it,
   which is why this is an exemption from the pairing and not from rule
   1. An author tells the two apart before writing a line, and an ADR
   that supersedes, or that decides anything the record did not already
   hold, is not this case whatever it says about itself.
   [ADR-0057](./adr/0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md)
   is one, confirming
   [ADR-0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)'s
   version rule. That rule is carried by
   `deploy/charts/garam/README.md` §Pinning a version and by
   `Chart.yaml`, and
   [deployment-contract.md](./deployment-contract.md) §What it is
   refuses to restate a value the chart carries, so writing it into a
   responsibility document to satisfy the pairing would breach that
   document's own boundary.
   **A pairing that arrives in a later pull request is a breach, and
   the pull request that performs it is where the breach is
   recorded.** By the time anyone can see the pairing did not come,
   the ADR's own pull request is merged and rejecting it reaches
   nothing; the later one is what a reviewer still holds, and it names
   the ADR it is pairing and the commit that added it. That puts both
   ends of the window in the record. Without it a late pairing and a
   performed one are the same diff. It is a breach and not a bound,
   because what decides how long a window stays open follows the ADR's
   merge, so a permission conditioned on the follow-up admits a long
   window on the terms that admit a short one
   ([ADR-0063](./adr/0063-a-late-pairing-is-a-breach-and-the-pull-request-that-performs-it-records-the-window.md)
   records the four late pairings that show it).
4. Keep this index current: every responsibility document and every ADR
   is listed here. `structures/` is not — bootstrap deletes it, and an
   entry for a file that is about to go leaves the index wrong. An ADR
   covers a decision about garam itself — what the system is, how it is
   partitioned, what it serves, and what this repository publishes —
   which is the ground the responsibility documents beside this index
   describe. A rule in `docs/convention/` is not on that ground and
   earns no ADR however much it binds: those files govern how the
   project writes code, tests, commits and documents, whatever system
   they are written for. That line divides which changes this track
   records, not which rule governs where a convention file and a
   structural rule reach one case — rule 8 decides that. A settled
   choice that changes nothing on the ground this track covers does not
   earn one either. A change to `docs/architecture/` itself is on
   neither ground: these rules, and the shape they give the documents
   under them, govern how the record is written rather than what the
   system is. Such a change decides what counts as a defect, and earns
   an ADR, unless both arms hold — no passage that had passed review is
   in violation after it, and the record it writes down was undivided,
   every case in it already sorted the way the change sorts it. Either
   arm alone can be passed by drafting, which is why both are asked
   ([ADR-0060](./adr/0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)).
   #946 rewrote this rule and rule 3, put no passage in violation and
   sorted 40 of 40 the way the history already had, so it earned none.
   #953 put
   [deployment-contract.md](./deployment-contract.md) §What it is in
   violation until the same pull request repaired it, and
   [ADR-0058](./adr/0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)
   was owed. This paragraph earned ADR-0060 on the second arm alone: it
   puts no passage in violation, and #946 and #953 had answered its
   question opposite ways with both merged. Where a rule was first
   decided is no part of this test: what binds a contributor here is
   this project taking a rule in, so an adoption is read by the same two
   arms as any other change — and the ADR one owes records the taking
   in, routing to the template commit rather than restating the rule.
   The test binds only what is written after it
   ([ADR-0061](./adr/0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md)
   records the adoptions that divided the record).
5. **Every domain or concern has a responsibility document.** The PR
   that introduces one adds its document; the PR that removes one
   deletes it. A system with no code yet has no more than `structure.md`
   (rule 6), and that is the correct state.
6. **Structural rules live in `structure.md`.** The shape chosen at
   bootstrap imposes rules — what the unit is, what may reference what,
   when a new one is earned — and they belong in the responsibility
   document for that concern, where a reviewer cites one by line and a
   correction is an ordinary edit. Not the ADR: rule 2 freezes that
   body, so superseding the choice of shape would be the price of fixing
   a rule's wording.
7. **A point the structural rules leave to the project is settled in an
   ADR** — the one recording the shape where the question is met at
   bootstrap, a later one where it is met later. Which side is taken
   decides what counts as a defect, so it is a decision under rule 4,
   and the ADR still holds the record after bootstrap. Nothing obliges a
   project to settle a question it has not met: until then the point
   waits under `structure.md`'s open questions, where it binds nothing,
   and moves into that document's current decisions when the project
   takes a side.
8. **Structural rules yield only to what a framework imposes.** Where
   `structure.md` and a convention file decide the same case
   differently, `structure.md` governs, unless the stack-specific file
   that applies decides it as its framework imposes it — the alternative
   fails to build or run, so the project cannot restate it. A stack
   file's spelling of a structural rule is not that: the rule it spells
   is `structure.md`'s. The shape chosen at bootstrap is this project's
   own refinement of what a stack-neutral file states for every project.
   A project with no `structure.md` meets no such conflict, and this
   rule asks nothing of it.
9. **A responsibility document says a mechanism is unbuilt before it
   describes it.** A section describing a mechanism nothing implements,
   or one built in part, opens with that fact; underneath the
   description it reaches a reader who has already believed the
   mechanism is there.
   [adapter.md](./adapter.md) §Readiness and §Subscriptions open that
   way. A document with no `##` heading is one
   section: the fact opens its body, below the line naming what the
   document governs, as [messaging.md](./messaging.md) does. An ADR is
   outside this: a decision is unbuilt when it is taken, and rule 2
   freezes the body afterwards.

## What nothing detects

Rule 1 requires a responsibility document's current decisions to be consolidated and synthesized. A decision restated at many sites in one document is neither, and rule 1 has said so since 981c5371a05447f378cf71375ff1e792495afa69, which predates every pull request named below. Nothing detects a document that has accumulated those sites, and nothing detects one of them being left behind when the decision changes. The requirement is stated; the detection is what is absent.

Rule 3 is not that detection, and it is not what let the sites accumulate. It pairs an ADR with the responsibility documents it affects, and its unit is the file: it is satisfied once the affected document is in the pull request. Of the four cases #558's investigation read, rule 3 reached one and was breached there: #393 added ADR-0035 and paired [domains.md](./domains.md) without opening [overview.md](./overview.md). In the other three it did what it says — the affected document was opened and moved, or no ADR landed for it to trigger on. What stayed stale in those three was a further site of a claim the document was already carrying at several, which is rule 1's ground and not rule 3's.

An author who has moved one site cannot find the rest by searching for the decision they just made, and that is a fact about the sites rather than about the search. The ones left behind are the ones that do not name the new decision, so the search returns precisely the set that already moved. `git show 4b1bef1f31a12bd32f092be355e0165c6e561fb3:docs/architecture/overview.md | grep -oiE '0035|consent'` returns nothing — 4b1bef1f31a12bd32f092be355e0165c6e561fb3 is the revision before #556 corrected that document — while `grep -oE 'ADR-[0-9]{4}'` over the same input returns 76: the search ran, over a file citing ADRs throughout, and the stale statement of the cross-organization refusal named neither the number nor the decision's word.

Searching for the superseded decision instead is a genuine improvement over searching for the new one, and it is still a search. Two careful searches of `docs/architecture/overview.md` at 7dd0682f05a449596bcc9fa49363ead04d7a7e81 for the removed development-only sign-in provider disagreed in opposite directions inside one count: one missed line 268, which reads `dev-only local`, and one counted line 106, an §In scope bullet about org structure that may not carry the claim at all. Between those two searches over that document the unit being counted was not fixed; that is what the disagreement shows, and it is not evidence that no unit could be fixed. What holds either way is rule 1's ground: a document that does not accumulate the sites has nothing to enumerate.

No pipeline stands behind this either: what `make ci` reads a file under `docs/` for is one thing, each Markdown file's `## Contents` list against that file's own `##` headings (`check-contents-list.sh`, #889), so no run of it passes or fails on anything this section describes. What this repository's workflows do run is §Operation.

[api-surface.md](./api-surface.md) §What nothing checks records the same shape for a different artifact, the prose inside the two API specifications. That half is stated there and not here.

## Operation

What this repository runs against itself. `docs/convention/ci.md` decides how a check is named and secured; this section records where one runs.

The runner class every job is to run on is unbuilt.
[ADR-0105](./adr/0105-garams-pipeline-runs-on-a-docker-capable-aurora-runner-class.md)
moves every job to a Docker-capable aurora runner class, isolated from
the nodes that hold cluster secrets. That class does not exist yet, so
every job in `ci.yml`, `images.yml` and `release.yml` still names
`ubuntu-24.04`, whose runs start nothing on this account, and every
merge still rests on the exception in `AGENTS.md` §Roles. The pull
request that moves the labels replaces this paragraph with the state it
leaves.

- `.github/workflows/ci.yml` triggers on `pull_request` into `main` and
  on nothing else; `.github/workflows/images.yml` on the same event and
  base except where a pull request changes only `docs/`;
  `.github/workflows/release.yml` triggers on a `v*` tag (ADR-0039).
  No workflow publishes the test image; an operator pushes it with
  `make test-image-push` and `make test-image-pin` (ADR-0093).
  `dev` is the default branch, and every pull request but the
  promotion pull request targets it
  (`docs/convention/git.md` §Promotion), so the base filter selects the
  promotion pull request and no pull request into `dev` starts a runner
  (#1059).
- **A superseded run is cancelled; a release run is not.** `ci.yml` and
  `images.yml` each declare a concurrency group keyed on
  `github.workflow` and `github.ref` — under their one trigger that ref
  is the pull request's merge ref, one value per pull request — with
  `cancel-in-progress`, so a push that replaces a head stops the run
  reading the old one, up to the four job-runs a full merge costs.
  `release.yml` declares none, and the absence is what enforces it: a
  group cancels a pending run of that group when a newer one queues
  behind it, whichever way `cancel-in-progress` is set, so a group there
  could leave a `vX.Y.Z` tag publishing nothing at all, and a run
  cancelled mid-publish leaves the version half-published with the next
  version as the only repair (ADR-0039 §Consequences). What reads the
  guard is that `grep -n '^concurrency:' .github/workflows/*.yml` names
  two files and not three (#899).
- **Both images a release publishes are built on a pull request.** `images.yml` builds each from what `release.yml` says it is built from and then checks the web one, which is the image a release used to be able to fail on irreversibly: the server image was pushed before the web image was built, and immutability makes the repair the next version (ADR-0039 §Consequences). `release.yml` builds both and pushes neither until both are built (#945), so what a build failure there now costs is the run. Until #850 neither image was built by any check.
- **No run here reaches a deployment, so `main` is not what a caller
  meets.** `release.yml` publishes two images on a `vX.Y.Z` tag and no
  other workflow pushes one, so a merge to `main` publishes nothing;
  an operator pushes the test image, from a checkout, to an ECR
  repository no deployment pulls (ADR-0092, ADR-0093);
  the only cluster a recipe here reaches is the local minikube, and
  what it runs there it built from the working tree (`Makefile`,
  `minikube-images`). What a running garam serves is fixed by the image
  reference its deployment set, and that reference is the deployment's
  record rather than this repository's — for the cluster this project's
  own agents call, it lives in `garamsh/gitops`. Resolving one back to
  a surface is the half this repository holds, and #1020 moved it from
  the tag onto the artifact: `release.yml` passes the commit
  `git rev-parse HEAD` names in the run that built the image, and the
  version of its `vX.Y.Z` tag (ADR-0039), to both Dockerfiles, which
  record the pair as `org.opencontainers.image.revision` and
  `…version` — so `docker image inspect -f '{{json .Config.Labels}}'`
  names the commit whose `api/openapi.yaml` and `api/machine.yaml` a
  caller of that deployment meets. **A label is what the build claimed
  and not a property of the bytes.** Nothing derives it from the
  layers, so this replaces an unenforced step with a readable one
  rather than removing it: both repositories are IMMUTABLE, which makes
  a tag's digest a fact the registry enforces, and the single link left
  resting on trust is the git tag still naming the commit it named.
  What the registry held when #1033 merged, 2026-09-04T16:44Z, was
  published before the change — `0.1.0` carried no labels at all and
  `0.2.0` a pair a person passed by hand — so the first image carrying
  them because these files say so is a release after that reading.
  A development build, which `make image-build` produces when given
  `IMAGE_REGISTRY`, carries the same two labels, the version being its
  tag (ADR-0071), and reaches `garam-dev/garam-server` or
  `garam-dev/garam-web` only by a person's push (ADR-0097);
  without it the target tags `:ci` and passes neither label a value.
  A reference this repository did not publish is still one only its
  builder can vouch for, which after the label is a claim about where
  it came from and no longer about what code is in it. An answer to
  "does this route exist" therefore names where it exists, and `main`
  answers a different question than the caller asked (#886).
- **The after-merge run is not taken.** A `push: branches: [main]` trigger ran on `ci.yml` until #811. Both triggers read the merge result, so what that one alone covered is a `main` that moved between the pull request's run and the merge.
- **Nothing mechanical covers that gap; the role that merges does.** The obligation is stated once, in `AGENTS.md` §Roles; this bullet records that the gap is covered and is not that statement. The mechanical replacement is a required status check with *branches must be up to date*, which is a repository setting and so the account owner's.
- **Mutation testing runs in no workflow, and that is the decision rather than an omission.** `make mutation` rewrites one operator at a time under `internal/` and reports the mutants no test noticed. Four full runs on 2026-09-02 each spent around 35 minutes of CPU on twelve cores and took between 5 minutes 42 seconds and 32 minutes of wall time; the spread is the per-mutant timeout, which the target's own comment accounts for. A runner with four cores has a third of those and slower ones. `internal/` holds 33 of the module's 40 Go packages; the seven outside it are `cmd/garam`, the three generated from `api/openapi.yaml`, `api/machine.yaml` and `proto/`, and `migrations`, `tests/e2e` and `tests/integration`, which hold test files only (`go list ./... | grep -v /internal/`). So triggering on changes under it would exclude a pull request that changes Go outside `internal/` as well as the ones that change no Go at all, and would still fire on almost every pull request that changes Go and roughly double what it costs: of the 120 commits to 3fbcf9367ad6b66d1ae5e553eb5630471c589036, 47 changed a Go, specification or migration file, and 41 of those touch `internal/` while six do not. A schedule spends the same minutes on a report no pull request is waiting for, and attributes a new survivor to no change rather than to the one that raised it. What the run buys is an audit of the suite rather than a gate on one change, which is why a person invokes it (#875).

## Index

Responsibility documents:

- [structure.md](./structure.md) — the shape garam is partitioned into,
  what may reference what, and when a new boundary is earned
- [overview.md](./overview.md) — what garam is and is not; module list
- [domains.md](./domains.md) — garam's domains named by subject matter,
  and what else under `internal/` is not one
- [organizations.md](./organizations.md) — the tenant hierarchy: the
  top-level tenant, what is grouped inside it, and who belongs to one
- [users.md](./users.md) — human identity
- [sign-in.md](./sign-in.md) — how a person proves who they are, and the
  session that carries the proof afterwards
- [certificate-authorities.md](./certificate-authorities.md) — the
  certificate authority an organization signs machine identities with
- [operators.md](./operators.md) — what an organization records about an
  operator, where that operator stands in obtaining a certificate, and
  what it authenticates with
- [agents.md](./agents.md) — autonomous identity, provisioned outside
  garam
- [machine-identity.md](./machine-identity.md) — who is calling on the
  listener a machine identity authenticates on
- [messaging.md](./messaging.md) — the durable queue a message reaches
  an agent through, the consent that lets one cross an organization
  boundary, and the signature on every message garam hands out
- [audit.md](./audit.md) — the record garam keeps of what it decided,
  which holds message sends alone
- [grn-model.md](./grn-model.md) — the GRN resource naming model
- [authz.md](./authz.md) — the authorization policy domain: the rules
  that decide what a subject may do, and the engine that decides
- [api-surface.md](./api-surface.md) — the listeners garam serves on,
  what admits a caller to each, and what each one's specification is
  authoritative for
- [adapter.md](./adapter.md) — what the adapter garam ships must do, and
  what it rests on
- [deployment-contract.md](./deployment-contract.md) — what a deployment
  must plan for that no manifest carries: readiness, termination,
  migration failure, key-material enforcement, and what is not
  established
- [web-app.md](./web-app.md) — what the application a person uses garam
  through is responsible for, the one seam it reaches the backend
  through, the design system its pages are built on, and what it does
  not decide

ADRs:

- [0001](./adr/0001-grn-segments.md) — GRN segments identify owning
  resources
- [0002](./adr/0002-iam-style-policy-model.md) — IAM-style policy
  documents, native evaluator
- [0003](./adr/0003-single-cli-topology.md) — single cobra CLI,
  independent api/authz packages
- [0004](./adr/0004-openapi-first-api.md) — OpenAPI spec as source of
  truth
- [0005](./adr/0005-server-side-sessions.md) — server-side sessions
- [0006](./adr/0006-invitation-tokens.md) — invitation tokens
- [0007](./adr/0007-hybrid-statement-collection.md) — hybrid statement
  collection
- [0008](./adr/0008-identity-based-policy-attachments.md) —
  identity-based policy attachments
- [0009](./adr/0009-web-frontend.md) — Next.js web frontend in `web/`
- [0010](./adr/0010-dev-auth-provider.md) — development-only sign-in
  provider behind GARAM_DEV_AUTH; superseded by 0044
- [0011](./adr/0011-console-shell.md) — console sidebar shell over org
  sections
- [0012](./adr/0012-agent-operator-model.md) — agents provisioned by
  operators garam never dials
- [0013](./adr/0013-agent-messaging.md) — agent messaging through a
  garam API an adapter polls; its per-organization authority issuing
  the adapter's certificates is superseded by 0100
- [0014](./adr/0014-agent-authentication.md) — agents authenticate with
  mTLS on a listener of their own; an agent's revocation by lifetime is
  superseded by 0078, and the client certificate, its authority and
  revocation by lifetime by 0100
- [0015](./adr/0015-operator-identity.md) — operators authenticate as
  agents do; key material travels as files; mTLS for operators, the
  out-of-band first certificate and the shared expiry are superseded by
  0100
- [0016](./adr/0016-ca-key-custody.md) — a certificate authority's key
  is sealed by a key the process holds; superseded by 0100
- [0017](./adr/0017-machine-process.md) — machine identities are served
  by a process of their own
- [0018](./adr/0018-delivery-acknowledgement.md) — a message is
  completed by the turn-end event that names it; its in-memory pairing
  is superseded by 0079
- [0019](./adr/0019-machine-openapi-spec.md) — the machine listener
  carries an OpenAPI specification of its own
- [0020](./adr/0020-published-artifacts.md) — garam publishes images and
  a contract, not manifests; superseded by 0042
- [0021](./adr/0021-liveness-and-readiness.md) — liveness and readiness
  answer on a port that carries no credential
- [0022](./adr/0022-watching-an-agent.md) — an agent is watched only
  while someone is watching
- [0023](./adr/0023-web-probes.md) — the web app answers the same two
  probes as the rest
- [0024](./adr/0024-policy-decides-what-an-agent-sends.md) — a policy
  decides what an agent may send
- [0025](./adr/0025-engine-scope-is-a-caller-obligation.md) — an
  unattached policy's org scope is the caller's to hold
- [0026](./adr/0026-the-console-defines-an-agent.md) — the console
  defines an agent and an operator reads that definition; the runtime
  values are superseded by 0077, and the define route by 0090
- [0027](./adr/0027-a-watch-crosses-processes-through-postgres.md) — a
  watch crosses between processes through Postgres
- [0028](./adr/0028-oauth-state-is-a-server-side-record.md) — a login's
  state is a server-side record the callback spends
- [0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md)
  — readiness reports what a replica cannot serve without, so not the
  policy listener
- [0030](./adr/0030-the-login-write-rate-is-bounded-at-the-store.md) —
  the login write rate is bounded at the store
- [0031](./adr/0031-detaching-an-active-policys-last-attachment-is-refused.md)
  — detaching an active policy's last attachment is refused
- [0032](./adr/0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)
  — garam ships the command that mints an operator's first certificate;
  superseded by 0100
- [0033](./adr/0033-an-operator-claims-the-agent-a-definition-mints.md)
  — an operator claims the agent a definition mints; superseded by
  0077 as the controller's source and as how a managed agent is
  created, and by 0100 as what admits it to a certificate route
- [0034](./adr/0034-a-delivered-message-keeps-its-key-and-drops-its-body.md)
  — a delivered message keeps its key and drops its body
- [0035](./adr/0035-a-cross-organization-message-needs-both-organizations-consent.md)
  — a cross-organization message needs both organizations' consent
- [0036](./adr/0036-a-secret-in-garam-is-a-reference-not-a-value.md) — a
  secret in garam is a reference, not a value
- [0037](./adr/0037-the-machine-listener-presents-a-per-organization-certificate.md)
  — the machine listener presents a per-organization certificate
  (superseded by 0040)
- [0038](./adr/0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md)
  — the engine learns a subject's memberships and enforces its own scope
- [0039](./adr/0039-a-version-tag-publishes-both-images-to-the-accounts-registry.md)
  — a version tag publishes both images to the account's registry;
  superseded in part by 0071, which lets a person push a development
  build, and by 0092 for non-release artifacts, whose test-image
  registry and publishing 0093 supersedes in turn
- [0040](./adr/0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
  — the machine listener presents a pinned garam server certificate, and
  certificates rotate without a restart; its client certificates, the
  issuing authority in the answer and the agent's self-renewal are
  superseded by 0100
- [0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)
  — a password is garam's own credential, verified by Argon2id and
  minted by an invitation
- [0042](./adr/0042-a-chart-is-the-description-of-a-garam-deployment.md)
  — a chart at `deploy/charts/garam/` describes the deployment,
  superseding ADR-0020
- [0043](./adr/0043-a-workloads-account-is-named-by-a-value-and-the-chart-creates-none-by-default.md)
  — a workload's account is named by a value the deployment sets, and
  the chart creates none by default
- [0044](./adr/0044-the-development-only-sign-in-provider-is-removed.md)
  — the development-only sign-in provider is removed, superseding
  ADR-0010
- [0045](./adr/0045-an-organization-registers-an-operator.md) — an
  organization registers an operator, and a definition may name no
  other; that a registration issues no credential is superseded by 0100
- [0046](./adr/0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md)
  — an operator renews its certificate by presenting the one it holds,
  and only the newest one renews; superseded by 0100
- [0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)
  — garam is partitioned by domain, and a boundary reaches a sibling
  through the surface that sibling declares
- [0048](./adr/0048-garams-domains-are-named-by-subject-matter.md) —
  garam's domains are named by subject matter, and the Go package list
  is not the domain list
- [0049](./adr/0049-neither-published-specification-promises-anything-across-versions.md)
  — neither published specification promises anything across versions;
  superseded by 0079 for the interservice contracts once a release's
  window is tested
- [0050](./adr/0050-a-specification-states-its-listeners-5xx-once-and-declares-none-per-operation.md)
  — a specification states its listener's 5xx once and declares none per
  operation
- [0051](./adr/0051-an-aggregate-exposes-what-it-persists.md) — an
  aggregate exposes what it persists; its accessor and its answer for
  the three aggregates are superseded by ADR-0069, and its ruling that
  the `policies` field and column stay where they are still stands
- [0052](./adr/0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md)
  — a concern is owned by the boundary whose subject matter it is, not
  by the one it was extracted from
- [0053](./adr/0053-an-agent-and-its-operator-report-state-on-the-machine-listener.md)
  — an operator and an agent each report state on the machine listener,
  fenced by the assignment's epoch; the agent route is superseded in
  part by 0078, which fences it by the active generation too
- [0054](./adr/0054-the-concrete-types-rule-binds-every-boundary-not-only-a-domain.md)
  — the concrete-types rule binds every boundary, not only a domain
- [0055](./adr/0055-a-sender-reads-the-state-of-a-message-it-sent.md) —
  a sender reads the state of a message it sent, and garam announces
  nothing
- [0056](./adr/0056-a-published-web-image-is-bound-to-the-release-name-garam-and-make-ci-checks-the-binding.md)
  — a published web image reaches the api Service a release named
  `garam` renders, and `make ci` compares the two
- [0057](./adr/0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md)
  — a template edit that changes the render moves the chart version,
  confirming ADR-0042 rather than superseding it, and review is what
  holds it
- [0058](./adr/0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)
  — a responsibility document routes to the artifact that enforces a
  rule, and a boundary that only refuses is not a route
- [0059](./adr/0059-the-two-specifications-prose-is-not-held-to-agree-and-each-defines-every-member-its-own-caller-meets.md)
  — the two specifications' prose is not held to agree, and each defines
  every member its own caller meets
- [0060](./adr/0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)
  — a change to the rules under `docs/architecture/` earns an ADR unless
  it puts no passage in violation and the record it writes down was
  undivided
- [0061](./adr/0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md)
  — adopting a rule the conventions template decided is a decision taken
  here, and the ADR records the adoption
- [0062](./adr/0062-an-adr-deciding-a-rule-of-the-index-pairs-with-the-index.md)
  — an ADR whose decision is a rule of this index pairs with the index,
  not with a responsibility document
- [0063](./adr/0063-a-late-pairing-is-a-breach-and-the-pull-request-that-performs-it-records-the-window.md)
  — a pairing that arrives in a later pull request is a breach,
  recorded on the pull request that performs it
- [0064](./adr/0064-an-operator-enrols-with-a-one-time-token-and-keeps-its-key-in-its-own-cluster.md)
  — an operator enrols with a one-time token, generates its own key pair
  in its own cluster, and garam signs a key it never holds; the token,
  the request, the route and the handshake identity are superseded by
  0100
- [0065](./adr/0065-garam-states-what-deleting-an-organization-destroys-before-the-act.md)
  — garam answers what deleting an organization would destroy before
  the act, counting on the server and naming the certificate authority
  separately as the one thing that cannot be made again
- [0066](./adr/0066-no-aggregate-gains-an-exported-restore-path-to-satisfy-a-layout-rule.md)
  — no aggregate gains an exported restore path in order to satisfy a
  layout rule, so a domain's store stays beside its aggregate and
  ADR-0051's reserved question is answered on its own evidence or not
  at all; superseded by ADR-0069, which answered it on that evidence
- [0067](./adr/0067-a-member-mints-a-second-enrollment-token-where-the-operator-holds-no-certificate.md)
  — a member mints an operator another enrollment token where it holds
  no certificate, superseding ADR-0064's open item, and the refusal
  where it holds one is where that question stays open; superseded by
  0100, which answers it
- [0069](./adr/0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md)
  — the aggregates are data carriers, so the driver leaves the domain
  package: no method on one assigns a field, so the fields are exported,
  the accessors go, and the store moves to `internal/<domain>/postgres/`
  named for the technology it implements against. Supersedes ADR-0066
  whole and ADR-0051 in part; `internal/user` is the pilot and the other
  nine domains follow in their own pull requests
- [0070](./adr/0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)
  — an organization deletes an operator, and the record it withdraws is
  what that operator authenticated on: the machine listener reads the
  registration per connection through a seam of the trust root's shape,
  so deleting stops the operator with no revocation list, no responder
  and no stored serial. Supersedes ADR-0045's refusal of a delete route
  and, for an operator alone, ADR-0014's "revocation is by lifetime";
  an agent is answered without a read and its certificate is untouched;
  its per-connection read is superseded by 0100's per-request one
- [0071](./adr/0071-a-development-image-shares-the-release-repository-and-its-tag-says-it-is-not-a-release.md)
  — a development image shares the release repository, and its tag says
  it is not a release: one repository per image, a bare `X.Y.Z` for a
  release alone, a commit-derived or prerelease tag for a development
  build `make image-build` produces locally and pushes nowhere.
  Supersedes ADR-0039's "the only trigger that publishes" in part; its
  one repository per image is superseded by 0095
- [0072](./adr/0072-the-console-presents-a-group-as-a-team-with-a-page-of-its-own.md)
  — the console presents a group as a team with a page of its own:
  Teams and People replace Groups and Members in the console, a team
  list opens a page per team for its membership and settings, and the
  group's API, identity and authorization are unchanged
- [0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)
  — the console manages policies and their attachments under
  Permissions: a Policies and an Attachments view over the existing
  policy operations, any stored version made the default, a stated and
  confirmed scope before a policy evaluates, and a subject's policies
  sorted by source with what was not read marked unchecked
- [0074](./adr/0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)
  — an operator carries a name and description beside its GRN, changed
  on `operator:update` and moving nothing it authenticates or enrols
  by; the console lists operators by name and opens a page per
  operator, with the identifier wherever a name alone could be
  ambiguous
- [0075](./adr/0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
  — an agent is started from a template copied once and carries a name
  of its own: templates are an organization's resource on
  `agent-template:*` actions with nothing linking them to the agents
  defined from them, a definition's name changes on `agent:rename`
  and moves nothing else, and the console lists desired agents by name
  apart from what operators report. Which service owns a template is
  superseded by 0077, its template writes and forms by 0088, and
  copying values into a new definition by 0090
- [0077](./adr/0077-execution-definitions-move-to-the-operator-control-service-and-each-agent-cuts-over-alone.md)
  — execution definitions and templates move to a separately deployed
  Operator control service with its own store, garam keeps identity,
  names, assignment and collaboration, and existing agents cut over one
  GRN at a time with one active source and no new workload or volume;
  garam's managed creation is built, and the rest is unbuilt. Its
  cutover of templates is superseded by 0088
- [0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)
  — a lifecycle authority activates a runtime generation distinct from
  the GRN, epoch and revision; old generations and replaced credential
  lineages are refused per request, applied configuration is the
  runtime's report, and execution authority is a scoped expiring
  delegation plus a high-trust permission; garam's generation fence and
  recovery are built, and the shipped adapter does not use them yet; a
  lineage of certificates is superseded by 0100's lineage of tokens
- [0079](./adr/0079-interservice-contracts-keep-adjacent-releases-compatible-and-replay-by-stable-identifier.md)
  — interservice contracts keep adjacent releases compatible in both
  upgrade orders and refuse what they do not support, a message replays
  by a stable identifier and a reply leaves through a durable outbox,
  and no effect is exactly-once; unbuilt
- [0080](./adr/0080-an-agents-execution-row-is-the-one-lock-its-writes-and-replacements-share.md)
  — every agent has one execution row, the lock its protected writes
  and its replacements share: other domains take it through the
  declared `agent_execution_admit` function in their own transaction,
  each reading its answer into its own input and refusal, and a
  replacement takes it exclusively, so no write of a replaced
  generation commits after its replacement
- [0081](./adr/0081-a-test-only-command-signs-in-the-first-principal-of-a-database-it-creates.md)
  — a test-only command under `tests/testprincipal` creates a fresh
  local database and signs in its first principal under a versioned
  contract; no Garam binary carries it, and everything after that
  principal goes through public routes. Superseded in part by 0092,
  whose test image carries it
- [0082](./adr/0082-a-standalone-host-runs-its-agent-under-a-supervisor-an-administrator-approves-by-its-keys.md)
  — a standalone host's lifecycle authority is a supervisor garam
  registers for one agent on an administrator's approval of the host's
  own keys; it activates that agent on the execution routes, renews
  its own certificate, and is replaced or revoked only by an
  administrator; the host's runner is 0085's; its key pairs, spending
  by certificate requests and certificate renewal are superseded by
  0100
- [0083](./adr/0083-a-durable-reply-is-admitted-under-a-fixed-budget-from-its-creation-and-its-key-is-answered-after-it.md)
  — `POST /outbox-messages` under `durable-delivery.v1` stores a
  durable reply only while its creation instant is inside a fixed
  seven-day budget, judged on the database's clock when the insert
  runs, and answers a key that already stores a message after the
  budget too
- [0084](./adr/0084-the-adapter-activates-its-generation-and-reports-runtime-status-under-a-contract-garam-owns.md)
  — the adapter activates the generation its runtime reports and
  reports that runtime's status under `agent-execution.v1`, a contract
  garam owns and the lifecycle authority serves: an adapter-derived
  request identifier, a verified binding, the highest token version,
  and no protected call before activation or after a definite refusal;
  the adapter's half is built, a standalone supervisor serves it
  (0085), and the Operator control service does not yet; its mTLS
  transport and `Garam-Placement` in `Authorization` are superseded by
  0100
- [0085](./adr/0085-a-standalone-supervisor-owns-each-generation-as-three-containers-on-a-rootless-docker-daemon.md)
  — a standalone supervisor, a foreground host process holding the
  Docker socket alone, owns each generation as a runtime, a workspace
  and an adapter container in one network namespace over two volumes
  it created and recognises by a nonce; it adopts by a recorded name
  and nonce, drains with SIGTERM and no deadline until both writers
  exit, and serves the adapter's activation over TLS on a socket only
  that generation mounts; the adapter's certificate there is superseded
  by 0100
- [0086](./adr/0086-a-legacy-agent-cuts-over-through-stages-garam-records-under-its-execution-lock.md)
  — a legacy agent moves its source to the Operator under its GRN
  through `agent-cutover.v1`: garam freezes the source against the
  import's digest and records the switch under the agent's execution
  lock, holds the agent's first activation and its operator's
  certificate route on that switch, keeps the values archival, and
  names each refusal in the machine `Error`'s `reason`; the
  certificate route's refusal is superseded by 0100
- [0087](./adr/0087-a-standalone-runtime-revision-names-its-ego-and-instructions-and-the-adapter-has-its-own-way-to-garam.md)
  — a standalone runtime takes its ego and operator instructions from
  files the host names, and its configuration's revision names both,
  so changing either is a new revision; the adapter may reach garam at
  an address of its own, refining ADR-0085
- [0088](./adr/0088-garams-agent-templates-are-retired-read-only-and-not-imported.md)
  — garam's agent templates are retired read-only and not imported: a
  schema guard refuses every write to them from whichever release
  sends it, a template still goes with its organization, the write
  routes and actions are removed, and the console defines only from
  the Operator's templates and shows garam's as archived records
- [0089](./adr/0089-a-standalone-agents-memory-moves-only-through-a-verified-copy-its-new-generation-accepts-before-it-activates.md)
  — a standalone agent's memory moves to a new volume only through
  `garam standalone run --move-memory-to`: the old runtime drained and
  exited 0, a copy made under Sherlock's writer lock from the old
  volume mounted read-only, verified by digest and accepted by the new
  runtime's `--require-store` before its activation replaces the old
  one; any failure leaves the agent stopped on the old volume, which is
  never written or removed
- [0090](./adr/0090-garam-accepts-no-new-agent-definition-and-keeps-the-ones-it-holds.md)
  — garam accepts no new agent definition: `defineAgent` and
  `agent:define` are removed, a schema guard refuses a new definition,
  a changed value or a direct delete from whichever release sends it
  while a rename and the organization's cascade stay admitted, every
  definition held stays readable, claimable and cut-over-able, and an
  agent is created through the control service
- [0091](./adr/0091-a-recovered-credential-names-its-chain-and-garam-stores-it-with-the-recovery.md)
  — a recovered credential's answer names its issuer and garam server
  root, added to `execution-fence.v1` without a version change; garam
  stores the chain the signer produced with the recovery so a retry
  answers it again, and a recovery recorded before 000036 is answered
  with the chain the signer signs under now, which holds while neither
  the organization's authority nor the server root has been replaced;
  superseded by 0100
- [0092](./adr/0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md)
  — a test image holding `garam`, `testprincipal` and the migrations
  is published on every push to `dev` to a private GHCR package of its
  own, tagged by commit and pinned by digest, keeping the 8 newest
  unpinned versions and every `pinned-*` one; supersedes ADR-0039 for
  non-release artifacts and ADR-0081's "no image Garam ships carries
  it" in part; its registry, its commit tag, its publishing from `dev`
  and its retention are superseded by 0093
- [0093](./adr/0093-an-operator-pushes-the-test-image-to-the-accounts-ecr.md)
  — an operator pushes the test image to a repository of its own in
  the development namespace of the account's ECR, which 0095 and then
  0097 name, under ADR-0071's commit-prefix tag, and moves its
  pins, under their own AWS identity with `make test-image-push` and
  `make test-image-pin`; no workflow holds a
  credential for it, so no image appears per `dev` commit; the
  repository's lifecycle policy, which `garamsh/aurora`'s
  `infrastructure/modules/aws/ecr-repositories.tf` declares at
  `4df93d567fddb9afe9e5120449053222dee2b7c7`, expires an unpinned
  image 90 days after its push and bounds pins by a count of 50, not
  by age; supersedes ADR-0092's registry, its commit tag, its
  publishing from `dev` and its retention; its repository URI and its
  namespace are superseded by 0097
- [0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)
  — a structural rule yields to a stack file only where the stack's
  framework imposes the case, taken from the conventions template at
  `b02fe378e81c03eaf38540372430f15bbfbc4396` with six more template
  commits; `web/` is governed by `structure.md` again, and
  `db3291285a9f89edf571a41bcdaee8528d77b021` makes `internal/cli` the
  composition site `stack-go.md` provides
- [0095](./adr/0095-development-images-live-under-dev-and-garam-holds-releases-only.md)
  — development images live under `dev/`, and `garam/` holds
  releases only: `make image-build` names each image under `dev/`
  and refuses a release version there, a person pushes under their
  own identity, and the costs the PM stated against it are accepted.
  Supersedes ADR-0071's repository decision only; superseded by 0097
- [0096](./adr/0096-garam-syncs-the-convention-template-to-f7de866.md)
  — garam syncs the conventions template to
  `f7de866df7185d523589c2bb69b80458e2247f7d`, taking every commit
  since `6fbfd5d52cb65c20dd8bcc92fc6f24da88f05831` that lands on a
  file it keeps; `stack-go.md` §Service layout keeps naming an
  implementation package for its technology, and
  three adapted stack files keep their checked-against headers
- [0097](./adr/0097-development-images-live-under-garam-dev-and-garam-holds-releases-only.md)
  — development images live under `garam-dev/`, as the owner set it
  after 0095; `garam/` still holds releases only, and
  `make image-build`, `make test-image-push` and `make test-image-pin`
  name `garam-dev/<image>`. Supersedes 0095, and ADR-0093's
  repository URI and namespace
- [0098](./adr/0098-garam-syncs-the-convention-template-to-b29f0f1.md)
  — garam syncs the conventions template to
  `b29f0f104c68b42bfb363351a8e0e3cdc9b52829`: the
  composition-site rule moves from `stack-go.md` into `structure.md`,
  the template's §Rules every shape carries and bootstrap line are
  declined, and the section-scope rule is taken where Garam keeps the
  file it lands on
- [0100](./adr/0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)
  — every machine caller exchanges a credential garam issues for
  short-lived tokens it presents over server TLS alone, retiring client
  certificates and the per-organization authorities; an operator's
  credential and tokens, and the tokens it mints for the agents it
  holds, are built with their per-request revocation (G1a, G1b), and
  the rest lands in the steps of
  `docs/plans/2026-10-09-machine-identity-migration.md`
- [0101](./adr/0101-garam-signs-every-message-it-delivers-and-the-agent-verifies-it-alone.md)
  — garam signs each message at every hand-out with an ES256 JWS beside
  the content, binding issuer, sender, audience, organization, message
  identifier, content digest and a lease-long expiry; it publishes its
  keys as a JWK Set and rotates them with overlap, and the agent
  verifies without trusting the path; garam's half is built, and the
  adapter's forwarding and Sherlock's verification are not; where its
  keys are held, and how a claim spends no attempt on a key that will
  not sign, are superseded in part by 0104
- [0102](./adr/0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
  — an operator proves its existing registration with
  private_key_jwt using issued RSA PEM and Garam stores public keys;
  managed/direct agents share admission separately from provisioning.
  Unbuilt; supersedes 0100 only for Operator long-lived
  credential issuance/exchange and separate target enrollment, keeping
  token transport, fencing and existing cohorts. 0101 is unchanged.
- [0103](./adr/0103-the-console-has-one-design-system-and-asks-the-server-what-a-caller-may-do.md)
  — the console is built on one design system: principles, oklch
  tokens in light and dark, Pretendard Variable and Geist Mono, a
  grouped shell, four page templates, a primitive list and patterns
  for secrets, destruction, lifecycle, policies, onboarding and
  feedback, on shadcn/ui over Radix; it asks the server through a
  permission-check API what a caller may do and hides or disables what
  is denied, while the server still enforces every write. The
  permission check is built; the rest is unbuilt
- [0104](./adr/0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md)
  — a deployment may hold its message signing keys in an OpenBao or
  Vault Transit key that signs each hand-out inside the claim's
  transaction, behind a key-custody seam replacing `message.Sealer`
  as the keyring's; with no provider the file key-encryption key stays
  the default, and the certificate authorities stay on it until G4
- [0105](./adr/0105-garams-pipeline-runs-on-a-docker-capable-aurora-runner-class.md)
  — garam's six jobs run on one Docker-capable aurora runner class,
  keeping every entry point CI invokes today, rather than on the
  kubernetes-mode `aurora` class that run 39051854702 showed admits
  this repository but provides no daemon; garam asks for the class
  isolated from the nodes that hold cluster secrets, with `dind` on a
  shared worker as aurora's fallback, and the local gate stands until
  the class exists. Unbuilt
- [0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)
  — garam stays the only route a message takes, and every send it
  decides writes one audit event without a body in the transaction of
  its outcome, kept until either party's organization is deleted and
  read by nobody through garam yet
- [0108](./adr/0108-a-reply-answered-outside-its-contract-is-offered-again-on-a-bounded-schedule-and-a-refusal-on-its-merits-is-not.md)
  — the adapter offers again, on its per-entry backoff up to the retry
  cap, a reply garam answered outside its contract, so an adapter
  upgraded before garam forwards it without a restart; a refusal on
  the entry's merits is still set aside until its file is replaced.
  0083's decision stands, and its 404 row now reads as a later attempt
  that may be admitted
