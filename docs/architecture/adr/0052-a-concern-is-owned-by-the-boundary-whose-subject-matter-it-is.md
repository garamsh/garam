# ADR 0052: A concern is owned by the boundary whose subject matter it is, not by the one it was extracted from

> Status: accepted
> Date: 2026-09-01

Every measurement below was taken at `18126b3`. The commands are in
`#608` and in the pull request carrying this ADR; re-run them there
rather than reading a count here as current.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[structure.md](../structure.md) §When a new boundary is earned sets a
threshold and a destination:

> A concern shared by three or more boundaries is extracted. One
> shared by two stays with the boundary that owns it and is reached
> through its declared surface — …

[ADR-0047](./0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)
retired the prohibition the clause after that sentence turns on, so
the destination is the operative half. It names an owner and does not
say what owning is.

`internal/listfilter` is where the omission has to be paid for. `#786`
settled its count at two — `org` and `agent`, with `internal/api` a
delivery surface and not counted — which is under the threshold, while
[domains.md](../domains.md) recorded it as earning its place. The row
contradicted the rule in its own middle column, and correcting it
requires knowing where a two-boundary concern goes.

**Two readings of "owns" answer that differently here.**

- **Provenance.** `111bdd5` (`#402`) created the package by extracting
  the type out of `internal/org`, where it was `org.ListFilter`, when
  the agents domain gained pagination. The reason it recorded is the
  prohibition ADR-0047 retired. Read this way `org` owns the concern
  and `agent` reaches it there, which is what the destination
  prescribes.
- **Subject matter.** Paging is a property of every list call. It is
  not what the organizations domain is about, and `org` holds it only
  because it was the first domain to page. Read this way no domain
  owns the concern and there is nothing to return it to.

The two had never had to disagree. ADR-0047 §Context already separates
the claims the two extracted packages left behind — `internal/keyfile`
gives "the rule is neither certificates nor listeners", "which is a
claim that no boundary owns it rather than a claim that no boundary
may reach one", while `internal/listfilter` made the second claim.
Retiring the prohibition removes listfilter's stated reason and leaves
only where it came from, which is exactly the reading in question.

**What the provenance reading would cost here is measurable.**

- Folding puts `org.ListFilter` in `agent`'s store signature. No
  domain imports another today: a pass over all nine domains' non-test
  imports reports zero domain-to-domain edges, against a control of
  `internal/api`, which reaches six of them. The fold would create the
  first.
- [domains.md](../domains.md) §What a domain is here states the rule
  that edge would break: domains cooperate through `grn.GRN`, "never
  through each other's concrete types". `Filter` is a concrete type.
- `90a3898` (`#736`) added `Parse`, which refuses a page the `integer`
  paging columns would truncate rather than reject. It is the check
  the paging values cross into the system through, and only
  `internal/api`'s four list handlers call it. Folding makes the
  organizations domain the validator for the agents listing's paging.
  `docs/convention/runtime-safety.md` §Trust boundaries puts the check
  where the value crosses; organizations is not that place.

## Decision

### Owning a concern is holding its subject matter

The boundary that owns a concern is the one whose subject the concern
is. Where a concern was extracted from is where it was born, and that
is a fact about the order this tree was written in rather than about
what the concern is about. A boundary does not come to own a thing by
having needed it first.

So the threshold's destination reads: a concern shared by two goes to
the boundary whose subject it is. Where no boundary's subject it is,
the destination names nothing, and the concern stays below the
boundaries that share it.

### `internal/listfilter` stays below the domains

Paging is the subject matter of no domain in garam, so neither of the
two that page with it owns it and neither is somewhere to return it
to. It is under the threshold and it stays extracted.

ADR-0047 §Consequences makes that "a choice to be argued rather than a
rule to be cited". This is the argument, and the three costs measured
in §Context are what it rests on.

## What this does not decide

- **What owns a concern once it is extracted below the boundaries.**
  [structure.md](../structure.md) §Open questions holds that, and this
  decides only which boundary a concern is owed to before it is
  extracted, not what holds it afterwards.
- **Whether `internal/keyfile` or `internal/health` move.** They wait
  on the open question above and are a different case: keyfile's own
  claim is that no boundary owns it, which is the reading taken here
  rather than the one in dispute. What this ADR gives them is the test
  to be read against, not an answer.
- **The threshold.** Three or more boundaries still earns extraction
  and two still does not. Nothing here moves the number or adds a
  branch to it.
- **Where `domains.md`'s concrete-types rule belongs.** It is stated
  in a responsibility document and it binds; whether rule 6 puts it in
  [structure.md](../structure.md) instead is a question about the
  documents and not about this concern.

## Consequences

- **`internal/listfilter`'s standing is decided by this and not by its
  count.** [domains.md](../domains.md) §Shared code below the domains
  records it that way, as it already does for `internal/wire`, which
  the wire-format carve-out places rather than the count.
- **A reviewer asks whose subject a shared concern is**, not who had
  it first. Provenance stops being an argument for a destination, in
  either direction: it is equally not a reason to keep a concern where
  it was born.
- **The shared area does not grow by this.** A concern with a
  subject-matter owner still goes to that owner at two, which is what
  the threshold is for. What stays below the boundaries is the set
  with no owner to go to, and naming that set is what makes it
  countable rather than open-ended.
- **`keyfile` and `listfilter` now say different things for a stated
  reason.** One has a candidate owner and waits on what the shared
  area is; the other has none, which this settles.
- **Zero domain-to-domain imports survives as a property of the
  tree**, and a pull request that would introduce the first has to
  argue against `domains.md` §What a domain is here rather than cite
  the threshold at it.

## Rejected alternatives

- **Read "owns" as provenance and fold `listfilter` into
  `internal/org`.** The destination the threshold names, taken
  literally. It creates the tree's first domain-to-domain edge,
  breaches the concrete-types rule, and makes organizations validate the
  agents listing's paging against `runtime-safety.md` §Trust boundaries.
  Each is a cost of the reading rather than of this
  concern, which is why the reading is what this ADR decides.
- **Read "owns" as provenance but carve out an exception for this
  package.** An exception decides one case and leaves the next to be
  argued from scratch, which is the state
  ADR-0047 §Rejected alternatives calls "a conditional rule turns every
  case into an argument about which branch garam is on".
- **Add a branch to the threshold's escape clause for a partial
  prohibition.** The clause extracts a concern where a boundary "may
  not reach a sibling at all", and `domains.md` forbids less than that
  — the reach is permitted and the concrete type is not. Widening the
  clause would put a structural rule's wording in the place rule 7
  reserves for the point the rules leave open, and it would answer by
  prohibition what is better answered by what owning is.
- **Dissolve `Filter` into a limit and an offset each store takes
  directly.** It removes the shared type and the question with it, and
  it removes the typed wrapper `runtime-safety.md` §Trust boundaries
  requires the boundary to return, leaving the `integer` bound to be
  re-checked or trusted at every store.
- **Give each domain its own copy.** `docs/convention/simplicity.md`
  prefers honest duplication to a wrong abstraction, but the
  abstraction here is not wrong — it is one value with one bound and
  one refusal. Copying it copies the bound and its test, and the two
  copies drift silently because nothing compares them.
