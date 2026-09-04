# Conforming internal/ to the package-layout rule is not a file move

What #1022 asked for, why the work stopped before a line was written,
and the measurement that stopped it. It changes no convention file, no
architecture file, no workflow and no source, and proposes no rule.

Checked against `efaf832`. Every count below was taken from the working
tree at that commit by the probe in §How it was measured, and the probe
was reverted with `git reset --hard` and `git clean -fd` afterwards.

## Contents
- What was asked, and what stopped it
- How it was measured
- The controls
- What it measured
- What conforming would require
- Why the record had already decided this
- The correction to the issue's table
- The judgement calls, answered
- What ran, what did not, and one defect not fixed
- What this does not settle

## What was asked, and what stopped it

`docs/convention/stack-go.md` §1 requires that an implementation of a
domain's dependency interface live in `internal/<domain>/<name>/`, *"a
package named for the interface rather than for the technology behind
it"*. #1022 measured 22 `postgres*.go` files directly under nine
domains, priced the move at 2642 LoC, and dispatched it as a pure
relocation:

> All of it `internal/`, all of it Go imports. **The compiler catches
> every missed one.** No API surface changes, no migration, no data, no
> runtime risk.

The file count is right. It is the wrong measurement. A file count says
nothing about what crosses a package boundary, and eight of the nine
domains' store files reach into their own domain's aggregates through
**unexported fields**. Fourteen of the 22 files go further and rehydrate
an aggregate from a database row by writing those fields in a struct
literal, which no exported path in this module can do.

So the move does not compile as a relocation. Reaching the layout the
rule describes requires giving fifteen aggregate types an exported way
to be restored from a row, which is a decision
[ADR-0051](../architecture/adr/0051-an-aggregate-exposes-what-it-persists.md)
explicitly reserved.

## How it was measured

For each of the nine domains: move the files, rewrite the `package`
clause, and add a dot import of the declaring domain.

```sh
for d in org user agent authz ca message operator watch authn; do
  mkdir -p internal/$d/store
  git mv internal/$d/postgres*.go internal/$d/store/
  for f in internal/$d/store/*.go; do
    sed -i "s|^package $d\$|package store\n\nimport . \"github.com/garamsh/garam/internal/$d\"|" "$f"
  done
done
gofmt -w internal/*/store/*.go
for d in org user agent authz ca message operator watch authn; do
  echo "$d: $(go build -gcflags=-e ./internal/$d/store/ 2>&1 | grep -c '^internal/')"
done
```

The dot import is what makes the result readable. It puts every
**exported** identifier of the domain back in scope unqualified, so the
errors that remain are exactly the references that cannot cross a
package boundary, and nothing else — no import bookkeeping, no
qualifier churn, no judgement about what a real move would have written
instead.

`-gcflags=-e` lifts the compiler's ten-error cap. Without it every
domain reports 11 and the domains are indistinguishable; that was the
first run and it is why the flag is here.

The errors were then classified by the text the compiler produced:

```sh
out=$(go build -gcflags=-e ./internal/$d/store/ 2>&1 | grep '^internal/')
echo "$out" | grep -c 'in struct literal'
echo "$out" | grep -c 'but does have method'
echo "$out" | grep 'undefined (cannot refer to unexported field' | grep -vc 'in struct literal'
echo "$out" | grep -c '^internal/.*: undefined: '
```

## The controls

A count of failures is unreadable without something that would have
come out differently. Three were run, and the third is the one that
matters.

- **The untouched tree.** `go build ./...` and `go vet ./...` at
  `efaf832` both report nothing. A failure after the probe is the
  probe's, not the toolchain's.
- **The domain package after its files leave.** `go build
  ./internal/<d>/` reports nothing for all nine. The residual errors
  are boundary crossings and not collateral breakage.
- **`watch` returns zero.** The instrument does report "this domain
  moves" when a domain moves — for one of the nine. That is what makes
  the other eight a property of those domains rather than of the
  method. Had every domain failed, this document would be evidence of a
  broken probe.

The direction is right for what is being reported. The claim here is
that the move *cannot* be done mechanically, so the control has to be a
case where the probe says it *can*, and `watch` is it.

## What it measured

Residual errors per domain, by kind:

| domain | struct-literal writes | reads, accessor exists | reads, no accessor | unexported functions | total |
|---|---|---|---|---|---|
| org | 28 | 22 | 5 | 0 | 58 |
| user | 6 | 5 | 0 | 0 | 12 |
| agent | 22 | 5 | 13 | 3 | 45 |
| authz | 7 | 6 | 1 | 0 | 16 |
| ca | 5 | 4 | 1 | 0 | 10 |
| message | 10 | 5 | 0 | 1 | 17 |
| operator | 4 | 6 | 1 | 1 | 13 |
| watch | 0 | 0 | 0 | 0 | 0 |
| authn | 3 | 2 | 1 | 0 | 6 |
| total | 85 | 55 | 22 | 5 | 177 |

The four kinds do not sum to the total for `org`, `user`, `agent`,
`authz`, `message` and `operator`. **Ten errors** match none of the four
patterns: a read of `grn` reports as *"has no field or method grn"*
rather than *"but does have method"*, because the accessor is `GRN()`
and differs from the field in more than its first letter — the same for
`message.Message`'s `id` and `ID()`. They are reads with an accessor,
and the second column reads 65 rather than 55 if they are put there.

The **85 struct-literal writes** are the ones with no exported
equivalent anywhere. The fourteen files that hold them:

- `internal/org/postgres_org.go`, `postgres_group.go`,
  `postgres_project.go`, `postgres_membership.go`,
  `postgres_invitation.go`
- `internal/user/postgres.go`
- `internal/agent/postgres_agent.go`, `postgres_assignment.go`,
  `postgres_definition.go`
- `internal/authz/postgres_policy.go`
- `internal/ca/postgres.go`
- `internal/message/postgres.go`
- `internal/operator/postgres.go`
- `internal/authn/postgres.go`

## What conforming would require

**Fifteen aggregate types across eight domains, holding 82 unexported
fields between them, would each need an exported way to be restored
from a stored row**, with its state and its timestamps:

| type | unexported fields | type | unexported fields |
|---|---|---|---|
| `org.Organization` | 4 | `agent.Assignment` | 5 |
| `org.Group` | 7 | `authz.Policy` | 7 |
| `org.Project` | 7 | `ca.Authority` | 5 |
| `org.Membership` | 4 | `message.Message` | 7 |
| `org.Invitation` | 8 | `message.Claim` | 3 |
| `user.User` | 6 | `operator.Operator` | 4 |
| `agent.Agent` | 6 | `authn.Session` | 3 |
| `agent.Definition` | 6 | | |

Three unexported package-level functions would also have to be exported
or moved: `validateOwnership` and `validateOperator` in
`internal/agent`, and `newClaimID` in `internal/message`.
`internal/operator` calls a `validateOwnership` of its own.

None of that is a relocation, and #1022 rules all of it out in the same
breath as it dispatches the work: *"It is a pure relocation: file
contents unchanged except the `package` line, plus import updates at
the call sites."* The issue asks for a layout that cannot be reached
under the conditions the issue itself sets.

## Why the record had already decided this

`docs/architecture/domains.md` §The shape a domain takes states it, with
its reason:

> It stays one package because the aggregates' fields are unexported and
> no exported constructor restores a stored row's state or timestamps,
> so whatever rehydrates one shares its package.

ADR-0051 §What this does not decide reserves the enabling change by
name:

> **How a caller writes the value.** `GroupPatch` and `ProjectPatch`
> remain the write path. A setter on the aggregate is a separate
> question and no evidence gathered here selects an answer to it.

ADR-0051 added accessors, which read. Conforming to `stack-go.md` §1
needs the other half, on fifteen types at once, and the record has left
that open on purpose. So this is not a worker's move to make: it is
`docs/convention/stack-go.md` §1 disagreeing with
`docs/architecture/domains.md` §The shape a domain takes and with
ADR-0051, which `AGENTS.md` §Start here, every time sends to the owner
rather than to whoever meets it.

The measurement that missed this was accurate about the tree. It read
`stack-go.md` and `internal/`, and did not open `docs/architecture/`,
where the answer was.

## The correction to the issue's table

#1022 lists `message` among six mechanical domains. It is not one.
`internal/message` declares both `Store` and `ConsentStore`, and a
single `PostgresStore` satisfies both — the compile-time assertions are
in `internal/message/postgres.go` and
`internal/message/postgres_consent.go`. That is the same shape as
`operator`, which the issue flags as its hardest case.

So the split is **five mechanical domains and four that need a
judgement**, not six and three. `docs/architecture/domains.md` already
groups `operator` and `message` together for this reason.

## The judgement calls, answered

Recorded because three of the four are independent of the blocker
above, and because the answers should not have to be re-derived if the
owner settles the conflict and the move is dispatched again.

- **`watch` — two packages, `internal/watch/store/` and
  `internal/watch/relay/`.** Two interfaces in two files, two
  implementation types, and no helper shared between them. Both
  interface names are single words, so §1 and §3 of `stack-go.md` agree
  here; this is the only domain where the rule applies without
  interpretation, and the only one the probe compiles. One consequence
  to note: the exported `watch.TurnEventChannel` moves with the relay,
  and `tests/integration/watch_relay_test.go` is its only reader.
- **`operator` and `message` — one package each,
  `internal/<domain>/store/`.** A Go type lives in exactly one package,
  so one implementation satisfying three interfaces cannot be split
  across three of them without splitting the type, which is a behaviour
  change and which the composition root contradicts:
  `internal/cli/machine.go` and `internal/cli/issue_certificate.go` each
  pass the same value twice to `operator.NewService`. The rule's own
  sentence, *"Two implementations of one interface are two files in that
  one package"*, already says a package holds implementations and that
  file count follows implementation count. The dual case has no text,
  and the same reading gives one package. `store` is the name because it
  is the interface the implementation is named after, and the only one
  of the three that §3 admits as a package name.
- **`authn` — the letter gives four packages, and they should not be
  built.** Four interfaces, four implementation types, one file. Three
  reasons, none of them preference. First, `stack-go.md` §3 requires a
  package name that is *"single word, lowercase, no underscores"*, and
  every example the layout rule gives is one — `Repository`, `Mailer`,
  `Store`, `Relay`. `PasswordCredentialStore` has no such spelling, so
  §1 and §3 give different answers, which they do nowhere else in this
  repository. Second, `stamp` in `internal/authn/postgres.go` is used by
  all four implementations, so four packages means four copies of it — a
  content change, and not a relocation. Third,
  `docs/convention/simplicity.md` §Rules: *"A division is a response,
  not a plan... An empty container is a claim the code does not
  support."*

  What to do instead: one `internal/authn/store/`, four files, one per
  interface. It satisfies what §1 and §2 enforce — the implementation is
  not beside its declaration, and its package is named for the contract
  rather than for Postgres — without breaking §3 or copying `stamp`. It
  does **not** satisfy the letter of *"a package named for the
  interface"*, and this document does not claim it does. That gap is
  what `AGENTS.md` asks a worker to surface rather than settle.

## What ran, what did not, and one defect not fixed

- `go build ./...` and `go vet ./...` — pass, on the untouched tree.
  These are the probe's first control.
- The probe above — run, on all nine domains, then reverted.
- `make ci` — pass. It runs `format`, `lint`, `test`, `build`,
  `check-gen`, `check-toolchain`, `chart` and `web-ci`; `test` excludes
  `tests/integration` and `tests/e2e` by construction and says so on its
  own first line.
- `scripts/check-line-width.sh` — 0 violations in this file. It is in no
  `make` target, so it was run by hand. It reports 142 violations in 47
  other files, none of them this branch's, which is what makes the zero
  here readable as a zero rather than as a check that matched nothing.
- `make integration`, `make e2e`, `make mutation` — **not run.** This
  branch changes one Markdown file and no Go file, so there is nothing
  for them to answer.

One defect was found and left alone, because #1022 forbids unrelated
edits and this document is not the pull request that should fix it. The
doc comment on `strs` in `internal/user/postgres.go` reads `as// NULL`:
a line break was lost inside the comment. Cosmetic, and no code reads
it.

## What this does not settle

- **Whether `stack-go.md` §1 should change, or an ADR should record the
  divergence, or the tree should move.** #1022 laid those out as A, B
  and C, recommended B, then reversed to C. This document removes C
  from the menu at the price C was given, and says nothing about A or
  B. `AGENTS.md` §Roles puts the choice with the owner.
- **Whether an aggregate should have an exported write path.** That is
  ADR-0051's reserved question, and 177 compiler errors are a cost, not
  an argument for a design. Nothing measured here selects an answer.
- **Whether `watch` should move on its own.** It is the one domain that
  compiles, and moving it alone would leave the repository conforming
  in one domain of nine, which is a smaller inconsistency than today's
  but still one. Whether that is worth a pull request is the owner's.
- **Whether the probe generalizes.** It answers "what does this file
  reference that it could not reference from another package", which is
  the question any package split asks. It was run once, on one
  repository, against one proposed split.
