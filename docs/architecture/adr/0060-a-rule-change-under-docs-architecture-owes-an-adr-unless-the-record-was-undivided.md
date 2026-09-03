# ADR 0060: A rule change under docs/architecture/ owes an ADR unless nothing goes into violation and the record was undivided

> Status: accepted
> Date: 2026-09-03

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[README.md](../README.md) rule 4 divides the ADR track's ground from
`docs/convention/`: an ADR covers a decision about garam itself, and a
rule in `docs/convention/` earns none however much it binds. A change to
the rules under `docs/architecture/` — rule 4 and its neighbours, and
the shape they give the documents beneath this index — is on neither
ground. It governs how the record is written rather than what the system
is.

**Two pull requests read that silence opposite ways and both merged.**

- #946 (`4e6d1c6`) rewrote rules 3 and 4 and added no ADR, on the stated
  reading that this file "is the index beside them, not one of them".
- #953 (`95886de`) rewrote rule 1 and §Structure and added
  [ADR-0058](./0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md),
  raising the disagreement as its own Convention concern 1 rather than
  resolving it.

Neither is an author's error. The reading each took is available from
rule 4's text, which is what makes it the rule's gap.

**The unit that sorts the two is a passage standing in the repository.**
#955 ruled the line as "an ADR is owed when the change decides what
counts as a defect; it is not owed when it states what was already
binding", and applying that needs something to count. Past pull requests
are not it: each was decided under the rules as they then stood.
Passages are, and the file set is the evidence, because a change that
puts a passage in violation has to repair it. #946 changed one file, 2
insertions and 2 deletions, and repaired nothing. #953 changed three —
the rule, ADR-0058, and a 14-line repair to
[deployment-contract.md](../deployment-contract.md) §What it is, which
ADR-0058 §Decision names as the boundary that became the defect.

**That test is necessary and not sufficient.** A rule drafted to ratify
everything currently standing passes it by construction, so its silence
is not evidence that the rule took no side. This is the shape
`docs/convention/git.md` §Commits met and answered for a commit title:
an "and" in the title "is not the test", because two unrelated changes
can be written to share a verb, and what decides is named separately
from the wording that can be arranged to pass.

**What #946 had beyond it is a second arm.** Its evidence was that the
rewritten rule "sorts all 40 the way the history did" — the record it
wrote down was undivided, every case in it already sorted the way the
rule then sorted them, so writing it down took no side and moved no
question. `docs/architecture/` was not undivided. It held two answers,
both merged, both defensible, and a rule that names one of them settles
the question rather than recording it. Rule 7 already says what that is:
"Which side is taken decides what counts as a defect, so it is a
decision under rule 4."

**A second reason concerns where the ruling lives.** #955's issue body
carries this line and nothing else does. An issue is closed and its body
is not part of the record a contributor reads before writing; rule 2
freezes an ADR's body against exactly that. A line binding every future
pull request that touches `docs/architecture/` belongs in the record,
and the pull request that writes the rule is the one that owes it.

## Decision

- **A change to the rules under `docs/architecture/` earns an ADR
  unless both arms hold.** The arms are conjunctive: one holding while
  the other fails is a decision, and the change carries an ADR.
- **Arm one — no passage that had passed review is in violation after
  the change.** The unit is a passage standing in the repository: a
  sentence in a responsibility document, in an ADR, or in this index. A
  past pull request is not a unit; it was judged under the rules as they
  then stood and is not re-judged. Because a change that puts a passage
  in violation has to repair it, the pull request's own file set is
  where the arm is read.
- **Arm two — the record the change writes down was undivided.** Every
  case the change now sorts was already sorted that way: no merged pull
  request, accepted ADR or standing rule answered the question the other
  way. Where the record held two answers, taking one is a side taken
  however little it moves today.
- **Both arms are required because either alone can be passed by
  drafting.** Arm one alone is passed by a rule written to ratify what
  stands. Arm two alone is passed by a rule that reverses settled
  practice on a question nobody had yet asked twice. Requiring both is
  what makes the test read the change rather than its wording.
- **Rule 4 carries the test and this ADR carries why.** A reader of rule
  4 alone has to reach the answer — the requirement #939 fixed and #955
  restated — so the two arms and the worked cases are stated there in
  the paragraph a reviewer cites by number. The argument for the second
  arm, the alternatives weighed, and what the test does not reach stay
  here, where rule 2 freezes them.

## What this does not decide

- **What a responsibility document may say.** This reaches the rules
  under `docs/architecture/` and not the state they govern. The content
  of a responsibility document is a description of garam, and rule 4's
  first clause already puts it on the ADR track's ordinary ground.
- **Whether adopting a rule the conventions template decided is a side
  taken here.** #603 and #612 took rules from
  `garamsh/convention-driven-project` rather than deciding them, and
  under the test as written both would owe an ADR: each put a passage in
  violation, and #603's was repaired only later, by #631. Whether a rule
  this project did not decide can be a side this project took is a
  second ruling, filed as #967 rather than carved in here.
- **Anything about `docs/convention/`.** Rule 4's second clause stands
  unchanged: a rule there earns no ADR however much it binds, and a
  convention change that divides the record is still not on this track.
- **Whether any past pull request was a breach.** The test binds changes
  made after it. The commits read in §Context are read as evidence about
  the rule, not as verdicts on their authors.
- **How a divided record is found.** Nothing enumerates the questions
  `docs/architecture/` has answered twice. An author reads the record
  they are about to write down, and §Consequences records what that
  costs.

## Consequences

- **An author of a rule change here reads the record before drafting.**
  Arm two cannot be answered from the diff. It asks what the rules,
  the ADRs and the merged pull requests already say about the question,
  which is the search #946 ran over 40 commits and #953 ran over 17
  documents. That cost is the arm's point: a rule written without it is
  a side taken without knowing a side was available.
- **A rule change that only tidies still costs nothing.** Fixing a
  wording, correcting an example, or stating a unit an earlier pull
  request fixed outside the rule puts no passage in violation and
  settles no divided question, so it carries no ADR — as #762, #809,
  #835 and #839 did not.
- **Nothing checks either arm.** `make ci` reads `docs/` for one thing,
  each Markdown file's `## Contents` list against its own `##`
  headings, and no target can tell a rule that settles a division from
  one that records agreement. What holds it is review, under
  `docs/convention/review.md` §What each check verifies, whose
  Architecture row fails a diff where "a decision or a domain changed
  without the documents that describe it". Recording that here is what
  stops the next measurement filing the absence as an unnoticed gap,
  the same shape ADR-0058 recorded for a route.
- **This ADR pairs with the index, and the index is not a
  responsibility document.** Rule 3 requires the affected responsibility
  documents in the same pull request so that the final state lives
  outside the ADR. The final state of this decision is rule 4, and the
  rules under `docs/architecture/` live in this index the way rule 6
  puts structural rules in `structure.md`. No responsibility document
  becomes wrong or incomplete when this merges, and this is not rule
  3's exempt case either: that exemption is for an ADR confirming a
  decision already in the record, and this one settles a record that
  held two. The pairing's purpose is met and its named site is not the
  one this decision has.

## Rejected alternatives

- **Every change to these rules earns an ADR, and no test is needed.**
  `docs/convention/simplicity.md` §Core principle asks whether the shape
  can be changed so the case cannot arise, before a test with two arms
  is added, and this is that shape: no arms, no unit to fix, nothing to
  measure. It is rejected on what it costs the record. Rule 2 freezes an
  ADR, so a typo in a rule, a corrected example and a unit stated more
  precisely would each be frozen beside the decisions they annotate —
  #762, #809, #835 and #839 would have added four. An ADR track whose
  entries are mostly wording fixes tells a later reader nothing about
  which questions were ever open, which is the whole of what the track
  is for. The test is the addition, and it is paid for by keeping the
  record readable.
- **The one-arm test, as #964 first wrote it.** Rule 4 would have said
  a change earns an ADR when a passage that had passed review goes into
  violation, and nothing else. It sorts #946 and #953 correctly, which
  is why it was written, and it is defeated by drafting: a rule that
  ratifies what stands passes it whatever question it settles. Kept as
  arm one rather than discarded, because it is the half that is
  checkable from a file set.
- **Leaving the ruling in #955's issue body with rule 4 pointing at
  it.** Cheapest, and it is what this ADR exists to refuse. Rule 2
  freezes an ADR; nothing freezes an issue body, and a contributor
  reading `docs/architecture/` before writing does not read a closed
  issue. The ruling would bind every future pull request from a place
  outside the record.
- **Putting the two arms only here and leaving rule 4 with #955's two
  phrases.** It would keep rule 4 short. It fails #955's third
  acceptance criterion, that a reader of rule 4 alone reaches the
  answer: "decides what counts as a defect" is the label the test
  replaces, and a reader who has only the label is back to weighing
  #946 against #953. Rule 1's own shape settles the split — it states
  the routing rule and cites ADR-0058 for the argument — so rule 4
  states the test and cites this.
- **A rule 10 of its own.** The material is about which changes the ADR
  track records, which is rule 4's subject; splitting it would leave a
  reader of rule 4 with an incomplete answer and no reason to look
  further. That is the defect #939 fixed in rules 3 and 4 and #955
  named again.
- **Restating rule 7's sentence in rule 4.** Rule 7 applies this test to
  a structural open point and points at rule 4 for it. Rule 4 stating
  the test is what makes that pointer land; repeating rule 7's sentence
  would be the second site `docs/convention/README.md` §Independence
  names, and ADR-0058 §What this does not decide draws the same line
  between routing to something and restating it.
- **Making the test retroactive.** Applied backwards it marks #603 and
  #612 as owing an ADR, which would convert two merged pull requests
  into breaches and re-open rules 5 to 8 on a technicality. The rules
  those two adopted are not in dispute; how the project adopts a
  template's rule is, and that is #967.
