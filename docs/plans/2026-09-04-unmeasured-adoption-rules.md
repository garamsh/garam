# What the other thirty-one adoptions brought, and what a measurement could settle

A survey of the rules the 31 template adoptions #1017 did not reach
brought into this repository, taken for #1019. It changes no
convention file, no architecture file, no workflow and no source, and
proposes no rule.

Checked against `bb5431d`, with `template/main` at `a6a2355`. Every
count below was taken from `git show <commit>:<path>` at the commit
named beside it, not from the working tree.

## Contents
- What this continues, and what it inherits
- The complement check
- The controls on the inherited enumeration
- The thirty-one, by verdict
- The rules that govern nothing here
- The two adoptions that brought no rule
- The rules a measurement cannot settle, and why
- The rules the tree satisfied
- The rules the tree did not satisfy
- What stands violated today
- The hits that are not violations
- What was not measured
- What this does not settle

## What this continues, and what it inherits

[#1017's report](2026-09-04-adoption-arms-survey.md) enumerated 44
commits that took content from the conventions template, measured the
rules 13 of them brought, and recorded the rest as **not measured**
rather than **satisfied**. This report takes that remainder.

The enumeration is inherited, not re-derived. What was re-run is the
extraction it rests on, because this report needs the adopted *lines*
per commit and not only the commit list: the template's 55 ever-held
paths, the 3017 lines of at least 40 characters it ever held at them
with the date each was first held, and the 241 commits on `main`
touching one of those paths. That reproduction returns the same
per-commit hit count as #1017's table for **all 44** — 223 at `d327b3e`,
95 at `981c537`, 379 at `2abb3ee`, 16 at `b117e89`, and so on down the
table. So the lines read below are the lines #1017 counted.

Two details of the reproduction are worth recording because getting
either wrong changes the answer.

- **Rename detection has to be off.** With `git show`'s default rename
  detection, a renamed file's body never appears as added lines, and
  `981c537` reads 60 adopted lines instead of 95 — the difference is
  `docs/ai/pm-playbook.md` becoming `pm-guide.md` and
  `docs/convention/documentation.md` arriving under a new name. The
  first run of this reproduction had it on and disagreed with #1017;
  the disagreement was the instrument, not the report.
- **The date comparison is same-day inclusive.** Strictly-earlier gives
  25 adoptions rather than 44, because the bootstrap and several later
  syncs took template content the same day the template wrote it.

## The complement check

The 13 #1017 measured, read off its own section headings: #1, #37,
#603, #612, #762 and #771 under §The six adoptions the arm reaches;
#98, #149, #151, #379 and #607 under §The same question over the
convention adoptions and the two verdict sections; #123 and #126 under
§The four the issue names.

Against the inherited 44 those 13 are **present, disjoint from the
remainder, and complete**: 13 + 31 = 44, no commit appears in both, and
no member of the 13 is missing from the 44. The 31 are #14, #27, #34,
#44, #45, #46, #49, #50, #51, #52, #127, #128, #177, #178, #206, #225,
#291, #293, #307, #318, #323, #325, #429, #616, #638, #643, #769, #890,
#897, #972 and #980.

Three of the 31 appear in #1017's text and are still correctly in the
complement, because appearing there is not being measured there.

- **#49 (`bb50cea`)** supplies a control datapoint for the
  cross-reference query — "4 at `bb50cea`". Its own adopted rules were
  never read.
- **#616 (`5668576`)** is named as the repair that retired
  `arch-domain.md` 9 minutes after #612. Its own adopted line was not
  measured.
- **#897 (`040df2d`)** is named as what finally closed the Contents
  rule. What it adopted was not measured.

## The controls on the inherited enumeration

- **The template has not moved.** `template/main` is at `a6a2355` with
  206 commits, the same tip and count #1017 recorded, re-fetched here
  rather than taken on report. So the inherited enumeration and this
  one rest on the same upstream tree.
- **The 44 is still 44 at `bb5431d`.** Re-running the sweep at today's
  HEAD rather than at #1017's `4cc6ebc` adds no adoption and drops
  none, so the two commits merged since (#1017, #1018) took no template
  content.
- **The reproduction surfaces a 45th candidate, and reading rejects
  it.** #163 (`b2eeff1`, "fix: correct two Go convention errors found
  in review") adds one line to `docs/convention/stack-go.md` the
  template already held there: `mock, output directory, package names,
  per-interface overrides.` The commit is *diverging* from the template
  — it renames `mockery.yaml` to `.mockery.yaml` against the template's
  undotted spelling — and the matching line is a wrap coincidence
  inside the paragraph it was rewriting. Its one genuinely new rule,
  the comma-ok form for a boundary that erases a static type, is five
  lines the template never held. This is the class #1017 rejected on
  reading under cross-path identity, and it is rejected here on the
  same ground. It is recorded rather than dropped because a
  reproduction that returns 45 against a report's 44 is worth stating
  either way.

## The thirty-one, by verdict

| adoption | commit | date | what it brought | verdict |
|---|---|---|---|---|
| #14 | `ab6c9aa` | 07-25 | PM review procedure, PR template field | governs nothing here |
| #27 | `da9f428` | 07-25 | QA role, three-role matrix | governs nothing here |
| #34 | `e9f6069` | 07-25 | QA context-building | governs nothing here |
| #44 | `613b19f` | 07-26 | merge-as-approval | governs reviews |
| #45 | `b52fb20` | 07-28 | output styles, dispatch guide | governs nothing here |
| #46 | `b51eefa` | 08-02 | dispatch guide, `simplicity.md` | unmeasurable |
| #49 | `bb50cea` | 08-02 | action process, three layers | part unmeasurable |
| #50 | `1770037` | 08-02 | dispatch and supervision | governs nothing here |
| #51 | `22fb51c` | 08-02 | bootstrap, file names, `unverified` | measured by #1017 |
| #52 | `1c3e22f` | 08-02 | `adoption.md` | governs nothing here |
| #127 | `ae648d2` | 08-15 | `AGENTS.md` orientation | governs roles |
| #128 | `78ba6a9` | 08-15 | docs, git, review, runtime-safety | satisfied in part |
| #177 | `9f8071a` | 08-16 | render helper, tier enforcement | satisfied |
| #178 | `b91a579` | 08-16 | mockery config, feature entry points | satisfied |
| #206 | `232d21f` | 08-16 | entry point reports what it did | **violated** |
| #225 | `5e038b9` | 08-17 | pin rule, `go test` output | satisfied |
| #291 | `cde0991` | 08-17 | evidence rule, expected failure | governs reviews |
| #293 | `d54c0ec` | 08-17 | Next.js errors section | part unmeasurable |
| #307 | `34bef09` | 08-17 | selection, H1 case, testcontainers | decided at #894 |
| #318 | `d3df7d8` | 08-17 | generated files, checked-against | satisfied in part |
| #323 | `7453f6e` | 08-17 | `GOTOOLCHAIN` floor | satisfied |
| #325 | `19ca6c2` | 08-17 | PM ownership reorder | governs roles |
| #429 | `ca3e46d` | 08-23 | checks belong to the head commit | governs reviews |
| #616 | `5668576` | 08-26 | selection reworded | satisfied |
| #638 | `a6b06c0` | 08-26 | interface and implementation layout | **violated, half stands** |
| #643 | `6994a4b` | 08-26 | recover middleware replies 500 | **violated** |
| #769 | `226f6fd` | 08-27 | `stack-container.md` | satisfied |
| #890 | `5d1c118` | 09-02 | `make ci` in the template | governs pull requests |
| #897 | `040df2d` | 09-02 | `check-contents-list.sh` | satisfied |
| #972 | `5a7c3d7` | 09-03 | — | brought no rule |
| #980 | `b43961a` | 09-03 | — | brought no rule |

Counted as adoptions: **8 govern nothing standing in this repository**,
**2 brought no rule at all**, **7 govern a review, a commit message, a
pull request body or a role rather than tree content**, **11 carry at
least one rule this report could measure**, and **3 of those 11 left
standing content in violation at their own commit**.

## The rules that govern nothing here

Eight of the 31 brought rules whose whole subject was the role
documents this repository later stopped keeping. #127 (`ae648d2`,
"chore: move agent roles and procedures out of the repository") removed
`docs/ai/`, `.claude/agents/` and `.opencode/agents/`; #616 retired
`docs/convention/arch-domain.md`. At `bb5431d` all of
`docs/ai/`, `.claude/agents/`, `.opencode/agents/`,
`.claude/output-styles/` and `arch-domain.md` are **absent**.

Control on that absence: the same listing at `1770037` returns 5 files
under `docs/ai/`, and at `b52fb20` returns 3 under `.claude/agents/`.
The query finds those paths where they exist.

- **#14, #27, #34, #45, #50** — PM review procedure, the QA role and
  its evidence rules, the three-role authority matrix, session output
  styles, and the Orca dispatch and supervision procedure. Every line
  landed in a file removed at #127.
- **#52** — `docs/ai/adoption.md`, the procedure for applying and
  updating the template. Removed at #127.
- **#46 and #51 in part** — the dispatch guide and `BOOTSTRAP.md`
  halves. Their convention halves are treated separately below.
- **#178 in part** — five lines of `arch-domain.md` on import
  direction across boundaries. `docs/architecture/structure.md` holds
  that rule now; the file the lines landed in is gone.

`this rule governs nothing here` is the verdict for these, stated
rather than omitted. It is not the same as satisfied, and the removals
that produced it were made for their own reasons years of commits
before this question was asked.

## The two adoptions that brought no rule

Both are diff artifacts, and both would have read as adoptions in any
count that did not open them.

- **#972 (`5a7c3d7`)** matches on `- E2E: a dedicated top-level
  directory, outside the application source tree.` That line was
  **already present at `5a7c3d7^`, at line 81**. It appears in the diff
  as a deletion and an addition only because the file's
  no-newline-at-end-of-file state changed when the sentence after it
  was removed. The commit removes four statements of a handoff rule; it
  adopts nothing.
- **#980 (`b43961a`, "docs: wrap ten convention files at 72 columns")**
  matches on the first wrapped segment of a `review.md` bullet that
  already stood. Its ratio in #1017's table — 1 of 578 — is the shape
  of a re-wrap, and reading the diff confirms it.

## The rules a measurement cannot settle, and why

This is the larger half of what the 31 brought, and the reason is
structural rather than incidental: the rules #1017 could measure were
the ones whose governed set is enumerable from the tree, and what was
left over is mostly what a reviewer decides.

Each of these is reported as **unmeasurable**, with what a count would
have had to stand in for. None is reported as satisfied.

- **`simplicity.md`, adopted whole at #46 (`b51eefa`).** "Every
  abstraction names a real, repeated problem." "If a competent engineer
  cannot understand a unit in a few seconds of reading, restructure
  it." "A wrong abstraction is worse than honest duplication."
  Nesting depth and call-chain length are countable, and counting them
  would answer a different question: the rule asks whether an
  abstraction hides a problem that occurs more than once, and no
  threshold decides that. A count here would be a number standing in
  for the judgement the rule exists to ask for.
- **`testing.md` §Failing, "A test is shown to fail before it is
  trusted" (#206).** The rule governs an act — remove the behaviour,
  watch the assertion break, put it back — and the tree records no
  trace of whether it was performed. What is checkable is whether a
  pull request body says which tests were seen to fail, and that is a
  property of pull requests, not of this repository.
- **`testing.md`, "Assert the property, not the absence of its
  consequence" (#206).** Deciding whether an assertion reads the
  property or a downstream symptom of it means knowing which is which
  for that test. This is the example #1019 itself names.
- **`testing.md`, "A test asserting a refusal carries a control that
  would break with it" (#206, restated at #307).** Two judgements, not
  one: which tests assert a refusal, and whether the control chosen
  isolates the mechanism rather than merely passing. The second is the
  reviewer's whole job on such a test.
- **`testing.md` §Waiting, four rules (#178).** "A step waits on the
  condition it depends on." Whether the awaited condition is the one
  the step depends on cannot be read off the call.
- **`review.md`, everything it gained across #44, #128, #291, #307,
  #318, #429 and #980.** These govern what a review carries. #1017
  excluded the class for the same reason and it is excluded here:
  reviews are GitHub artifacts, not passages in the repository.
- **`git.md` commit and branch rules (#128).** Same class. A branch
  name and a commit body are not tree content at a commit.
- **`AGENTS.md` role and authority statements (#127, #318, #323,
  #325).** "Does not implement", "is the only role that merges one".
  These describe how the project is run. `documentation.md` §Keep it
  current makes a claim about this repository checkable, but these
  claims are about conduct, and conduct leaves its record on GitHub.
- **`stack-go.md`'s "A domain owes a `service.go` once it has
  behaviour of its own to offer. Holding types and persisting them is
  not that" (#638).** Counting domains without a `service.go` is easy
  and answers nothing: the rule turns on whether the domain has
  behaviour of its own, which is the judgement. #629 met exactly this
  question — "Four domains have no service.go — does each hold
  behaviour of its own?" — and it was settled by reading.
- **`stack-container.md`, "Two images stay two when one holds what the
  other may not reach" (#769).** The rule is about what a merge would
  cost, and it binds an argument rather than a file.
- **`stack-container.md`, the multi-platform index clause (#769).**
  "Take the digest of the multi-platform index, not of one platform's
  manifest." This one is measurable in principle and simply was not
  measured here: settling it means asking a registry whether each
  pinned digest resolves to an index or to a single manifest, which is
  a network query against Docker Hub rather than a read of the tree.
  Stated as not measured, not as satisfied.
- **`documentation.md`, sentence-case headings (#128, #307).**
  Measurable only down to a candidate set. A query for sub-headings
  carrying two or more capitalised words returns 9 of 237 at
  `78ba6a9`, 9 of 357 at `34bef09` and 5 of 683 at `bb5431d`.
  Separating Title Case from a proper noun is the reader's call, and
  the 5 at HEAD are all the second kind — see §The hits that are not
  violations.

## The rules the tree satisfied

Named here so the negatives below can be read as negatives. Every zero
is a query shown to return non-zero somewhere.

- **`stack-nextjs.md`, the render helper at `src/testing/render.tsx`
  (#177, `9f8071a`).** Present at the adoption commit and at
  `bb5431d`. Control: the same listing at the parent of #100
  (`1703863`), the commit that scaffolded the web app, returns **0**
  files under `web/src/testing/`.
- **`stack-nextjs.md`, tier enforcement by `import/no-restricted-paths`
  (#177).** Configured in `web/eslint.config.mjs` at `9f8071a` and at
  `bb5431d`. It arrived at #176 (`f5480d1`, "fix: enforce the import
  direction with import/no-restricted-paths") — **the project
  configured it and then adopted the rule requiring it**, not the other
  way round.
- **`stack-go.md`, one mockery config at the module root (#178,
  `b91a579`).** Exactly one at the adoption commit and at `bb5431d`:
  `.mockery.yaml`. Control: the same listing at `b2eeff1^` returns the
  undotted `mockery.yaml`, so the query tells the two spellings apart.
  As with the tier rule, the repair came first — #163 dotted the name
  the day before #178 adopted the sentence permitting it.
- **`stack-nextjs.md`, a feature declares two entry points (#178).**
  Both features, `auth` and `orgs`, carry `index.ts` and `actions.ts`
  at `b91a579` and at `bb5431d`. 2 of 2 at both.
- **`stack-go.md`, the `GOTOOLCHAIN` floor with an `+auto` suffix
  (#323, `7453f6e`).** Present in the `Makefile` at the adoption commit
  and at `bb5431d`. It was already present at `7453f6e^`: #134
  (`c7221a5`, "fix: run pinned tools under this module's Go toolchain")
  set it on 2026-08-16, a day before the rule arrived. Control: the
  same grep at `c7221a5^` returns **0**.
- **`ci.md`, the pin rule as widened at #225 (`5e038b9`).** The
  widening admits a Go module version verified against `go.sum` and the
  checksum database. `go.sum` is present at the adoption commit and at
  `bb5431d`, and a search for `GONOSUMDB`, `GONOSUMCHECK` and
  `GOPRIVATE` across the `Makefile` and `.github/` returns **0** — the
  database is not bypassed, which is the condition the widened rule
  attaches.
- **`stack-go.md`, an entry point prints what it covered before
  invoking `go test` (#225).** The `Makefile`'s `test` target prints
  its package count and fails when the list is empty, which is what the
  rule asks for.
- **`stack-container.md`, digest-pinned bases (#769, `226f6fd`).** 4 of
  5 `FROM` lines across both Dockerfiles carry `@sha256:` at the
  adoption commit and at `bb5431d`. The fifth is `FROM scratch` — see
  §The hits that are not violations.
- **`stack-container.md`, a build secret arrives on a mount (#769).**
  The build takes no secret: no `--mount=type=secret` and no
  secret-shaped `ARG` or `ENV` in either Dockerfile. Control: the same
  case-insensitive pattern returns 5 hits under `deploy/`, in the
  chart's README, so it fires where the words appear. Verdict on the
  rule proper: governs nothing here.
- **`README.md`, the `stack-container.md` index row (#769).** The row
  and the file both present at the adoption commit and at `bb5431d`.
- **`check-contents-list.sh` and its wiring (#897, `040df2d`).**
  Referenced 4 times in the `Makefile` at the adoption commit and at
  `bb5431d`. Run here at `bb5431d`: **exit 0, 115 Markdown files
  checked**, the count printed with the selector that produced it.
- **`runtime-safety.md`, no silent assertions (#128, restated #206).**
  Unchecked type assertions under `internal/` and `cmd/`: **0** at
  `78ba6a9`, **0** at `232d21f`, **0** at `bb5431d`. Control: the
  comma-ok form on the same sweep returns 150, 158 and 97, so the query
  family finds assertions where they exist. Lint suppressions: 0, 0 and
  2 — the 2 are recorded below as non-violations.
- **`stack-go.md`, `cmd/<binary>/main.go` (#638).** `cmd/garam/main.go`
  at the adoption commit and at `bb5431d`.
- **`README.md`, selection reworded (#616).** The commit narrows the
  selection sentence from "stack or its structure" to "stack" and drops
  the Architecture tier in the same change, and its Contents list drops
  the matching entry. The Contents rule is #1017's ground and is
  satisfied here.
- **`testing.md`, no testcontainers in integration (#307).** Not a
  standing violation, because it was decided. `testcontainers` appears
  at `bb5431d` only in the convention text itself, never in test code.
  The substantive tension — that `tests/integration` runs against the
  Postgres `make db-up` starts — was raised as a Convention concern on
  #891, filed as **#894**, and **decided on 2026-09-03: option 2,
  `testing.md` records that this project's integration layer runs
  against a real Postgres, and why.** `testing.md` line 40 carries that
  record now. The decision was reached on coverage and mutation
  evidence, quoted in the issue.

## The rules the tree did not satisfy

Three of the 31 left standing content failing a rule at their own
commit. **All three were named by the pull request that adopted them.**

- **`ci.md`, "An entry point reports what it did", at #206
  (`232d21f`) — violated, named, tracked, repaired after 7 days 5
  hours.** The adopted sentence requires an entry point to print a
  count of what it covered or fail when the count is zero. At the
  adoption commit the `Makefile` had **0** targets printing such a
  count; at `bb5431d` it has 4. They arrived at #464 (`e5cf5ad`, "fix:
  report what make ci's format, lint and build covered") on
  2026-08-24T04:25:11+09:00, against the adoption at
  2026-08-16T22:56:32+09:00. #206's own body says it: "This project's
  `make ci` violates it today, which is why it was proposed; tracked as
  #207", and its Convention concerns field adds "the violation is the
  evidence that produced the rule".
- **`stack-go.md`, the recover middleware, at #643 (`6994a4b`) —
  violated, named with its own control, repaired after 1 hour 2
  minutes.** The adopted sentence says an HTTP server holds a recover
  middleware because `net/http` aborts the response instead of
  answering. At the adoption commit `recover()` returns **0 hits across
  every Go file in the module**. At `bb5431d` it returns 4 across 3
  files. Repaired at #654 (`7610e8a`, "fix: answer a panicking handler
  with 500 rather than dropping the connection"),
  2026-08-27T00:30:50+09:00 against 2026-08-26T23:28:16+09:00. #643's
  body ran the same sweep and attached a positive control to its own
  zero: "`recover()` → 0, `defer ` → 163, `panic(` → 28, `func (` →
  813. The zero is a real absence, not a failed search."
- **`stack-go.md`, one dependency interface per file, at #638
  (`a6b06c0`) — violated at 16, priced at 6, repaired after 7 days 8
  hours.** This is the largest of the three and the only one where the
  adoption's own count was short.

  #638 **flipped** the rule rather than adding one. Before it,
  `stack-go.md` §1 put dependency interfaces *in* `service.go` — "and
  any secondary dependency interfaces it coordinates" — and promoted
  one to its own file only "when it grows and other domain files need
  it". #638 replaced that with "**Every dependency interface gets its
  own file**, named for the interface... No condition and no
  threshold." The tree had been written to the old rule.

  Measured at the adoption commit: **16 of 38** dependency interfaces
  under `internal/<domain>/` were not in a file named for them, across
  10 files. Under the narrower reading the pull request itself used —
  a file declaring more than one interface — **8 files** qualified, 7
  of them under `internal/<domain>/`.

  #638's Convention concerns field names the breach and prices it at
  "**six interfaces across two files**", reached by looking at
  `internal/authn/store.go` (four) and `internal/operator/store.go`
  (two). The five files it did not reach are
  `internal/agent/service.go`, `internal/delivery/service.go`,
  `internal/machine/service.go`, `internal/operator/service.go` and
  `internal/watch/service.go` — each declaring a dependency interface
  beside the domain's `Service`. Three more,
  `internal/authz/cache.go`, `internal/message/consent.go` and
  `internal/machineapi/server.go`, hold a single interface in a file
  named for something else, which only the "named for the interface"
  half reaches.

  The project corrected the number itself. **#910**, filed and closed
  2026-09-02, is titled "Sixteen dependency interfaces share a file..."
  and reaches 16 by the same route, excluding
  `internal/errorx/errorx.go` and `internal/health/health.go` as
  crosscutting on the same ground read independently here. #923
  (`bde40fa`, "refactor: give internal/ the file inventory stack-go.md
  §1 describes") took it to **0** on 2026-09-03T07:06:38+09:00, 7 days
  8 hours after the adoption at 2026-08-26T23:06:29+09:00. At
  `bb5431d` the count is **0 of 40**.

So on this half, as on #1017's: where an adoption put standing content
in violation, the pull request that made the adoption said so. Three
for three, and #1017 found four of its five the same way. What #638
adds is that **naming a breach and counting it are different acts** —
its author ran the check, named the right class, and returned a number
2.7 times short of the tree, because the sweep looked at `store.go` and
the violations were mostly in `service.go`.

## What stands violated today

- **`stack-go.md`'s "Its implementations go in
  `internal/<domain>/<name>/`" —
  24 files, and it was adopted knowing this.** The rule #638 adopted
  says an implementation lives in `internal/<domain>/<name>/`, "a
  package named for the interface rather than for the technology behind
  it". At `bb5431d` **no such package exists**, and 22 implementation
  files sit directly under `internal/<domain>/` as `postgres.go`,
  `postgres_org.go`, `postgres_group.go` and so on, across 9 domains —
  which is what the *previous* rule required. The domain count matches
  the "9 packages that do not exist" #638 recorded, arrived at here by
  a different route. Two further `postgres_*_internal_test.go` files
  under `internal/authz/` are tests and are excluded.

  This is a recorded decision, not an unnoticed gap, and the record is
  unusually complete. #638's Convention concerns field states it in
  bold — "**Adopting it makes this tree non-conforming, deliberately.**
  21 files, 2,531 LoC, 9 packages that do not exist" — reproducing
  #630's independent figures, and prices the move rather than
  proposing it: "7 of the 9 domains build their aggregates from
  unexported fields inside the store... the price is not 'export 6
  identifiers' but 'for 7 domains, either export the fields or add
  hydration constructors'. That confirms the PM's reading that the
  coupling is the interesting part and it is an architecture question.
  **It prices the tree move; it is not a proposal to make it.**"

  Checked before reporting it, per the discipline that
  `docs/architecture/README.md`'s "ADRs are append-only" makes
  necessary: **no ADR and no
  architecture document decides where a Go implementation file lives.**
  #606 ("Decided (B): garam moves to the template's structural
  arrangement") is closed and decides the adjacent question — where
  *structural documentation* lives, `arch-domain.md` against
  `structure.md` and `structures/` — not this one. ADR-0051 names
  `internal/org/postgres.go` only as evidence about aggregate
  accessors. #439 ("The template's Go interface-layout rules: what
  adopting them would cost garam") is closed.

  What is true today is that the breach is named in a merged pull
  request body and in a closed issue, and **no open issue carries the
  move**. The four open issues at `bb5431d` are #1007, #1008, #1019 and
  #1020, none of them this. Whether that wants an issue is the PM's,
  and this report does not file one.
- **Nothing else among the rules measured above.** At `bb5431d`: 0 of
  40 dependency interfaces misplaced, 0 unchecked type assertions, 0
  Markdown files failing the Contents check over 115 read, 4 of 5
  `FROM` lines digest-pinned with the fifth exempt, both features
  carrying both entry points, one mockery config, `GOTOOLCHAIN` set
  with `+auto`, and the tier rule enforced in the lint config. Each of
  those is a query shown above to return a different answer somewhere
  in this history.
- **The hard-wrap rule is not re-measured here.** #1017 recorded 141
  violations in 46 files at `4cc6ebc` and noted the rule is this
  project's own, not an adoption. It is named again only so that
  "nothing else stands violated" is not read as a claim about the whole
  corpus. It was not re-run at `bb5431d`.

## The hits that are not violations

Recorded because each query fired, and a bare count would read as a
violation count.

- **`FROM scratch`, the 1 of 5 unpinned base.** `scratch` is the
  reserved empty base rather than an image: it names no content, sits
  in no registry, and there is no digest to take. The rule's own
  ground — "a tag is a name that can be moved to different content" —
  does not reach it.
- **Two `//nolint:gosec` suppressions at `bb5431d`**, in
  `internal/machineapi/identity_test.go:65` and
  `internal/machineapi/server_test.go:292`, each carrying the same
  stated reason: "the listener's own certificate is not what is under
  test." The rule forbids a suppression that "does not check its own
  result" and silences a type mismatch. These are annotated, in tests,
  and against a security linter rather than the type system.
- **`internal/health/health.go` (`Dependency`, `Pinger`) and
  `internal/errorx/errorx.go` (`Error`).** The interface-per-file rule
  sits under the heading "**Rules for `internal/<domain>/`**", and the
  same file's layout lists `internal/<crosscutting>/` separately. These
  two are crosscutting packages. #910 excluded exactly these three
  interfaces on the same reading, reached here before that issue was
  found.
- **`internal/*/db/db.go` (`DBTX`), 9 files.** Generated by sqlc, and
  `docs/convention/README.md` §Scope — itself adopted at #318, one of
  these 31 — exempts a file "regenerating changes nothing".
- **`internal/authn/oauth_state_store.go` (`OAuthStateStore`).** The
  one apparent violation left at `bb5431d` is an artifact of the
  measuring script's snake-casing, which renders the interface
  `o_auth_state_store`; the file's `oauth_state_store` is the correct
  rendering. The true count at HEAD is 0 of 40, not 1 of 40.
- **Five `stack-nextjs.md` headings** flagged by the sentence-case
  candidate query at `bb5431d`: "2. App Router file conventions", "3.
  Server Component vs Server Action vs Route Handler", "4. Server
  Components — pages and layouts", "5. Server Actions", "6. Route
  Handlers — external use only". Every capitalised phrase is a
  framework proper noun.
- **#163 as a 45th adoption.** Covered under §The controls on the
  inherited enumeration.

## What was not measured

- **Whether the three violations were reported anywhere but their own
  pull request.** The bodies of #206, #638 and #643 were read, and
  issues #207, #630, #894, #910 and #606 were opened. Review comments
  were not read, and no other pull request body was.
- **`stack-nextjs.md`'s errors section (#293), beyond its headings.**
  Whether an expected failure comes back as a return value rather than
  a throw, whether `redirect()` is called outside `try`, and whether an
  error boundary covers what the rule says it covers are readable from
  `web/src/`, and reading them was not attempted here. Their verdict is
  *not measured*, not *satisfied*.
- **`testing.md`'s three-layer table as adopted at #49 (`bb50cea`).**
  What each layer may mock is checkable against the suites; the
  boundary between "mock any module boundary you control" and what the
  integration layer substitutes is not, and the two were not
  separated.
- **`documentation.md`, one file one responsibility (#128).** "A file
  that outgrows a single topic gets split by responsibility." Topic
  count is not a line count, and no attempt was made.
- **The multi-platform index clause (#769)**, for the reason given
  above: it wants a registry query.
- **Every rule the removals made moot was read once and not further.**
  Where a file was gone the rule was recorded as governing nothing and
  its content was not measured against anything else it might reach.

## What this does not settle

- **Whether any of this earns a rule.** It is not proposed, and
  `AGENTS.md` §What earns a rule says what it would take: a decision
  that divided for lack of the rule, met in more than one project — or
  in one, where the divergence belongs to the tool rather than the
  project. What the two reports now hold between them is **one project
  and two clear instances**: #379's mover rule from #1017, where the
  sync's author did not ask, and #638's count from this half, where the
  author did ask, named the class correctly, and got the number short
  by scoping the sweep to the wrong filename. Those are different
  failures. The first is not asking; the second is asking narrowly, and
  a rule addressing one would not have caught the other. Against them
  stand #603, #612, #206, #643 and #638 itself, where the author ran
  the check and wrote the answer into the pull request — five
  instances of the habit working, including on the very commit that
  supplies the counter-example. §What earns a rule also says a case
  that travelled between projects is one case, and that one instance is
  answered with what would change it. What would change it here is the
  same class met in a second project and reported as met, or a third
  instance in this one. That judgement is the PM's and the owner's.
- **What to do about the standing breach.** Naming it is this report's
  job and it is named above with its record. Whether garam moves
  `internal/` to `internal/<domain>/<name>/`, or the convention moves,
  is an architecture question #638 already priced and explicitly
  declined to propose.
- **Whether the unmeasurable rules are being followed.** This report
  says they cannot be settled by a query, not that they are unmet. A
  reviewer settles them, one pull request at a time, which is what
  #1019 asked to have stated rather than guessed.
- **Whether the 31 behave like the 13.** They do not, and the
  difference is the finding. #1017 measured 13 adoptions and found 5
  that left content in violation. This half measured 11 that carried a
  measurable rule at all — the other 20 govern nothing here, brought no
  rule, or want a reviewer — and found 3. The comparable statement is
  not "3 against 5" but that **roughly two-thirds of what a template
  adoption brings into this repository is not the kind of thing a query
  can be pointed at**, and that is the answer to what a rule here could
  ever enforce.
