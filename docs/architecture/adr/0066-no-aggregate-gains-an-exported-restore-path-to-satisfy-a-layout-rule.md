# ADR 0066: No aggregate gains an exported restore path in order to satisfy a layout rule

> Status: superseded by [ADR-0069](./0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md)
> Date: 2026-09-05

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

`docs/convention/stack-go.md` §1 requires that an implementation of a
domain's dependency interface live in `internal/<domain>/<name>/`, *"a
package named for the interface rather than for the technology behind
it"*. **No such package exists.** At `33ad3e4`, 22 `postgres*.go` files
sit directly under nine domains, and the only depth-2 directories under
`internal/<domain>/` are `db/` in nine domains and `mocks/` in three —
both generated. That second count is the control: the same search
reports a subdirectory where one exists, so the zero for implementation
packages is a zero and not a query that matched nothing.

**#638 adopted the rule knowing that, and priced it.** Its Convention
concerns field states the breach — *"Adopting it makes this tree
non-conforming, and that is the point"*, 21 files and 2,531 LoC across
9 packages that do not exist — and its concern 3 goes further than a
file count: *"7 of the 9 domains build their aggregates from unexported
fields inside the store … so the price is not 'export 6 identifiers'
but 'for 7 domains, either export the fields or add hydration
constructors'"*, which it calls an architecture question. The author
asked the question, named the class correctly, and declined to answer
it alone. That is the practice working.

**Its figure was low, which is the reason to re-measure rather than
quote.** Concern 3 names `authn` and `watch` as the two domains that do
not build aggregates from unexported fields, making it 7 of 9. The
probe puts `internal/authn/postgres.go` among the fourteen files that
write an aggregate's unexported fields in a struct literal, so it is 8
of 9 and `watch` is alone. Nothing in the decision below turns on the
difference; what turns on it is that a figure reached by reading is not
a figure reached by compiling.

**What failed is where the answer lived.** A merged pull request's body
is not part of the record a contributor reads before writing, which is
the defect rule 2 freezes an ADR against and the one
[ADR-0060](./0060-a-rule-change-under-docs-architecture-owes-an-adr-unless-the-record-was-undivided.md)
§Rejected alternatives refused to accept for a ruling left in an issue
body. So `stack-go.md` says one thing, `internal/` does another, and
the reconciliation is reachable only by knowing to open #638.

**What conforming costs was measured**, in
[`../plans/2026-09-04-package-layout-boundary-crossings.md`](https://github.com/garamsh/garam/blob/5f178278626fa295aaab08afef158625bfe9945e/docs/plans/2026-09-04-package-layout-boundary-crossings.md):
move each domain's store files into `internal/<domain>/store/`, rewrite
the package clause, dot-import the domain so every *exported*
identifier still resolves, and build. The residual errors are exactly
what cannot cross a package boundary. That document measured 177 at
`efaf832`; #1031 has since changed `internal/operator/`, so the probe
was re-run for this ADR at `33ad3e4`:

| domain | crossings | domain | crossings |
|---|---|---|---|
| `org` | 58 | `message` | 17 |
| `agent` | 45 | `authz` | 16 |
| `operator` | 16 | `user` | 12 |
| `ca` | 10 | `authn` | 6 |
| `watch` | **0** | total | **180** |

Three controls. The untouched tree builds and vets clean, so a failure
after the probe is the probe's. Each domain package still builds after
its own files leave — 0 for all nine — so the residue is boundary
crossings and not collateral breakage. And **`watch` returns 0**, which
is the one that matters: the instrument does report *"this domain
moves"* when a domain moves, so the other eight are a property of those
domains rather than of the method. `operator` moved from 13 to 16 in
nine days, which is why this ADR names its commit and asks a reader to
re-run rather than to quote.

**The residue is the aggregate's own encapsulation.** Fourteen of the
22 files rehydrate an aggregate from a database row by writing
unexported fields in a struct literal, which no exported path in this
module can produce. Conforming needs **15 aggregate types holding 82
unexported fields** to each gain an exported way to be restored from a
stored row with its state and its timestamps, plus
`agent.validateOwnership`, `agent.validateOperator` and
`message.newClaimID` exported or moved.

**That change is reserved.**
[ADR-0051](./0051-an-aggregate-exposes-what-it-persists.md) §What this
does not decide says so by name: *"A setter on the aggregate is a
separate question and no evidence gathered here selects an answer to
it."* It added accessors that read, deliberately.
[domains.md](../domains.md) §The shape a domain takes already carries
the consequence with its reason — *"It stays one package because the
aggregates' fields are unexported and no exported constructor restores
a stored row's state or timestamps, so whatever rehydrates one shares
its package"* — as a description of the tree, with nothing recording
that it is a position the project holds.

[README.md](../README.md) rule 7 is the shape this meets: a point the
structural rules leave to the project is settled in an ADR, and the
project has now met this one three times — #638 priced it, #1022
dispatched the move on a file count, and the probe stopped it.

## Decision

### No aggregate gains an exported restore path in order to satisfy a layout rule

The write half of an aggregate's surface is decided on evidence about
that surface — what a caller needs, what the aggregate's invariants
allow — and never as a side effect of where a file sits. ADR-0051's
reservation stands, and it is answered by an ADR that argues the
surface or it is not answered.

### A store beside its aggregate is therefore not a defect

Go decides the rest: fields are unexported, so whatever writes them
shares their package. A `postgres*.go` directly under
`internal/<domain>/` is the shape this decision leaves, and a review
that blocks the next one on §1 is asking for the change above. That is
what #1005 met — a file landed there nine days after #638 and was
approved — and the answer is that it was correct, not that it was
missed.

### What §1 still governs is untouched

An implementation of a dependency interface that does not reach an
aggregate's unexported state is outside all of this. `watch` is the
measured instance: 0 crossings, the only domain the probe compiles, and
the only one where §1 applies without interpretation. Nothing here
weakens the rule for a store over a queue, a mailer, a clock, or any
future dependency whose implementation the compiler will let leave.

### This does not claim the tree conforms, and does not touch the rule

`stack-go.md` §1 is not narrowed, amended or waived here, and this
record has no power to do any of the three. Rule 8 puts a
stack-specific convention file above `structure.md` where the two
decide one case, and [structure.md](../structure.md) §The shape leaves
what goes *inside* a boundary to the stack in its own words. What this
ADR decides is what garam will not do to reach §1's layout — which is
this record's own ground, and reaches the same answer for a reviewer
without claiming an authority the index withholds. Whether §1 should
name a path or a property for a domain's own aggregate store is the
convention owner's question, and it is referred in #1035 rather than
settled here — the same move §Where the composition root lives makes
for §Project envelope in #756, and #1035 is the second instance #756
asked for.

## What this does not decide

- **Whether an aggregate should ever have an exported write path.**
  Still ADR-0051's reserved question, and still reserved. 180 compiler
  errors are a cost, not an argument for a design.
- **Whether `stack-go.md` §1's text should change.** Rule 4 puts a rule
  in `docs/convention/` off this track however much it binds, and
  `AGENTS.md` §Roles puts a convention change with the PM as a proposal
  rather than an act. #1022's option A is neither taken nor refused
  here.
- **Whether `watch` should move on its own.** It is the one domain that
  compiles. Moving it alone would leave the repository conforming in
  one domain of nine, which the plan document already put to the owner.
- **The four judgement calls the plan document answered** — `authn`'s
  four interfaces, and the dual-interface shape `operator` and
  `message` share. They are recorded there against the day the question
  is live, and nothing here adopts them.
- **What a domain owes for state it does not persist.** ADR-0051 left
  derived and in-memory state untouched and so does this.

## Consequences

- **A reviewer meeting the next store file has an answer in the
  record.** That is the whole of what this ADR moves: the position was
  already held, already priced, and reachable only through a merged
  pull request's body.
- **The reopening condition is named.** If the setter question is
  answered — an ADR that gives aggregates an exported restore path on
  evidence about the surface — this boundary is worth re-examining, and
  this ADR is to be read then rather than assumed permanent. It is
  contingent on a reservation, and it lasts exactly as long as the
  reservation does.
- **The repository keeps a §1 clause its own stores do not satisfy.**
  That is the cost this decision accepts and does not hide. A reader of
  §1 alone still meets a rule the tree breaks and learns nothing about
  why; #1035 is what bounds that, and until it is answered the gap is
  real.
- **The measurement drifts, so it is dated rather than stated.**
  `operator` moved from 13 crossings to 16 in nine days without anyone
  intending it. A reader who needs a current figure re-runs the probe
  at their own commit; the numbers here belong to `33ad3e4`.
- **Nothing detects any of this.** `make ci` reads `docs/` for one
  thing, each Markdown file's `## Contents` list against its own `##`
  headings, and no target can tell a store that must share its
  aggregate's package from one that need not. Review holds it, under
  `docs/convention/review.md` §What each check verifies, whose
  Architecture row fails a diff where *"a decision or a domain changed
  without the documents that describe it"* — the same shape ADR-0058,
  ADR-0060 and ADR-0061 each recorded.

## Rejected alternatives

- **Move the tree into `internal/<domain>/store/` (#1022's option C).**
  Dispatched once, on the reading that it was *"a pure relocation: file
  contents unchanged except the `package` line"*. It is not one, and
  the price is the barred change on 15 types at once — chosen by a
  layout rule rather than by evidence about the surface, which is the
  order this ADR exists to refuse.
- **Record the divergence as an exception because the move is large
  (#1022's option B).** The owner rejected it and was right: a deferral
  used as precedent becomes permanent, and the debt was live rather
  than historical — a new store file landed in #1005 nine days after
  #638 and was approved. This decision does not rest on what the move
  costs. It rests on which track owns the change the move requires, and
  it would hold if the move were free.
- **Amend `stack-go.md` §1 to describe the layout garam uses (#1022's
  option A).** It makes the contradiction vanish by manufacturing an
  argument nobody made — that the rule is wrong for every project — on
  the strength of nobody having done the work. ADR-0051 §Rejected
  alternatives already refused writing a one-project answer into
  `docs/convention/`: *"the conventions bind every project and are the
  PM's to change … `AGENTS.md` §What earns a rule also bars drawing a
  rule from one project's evidence, and one project is all this has."*
- **Write the decision as a narrowing of §1's reach — that the rule
  does not govern a domain's own persistence.** This is what #1022's
  ruling asks for, and it is the one form this record cannot take. Rule
  4 hands *"which rule governs where a convention file and a structural
  rule reach one case"* to rule 8, and rule 8 gives it to the
  stack-specific file. `structure.md` §The shape disclaims what goes
  inside a boundary from this side. And the same collision has been met
  once already: `structure.md` §Where the composition root lives
  records a §Project envelope divergence, cites rule 8 as the reason
  the convention file governs, states in terms that it *"records the
  divergence and does not change the rule"*, and refers the rule to the
  convention owner (#756). Deciding what garam will not do reaches the
  same operational answer on ground this record holds.
- **Settle the setter question here, so that the move becomes
  available.** It answers a reserved question inside the pull request
  that wants the layout, which is the exact order ADR-0051 refused when
  it declined to let a removal sweep answer the read question quietly.
  The question deserves its own evidence and its own ADR.
