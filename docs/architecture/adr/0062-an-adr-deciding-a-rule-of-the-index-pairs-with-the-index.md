# ADR 0062: An ADR whose decision is a rule of the architecture index pairs with the index, not with a responsibility document

> Status: accepted
> Date: 2026-09-03

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[README.md](../README.md) rule 3 named one site for a decision's final
state: "A PR that adds or supersedes an ADR must update the affected
responsibility documents in the same PR. A PR with only one of the two
is rejected — the final state must always live in the responsibility
documents."

**The index is not a responsibility document.** §Structure lists the
responsibility documents and `adr/` as two kinds and the file holding
both lists is neither, which is the reading #957 also rests on. So an
ADR whose decision's final state is one of this index's own rules has
no responsibility document to pair with, and the clause above says the
pull request carrying it is rejected.

**Two merged pull requests are the fixture, and each carried two
files.**

- #964 (`626986d`, merged 2026-09-03T11:37:29Z) added
  [ADR-0060](./0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)
  and rewrote rule 4. Its §Consequences argues the point rather than
  citing a rule: "The pairing's purpose is met and its named site is
  not the one this decision has."
- #982 (`1f2c511`, merged 2026-09-03T12:41:08Z) added
  [ADR-0061](./0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md)
  and added a sentence to rule 4. Its §Consequences repeats the
  argument and defers: "#971 is open on whether rule 3 should name it;
  this ADR follows that precedent and does not settle it."

Neither touched a responsibility document. `git show --name-only` on
each names `docs/architecture/README.md` and the new ADR, and nothing
else.

**The measurement, with the control in the direction this decision
moves.** Over the history to `1f2c511`, 60 commits add a file under
`docs/architecture/adr/`. 53 moved at least one responsibility document
and 7 moved none: ADR-0009 (#98), ADR-0027 (#329), ADR-0037 (#395) and
ADR-0038 (#409), which #948 holds open on their merits; ADR-0057
(#937), which rule 3's existing exemption names; and ADR-0060 and
ADR-0061. The control is that the query reports a pairing where there
is one — it reports 53 of 60 — and that it reads the whole track: 62
ADR files stood under that path at that commit, `0000-template.md` and
61 numbered. This decision reaches 2 of the 60. Because it narrows what
rule 3 demands rather than widening it, the control that matters is the
one that still fires: the other 58, #948's four included, keep owing
the responsibility documents they affect.

**The record was divided, which is why this is an ADR under rule 4.**
Two merged pull requests and two accepted ADRs sort an index-rule ADR
as owing no responsibility document; rule 3 as it stood sorts those
same two pull requests as rejected. Both answers stand in the
repository, which is the shape ADR-0060 §Decision arm two names.

**The two shapes that pair with no responsibility document are not one
shape.** ADR-0057 confirms a decision already in the record and moves
no current state anywhere. ADR-0060 and ADR-0061 each move state — a
rule — into a file rule 3 did not name. #953 drew that line and #971
asks that it be kept.

## Decision

- **An ADR whose decision's final state is a rule of
  [README.md](../README.md) pairs with that index, in the same pull
  request.** The site is the rule it changes.
- **The pairing is performed, not waived.** Such a pull request carries
  the rule as changed, so what a reviewer checks is unchanged in shape:
  the final state left the ADR and landed in a file in this pull
  request. This is why the case is written as a site and not as a
  second exemption — the two fixtures each did pair something.
- **Rule 3's purpose clause is revised rather than dropped.** It read
  that the final state "must always live in the responsibility
  documents"; it now reads that the final state lives outside the ADR,
  in the document a reader of the present state opens. The universal it
  carried was the true one — the ADR is never where the final state
  stays, because rule 2 freezes that body — and only the site it named
  was too narrow.
- **Which document that is, per kind of state, is not rule 3's to
  say.** Rule 1 says where a responsibility document's state lives and
  rule 6 says where a structural rule lives; rule 3 states its own site
  and the revised purpose clause covers the rest without enumerating
  them.
- **This does not widen the exemption and does not touch it.** The
  exemption's test is whether the decision moves current state at all,
  and an author applies it before writing a line. An index-rule ADR
  moves state and fails that test, which is correct: it is a pairing,
  not an exemption.
- **Everything else keeps the named site.** An ADR whose decision is
  anything but a rule of this index owes the responsibility documents
  it affects, whatever else the pull request also carries.
- **Rule 3 carries the site and this ADR carries why**, the split rule
  1 sets with ADR-0058 and rule 4 sets with ADR-0060.

## What this does not decide

- **Whether the index may hold current state of the system.** That is
  #957, and this reaches only the rules the index carries. A rule about
  how the record is written is not a description of garam, so
  §Structure's promise about where the current state lives is
  untouched here and #957 is free to decide it either way.
- **Whether the four ADRs #948 holds open owed a pairing.** ADR-0009,
  ADR-0027, ADR-0037 and ADR-0038 each decide something about garam,
  none has an index rule for its final state, and none is on this
  line's ground. Their reading is #948's.
- **Whether rule 3 reaches a specification.**
  [api-surface.md](../api-surface.md) §Not decided asks it and it stays
  open. That bullet reasoned from a premise this change falsifies —
  that everything outside the responsibility documents is outside rule
  3's reach — so the premise is repaired in the same pull request and
  the question is left as it was.
- **What a decision landing in `docs/convention/` owes.** Nothing on
  this track: rule 4's second clause is unchanged and a rule there
  earns no ADR however much it binds.
- **Whether #964 or #982 was a breach.** Both are read here as evidence
  about the rule, not as verdicts on their authors, which is how
  ADR-0060 §Rejected alternatives settled retroactivity.

## Consequences

- **An author whose ADR decides a rule here answers rule 3 by number.**
  #964 and #982 each had to argue from the purpose clause in their own
  §Consequences, in a body rule 2 then froze, and a reader of rule 3
  alone reached the opposite answer. That argument is now the rule's to
  make once.
- **A pull request adding such an ADR is still incomplete without the
  rule.** Under a second exemption it would not have been, and the
  final state would have been free to stay in the ADR body. Naming the
  index as the site is what keeps the reviewer's question answerable.
- **This ADR is its own fixture.** Its final state is rule 3, which
  lives in the index, and this pull request carries both. Under rule 4
  it is owed on both arms: it puts
  [api-surface.md](../api-surface.md) §Not decided's premise in
  violation, repaired here, and the record it writes down held two
  answers.
- **One passage was repaired.** `api-surface.md` §Not decided said rule
  3 "names responsibility documents" and inferred from a file not being
  one that it is outside rule 3's reach. The conclusion about the two
  specifications stands; the inference no longer does, because the
  index is inside the reach and is not a responsibility document.
- **Nothing checks this.** `make ci` reads `docs/` for one thing, each
  Markdown file's `## Contents` list against its own `##` headings, and
  no target can tell which file a decision's final state belongs in.
  Review holds it, under
  `docs/convention/review.md` §What each check verifies, whose
  Architecture row fails a diff where "a decision or a domain changed
  without the documents that describe it". Recording that here is what
  stops the next measurement filing the absence as an unnoticed gap, the
  same shape ADR-0058, ADR-0060 and ADR-0061 each recorded.

## Rejected alternatives

- **Moving these rules out of the index into a responsibility document
  of their own, so that every ADR pairs one.** The restructure
  `docs/convention/simplicity.md` §Core principle asks for before a
  clause is added: with the rules in a responsibility document the case
  cannot arise and rule 3 needs no second site. Rejected on three
  counts. It decides #957's neighbouring question — whether a concern
  of this kind earns a document under rule 5 — from inside a pull
  request scoped to rule 3. It moves nine rules out of the file
  `AGENTS.md` sends every contributor to before writing, which is a
  larger change to how the record is read than the one being fixed. And
  it relocates the case rather than removing it: the index would still
  carry §Structure, §Index, §What nothing detects and §Operation, and a
  decision changing any of those would meet the same silence.
- **A second exemption: an ADR deciding a rule of this index pairs with
  nothing.** The shape #971 puts first, and it is false as a
  description before it is unsafe as a rule. #964 and #982 each paired
  something — the index, in the same pull request, in the rule their
  ADR decided. Written as an exemption it would permit the next such
  ADR to merge with the rule unwritten, leaving the final state in a
  body rule 2 freezes, which is the whole of what the pairing exists to
  prevent. The two fixtures show the pairing performed, so the rule
  records a site.
- **Widening the existing exemption to cover both shapes.** Refused by
  #971 and rightly. The exemption's test is whether the decision moves
  current state; an index-rule ADR moves a rule into the index, so
  admitting it makes that test stop sorting anything, and it is the
  test an author applies before writing a line. It would also lose the
  line #953 drew.
- **Ruling that no such ADR is legitimate.** #971's alternative
  acceptance criterion, and the record refuses it rather than an
  argument having to: rule 4 as ADR-0060 wrote it *requires* an ADR for
  a change to these rules unless both arms hold, so an ADR whose
  decision is a rule here is not merely permitted but compulsory for a
  divided change. Taking this would reopen ADR-0060 the day after it
  merged, on a question ADR-0060 did not raise.
- **Naming each kind of state's home in rule 3** — the responsibility
  documents for a decision about garam, `structure.md` for a structural
  rule, this index for a rule of it. It reads well and it is the second
  site `docs/convention/README.md` §Independence names: "A rule appears
  once — inside a file as much as across files." Rules 1 and 6 already
  say where their kinds live. Rule 3 states its own site, and the
  revised purpose clause says the rest without enumerating.
- **Leaving rule 3 as it stood and letting each such pull request argue
  it.** Cheapest, and it is what #964 and #982 did — twice in one day,
  each in an ADR body, each reaching the same answer the rule's text
  denies. It is the shape ADR-0060 §Rejected alternatives refused when
  it moved #955's ruling out of an issue body and into the record: a
  line binding every future pull request belongs where a contributor
  reads before writing.
- **A rule 10 of its own.** The material is what a pull request adding
  an ADR must carry, which is rule 3's subject. Splitting it would
  leave a reader of rule 3 with an incomplete answer and no reason to
  look further — the defect #939 fixed and ADR-0060 refused to
  reintroduce.
- **Repairing `api-surface.md` §Not decided by deleting the bullet.**
  It would remove the falsified premise and the open question with it,
  settling by deletion something that document says "is the index's to
  decide and not this document's". The bullet keeps its question and
  loses only the premise.
