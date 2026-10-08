# Structure

The shape garam is partitioned into, what may reference what, and when a new boundary is earned.

## Contents
- The shape
- Boundaries and dependency direction
- Where the composition root lives
- When a new boundary is earned
- Decisions this rests on
- Open questions

## The shape

garam is divided by subject matter — one folder per domain, feature or bounded context. What composes several boundaries is not itself one of them.

The division is what *Fundamentals of Software Architecture* (Richards and Ford, 2020) calls domain partitioning, against the technical partitioning a layered system uses. *Domain-Driven Design* (Evans, 2003) names the same boundary Bounded Context.

Nothing here states what goes *inside* a boundary, or how the system is deployed. The first divides by stack, not by architecture, and a boundary owns whichever parts it needs; the second is a separate decision, and the same partitioning serves one deployable or many.

## Boundaries and dependency direction

- **A boundary is the unit.** A boundary is a part of the system with
  its own identity: a domain, a feature, a bounded context. It owns one
  folder, named for its subject matter and never for a technical role,
  and that folder holds everything the boundary needs and nothing
  another boundary needs.
- **A boundary is reached only through what it declares public.**
  Everything else inside it is private, whether or not the language
  enforces it.
- **Reaching past the declared surface is a defect**, even where the
  language permits it and even where it works.
- **An aggregate exposes what it persists, and the field itself is the
  exposure.** A value a boundary's store writes and reads back is
  reachable through the surface that boundary declares, as an exported
  field on the aggregate. A persisted value no caller can reach is a
  defect and not an open point. **The write half is open because
  nothing stood behind it**: no method on any of these aggregates
  assigns a field, so there is no invariant for an accessor to protect,
  and whatever restores a stored row does it from outside the package
  ([ADR-0069](./adr/0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md)). That ADR supersedes ADR-0051's accessor and ends
  ADR-0066's reservation; an aggregate that does hold an invariant over
  its fields is not decided by it.
- **Two domains of ten have converted.** `internal/user` and
  `internal/ca` carry exported fields on what their stores persist, and
  each store sits in `internal/<domain>/postgres/`. The other eight
  keep their accessors and keep their store beside the aggregate until
  each follows in its own pull request (#1083), so the tree reads in
  two shapes meanwhile and the rule above is what both are measured
  against.
- **A boundary reaches a sibling directly, through the surface that
  sibling declares**
  ([ADR-0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)).
  Nothing has to be lifted below both to let one reach the other, and a
  reference the sibling's surface does not name is the defect above
  however it is routed.
- **A decision of its own can still refuse one edge.** The permission
  above is general; where two boundaries are to stay independent of each
  other, an ADR says so and names them —
  [ADR-0003](./adr/0003-single-cli-topology.md) does that for the two
  services it separates.
- **Boundaries cooperate through `grn.GRN`, never through each other's
  concrete types.** Every boundary, and not only the ones that are
  domains
  ([ADR-0054](./adr/0054-the-concrete-types-rule-binds-every-boundary-not-only-a-domain.md)).
  A composing layer is outside it (§The shape) and holds the concrete
  types of what it composes.
- **Inside a boundary, declarations come first** and implementations
  depend on them — never the reverse. A file declaring a contract does
  not import the file that satisfies it.
- **Across boundaries, imports run from the specific to the general**: a
  composing layer imports the boundaries it composes, a boundary imports
  shared code, and neither direction reverses.
- **Composition stacks.** What composes several boundaries can itself be
  composed by something above it, and that does not make it a boundary
  reaching upward. The rule fixes the direction, not the number of
  layers.
- **Shared code sits below every boundary, never beside one.** What is
  extracted is extracted below the boundaries that need it; it never
  becomes a sibling folder no boundary owns.
- **A cycle between two boundaries means the split is wrong**, and so
  does a file that belongs in two of them. Break it by inverting one
  direction — the side that needs the behaviour declares the contract
  and the other implements it — or by merging them, or by extracting
  what both need into a third. What keeps a cycle out is that direction,
  held when the split is drawn, rather than a reviewer noticing one
  afterwards
  ([ADR-0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)).

## Where the composition root lives

garam's composition root is `internal/cli`, the package `cmd/garam/main.go` calls: the concrete types behind the seams the domains declare are constructed there, and `cmd/garam/main.go` constructs nothing itself.

This is the composition site `docs/convention/stack-go.md` §Project
envelope provides for a module whose tests have to reach it. That
section puts the site in `cmd/<binary>/main.go` unless a test outside
`cmd/<binary>/` has to reach it, and puts it in a package `main.go`
calls when one does. The
paragraph below names the tests that reach it, so the two documents
decide this case the same way
([ADR-0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)).

**The environment is read in the composition root and nowhere else.** Every name the module reads is declared in one block in `internal/cli/cli.go`, and the `envOr` beside it holds the only environment read under `internal/` and `cmd/`, so what the source reads is enumerable from those declarations rather than from the spelling of the calls that read them. A second read there is the defect this states, and `make chart-render` fails on one rather than leaving it to a reviewer: it counts the reads under those two trees and expects the one, because a name read outside the block is one it cannot enumerate against the chart. The suites under `tests/` read `GARAM_TEST_DATABASE_URL` for themselves and are outside both the rule and the count.

**The placement is what the in-process integration tests rest on.** Only
a test in its own directory can import a main package, which
`docs/convention/stack-go.md` §Project envelope states where it moves
the site out of `main.go`, and §7 of that file designates `tests/` for
the integration and E2E tests that wire multiple domains
([ADR-0096](./adr/0096-garam-syncs-the-convention-template-to-f7de866.md)).
`tests/integration/issue_certificate_test.go` and
`tests/integration/migrate_test.go` reach the shipped command tree
through `cli.NewRootCommand()`. Construction moved into
`cmd/garam/main.go` is construction those two cannot reach: what they
exercise goes uncovered, or is rebuilt beside them as a second copy of
the wiring — and a copy does not fail when the shipped wiring breaks.

**This section once recorded a divergence, and that record has ended.**
`stack-go.md` used to put construction in `cmd/<binary>/main.go` and
nowhere else, and this section recorded `internal/cli` as diverging from
it. #756 put the question to the template, which answered it in
`garamsh/convention-driven-project@db32912` (#337). ADR-0094 adopts that
answer, so the divergence record ends and no question is left open.

## When a new boundary is earned

A concept earns its own boundary when all three hold:

- It has its own lifecycle, identity and vocabulary.
- Something outside it needs to depend on it.
- It has a surface that can stay stable while its inside changes.

A concern shared by three or more boundaries is extracted. One shared by two stays with the boundary that owns it and is reached through its declared surface — unless a boundary may not reach a sibling at all, in which case it is extracted too. garam's side is the bullet above, so the clause after the dash does not fire here: a concern shared by two is not extracted on that ground. The boundary that owns a concern is the one whose subject matter it is, and not the one the concern was extracted from ([ADR-0052](./adr/0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md)); where no boundary's subject it is, the destination names nothing and the concern stays below them.

A conversion to a wire format the boundaries already share is not a shared kernel: a package holding it is depended on for the format, not for each other's behaviour, so it does not couple the boundaries that import it. What the format names — a wire type and the two directions of its conversion — belongs to that package; anything that would choose a behaviour rather than convert one does not.

## Decisions this rests on

- [ADR-0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)
  — domain partitioning is the shape garam takes, and a boundary reaches
  a sibling through the surface that sibling declares. That last point
  is the one this shape leaves contested and every project answers for
  itself; the ADR records which side garam took, what the other side
  produced here while it went unrecorded, and why the answer decides
  what counts as a defect rather than a preference.
- [ADR-0003](./adr/0003-single-cli-topology.md) — the two services stay
  independent of each other, which is the named exception the reach
  above allows for.
- [ADR-0051](./adr/0051-an-aggregate-exposes-what-it-persists.md) — an
  aggregate exposes what it persists. The shape above says a boundary is
  reached only through what it declares public and leaves to the project
  what must be in that surface; the ADR records which side garam took,
  the three aggregates that gave two answers while it went unrecorded,
  and why the answer decides what counts as a defect. Its first two
  decisions are superseded by ADR-0069 — the accessor it chose is now
  the field — and its third, that the `policies` field and column stay
  where they are, still stands.
- [ADR-0066](./adr/0066-no-aggregate-gains-an-exported-restore-path-to-satisfy-a-layout-rule.md)
  — no aggregate gains an exported restore path in order to satisfy a
  layout rule. Superseded by ADR-0069, on the condition ADR-0066 wrote
  for itself: it was contingent on ADR-0051's reservation and lasted as
  long as that reservation did. Read it for what conforming to
  `docs/convention/stack-go.md` §1 would have cost while the fields
  were closed, and for why a layout rule is still not a reason to open
  one.
- [ADR-0069](./adr/0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md) — the aggregates are data carriers, so the driver
  leaves the domain package. It answers the write half on evidence
  about the surface rather than about the layout: 122 one-line getters
  across the ten domain packages and no method that assigns a field.
  The store moves to `internal/<domain>/postgres/`, named for the
  technology it implements against, which is what
  `docs/convention/stack-go.md` §1 requires.
- [ADR-0052](./adr/0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md)
  — a concern is owned by the boundary whose subject matter it is. The
  threshold above names a destination for a concern shared by two and
  leaves what owning is to the project; the ADR records which reading
  garam took, the package that made the two readings disagree, and what
  the other reading would have cost.
- [ADR-0054](./adr/0054-the-concrete-types-rule-binds-every-boundary-not-only-a-domain.md)
  — the concrete-types rule binds every boundary and not only a domain.
  Every other bullet in §Boundaries and dependency direction is stated
  over boundaries and that one was stated over domains; the ADR records
  what the tree does under both readings, why widening forbids nothing
  it does, and why what composes several boundaries stays outside the
  rule.
- [ADR-0094](./adr/0094-structural-rules-yield-only-to-what-a-framework-imposes.md)
  — this document's rules yield to a stack file only where the stack's
  framework imposes the case. The ADR records the adoption of that rule
  from the conventions template, which moves `web/`: a feature there now
  reaches a sibling through its declared surface. It also records the
  adoption of the template's composition-site rule, which ends
  §Where the composition root lives' divergence from
  `docs/convention/stack-go.md`.

## Open questions

- **What owns a concern once it is extracted below the boundaries.** The
  threshold above says when a concern leaves the boundary that held it.
  Nothing here says what owns it afterwards, or what admits the next one
  beside it, and no decision recorded above settles either. Until one
  does, this binds nothing.
