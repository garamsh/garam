# ADR 0051: An aggregate exposes what it persists

> Status: accepted
> Date: 2026-09-01

Every measurement below was taken at `51ea014`. The commands are in
`#752` and in the pull request carrying this ADR; re-run them there
rather than reading a count here as current.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

Three tables carry the same column. `migrations/000001_init.up.sql`
declares `policies text[] NOT NULL DEFAULT '{}'` at lines 5, 22 and 32
— on `users`, `groups` and `projects` — and each of the three
aggregates carries the matching unexported field.

**Two of the three would not surrender it.** `internal/user/user.go`
declares `Policies()`; `internal/org/group.go` and
`internal/org/project.go` declared no such method, so a value their
store wrote and read back could not be reached by any caller holding
the aggregate. Updates arrive through `GroupPatch.Policies` and
`ProjectPatch.Policies` (`internal/org/store.go`), which never touch
the aggregate's field.

**The one accessor that existed had no caller either.**
`git grep -F '.Policies()' 51ea014 -- '*.go'` returns nothing. A bare
absence proves nothing about the instrument, so the same fixed-string
search was run for accessors that are called: `.Roles()` returns 20
hits, `.Nickname()` 10, `.GRN()` 73. The search finds an accessor when
one is called; it found none here.

**The field is not the evaluation path.** A policy reaches a subject
through an attachment the authz engine owns
([ADR-0008](./0008-identity-based-policy-attachments.md)), recorded in
`policy_attachments` and matched by GRN. [users.md](../users.md) has
said since the field was documented that it is stored and not
evaluated. So nothing about this column's meaning was ever going to
supply the missing accessors on its own.

**The project met the question twice before this ADR.** `#729` listed
the field on all three aggregates as "unreachable and unwritable" and
put it up for removal. `#743` re-established the list rather than
inheriting it and found the second half wrong: the field is read at the
insert, where `strs` maps a nil slice to `[]string{}`
(`internal/org/postgres.go`, `internal/user/postgres.go`) because the
column is `NOT NULL`, so removing the field would send NULL. `#749`
then declined to delete `user.Policies()`, on the ground that deleting
it would answer this question `no` for all three aggregates quietly,
inside a removal sweep.

That is the shape [README.md](../README.md) rule 7 reserves. The
structural rules say a boundary is reached only through what it
declares public and leave to the project what must be in that surface;
until the project meets the point it waits under
[structure.md](../structure.md)'s open questions, binding nothing. Two
sweeps have now met it, and a third would meet it again.

## Decision

### An aggregate exposes what it persists

A value a domain's store writes and reads back into an aggregate is
reachable through the surface that domain declares, by an accessor on
the aggregate named after the field (`docs/convention/stack-go.md` §3).
The accessor reads; it does not write. A persisted value no caller can
reach is a defect from here on, not an open point.

### The three aggregates take the same answer

`Group` and `Project` gain `Policies()`. `User` keeps the one it has,
now by decision rather than by deferral. The column is on all three
tables and the same shape of code reads all three back, so one answer
covers them; `User` stops being the outlier without anything being
removed.

### The field and the column both stay where they are

No migration moves and no store logic moves. The field's job at the
insert — turning a nil slice into `[]string{}` for a `NOT NULL` column
— is what `#743` established and is unchanged here. This decision adds
a way to read what was already stored, and nothing else.

## What this does not decide

- **Whether the `policies` field is ever evaluated.** ADR-0008 owns how
  a policy reaches a subject, and this field is not that path. Making
  the value readable is not a step toward evaluating it.
- **Whether any surface publishes the value.** Neither
  `api/openapi.yaml` nor `api/machine.yaml` mentions it, and nothing
  here proposes that they should. An accessor is what a Go caller
  inside this module holds.
- **How a caller writes the value.** `GroupPatch` and `ProjectPatch`
  remain the write path. A setter on the aggregate is a separate
  question and no evidence gathered here selects an answer to it.
- **What an accessor returning a slice owes about aliasing.** The new
  methods return the backing slice, which is what `Roles()`,
  `User.Roles()` and every other slice accessor in these packages
  already do. Whether that should be a copy is one question about all
  of them, not this ADR's.
- **What a domain owes for state it does not persist.** The rule above
  is about a value that reaches the store and comes back. Derived or
  in-memory state is untouched.
- **Whether code reachable only from a test is code this project
  keeps.** `#749` named that as the narrower question its sweeps kept
  surfacing and did not file it. It is still unfiled, and the three
  accessors this ADR settles are now instances of it.

## Consequences

- **The next removal sweep meets a decision instead of a trap.** That
  is the one cost `#749` named for leaving the point open, and it is
  what this closes: a sweep that finds a persisted value with no
  accessor now adds the accessor rather than re-deriving the question.
- **What counts as a defect moved, which is why rule 7 makes this an
  ADR.** Before, an aggregate with no accessor for a stored field was
  one of two defensible shapes. Now it is a defect and the sweep that
  removes the field is the wrong fix.
- **Three accessors have no production caller.**
  `docs/convention/simplicity.md` §Rules is the objection to read this
  against, and the two bullets that could bite do not. *No speculative
  abstraction* bars building for an imagined need; the value here is
  already written and already read back, and the accessor is what makes
  the existing value reachable rather than a provision for a future
  one. *An abstraction that hides nothing* removes a layer standing
  between a caller and a concrete thing; an accessor on an aggregate
  with unexported fields is not standing between anything — without it
  there is no path at all.
- **`user.Policies()` is now load-bearing for a reason a reader can
  check.** It was previously kept because removing it would prejudge
  this question. The reason is now the decision itself.
- **Nothing detects a persisted field added without an accessor.**
  `make ci` reads no rule of this kind, and this repository's own `ci`
  workflow does not run at all — `AGENTS.md` §This project names the
  command that shows that. The guard is review, which is the class
  [README.md](../README.md) §What nothing detects already describes for
  other claims.

## Rejected alternatives

- **Take the field off all three aggregates and let the store carry the
  column.** This was the other answer `#749` named, and `#743` had
  already established it is not a plain deletion: the field's job at
  the insert is turning nil into `[]string{}` for a `NOT NULL` column,
  so the store would need that logic where the field used to be. It
  also answers the general question the other way — that a domain may
  persist a value its model does not carry — which is a larger claim
  than the case in front of it supports, and one this evidence does not
  select.
- **Leave the point open under `structure.md` §Open questions.** Rule 7
  permits it, the current state binds nothing, and no caller wants the
  value today. What it costs is a fourth sweep meeting the same trap,
  and the question has now been met twice, which is what rule 7 asks
  for before a project settles one.
- **Decide it for `Group` alone.** `Project` has no policy story of its
  own yet, so deciding only the aggregate with one is tempting. It
  leaves an outlier, and the outlier is exactly the state this ADR
  exists to end: the column is on all three tables and read back by the
  same code.
- **Give the value a caller first — publish it on a surface — and let
  that settle the shape.** It answers a question about the HTTP surface
  in order to avoid answering the one actually asked, and it would put
  a field ADR-0008 does not evaluate into a contract a consumer reads.
- **Write the rule into `docs/convention/` instead.** The conventions
  bind every project and are the PM's to change
  (`AGENTS.md` §Roles); this is one project's answer to a point its own
  structural rules reserve, which rule 7 places in an ADR.
  `AGENTS.md` §What earns a rule also bars drawing a rule from one
  project's evidence, and one project is all this has.
