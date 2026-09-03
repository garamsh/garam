# ADR 0061: Adopting a rule the conventions template decided is a decision taken here, and the ADR records the adoption

> Status: accepted
> Date: 2026-09-03

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[README.md](../README.md) rule 4 divides the ADR track's ground three
ways. A decision about garam itself earns an ADR; a rule in
`docs/convention/` earns none however much it binds; a change to the
rules under `docs/architecture/` earns one unless both of
[ADR-0060](./0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)'s
arms hold. None of the three asks **who decided the rule**, and that
question has an answer here that is not this project.

ADR-0060 §What this does not decide names the gap and leaves it: whether
"a rule this project did not decide can be a side this project took is a
second ruling, filed as #967 rather than carved in here."

**Two adoptions from `garamsh/convention-driven-project` are the
fixture.**

- #603 (merged 2026-08-26T11:56:10Z) added rule 5 byte-identical to the
  template's and declined rules 6 to 8, on the stated ground that they
  turn on a `structure.md` garam then had none of.
- #612 (merged 2026-08-26T12:50:29Z, 54 minutes later) added rules 6, 7
  and 8 verbatim, and says so in its own body: it "reverses the part of
  #602 that declined them for want of a `structure.md`".

Neither recorded the adoption. #612 carries
[ADR-0047](./0047-a-domain-reaches-a-sibling-through-its-declared-surface.md),
which records garam's domain shape and the sibling side — not the taking
in of three index rules that bind every pull request afterwards.

**The template holds no record to send a reader to.** At
`garamsh/convention-driven-project@a6a2355` — `template/main`, committed
2026-08-31 — `docs/architecture/adr/` holds one file,
`0000-template.md`, which is the blank form, and it is the only file
ever added under that path in that branch's history. Two controls read
the same way at the same commit: the tree lists 21 files under `docs/`,
16 of them under `docs/convention/`, so the query reads the tree; and
the same history query over `docs/convention/` names 18 files ever
added, so it reports additions where there are any. The record reading A
below would send a reader to does not exist.

**An adopted rule can land here pointing at a rule this project does not
have.** Rule 7, taken verbatim by #612, says a point the structural
rules leave open "is a decision under rule 4". In the template, rule 4
carries the sentence that makes that reference mean *earns an ADR*. At
`bfcc1f4`, the merge commit of #612, garam's rule 4 read in full: "Keep
this index current: every responsibility document and notable ADR is
listed here." The author of #612 saw it and raised it as a Convention
concern rather than working around it; #762 repaired it ten hours later
by adopting the template's clause as well, and what that clause decides
here stayed divided — #946 against #953 — until ADR-0060 on 2026-09-03.
An adopted rule is read against this project's record by whoever adopts
it or by nobody.

## Decision

- **Where a rule was first decided is no part of rule 4's test.** An
  adoption is read by the same two arms as any other change to the rules
  under `docs/architecture/`. What binds a contributor here is this
  project taking the rule in, which is a decision taken here: #603 and
  #612 took it oppositely within the hour, on the same three rules, and
  both merged.
- **The ADR an adoption owes records the taking in, not the rule.** It
  names the template repository, the commit adopted from, and what that
  commit decided, and it does not restate the rule — which now stands in
  this project's own rules, where a contributor meets it. That is
  [ADR-0058](./0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)'s
  route shape, applied to another repository instead of a working
  artifact, and a commit is the one route that cannot go stale.
- **The unit is the adoption, not the rule.** A pull request that takes
  in six rules and owes an ADR owes one, naming the six. The decision
  being recorded is that this project takes these rules now, and it is
  taken once per pull request.
- **This does not make a sync expensive: an adoption that passes both
  arms earns nothing.** One that puts no passage in violation and
  settles nothing the record had divided is such an adoption. #762
  adopted the template's index rules — including the clause rule 7
  needed — and is that case; ADR-0060 §Consequences sorts it as earning
  none, and this decision leaves that sorting standing.
- **Nothing checks this.** `make ci` reads `docs/` for one thing, each
  Markdown file's `## Contents` list against its own `##` headings, and
  no target can tell an adoption from an authored rule. Review holds it,
  under `docs/convention/review.md` §What each check verifies, whose
  Architecture row fails a diff where "a decision or a domain changed
  without the documents that describe it". Recording that here is what
  stops the next measurement filing the absence as an unnoticed gap, the
  same shape ADR-0058 and ADR-0060 each recorded.

## What this does not decide

- **What an adoption into `docs/convention/` owes.** Nothing, and rule
  4's second clause already says so: a rule there earns no ADR however
  much it binds. #972, #975 and #978 all proceed on that reading and
  none is disturbed here. The case this ADR reaches is an adoption into
  `docs/architecture/`, where rule 4's third ground is what the change
  lands on.
- **Which of rule 4's grounds an adoption lands on.** That division is
  rule 4's already and is unchanged: an adopted rule describing garam is
  on the first ground, an adopted rule under `docs/architecture/` on the
  third. This decides only that the answer does not turn on the rule
  having been written elsewhere.
- **Whether the project should sync from a template at all.** The
  relationship is the PM's, and `Makefile`'s `TEMPLATE_SYNCED` and
  `scripts/template-diff.sh` are where it is operated.
- **Where the template relationship is described.** No responsibility
  document says this project takes rules from a template; the pull
  request adding this ADR reports that as a rule 1 finding and does not
  act on it.
- **Whether any past adoption was a breach.** #603, #612 and #762 are
  read here as evidence about the rule, not as verdicts on their
  authors. Rule 4 binds what is written after it, which ADR-0060
  §Rejected alternatives settled when it refused to make the test
  retroactive.

## Consequences

- **An author syncing from the template reads the adopted rules against
  this project's record before merging.** That is arm two's cost, and it
  is where rule 7's dangling reference would have been met: the question
  "does this rule mean here what it means there" is asked once, by
  whoever takes the rule in.
- **The ADR track gains at most one entry per sync**, and only for a
  sync that changes what counts as a defect here. A top-up of rules the
  record had already sorted the same way still costs nothing.
- **A reader of this repository's ADRs can find why an adopted rule
  binds them without leaving for another repository.** They find a route
  rather than a copy, so the reason stays owned by whoever wrote it and
  this repository's record stays honest about what it decided.
- **This ADR pairs with the index, and the index is not a
  responsibility document.** Its final state is rule 4, which lives in
  `docs/architecture/README.md` the way rule 6 puts structural rules in
  `structure.md`. No responsibility document becomes wrong or incomplete
  when it merges. ADR-0060 §Consequences met the same shape and #971 is
  open on whether rule 3 should name it; this ADR follows that
  precedent and does not settle it.

## Rejected alternatives

- **Adoption earns none, because the decision was the template's.** The
  reading #967 puts first, and the cheaper one. It fails on its own
  premise: the record it would send a reader to does not exist, as
  §Context measures at `a6a2355`. It also mistakes what binds — the
  template decided the rule's content, and this project decided that it
  binds here, twice and oppositely within one hour. And it is passed by
  construction: it ratifies every adoption standing in the repository,
  which is the drafting defeat ADR-0060 §Decision names for arm one
  alone and built arm two to catch. Under it, #612's dangling rule 7
  reference would have been nobody's to notice.
- **A clause in rule 4 exempting an adoption.** What #967 forbids, and
  the reason is rule 4's shape rather than the ruling's merit: #955 put
  one ruling into that paragraph and #964 wrote it, and a second
  exception turns a test into a list of exceptions. The line written
  instead adds no exception — it says the test has no third arm — and it
  is a phrase where a clause was refused.
- **A rule 10 of its own.** The material is which changes the ADR track
  records, which is rule 4's subject. Splitting it would leave a reader
  of rule 4 with an incomplete answer and no reason to look further,
  which is the defect #939 fixed and ADR-0060 refused to reintroduce.
- **Copying the template's reasoning into the adoption ADR instead of
  routing to it.** Rule 2 would then freeze this repository's copy of
  another repository's argument, which its owner can revise and this
  project cannot follow. The route costs one line and names a commit,
  and `docs/convention/documentation.md` §Keep it current asks for
  exactly that where a claim is about something outside this repository.
- **A responsibility document for the template relationship, instead of
  a line in rule 4.** It would answer where a reader learns that this
  project syncs from a template, which is a real gap and is reported
  with this pull request. It answers a different question: rule 4 asks
  what a change owes, and a document describing the relationship would
  leave a worker mid-sync with the same silence. Whether such a document
  is owed under rule 5 is the PM's, and creating one here would decide
  it without an issue.
