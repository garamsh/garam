# ADR 0069: The aggregates are data carriers, so the driver leaves the domain package

> Status: accepted
> Date: 2026-09-06

Every measurement below was taken at `cb4cbcb`, the commit this
decision's pull request branched from. The commands are in that pull
request; re-run them at your own commit rather than reading a count
here as current.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives
- Errata

## Context

`docs/convention/stack-go.md` §1 puts the implementation of a domain's
dependency interface in a sub-package. The reason
`garamsh/convention-driven-project` gave that rule on 2026-09-05 is
narrower than its path: *"so a driver import (`pgx`, a vendor SDK)
stays out of the domain package's import graph and its tests."* garam
broke the reason and not only the path — at `cb4cbcb`,
`go list -deps` puts eleven `jackc/pgx` packages inside
`internal/user`.

**What kept the store beside the aggregate was encapsulation, and this
ADR is about whether there is any.**
[ADR-0051](./0051-an-aggregate-exposes-what-it-persists.md) §What this
does not decide reserved the write half: *"A setter on the aggregate is
a separate question and no evidence gathered here selects an answer to
it."*
[ADR-0066](./0066-no-aggregate-gains-an-exported-restore-path-to-satisfy-a-layout-rule.md)
then refused to answer it as a side effect of a layout rule, and named
its own reopening condition: *"If the setter question is answered — an
ADR that gives aggregates an exported restore path on evidence about
the surface — this boundary is worth re-examining… It is contingent on
a reservation, and it lasts exactly as long as the reservation does."*
This is that ADR, and the evidence below is about the surface.

**The surface, measured over the ten domain packages'
non-test files at `cb4cbcb`:**

| what | count |
|---|---|
| one-line getters (`func (u *User) Name() string { return u.name }`) | 122 |
| methods on a pointer receiver that assign a field | **0** |

**The zero is the finding, so it is the one that carries a control.** A
zero and a broken query have the same shape. The same assignment probe
widened to all of `internal/` with test files included returns 44 — the
in-memory fakes and the service structs do assign their fields — so the
probe reports a write where one exists, and the zero over the
aggregates is a zero.

**There is no write half to keep closed.** ADR-0051 reserved the
setter question against a surface presumed to have something to
protect. It does not: every public method on these aggregates reads,
and the ones that are not field getters are derived reads —
`Invitation.Accepted`, `Invitation.Expired`, `Session.Expired`,
`Consent.Stands`, `Enrollment.TokenHash`. No aggregate validates an
assignment, because no aggregate accepts one. The types are data
carriers with a getter per field, and the getters are the whole of what
the encapsulation buys.

**One figure in `#1083` is corrected here rather than quoted.** The
issue reads the non-getter public method as a single exception,
*"Enrollment 6 / 5 — the one exception is TokenHash(), a derived
value"*. That holds for `Enrollment` and not across the ten packages,
where the five derived reads listed above are all non-getters. Nothing
in this decision turns on the difference; what turns on it is that the
count the decision rests on is the zero, and the zero survived being
re-measured with a control. A figure reached by reading is not a figure
reached by compiling, which is the lesson ADR-0066 §Context recorded
about its own predecessor.

**This reverses a decision merged the same day, and hiding that would
cost the next reader the reason.** ADR-0066 merged as `#1039` on
2026-09-06 at 12:35 and this ADR was written after it, on the same
day. What moved in those hours is not the price of the change and not
anyone's patience with it: ADR-0066 §Rejected alternatives refused
*"the barred change on 15 types at once — chosen by a layout rule
rather than by evidence about the surface, which is the order this ADR
exists to refuse"*, and its ground was that the record held no such
evidence. The table above is that evidence, gathered for this
question and not as a by-product of wanting the layout. A reader who
finds two opposite decisions one day apart is owed which of the two
facts changed, and it is this one.

## Decision

### An aggregate's persisted state is exported fields

A value a domain's store writes and reads back is an exported field on
the aggregate. The accessor that read it is deleted, not kept beside
it: two ways to read one field is the drift ADR-0051's own
consequences warned a reader to check for. ADR-0051's rule survives in
its purpose — a persisted value no caller can reach is still a defect —
and the exported field is what discharges it.

This is the pilot's answer for `internal/user` and the shape the nine
remaining domains take in their own pull requests. It reaches an
aggregate that holds no invariant over its fields, which at `cb4cbcb`
is all fifteen of them.

### The store moves to a sub-package named for the technology

`internal/<domain>/postgres/` holds the implementation, and the
package is named for what it implements against rather than for the
interface it satisfies. `garamsh/gagent` is where the owner pointed and
what the name means there: `internal/memory/sqlite/` is *the
implementation against SQLite*, beside `internal/agent/anthropic/` and
`internal/agent/openai/`.

The type in it is `Store` and its constructor is `New`, so a caller
writes `postgres.New(pool)` returning `*postgres.Store` — the stutter
`postgres.PostgresStore` is what the rename exists to remove. A domain
whose package holds more than one store names each after what it
stores rather than collapsing them onto `New`.

**A store package is imported under an alias, never bare.** Nine more
packages named `postgres` are coming, and
`tests/integration/postgres_test.go` and `internal/cli/serve.go` will
each reach several. `userpg "…/internal/user/postgres"` is the form.
Aliasing from the first domain is what keeps the second from editing
the first's import line, and a bare `postgres` is a name the next
domain has to take away.

This diverges from `stack-go.md` §1, which names the sub-package for
the interface (`store/`) and puts two implementations in it as two
files. **It records the divergence and does not change the rule**, the
way [structure.md](../structure.md) §Where the composition root lives
records one for §Project envelope: `docs/architecture/README.md` rule 8
leaves the stack-specific file governing, and whether §1 should name a
path or a property is the convention owner's, referred in `#1035`.

### A creation constructor stays; a field-copying one is not written

`user.New` stays, because it sets two fields its caller does not pass:
`CreatedAt` and `UpdatedAt`, stamped from one instant. That property is
asserted, not incidental —
`tests/integration/postgres_test.go` holds that *"a row nothing has
updated carries one instant in both columns"*.

A constructor that only copies its arguments into the fields is not
written. With the fields exported a composite literal is that
constructor, and a second spelling of it is a function whose body a
reader has to open to learn it does nothing. The store restores a row
with a literal for the same reason.

### The generated queries follow the implementation

`queries.sql` and the `db/` package sqlc generates from it move to
`internal/<domain>/postgres/`, and `sqlc.yaml` moves with them.

The reason `stack-go.md` §1 gives is satisfied either way, and this was
measured before it was decided: with `db/` left at `internal/user/db/`,
`go list -deps internal/user` still reports zero `pgx` packages,
because the domain package imports neither. So this half is a decision
about shape and not about the rule, and it is recorded as one.

What decides it is that both files are Postgres and nothing else is.
`queries.sql` is hand-written PostgreSQL; `db/` is generated with
`engine: postgresql` and `sql_package: pgx/v5`; the only importer of
`db/` is the store beside it. Leaving them under `internal/user/`
leaves hand-written PostgreSQL and a pgx-importing package in the
directory of a domain that has just been emptied of both, where the
next reader meets them before meeting the sub-package that owns them.

## What this does not decide

- **Where the `policies` field and column live.** ADR-0051's third
  decision — *"The field and the column both stay where they are"* —
  is untouched and is not superseded. No migration moves and the
  insert still maps a nil slice to `[]string{}` for a `NOT NULL`
  column.
- **Whether `stack-go.md` §1's text should change.** `docs/architecture/README.md`
  rule 4 puts a convention rule off this track, and `AGENTS.md` §Roles
  makes a convention change the PM's. `#1035` is the referral and this
  adds a second instance to it rather than answering it.
- **The nine remaining domains.** Each converts in its own pull
  request, against this shape. Until one does, its aggregate keeps its
  accessors and its store keeps its place, and the responsibility
  documents say so.
- **An aggregate that does hold an invariant.** The decision above
  rests on a measured zero over the aggregates that exist. A future
  type that validates an assignment is not answered here, and it is
  not answered by pointing at this ADR either.
- **Whether a domain gains a second `Store` implementation.** Nothing
  here asks for an in-memory one;
  §Store implementation status in [domains.md](../domains.md) still
  reads *not planned* for every domain.

## Consequences

- **`internal/user` no longer imports a driver, and the control says
  the query still works.** `go list -deps` reports 11 `jackc/pgx`
  packages in `internal/user` before and 0 after, 11 in
  `internal/user/postgres` after, and 11 in `internal/org` and
  `internal/watch` throughout — so the zero is the change and not the
  instrument. `internal/machine`, which persists nothing, reads 0 on
  both sides.
- **ADR-0066 ends, on the condition it wrote for itself.** Its status
  moves to superseded. A reviewer meeting a store beside its aggregate
  in one of the nine unconverted domains still has an answer, and it is
  now this ADR and the follow-up issue rather than a standing
  permission.
- **ADR-0051 is superseded in two of its three decisions.** The
  accessor is no longer the mechanism, and `User` no longer keeps the
  one it had. Its third decision stands, so its status records a
  partial supersession rather than a whole one.
- **A shared test helper stopped being an interface.** `assertInserted`
  and `assertUpdated` took a `timestamps` interface over two accessors;
  a field satisfies no method set, so a converted aggregate could not
  be passed to it. They take a `stamps` value the caller reads off
  either shape, which is what lets one helper serve the converted
  domain and the eight unconverted ones at once. It is written once, as
  its comment said, and the interface goes when the last domain
  converts.
- **Every reader of a `User` field now reads a field.** 38 call sites
  across `internal/api`, `internal/cli` and `tests/integration` lost
  their `()`, each one enumerated by the compiler rather than by a
  search — `.GRN()` is spelled the same on six other aggregates, and a
  textual sweep would have reached them.
- **Nothing detects a regression of this.** No target can tell an
  aggregate that must share its store's package from one that need
  not, and `make ci` reads `docs/` only for each file's `## Contents`
  list. The guard is review, the class
  [README.md](../README.md) §What nothing detects already describes.

## Rejected alternatives

- **Keep the accessors and move the store anyway.** This is #1022's
  option C, and ADR-0066 measured it at 180 compiler errors and refused
  it. Nothing here revives it: the move is cheap *because* the fields
  open, and if they did not open it would still be the wrong order.
- **Name the sub-package `store`, as `stack-go.md` §1 says.** It is
  what the rule's letter asks and it loses what the owner named the
  shape for: `postgres` says the package implements against Postgres,
  so a second implementation is a sibling directory rather than a
  second file whose name carries the only clue. The divergence is
  recorded above and referred to the convention owner instead of being
  resolved by taking the rule's word and dropping its point.
- **Leave `queries.sql` and `db/` at `internal/<domain>/`.** Measured
  and sound on the rule's own reason, and rejected on shape: it leaves
  the domain directory holding the two most Postgres-specific artifacts
  in the domain.
- **Add a hydration constructor — `user.Restore(...)` — instead of
  exporting the fields.** It keeps the getters, adds a sixteenth
  spelling of the aggregate's shape, and answers the reserved question
  in the affirmative anyway: a constructor that sets every field from
  arguments is the write path, wearing a name that suggests otherwise.
  ADR-0066 barred granting one to satisfy a layout rule and this would
  be exactly that, with the evidence above ignored.
- **Wait, and convert all ten domains in one pull request.** `#1083`
  refused it before this ADR existed and the reason holds: nine
  domains converting in parallel before the shape is settled produces
  nine shapes. The cost is that the tree spends the interval in two
  shapes at once, which the responsibility documents state rather than
  hide.

## Errata

- 2026-09-06 — `#1087` changed `docs/convention/stack-go.md` §1 to name
  an implementation sub-package for the technology it implements
  against rather than for the interface it satisfies, which is the
  shape §Decision chose. Three passages here were written against the
  rule as it then stood and are wrong after it: §Decision's "The store
  moves to a sub-package named for the technology" closes by recording
  a divergence from §1 and referring the wording to the convention
  owner in `#1035`; §What this does not decide reserves "Whether
  `stack-go.md` §1's text should change" and says this ADR adds a
  second instance to that referral rather than answering it; and
  §Rejected alternatives rejects "Name the sub-package `store`, as
  `stack-go.md` §1 says". There is no divergence left to record, §1's
  own examples are `postgres/` and `memory/` where they were
  `repository/`, and `#1035` is closed by the pull request that
  changed the rule. The decision is unaffected
  and this entry changes none of it: the layout it chose is the layout
  it chose, and what moved is the rule it was measured against. The
  other two references to §1 stand — §Context's opening, that §1 puts
  the implementation in a sub-package, and §Decision's "The reason
  `stack-go.md` §1 gives is satisfied either way" — because the change
  moved the sub-package's name and neither its existence nor the
  reason for it.
