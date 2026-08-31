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

- **A boundary is the unit.** A boundary is a part of the system with its own identity: a domain, a feature, a bounded context. It owns one folder, named for its subject matter and never for a technical role, and that folder holds everything the boundary needs and nothing another boundary needs.
- **A boundary is reached only through what it declares public.** Everything else inside it is private, whether or not the language enforces it.
- **Reaching past the declared surface is a defect**, even where the language permits it and even where it works.
- **An aggregate exposes what it persists.** A value a boundary's store writes and reads back is reachable through the surface that boundary declares, by a read-only accessor on the aggregate ([ADR-0051](./adr/0051-an-aggregate-exposes-what-it-persists.md)). A persisted value no caller can reach is a defect and not an open point.
- **A boundary reaches a sibling directly, through the surface that sibling declares** ([ADR-0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)). Nothing has to be lifted below both to let one reach the other, and a reference the sibling's surface does not name is the defect above however it is routed.
- **A decision of its own can still refuse one edge.** The permission above is general; where two boundaries are to stay independent of each other, an ADR says so and names them — [ADR-0003](./adr/0003-single-cli-topology.md) does that for the two services it separates.
- **Inside a boundary, declarations come first** and implementations depend on them — never the reverse. A file declaring a contract does not import the file that satisfies it.
- **Across boundaries, imports run from the specific to the general**: a composing layer imports the boundaries it composes, a boundary imports shared code, and neither direction reverses.
- **Composition stacks.** What composes several boundaries can itself be composed by something above it, and that does not make it a boundary reaching upward. The rule fixes the direction, not the number of layers.
- **Shared code sits below every boundary, never beside one.** What is extracted is extracted below the boundaries that need it; it never becomes a sibling folder no boundary owns.
- **A cycle between two boundaries means the split is wrong**, and so does a file that belongs in two of them. Break it by inverting one direction — the side that needs the behaviour declares the contract and the other implements it — or by merging them, or by extracting what both need into a third. What keeps a cycle out is that direction, held when the split is drawn, rather than a reviewer noticing one afterwards ([ADR-0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)).

## Where the composition root lives

garam's composition root is `internal/cli`, the package `cmd/garam/main.go` calls: the concrete types behind the seams the domains declare are constructed there, and `cmd/garam/main.go` constructs nothing itself.

`docs/convention/stack-go.md` §Project envelope decides this case differently. Its first bullet puts that construction in `cmd/<binary>/main.go` and nowhere else, and asks that the file be kept thin. That rule governs — a stack-specific convention file outranks this document ([README.md](./README.md) §Rules, rule 8) — and what garam does not satisfy is the path it names: `cmd/garam/main.go` is thin because the construction is not in it.

**The placement is what the in-process integration tests rest on.** `package main` cannot be imported, which `docs/convention/stack-go.md` §7 states where it makes a test of one internal by necessity, and the same section designates `tests/` for the integration and E2E tests that wire multiple domains. `tests/e2e/issue_certificate_test.go` and `tests/e2e/migrate_test.go` reach the shipped command tree through `cli.NewRootCommand()`. Construction moved into `cmd/garam/main.go` is construction those two cannot reach: what they exercise goes uncovered, or is rebuilt beside them as a second copy of the wiring — and a copy does not fail when the shipped wiring breaks.

**This records the divergence and does not change the rule.** Nothing here amends `stack-go.md`, and nothing here makes garam compliant. Whether that rule should name a property rather than a path is the convention owner's to answer; #756 carries the referral, and the alternative it rejected — moving the construction up into `cmd/garam/main.go` — with what that would cost. Until the rule is answered, `internal/cli` holding the composition root is a position this project recorded and not a defect nobody noticed.

## When a new boundary is earned

A concept earns its own boundary when all three hold:

- It has its own lifecycle, identity and vocabulary.
- Something outside it needs to depend on it.
- It has a surface that can stay stable while its inside changes.

A concern shared by three or more boundaries is extracted. One shared by two stays with the boundary that owns it and is reached through its declared surface — unless a boundary may not reach a sibling at all, in which case it is extracted too. garam's side is the bullet above, so the clause after the dash does not fire here: a concern shared by two is not extracted on that ground.

A conversion to a wire format the boundaries already share is not a shared kernel: a package holding it is depended on for the format, not for each other's behaviour, so it does not couple the boundaries that import it. What the format names — a wire type and the two directions of its conversion — belongs to that package; anything that would choose a behaviour rather than convert one does not.

## Decisions this rests on

- [ADR-0047](./adr/0047-a-domain-reaches-a-sibling-through-its-declared-surface.md) — domain partitioning is the shape garam takes, and a boundary reaches a sibling through the surface that sibling declares. That last point is the one this shape leaves contested and every project answers for itself; the ADR records which side garam took, what the other side produced here while it went unrecorded, and why the answer decides what counts as a defect rather than a preference.
- [ADR-0003](./adr/0003-single-cli-topology.md) — the two services stay independent of each other, which is the named exception the reach above allows for.
- [ADR-0051](./adr/0051-an-aggregate-exposes-what-it-persists.md) — an aggregate exposes what it persists. The shape above says a boundary is reached only through what it declares public and leaves to the project what must be in that surface; the ADR records which side garam took, the three aggregates that gave two answers while it went unrecorded, and why the answer decides what counts as a defect.

## Open questions

- **What owns a concern once it is extracted below the boundaries.** The threshold above says when a concern leaves the boundary that held it. Nothing here says what owns it afterwards, or what admits the next one beside it, and no decision recorded above settles either. Until one does, this binds nothing.
