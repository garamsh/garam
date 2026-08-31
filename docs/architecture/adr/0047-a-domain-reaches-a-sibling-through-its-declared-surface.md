# ADR 0047: garam is partitioned by domain, and a domain reaches a sibling through the surface that sibling declares

> Status: accepted
> Date: 2026-08-26

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives
- Errata

## Context

garam is partitioned by domain and no decision records it.
[domains.md](../domains.md) describes the partitioning that exists and
[overview.md](../overview.md) §Module boundaries lists it folder by
folder; the rules it imposes are in `docs/convention/arch-domain.md`,
selected through `docs/convention/README.md` §Architecture.

Those rules leave one point conditional:

> A concern shared by three or more boundaries is extracted. One shared
> by two stays with the boundary that owns it and is reached through its
> declared surface — unless a boundary may not reach a sibling at all,
> in which case it is extracted too.

Whether a boundary may reach a sibling is the point the shape leaves to
the project rather than deciding for it. The conventions template's
`structures/domain.md` says so where it describes this shape: the
relations run "from Shared Kernel, where two share code, to Separate
Ways, where neither may reference the other. A project takes one,
records which, and a reference the recorded choice does not permit is a
defect." garam never recorded which.

It was taken anyway, in the tree, and the record of it is two packages.

- **`internal/listfilter`** holds an offset and a limit. Its package
  comment gives the reason it exists: "a domain may not import a
  sibling to borrow the type, so it lives here once." That is the
  Separate Ways branch stated outright, and
  [domains.md](../domains.md) restates it. Three domains page their
  lists with the type — `org`, `agent` and `user` — so the threshold
  above accounts for where it sits; the reason recorded beside it does
  not come from the threshold.
- **`internal/keyfile`** holds the one rule key material is read under
  ([ADR-0015](./0015-operator-identity.md)). `internal/ca` reads it, and
  so does the `internal/cli` wiring that opens the machine listener's
  pair and the adapter's; `internal/machine` reaches it from a test
  alone. That is one domain and a composition root — fewer than the
  three boundaries the threshold names — and the concern was extracted
  rather than left with an owner to be reached through. Its own comment
  gives "the rule is neither certificates nor listeners" as the reason,
  which is a claim that no boundary owns it rather than a claim that no
  boundary may reach one.

Neither package is expensive on its own. What they show is a branch
deciding cases while unrecorded, which is the state the template calls
a defect either way: whichever side a reference falls on, nothing here
said which side that was.

The branch is also not the one this project's stack convention assumes.
`stack-go.md` §2 tells a consumer to import the producer's interface
rather than its concrete type, and its example is `order` importing
`billing.Charger` — one domain reaching another.

## Decision

### garam is partitioned by domain

One folder per domain under `internal/`, named for the subject matter it
owns and never for a technical role. What composes several domains is
not itself a domain, and composing several of them does not make it
one.

The rules this shape imposes are in [structure.md](../structure.md),
added by the pull request that adds this ADR. Rule 6 puts them in a
responsibility document and rule 2 is why they are not here: an ADR's
body is frozen once merged, so a rule kept here could not have its
wording corrected without superseding the choice of shape itself.

### A domain reaches a sibling through the surface that sibling declares

Directly. No intermediary is owed, and nothing has to be lifted below
two domains for one of them to reach the other. What stays forbidden is
what was forbidden before: reaching past the declared surface is a
defect, and reaching a sibling's private inside through a third package
that re-exports it is that same defect wearing a different import path.

Of the two ends the template names, this is the far side from Separate
Ways. The question a reviewer asks about a reference is whether the
sibling declares what is reached — not whether the reference is allowed
to exist.

### A cycle between domains is prevented by the split, not caught in review

A cycle means the split is wrong, and it is broken by inverting one
direction, by merging the two, or by extracting what both need into a
third. The dependency direction is what keeps one from forming, and
where the toolchain refuses an import cycle outright — Go's does, at
compile time, so `make build` is where a cycle between two of these
folders stops — nothing further is needed. `stack-go.md` §2 records
what that does not reach: a `type → struct → type` cycle inside one
package is found by reading, and it is not a cycle between boundaries.

Permitting a direct sibling reference is what makes this half
load-bearing. A project that forbids the reference has fewer edges that
could close a cycle; one that permits it holds the direction instead.

## What this does not decide

- **What a domain does when it needs a service for itself.** It
  declares the interface it needs and the implementation lives with the
  declaration; `stack-go.md` §1 and §2 carry that pattern for this
  stack, and nothing here restates or changes it.
- **Where anything currently lives.** No folder moves, is renamed or is
  split by this ADR, and nothing under `internal/` changes in the pull
  request carrying it. What changes is what a reference is judged
  against.
- **What owns a concern once it is extracted below the domains.** The
  threshold says when a concern leaves the domain that held it and not
  what owns it afterwards. [structure.md](../structure.md) §Open
  questions holds that, where it binds nothing until garam meets it.

## Consequences

- **The sibling rule stops being conditional.**
  [structure.md](../structure.md) states garam's side, so the clause
  that extracts a two-boundary concern because no sibling may be
  reached does not fire here. A concern shared by exactly two
  boundaries stays with the boundary that owns it, and extracting one
  anyway is a choice to be argued rather than a rule to be cited.
- **`internal/listfilter`'s recorded reason no longer holds.** The
  prohibition its comment names is not garam's rule. The type does not
  move — three domains page with it and the threshold puts it below
  them — but the sentence is now wrong in the package comment and in
  [domains.md](../domains.md)'s entry for it.
- **A review has one question about a reference between domains**:
  does the sibling declare what is being reached? A review asking
  whether a domain may reach a sibling at all is arguing a settled
  point.
- **`docs/convention/arch-domain.md` still carries these rules too.**
  Convention files are the PM's (AGENTS.md §Roles) and the pull request
  carrying this ADR does not change them, so until that file is retired
  the shape's rules are stated in two places. Rule 6 names
  [structure.md](../structure.md) as the one that holds them and rule 8
  ranks it against the convention tiers.

## Rejected alternatives

- **Separate Ways: a domain may not reference a sibling at all.** This
  is the branch the tree took twice without recording it, and the two
  packages above are what it produced. Each is small; the pattern is
  not. Under it every type two domains share is a candidate for a
  folder below them, the shared area grows with no domain owning it,
  and the count that the threshold makes an argument is never argued
  because the prohibition has already answered it. It also contradicts
  what `stack-go.md` §2 tells a consumer to do, so a project holding
  both would have a rule its own stack convention breaches by example.
- **Leave the rule conditional and settle each case as it arrives.**
  The state this ADR ends. A conditional rule turns every case into an
  argument about which branch garam is on, and the record shows that
  argument being settled quietly, in the branch nobody wrote down.
- **Record the side here and keep the structural rules here too.** Rule
  6 refuses it and rule 2 is the reason: correcting a rule's wording
  would cost a superseding ADR about the shape of the system.
- **Permit the reference, but only through an interface the consumer
  declares.** Inversion at every edge. Where the producer already
  declares a surface, a second declaration on the consumer's side is
  another site to keep in sync and hides which domain is actually
  reached. Inversion is the remedy for a direction that must be
  reversed — a cycle, or a domain needing a service of its own — which
  `stack-go.md` §1 and §2 already cover.
- **Permit the reference and extract every concern two domains share
  anyway.** It keeps the reach legal and still grows the shared area
  below the domains: the cost of Separate Ways without the consistency
  that at least makes that branch coherent. The threshold at three is
  what stops it.

## Errata

- 2026-08-31 — §Context's "the rules it imposes are in
  `docs/convention/arch-domain.md`, selected through
  `docs/convention/README.md` §Architecture" and §Consequences'
  "**`docs/convention/arch-domain.md` still carries these rules
  too.** … so until that file is retired the shape's rules are stated
  in two places" were true when written and are false since `5668576`
  (#616), which retired that file and removed §Architecture from the
  convention index nine minutes after `bfcc1f4` (#612) merged this
  ADR. The §Consequences bullet is the one that misleads rather than
  dangles: its condition has been met, so the shape's rules stand in
  one place and not two. That place is
  [structure.md](../structure.md) — §Boundaries and dependency
  direction carries the sibling rule and the dependency direction,
  and §When a new boundary is earned carries the threshold §Context
  quotes. The decision is untouched: a domain still reaches a sibling
  through the surface that sibling declares.
