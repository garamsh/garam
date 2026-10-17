# ADR 0058: A responsibility document routes to the artifact that enforces a rule, and a boundary that only refuses is not a route

> Status: accepted
> Date: 2026-09-03

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[README.md](../README.md) §Structure calls the responsibility documents
"the single source of truth for how the system is shaped *now*" and
tells a reader "To know the current state, read only these." One rule of
current state falsifies that sentence.

**The chart version rule is in no responsibility document.**
`Chart.yaml`'s `version` "moves when the chart changes and names no
garam version"
([ADR-0042](./0042-a-chart-is-the-description-of-a-garam-deployment.md)
§Decision), read against the case that divided by
[ADR-0057](./0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md).
It is carried by `deploy/charts/garam/README.md` §Pinning a version and
by `Chart.yaml`'s own comment above the field. Over
`docs/architecture/*.md` excluding the index,
`grep -n version <file> | grep -iE 'chart|helm|pin'` returns nothing for
all 17 files. Two controls, read 2026-09-03: the same query returns 7
lines in `deploy/charts/garam/README.md`, 2 in `Chart.yaml`, 2 in
ADR-0042 and 10 in ADR-0057, so the query finds the rule where the rule
is; and `grep -ic version` unfiltered returns 33 lines across 7 of those
same 17 files, so the files it reports nothing for are being read. Three
earlier independent runs — on #937, by the PM, and on #946 — agree.

The obvious repair is closed off.
[deployment-contract.md](../deployment-contract.md) §What it is says
"Where the two could both answer a question, the chart answers it.
Nothing here restates a value the chart carries", and names the
duplication ADR-0042 exists to remove as the reason. #937 declined to
write the rule there and said so; #946 declined again and raised the
gap as a Convention concern rather than working around it.

What those two pull requests met is not this rule alone. Four
handovers to a working artifact already stand in three responsibility
documents, and they are not all written the same way:

- [web-app.md](../web-app.md) — "`deploy/charts/garam/README.md` is
  authoritative for what a deployment sets — `web.apiUrl` included",
  and separately "§Choosing a release name is authoritative for what a
  deployment does about it".
- [api-surface.md](../api-surface.md) §What nothing checks — "What may
  stand in front of each listener differs between them, and
  `deploy/charts/garam/README.md` is where that is stated."
- `deployment-contract.md` §What it is — "Nothing here restates a value
  the chart carries."

The first three name what the other artifact is authoritative for. The
fourth names only what this document will not say. A reader who arrives
at it learns that the chart answers some questions and does not learn
that a version rule is one of them, so there is nothing to follow. The
practice already answers §Structure's promise in three places out of
four, and the fourth is where a rule went missing.

Raised as #949 after #946 flagged it, and framed there as a choice
between narrowing §Structure's claim and moving `deployment-contract.md`'s
boundary.

## Decision

- **Current state that a working artifact carries and enforces stays in
  that artifact.** Where a rule is checkable — and breakable — only
  against the artifact it governs, that artifact is where it belongs,
  and a second statement of it in a responsibility document is the
  duplication ADR-0042 removed rather than the coverage rule 1 asks
  for. This clause on its own would weaken §Structure's "read only
  these"; the next one is what keeps it true.
- **The document that owns the concern routes to that artifact, naming
  it, the section of it, and what that section is authoritative for.**
  That is what keeps "read only these" true: a reader of the
  responsibility documents alone either finds the state or is told by
  name where it is held, and the route costs one line rather than a
  second description to keep in sync. `docs/architecture/README.md`
  rule 1 carries this; the three routes quoted in §Context are what it
  is drawn from.
- **A boundary that states only what a document will not restate is not
  a route.** It is the defect this ADR names. A refusal tells a reader
  what is absent from the page in front of them and nothing about what
  exists elsewhere, so a rule can go missing behind one without any
  document being wrong. `deployment-contract.md` §What it is was such a
  boundary for the whole of the chart's prose.
- **`deployment-contract.md` §What it is keeps its boundary and gains
  the route it lacked.** The sentence refusing to restate a value the
  chart carries stands unchanged and unweakened, and a sentence beside
  it names `deploy/charts/garam/README.md` §Pinning a version as
  authoritative for what `Chart.yaml`'s `version` names and when it
  moves. The rule itself is not copied: naming the section is the
  route, and restating the rule would be the fourth site of a rule
  ADR-0057 §Context already found written in three places and enforced
  in none.
- **Nothing checks this, and nothing is added.** Like ADR-0057's
  version rule, it is held by review under
  `docs/convention/review.md` §What each check verifies, whose
  Architecture row fails a diff where "a decision or a domain changed
  without the documents that describe it, or a changed line contradicts
  what a responsibility document in `docs/architecture/` states". `make ci` reads `docs/` for
  one thing — each Markdown file's `## Contents` list against its own
  `##` headings — and no target can tell a route from a refusal.
  Recording that here is what stops the next measurement filing the
  absence as an unnoticed gap.

## What this does not decide

- **Which concerns leave a responsibility document.** This decides how a
  handover is written, not that any state stated in a responsibility
  document today should move out of one. Nothing here licenses replacing
  a description with a link.
- **Whether a given artifact is the right home for a rule.** The test
  above is that the artifact carries and enforces the rule; whether a
  particular rule meets it is read case by case, and ADR-0042 already
  answered it for the chart.
- **What §Structure promises about the ADRs.** Rule 1 already sends a
  reader from a responsibility document to the ADRs behind it, and the
  route this ADR requires is to a working artifact. The two are
  separate and neither is restated in terms of the other.
- **The rest of what the survey behind this ADR found absent.** The
  chart version rule is not the only current state no responsibility
  document holds or routes to. Sweeping 23 working artifacts against
  the same 17 documents — run twice, once by this ADR's author and once
  independently — measured ten further statements absent, each with a
  control. Three of them sit inside
  `deploy/charts/garam/README.md` §Pinning a version itself (lines 365,
  370 and 376: the registry's refusal of an anonymous pull, `0.1.0` as
  the first released version, and the still-absent `appVersion`), so a
  reader following the route written here meets them. The other seven do
  not: `strategy: type: Recreate` and the service gap an upgrade
  therefore is, the migrate Job's hook phase, `deploy/local/minikube/`,
  the per-worktree test database and `make db-prune`, this repository's
  hard-wrapped prose and `make search`, the single `os.Getenv` call
  `envOr` wraps, and the web build's `standalone` output. None of the
  seven is closed off by a boundary the way the version rule was, so
  each is an ordinary rule 1 omission rather than this decision's case
  and is reported for an issue of its own rather than settled here.

## Consequences

- **A responsibility document that hands a concern over writes one more
  sentence than a refusal costs.** It has to know what the other
  artifact is authoritative for, which is a claim about a file this
  repository holds and so is checkable under
  `docs/convention/documentation.md` §Keep it current.
- **A route goes stale in a way a refusal cannot.** Renaming a section
  of `deploy/charts/garam/README.md` breaks every document naming it,
  and nothing reports that. The trade is deliberate: a stale route is a
  wrong pointer a reader notices at once, and the alternative it
  replaces is a rule no reader finds at all.
- **The chart version rule is reachable from the responsibility
  documents and still written once.** A contributor reading
  `deployment-contract.md` is sent to §Pinning a version; the rule stays
  where `Chart.yaml` sits beside it, which is where ADR-0057 says a
  contributor meets it or does not.
- **This does not make the rule enforced.** ADR-0057 recorded that
  nothing checks the version bump and that review holds it. A reader can
  now find the rule; a change that skips the bump still passes every
  check, which is that ADR's consequence and not this one's.

## Rejected alternatives

- **Moving `deployment-contract.md`'s boundary instead — the second
  reading #949 puts.** §What it is excludes restating a *value* the
  chart carries, and a rule about when the version moves is not a
  value, so the boundary could be refined to admit rules about the
  chart while still excluding its values. The distinction is real and
  it does not survive contact with what the boundary is for. Applied
  evenly it readmits most of the chart's prose: §Choosing a release name
  states "A published web image reaches the api Service a release named
  `garam` renders", §Workload identities states "The name is taken
  verbatim, and the chart composes none",
  §Exposure is the cluster owner's states "Nothing that terminates TLS
  may stand in front of port 8443" — each a rule about the chart rather
  than a value in it, and each therefore no longer excluded. That is two
  descriptions of one deployment, which is the defect ADR-0042 exists to
  remove. It would also contradict the routes already in place:
  `web-app.md` names §Choosing a release name and `api-surface.md` names
  the file for what may stand in front of a listener, and a boundary
  admitting rules about the chart says both documents should have
  restated those rules instead of routing to them.
- **Writing the version rule into `deployment-contract.md` under the
  boundary as it stands.** The move #937 and #946 each declined, for
  the reason each stated. It also misfiles the rule: that document
  opens as "What a garam deployment must plan for that no manifest can
  carry", and the version rule binds whoever edits the chart — ADR-0057
  §Consequences, "A change touching `deploy/charts/garam/templates/`
  carries a `Chart.yaml` edit". A consumer's document is not where a
  contributor's obligation is read.
- **A new responsibility document for the chart.** Every domain or
  concern has one (`docs/architecture/README.md` rule 5), so a document
  owning the chart would be the ordinary answer. The chart is already
  its own description under ADR-0042 and carries its own README; a
  responsibility document beside it would hold either a copy of that
  README or a table of contents for it, and
  `docs/convention/documentation.md` §Keep it current refuses a parallel
  document for a facet of a topic an existing file owns.
- **Deleting §Structure's "read only these" sentence.** It is the
  sentence the gap falsifies, and removing it would close the report
  without answering it: a reader would then know of no promise at all
  about where current state is found, which is worse than the promise
  being qualified. #949 states the same test for rule 1 — "It is
  elsewhere" without a route is worse than the current silence.
- **Adding a check that a route resolves.** A target could read every
  `§<name>` this repository writes against the headings of the file
  named beside it, and it would catch a renamed section. It would not
  catch the failure this ADR is about, which is a route that was never
  written; nothing distinguishes a document that owes a route from one
  that does not without knowing what the other artifact holds. Building
  the half that is mechanical would report the absence of the half that
  is not as a pass.
