# ADR 0096: Garam syncs the conventions template to `f7de866`, and keeps two local positions it states

> Status: accepted
> Date: 2026-10-09

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives

## Context

`Makefile`'s `TEMPLATE_SYNCED` named
`garamsh/convention-driven-project@6fbfd5d`. Between that commit and
`f7de866`, the template's `main` on 2026-10-09, there are 44 commits.
[ADR-0094](./0094-structural-rules-yield-only-to-what-a-framework-imposes.md)
took seven of them and left `TEMPLATE_SYNCED` where it was, because
the rest were unread. The divergence grew with every template merge, so
#1235 syncs the whole range now rather than waiting for the template
owner's decision on #415.

[README.md](../README.md) rule 4 and
[ADR-0061](./0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md)
make the adoption the unit an ADR records. Most of the range lands in
`docs/convention/`, which earns no ADR on its own. Three parts of it do
not:

- **`adr/0000-template.md` gains §Signs the decision was wrong**
  (`b55aebc`, template #341). That changes the blank form kept under
  `docs/architecture/adr/`, which is a change to `docs/architecture/`
  itself.
- **`stack-go.md` §Project envelope and §7 moved** (`e0ffe77`, #381,
  and `f7de866`, #414). [structure.md](../structure.md) §Where the
  composition root lives cited §7's sentence that `package main` cannot
  be imported, and `e0ffe77` deletes that sentence.
- **Two template changes met Garam decisions.** They went to the PM on
  #1235 before anything was written.

## Decision

**garam takes every commit in `6fbfd5d..f7de866` that lands on a file
it keeps.** The exceptions are the six below. The pull request that
adds this ADR carries the per-commit table. Each row names the template
commit and what happened to it. The rules stand in the files they
landed in, and this ADR does not restate them.

**Four exceptions follow from what Garam already holds.**

- **`a6a2355` (#323) is declined.** It adds the template's skeleton
  `Makefile`, `ci.yml`, `dependabot.yml` and README text. Garam's own
  files already carry `make lint` and `make ci`, a `ci.yml` that runs
  `make ci`, and a Dependabot entry for `github-actions`. Its pull
  request template and `check-contents-list.sh` halves were already
  held here.
- **`70b7359` (#336) is declined.** `stack-nextjs.md` §12 already
  states the identity and presence form of a text assertion for each
  tool in prose. The template's table would be a second site for the
  same rule.
- **The "Concrete naming forms" half of `f159947` (#397) is
  declined.** It rewords a `style.md` bullet that sends the reader to
  the stack file. Garam removed that bullet in #979, because a
  convention file does not send the reader to another. The commit's
  new "Shared concept, named file" bullet is taken.
- **`f7de866` (#414) is placed differently.** Its paragraph goes at the
  end of `testing.md` §Three layers, after Garam's own paragraph on the
  Postgres integration layer (#900). The template places it after the
  testcontainers exception. Garam's paragraph applies that rule and its
  exception, so it stays next to them.

**Two local positions are kept and stated, on the PM's decision on
#1235.** These are the remaining two exceptions.

- **`stack-go.md` §1 names an implementation package for its
  technology.** The template moved to `internal/<domain>/<pkg>/`
  (`0440057`, `37be164`, `f2e846a`, `61e4ada`, `e6f0c55`), which admits
  either a technology name or an interface name. Garam takes the
  structure and the new prerequisite: the implementation leaves the
  domain package only where a neighbouring package can construct the
  aggregate. Garam keeps `<technology>/` as the name. That narrows the
  template's rule and does not contradict it. The PM decided that name
  on #1087, and #1089 wrote it. So [domains.md](../domains.md) and
  [structure.md](../structure.md), which say `postgres/` is what §1
  requires, stay true.
- **The checked-against headers of `stack-go.md`, `stack-nextjs.md`
  and `stack-tailwind.md` stay as they were.** `b943308` (#370) and
  `6d1c4f4` (#398) name the exact releases the template checked. Garam
  has adapted claims in all three files, and nobody checked those
  claims at those releases. Garam also runs go 1.26.7, next 16.3.1 and
  Playwright 1.62.1. `c462302` (#402), taken in the same sync, says a
  project receiving such a statement over claims it changed re-checks
  them or drops the statement. Taking the headers would assert a check
  nobody made here. `stack-container.md` is unadapted, so its header is
  taken verbatim.

**`TEMPLATE_SYNCED` moves to `f7de866`.** Every commit before it is
taken, declined with a reason, or lands on a file Garam does not keep.

## What this does not decide

- **Template #415.** It is still open with the template owner. When it
  is decided, it gets its own sync and its own ADR.
- **The re-check of the three stack files at Garam's own releases.**
  That is #1239. Until it lands, the current headers ("Go 1.21 and
  mockery 3", "Next.js 16", "Tailwind CSS 4") name no checkable
  release, which `documentation.md` §Keep it current asks for.
- **Whether an ADR is bound to carry §Signs the decision was wrong.**
  That is a rule about how the record is written. It belongs in the
  index's §Rules, and the PM will raise it separately.

## Consequences

- **The blank ADR form now carries §Signs the decision was wrong, and
  this ADR uses it.** The section is written while the rejected
  alternatives are still in view. No ADR before this one has it.
- **`stack-go.md` §1 no longer reaches a domain whose unexported fields
  close its aggregate.** It does not change which of Garam's domains
  are defective. [structure.md](../structure.md) §Boundaries and
  dependency direction already requires a persisted value to be an
  exported field
  ([ADR-0069](./0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md)).
  So the eight domains #1083 has yet to convert remain measured against
  that rule, and §1 reaches each of them once it converts.
- **What a boundary may reference across boundaries, and what a unit
  is named for, are no longer stated in `stack-go.md` or
  `stack-nextjs.md`** (`ef3a784`, `7f58495`). Garam's
  [structure.md](../structure.md) already holds both: a boundary is
  named for its subject matter and is reached through its declared
  surface. So nothing a contributor relied on is lost.
- **This ADR pairs with [structure.md](../structure.md) and with the
  index.** The first cited a sentence this sync removed, and now cites
  the section that holds it. The second lists this ADR.

## Signs the decision was wrong

- A later template commit lands on a passage this sync adapted, and the
  author syncing it cannot tell Garam's position from the template's
  without opening this ADR. If that happens, the two local positions
  belong in the files themselves rather than here.
- The three declined headers are still unchanged at the next sync,
  because the follow-up never landed. That would mean declining was a
  way of not checking rather than a refusal to claim a check.
- A pull request cites `stack-go.md` §1 to put an implementation
  package under an interface name, and the review cannot say which
  rule refuses it.

## Rejected alternatives

- **Taking the template's `<pkg>/` verbatim.** It would undo #1089 in a
  sync rather than in a decision about naming, and it would make
  [domains.md](../domains.md) false without anyone deciding that.
- **Taking the headers and re-checking every adapted claim in this pull
  request.** That is a measurement of three files against three
  toolchains, and it is not a sync. It would hold up the range it
  exists to close.
- **Waiting for #415 before syncing.** Every template merge meanwhile
  widens the range a later sync has to read. #415 is one commit, and it
  can be taken on its own.
