# ADR 0054: The concrete-types rule binds every boundary, not only a domain

> Status: accepted
> Date: 2026-09-01

Every measurement below was taken at `e1e69fd`. The commands are in
`#825` and in the pull request carrying this ADR; re-run them there
rather than reading a count here as current.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives
- Errata

## Context

[structure.md](../structure.md) §Boundaries and dependency direction
carries this rule, which `#824` moved there verbatim under rule 6:

> Domains cooperate through `grn.GRN`, never through each other's
> concrete types.

**Every other bullet in that section is stated over boundaries.** This
one is stated over domains, and garam has boundaries that are not
domains: `internal/watch` and `internal/delivery`.
[domains.md](../domains.md)
§Sub-responsibilities in a boundary of their own holds each in a
boundary of its own and says of the pair, outright,
"Neither is a domain". So whether the rule binds those two is not
decided by its text. It was not decided by its text in `domains.md`
either, and the move carried the ambiguity rather than creating it.

**The categories the question turns on are already defined.** A boundary
is "a domain, a feature, a bounded context"
(§Boundaries and dependency direction). What composes several boundaries
is not itself one of them (§The shape), which is what
`domains.md` §Delivery surfaces cites to say `internal/api`,
`internal/machineapi` and `internal/cli` are not boundaries. The tree
therefore holds three kinds of folder under `internal/` — boundaries,
what composes them, and shared code below them — and the rule as written
names only the domains among the first.

### Widening to every boundary forbids nothing the tree does

Four passes over `internal/`, each with a control.

- **No domain imports another.** A pass over all nine domains' non-test
  imports reports zero domain-to-domain edges, which is the property
  [ADR-0052](./0052-a-concern-is-owned-by-the-boundary-whose-subject-matter-it-is.md)
  §Consequences already records.
- **Neither boundary that is not a domain imports one.**
  `internal/watch` imports `errorx`, `grn`, `pgxerr` and its own
  generated `db` subpackage. `internal/delivery` imports `errorx`,
  `grn` and the models generated from `api/machine.yaml`. Both address
  a domain's subject by GRN and hold none of its types.
- **Nothing but a composing layer imports either.** `internal/watch` is
  imported by `internal/api`, `internal/machineapi` and `internal/cli`;
  `internal/delivery` by `internal/cli` alone. No domain imports
  either, and no test import does.
- **The control is the same query over the composing layers.** It
  reaches six domains from `internal/api` and four from
  `internal/machineapi`. A zero above is a measurement and not a query
  that finds nothing.

So the two boundaries that are not domains already cooperate the way
the rule asks of a domain.

### The premise that widening would break the tree does not hold

`#825` raised the rule on the ground that restating it over all
boundaries "would forbid something the tree does deliberately" —
`internal/api` reaching six domains' concrete types. It would not.
§The shape puts what composes several boundaries outside the word
*boundary*, so a rule stated over boundaries has never reached the
delivery surfaces, and widening this one does not start.

### Widening binds rather than describes

A rule that permits everything a tree does can be describing it rather
than binding it. This one forbids an edge nobody has drawn: a watch
taking `agent.Agent`, whose subject it is about, or handing a domain
one of its own types. That edge is the one the boundary exists to keep
out — `domains.md` §Sub-responsibilities in a boundary of their own
makes the watch its own boundary because "a watch has a lifecycle
neither the agents domain nor the messaging one contains", and it stays
separable only while it addresses the agent by GRN. The same holds for
the delivery loop, which runs outside garam as a client of the machine
listener.

## Decision

### The rule binds every boundary

Boundaries cooperate through `grn.GRN`, never through each other's
concrete types — every boundary, and not only the nine that are
domains. `internal/watch` and `internal/delivery` are bound by it as a
domain is, in both directions: neither takes a domain's concrete type,
and no domain takes one of theirs.

The rule is stated over the same unit as every other bullet in its
section. What made the question askable was that one bullet used a
narrower word than its neighbours, and nothing recorded whether the
narrowness was meant.

### What composes several boundaries stays outside it

A delivery surface composes domains, which is what stops it being a
boundary (§The shape), and composing them means holding their concrete
types. That is not an exception to the rule; it is what the rule's
unit already excludes, and the wording now says so rather than leaving
a reader to derive it from §The shape.

## What this does not decide

- **Whether `internal/watch` or `internal/delivery` should be
  domains.** [domains.md](../domains.md)
  §Sub-responsibilities in a boundary of their own says they are not,
  with a reason recorded for each, and nothing here reopens it.
- **Where the rule lives.** `#824` settled that under rule 6 and this
  changes only what the rule says.
- **What a composing layer may do with what it composes.** §The shape
  and `domains.md` §Delivery surfaces hold that, and the second half of
  the decision above only names the exclusion rather than widening it.
- **Anything about shared code.** `errorx`, `grn`, `listfilter`,
  `pgxerr`, `wire`, `keyfile` and `health` sit below every boundary
  rather than beside one, so they are not what a boundary cooperates
  *with*, and every domain holding `errorx.Error` or `listfilter.Filter`
  today is untouched.

## Consequences

- **A reviewer reads the rule's unit off the rule.** `#825`'s question
  — does this bind `internal/watch`? — is answered in the bullet
  instead of by reading §The shape and
  `domains.md` §Sub-responsibilities in a boundary of their own against
  each other.
- **The next boundary that is not a domain is bound on arrival.**
  garam has met this twice; a rule listing the two by name would have
  to be edited a third time.
- **ADR-0052's reasoning is intact.** It cites the rule to price the
  provenance reading of "owns" — folding `internal/listfilter` into
  `internal/org` would create the tree's first domain-to-domain edge
  and put a concrete type across it. That case is domain-to-domain and
  reads identically under the wider rule, which strictly adds cases and
  removes none. No supersession question arises.
- **Zero concrete-type edges between boundaries is now the property the
  tree holds**, where zero domain-to-domain imports was. Both are true
  today; the second is what a pull request now has to argue against.

## Rejected alternatives

- **Leave it stated over domains.** The measurement says the two
  boundaries that are not domains behave as though bound, so nothing
  breaks tomorrow. What it costs is the question itself: the next
  author reads a rule whose unit differs from every neighbour's for no
  recorded reason, and infers the answer or does not. `#825` is that
  cost already paid once.
- **Restate it over every folder under `internal/`.** This is the
  reading `#825` warned against, and it is the one that would forbid
  what the delivery surfaces do deliberately. It also contradicts
  §The shape, which is where the word *boundary* is defined.
- **Name `internal/watch` and `internal/delivery` in the rule.** An
  enumeration decides today's tree and leaves the next boundary to be
  argued from scratch, which is the state
  [ADR-0047](./0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)
  §Rejected alternatives calls "a conditional rule turns every case
  into an argument about which branch garam is on".
- **Widen it and carve out the delivery surfaces by name.** The carve-
  out would restate §The shape at a second site, which
  `docs/convention/README.md` §Independence forbids inside a file as
  much as across files. The clause kept here points at §The shape
  rather than repeating what it decides.

## Errata

- 2026-09-01 — Three statements here were wrong when written. The
  author's own review found all three after the pull request carrying
  this ADR had merged, which is what put them under this heading rather
  than in the body. **§Context's "Four passes over `internal/`, each
  with a control"** miscounts its own evidence: what follows is three
  passes and one control, not four passes each controlled. The
  measurements themselves are unaffected. **§Consequences' "Both are
  true today; the second is what a pull request now has to argue
  against"** names the narrower property where it means the wider one —
  what a pull request must argue against is zero concrete-type edges
  between boundaries, not the zero domain-to-domain imports that
  property supersedes. **§Rejected alternatives' "The clause kept here
  points at §The shape rather than repeating what it decides"** was
  false of the clause it described: the rule as this ADR's pull request
  merged it read "What composes several boundaries is not one of them
  (§The shape)" against §The shape's own "What composes several
  boundaries is not itself one of them". The rule has since been
  reworded to "A composing layer is outside it (§The shape) and holds
  the concrete types of what it composes", which cites §The shape for
  the reason and states the consequence without repeating the
  definition — so the wording that sentence describes is achievable,
  and it is what [structure.md](../structure.md) now carries. The
  decision stands unchanged: what the rule binds, and what it excludes,
  is what §Decision says.
