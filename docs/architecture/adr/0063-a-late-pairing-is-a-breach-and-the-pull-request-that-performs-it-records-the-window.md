# ADR 0063: A late pairing is a breach, and the pull request that performs it records the window

> Status: accepted
> Date: 2026-09-04

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[README.md](../README.md) rule 3 states two verdicts for a pairing:
it is performed in the pull request that adds or supersedes the ADR,
or "A PR with only one of the two is rejected." Neither reaches the
pairing that arrives in a *later* pull request, which is what the
record holds.

**Four cases, and they are the whole of the unpaired history that
owed a pairing.** #948 read each against the responsibility documents
as they stood at its own commit and found all four owed one, none
performed it, and every one was performed later.

| ADR | added by | paired by | window |
|---|---|---|---|
| 0009 | #98 `25207e9` | #100 `1703863` | 34 minutes |
| 0027 | #329 `225a837` | #328 `7090dd8` | 3 hours 45 minutes |
| 0037 | #395 `741bb0e` | #453 `40a0c32` | 4 days 17 hours |
| 0038 | #409 `01395db` | #411 `1d67d5c` | 20 minutes |

**The measurement, re-run here rather than taken from #948.** Over
the history to `24b177d`, 61 commits add a file under
`docs/architecture/adr/`. 54 moved at least one responsibility
document and 7 moved none: the four above, ADR-0057 (#937), which
rule 3's exemption names, and ADR-0060 (#964) and ADR-0061 (#982),
which #984 made a performed pairing against this index. The control
is that the query reports a pairing where there is one — 54 of 61 —
so the 7 is a filter returning a small set out of a large one and not
a search that matched nothing.

**A window costs more than an omission, and two of these four show
it.** At `01395db`, [authz.md](../authz.md) still carried the line
"An unattached policy's scope is the caller's obligation, not the
engine's", an obligation ADR-0038 had just moved to the engine. At
`741bb0e`, [adapter.md](../adapter.md) §Not decided still carried
"Where garam's own server certificate for the machine listener comes
from is undecided and tracked separately", which ADR-0037 had just
decided. Neither document was incomplete; both were wrong, and #453's
own commit message names its repair: "adapter.md's §Not decided still
called garam's own server certificate undecided, which ADR-0037
decided". The other two windows left a document silent rather than
false — `overview.md` §Out of scope (today) named neither the
frontend ADR-0009 decided nor the crossing ADR-0027 decided.

**What separated 20 minutes from 4 days 17 hours is not visible when
the ADR merges.** Three of the four decisions had a pull request
implementing them, and it carried the pairing: #100 scaffolded the
web app, #328 built the fan-out, and #411 gave the engine
`subject_org` under a title naming ADR-0038. ADR-0037's decision was
never
implemented —
[ADR-0040](./0040-a-pinned-garam-server-certificate-that-rotates-without-a-restart.md)
superseded it on 2026-08-24 and #495 implemented that instead — so
nothing was coming, and the false line waited until #453, a pull
request about the delivery loop, met it and repaired it in passing.
Whether a decision gets implemented, and whether it survives to be,
are both settled after its ADR merges.

**Nothing called any of the four, and the reason is structural.** On
`main` a late pairing and a performed one are the same diff: every
one of these decisions is in its responsibility document today, and
the breach is legible only by reading the ADR's own commit against
what moved with it. Rule 3's rejection is aimed at a pull request
that is merged before the lateness can be observed.

**This is an ADR under rule 4, owed on the second arm alone.** No
passage that had passed review is in violation after it: the four
windows are closed, every one of these decisions is in its
responsibility document today, and §What nothing detects still reads
true where it says rule 3 is not that detection. The record was
divided all the same. Four pull requests merged carrying an ADR and
no pairing, and four more merged carrying the pairing with nothing
said about the window; rule 3 as it stands sorts the first four as
rejected, and review sorted all eight as ordinary merges. Both
answers stand in the repository, which is the shape
[ADR-0060](./0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)
§Decision arm two names.

## Decision

- **A pairing that arrives in a later pull request is a breach.** Not
  a permitted shape with a bound, and not a lesser kind of pairing.
  Rule 3 keeps its site: the pairing is owed in the pull request that
  adds or supersedes the ADR, and nothing here defers it.
- **The verdict is stated on the pull request that performs the
  pairing.** That one is not merged yet, which is the whole of why it
  is the site. It names the ADR it is pairing and the commit that
  added it, and it does not merge without them.
- **What that buys is both ends of the window in the record.** The
  ADR's commit is one end and the pairing's is the other, so the
  breach stops being invisible on `main` and a later reader counts
  what took four days apart from what took twenty minutes without
  re-reading every ADR commit.
- **Rejection is not withdrawn.** It is what the ADR's own pull
  request meets when a reviewer catches the missing pairing there,
  which is where the breach is still preventable. This decision adds
  the verdict for after that moment has passed; it does not replace
  the one before it.
- **Rule 3 is the only site.** The verdict is written in the rule it
  belongs to, and no ledger, no second section and no per-case issue
  records the windows.
- **Rule 3 carries the verdict and this ADR carries why**, the split
  rule 1 sets with ADR-0058 and rule 4 sets with ADR-0060.

## What this does not decide

- **Whether any of the four was a breach.** #948 settled that and
  closed them as a sequencing failure with nothing lost. They are
  read here as evidence about the rule, not as verdicts on their
  authors, which is how ADR-0060 §Rejected alternatives settled
  retroactivity. This binds what is written after it.
- **Whether a falsified document is a worse breach than a silent
  one.** ADR-0037 and ADR-0038 each left a responsibility document
  positively false and ADR-0009 and ADR-0027 left one silent. The
  distinction is evidence here — it is why a short window is not a
  lesser defect — and it is not written into the rule as a second
  grade of breach.
- **Rule 4 and the index-rule pairing.** Both untouched. This ADR
  decides a rule of this index and so pairs with the index under the
  line #984 added, which is a use of that line and not a change to
  it.
- **Whether an ADR whose decision nothing implements owes anything
  extra.** ADR-0037 is the case and the answer here is the same as
  for the others: the pairing was owed in #395. Whether such a
  decision should be written at all is not this rule's question.
- **Rule 2.** No ADR body is edited, ADR-0037's and ADR-0038's
  included.

## Consequences

- **A reviewer of a pull request that pairs an earlier ADR has a
  rule to cite.** Before this, the only reading of rule 3 available
  to them was one about a pull request that had already merged, so
  the review had nothing to hold and none of the four pairings was
  called.
- **An author who cannot pair in the ADR's own pull request knows the
  cost is recorded and not waived.** Three of the four were not
  oversights but deferrals to work that followed within hours, and
  the rule's silence is what made that look free.
- **Nothing is annotated retroactively.** This binds pull requests
  written after it, so the record of these four stays #948's report
  and the table above; `1703863`, `7090dd8`, `40a0c32` and `1d67d5c`
  are left as they are.
- **Review is what holds it**, under
  `docs/convention/review.md` §What each check verifies, whose
  Architecture row fails a diff where "a decision or a domain changed
  without the documents that describe it".

## Rejected alternatives

- **Permitting a late pairing under stated conditions** — a same-day
  follow-up, or a repair the author files themselves. The issue puts
  this first and the fixture refuses it. Every condition that would
  have admitted ADR-0038's twenty minutes reads on what followed the
  ADR: that a pull request implementing the decision was coming, that
  the author was carrying it. None of that is knowable when the ADR
  merges — ADR-0037's decision stopped having an implementation five
  days later, when ADR-0040 superseded it — so the condition would be
  a promise, and a rule resting on one admits the four-day window on
  the terms that admit the twenty-minute one. Both left a
  responsibility document positively false, so the short window is
  the same defect and not a smaller one.
- **A duration bound**: late is permitted within a day, a breach
  after. It reads the outcome rather than the choice, and it would
  have passed three of these four while the author of each made the
  same call as ADR-0037's. It also invites the reading that a
  document may be wrong for a stated number of hours, which is the
  opposite of what rule 1 promises a reader.
- **Recording the window as an erratum on the ADR.** Rule 2 admits
  one where "a decision stands but a fact supporting it was wrong".
  Here the decision and its facts were right and a responsibility
  document was wrong, so the erratum would be about the wrong
  artifact. It would also put the record of a breach in a body rule 2
  freezes, where a reader of the present state does not look.
- **A ledger of late pairings** — a section of this index, or an
  issue per case. It is the second site #987 refuses by name, and it
  would go on growing after every window it records had closed. The
  pairing's own pull request already exists, is already reviewed, and
  is the artifact the breach produced.
- **A check.** `docs/convention/ci.md` §Core principle governs how a
  check is invoked rather than whether one exists, so it does not
  decide this either way; the measurement does. 54 of the 61 commits
  that add an ADR paired in the same pull request with no gate at
  all, and each of the seven that did not is either exempt, an
  index-rule pairing, or one of the four — every one of which was
  repaired, three within hours. A gate would fire on a state that has
  never persisted, and what it could read is the wrong thing anyway:
  a commit touching a file under `docs/architecture/` is not a
  commit pairing the decision that ADR took, and no target can tell
  the two apart. Recording this here is what stops the next
  measurement filing the absence as an unnoticed gap, the same shape
  ADR-0058, ADR-0060, ADR-0061 and ADR-0062 each recorded.
- **Leaving rule 3 with two verdicts.** The state that produced four
  unremarked breaches, and the one #987 names: "silence is what let
  four happen without one being called". A reviewer meeting a late
  pairing today reads a rule that speaks only about a pull request
  they cannot reach.
