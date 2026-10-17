# ADR 0048: garam's domains are named by subject matter, and the Go package list is not the domain list

> Status: accepted
> Date: 2026-08-26

Every count below was measured at `5a70c6c`, over authored non-test
files. `_test.go` is excluded, and so is committed generator output —
the sqlc packages under `internal/*/db/` and the mockery packages under
`internal/*/mocks/` — which `docs/convention/README.md` §Scope places
outside what the project writes. `#608` carries the commands and the
record of the four revisions they went through; re-run them there
rather than reading a number here as current.

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives
- Errata

## Context

[ADR-0047](./0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)
decided that garam is partitioned by domain and that a domain's folder
is named for the subject matter it owns and never for a technical role.
It moved nothing and named no domain. Which subject matters those are
was left to whatever the tree happened to hold, and what the tree holds
is a Go package list.

[domains.md](../domains.md) took that list as the map. Its section
headings were `internal/…` paths, and six of them named a technical
role rather than a subject: error mapping, Postgres error
classification, file reading, a paging struct, a wire format and a
probe. `#608` measured the same substitution from the other side —
authorization spread across three packages, one package holding six
concepts, a package existing to satisfy a rule.

The classification was also being made in a second place.
`docs/convention/stack-go.md` §1 carried a package-by-package
enumeration naming which folders were surfaces, primitives, listeners
and probes.
`docs/convention/README.md` §Independence settles which file should
hold it:

> **One file, one territory.** No artifact is governed by two files,
> unless one extends the other … Where two files with no such
> relationship could both decide a case, one of them is holding the
> wrong rule.

Which packages are domains is an architecture decision, so the
convention file was the one holding the wrong rule, and `#622` retired
the enumeration from it. The two Go rules in that paragraph were not
part of the classification and stayed.

That enumeration was also wrong on its own terms, which is why nine is
the answer rather than a compromise. It called `authz` a request
surface while its own closing sentence made `store.go` the marker of a
domain package: `internal/authz` has `store.go`, `queries.sql`, `db/`
and `mocks/` across seventeen files, and `internal/machine`, which the
same paragraph did not call a surface, has none of the four across
five. The paragraph's own test contradicted the paragraph on both
packages it is cited for here.

## Decision

### garam has nine domains, and each is named by its subject matter

The name is the subject. The Go package implementing a domain is named
as its implementation and is not the domain's name.

| Domain | Responsibility it holds | Implemented by |
|---|---|---|
| Organizations | the tenant hierarchy: the top-level tenant, what is grouped inside it, and who belongs to one | `internal/org` |
| Users | human identity | `internal/user` |
| Sign-in | how a person proves who they are, and the session that carries the proof afterwards | `internal/authn` |
| Authorization policy | the rules that decide what a subject may do, and the engine that decides | `internal/authz` |
| Certificate authorities | the certificate authority an organization signs machine identities with | `internal/ca` |
| Operators | what an organization records about an operator, and what that operator authenticates with | `internal/operator` |
| Agents | autonomous identity, provisioned outside garam | `internal/agent` |
| Machine identity | who is calling on the listener a machine identity authenticates on | `internal/machine` |
| Messaging | the durable queue a message reaches an agent through, and the consent that lets one cross an organization boundary | `internal/message` |

### `api`, `machineapi` and `cli` are delivery surfaces, not domains

Each composes several boundaries, and
[structure.md](../structure.md) §The shape makes that the reason: what
composes several boundaries is not itself one of them.

`internal/api` is the clearest case and the largest package in the
tree. It is 2913 lines — 21% of the 14,019 under `internal/`, and
nearly twice the next largest — and declares thirteen exported types of
its own, which is what a package accumulates when it holds behaviour
rather than composing it. That next largest is `internal/cli`, so the
two biggest folders under `internal/` are both things this decision
says are not domains. `internal/api/policy.go` is its largest file at 286
lines, and it depends on `org` and `user` rather than on `authz`: the
management of a policy sits in the surface while the evaluation of one
sits in the domain, with `Policy`, `PolicyVersion`, `PolicyAttachment`,
`Statement`, `Evaluator` and `EvalInput` declared in both.

That is the shape this decision names as a defect. It is not what this
decision moves — see below.

### `delivery` and `watch` are sub-responsibilities

Neither is a domain. Each holds a responsibility belonging to another
domain's subject, and each is a boundary of its own for a reason
already recorded.

- **Watching an agent work** (`internal/watch`) belongs to the agents
  subject. It is a boundary because a watch has a lifecycle no agent's
  contains and its two ends fall on listeners that may not reach each
  other ([ADR-0022](./0022-watching-an-agent.md),
  [ADR-0017](./0017-machine-process.md),
  [ADR-0027](./0027-a-watch-crosses-processes-through-postgres.md)).
- **The adapter's delivery loop** (`internal/delivery`) belongs to the
  messaging subject. It is a boundary because it runs outside garam,
  beside the agent, as a client of the machine listener rather than a
  part of it ([ADR-0018](./0018-delivery-acknowledgement.md)).

[structure.md](../structure.md) §Boundaries and dependency direction
makes a boundary a domain, a feature or a bounded context, so holding
another subject's responsibility and owning a folder are not in
tension.

### Five packages are shared code below the domains, and two are not

A concern three or more boundaries share is extracted below them
([structure.md](../structure.md) §When a new boundary is earned). The
count is domains importing the package from a file that is not a test.

| Package | Domains importing it | Standing |
|---|---|---|
| `errorx` | 9 | earns it |
| `grn` | 9 | earns it |
| `pgxerr` | 6 | earns it |
| `listfilter` | 3 | earns it |
| `wire` | 1 | permitted by the wire-format carve-out, not by the count |
| `keyfile` | 1 | does not earn it |
| `health` | 0 | does not earn it |

`wire` is placed by the carve-out
[structure.md](../structure.md) §When a new boundary is earned states
for a conversion to a wire format: its whole exported surface is one
wire type and conversions in both directions, and nothing in it
chooses a behaviour.

`keyfile` and `health` are the two that do not earn their place. Each
is a sibling folder no boundary owns, which
[structure.md](../structure.md) §Boundaries and dependency direction
names as the thing an extraction must not become. What owns each
afterwards is that document's open question and is not settled here.

### This ADR is where the classification lives

A later reader asking which folders are domains reads this decision and
[domains.md](../domains.md), which carries the present state under
rule 1. The classification does not belong in a stack convention file:
`docs/convention/README.md` §Independence gives each artifact one
territory, and this one is architecture's.

## What this does not decide

- **Where anything lives.** No folder moves, is renamed, is split or is
  merged by this ADR, and nothing under `internal/` changes in the pull
  request carrying it. What changes is what the map says, and what a
  later move may cite.
- **Whether the organizations domain holds one responsibility or
  several.** `#608` counts six concepts in `internal/org`. Naming a
  domain settles what its subject is, not how many subjects a folder
  has, and nothing here divides it.
- **Where the authorization types spread across `api`, `authz` and
  `machineapi` should end up.** This ADR names the surfaces as
  surfaces, which is what makes the spread describable. Collapsing it
  is a change to the tree.
- **What owns `keyfile` and `health`.** They fail the threshold; which
  boundary takes each is
  [structure.md](../structure.md) §Open questions.
- **How `domains.md` is divided into files.** It still carries every
  subject in one document, which
  `docs/convention/documentation.md` §Keep it maintainable does not
  permit; `#620` carries that split, and it depends on this decision
  having settled what the subjects are.

## Consequences

- **A section heading in [domains.md](../domains.md) is a subject, and
  a folder path is not one.** A heading naming `internal/…` is a defect
  a reviewer can cite this decision against.
- **A package that is not a domain is documented as what it is.** The
  delivery surfaces, the two sub-responsibilities and the shared code
  each have a section saying so, rather than a section that reads like
  a domain's.
- **A new folder under `internal/` has to be placed before it is
  added.** It is a domain, a sub-responsibility in a boundary of its
  own, a delivery surface, or shared code below the domains — and
  shared code has a count to meet.
- **The count that places shared code is a measurement, and it excludes
  tests.** A test import is not a dependency of a domain's code.
  Counting one is what made `keyfile` look like two domains through
  three revisions of `#608`.
- **Two packages are recorded as sitting where the rules do not put
  them.** `keyfile` and `health` were not defects anybody had named
  before this was measured; they are now named, and naming them is what
  a later change cites.
- **The map and the tree can now disagree visibly.** Before this, the
  map was the tree by construction, so no gap between them could be
  stated. Every item under §What this does not decide is a gap this
  decision makes sayable.

## Rejected alternatives

- **Leave the map as the package list.** The state this ends. Its cost
  is that no defect in the partitioning can be described: when the
  document's headings are the folders, a folder named for a technical
  role reads as a domain named for a technical role, and the rule
  ADR-0047 set has nothing to bite on. It also hides the two directions
  `#608` measured at once — a subject spread across three folders and a
  folder holding six subjects — because both are invisible to a map
  whose unit is the folder. And it leaves the Go package layout
  deciding an architecture question, which is the substitution this
  project has now refused twice, here and in `#622`.
- **Name the domains and move the folders to match in one change.**
  Rejected on reviewability. The map is a claim that can be checked
  against the tree by reading; a move is a diff across every importer,
  and pairing them means a reviewer cannot disagree with the map
  without re-reviewing the move. Deciding first also gives a later move
  something to cite, which a simultaneous one would not have.
- **Record the classification in `stack-go.md`, where it already
  was.** It decided an architecture case in a Go convention file, and
  `docs/convention/README.md` §Independence gives an artifact one
  territory. The enumeration there had also drifted into contradicting
  its own paragraph's test on both packages this decision is most often
  cited for.
- **Split `internal/org` into six domains now, since `#608` counted
  six.** Rejected as premature: the count is of concepts in a package,
  and whether they are one subject or several is a question about the
  subject rather than about the file. It stays an open question in
  [domains.md](../domains.md) rather than being answered by a
  measurement that was not asked to answer it.
- **Fold `keyfile` into `ca` and `health` into the serving surfaces as
  part of this decision.** Both are moves, and this ADR moves nothing.
  Recording that they fail the threshold is what lets the move be
  proposed against a rule instead of a preference.

## Errata

- 2026-08-31 — §Decision's table under "Five packages are shared code
  below the domains, and two are not" gives `pgxerr` 6 and
  `listfilter` 3. Both were true at `5a70c6c`, which the header above
  names as where every count was measured, and both were falsified on
  2026-08-27: `8961720` (#703) routed the sign-in and messaging stores
  through `pgxerr`, taking it to 8, and `3bd96aa` (#749) removed
  `internal/user`'s `listfilter` import, taking it to 2. The other
  five rows are unchanged. The rule the column is counted under is not
  stated here, and #783 records the same column being counted four
  times and answering with four different numbers for want of it. It
  is that an importer counts
  only when it is one of the nine domains — a delivery surface, a
  sub-responsibility held in a boundary of its own and another shared
  package are not domains — read from its non-test files and not from
  the generated `db/` and `mocks/` packages the header above already
  excludes. Under that rule `go list` names nine importers of
  `pgxerr`, eight of them domains and `internal/watch` the ninth, and
  three of `listfilter`, two of them domains and `internal/api` the
  delivery surface; run against `5a70c6c` it returns the 6 and the 3 the
  table has. [domains.md](../domains.md) §Shared code below the domains
  states the same rule beside the current counts and carries
  the query. This entry corrects the two counts only: what standing a
  count of 2 earns `listfilter` is not decided here, and the Standing
  column stands as the body has it.
