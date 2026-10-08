# ADR 0094: Structural rules yield only to what a framework imposes, and garam takes five more template changes with that one

> Status: accepted
> Date: 2026-10-08

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[README.md](../README.md) rule 8 read, at `origin/dev` `7d67c88`:
"Structural rules rank between the convention tiers. Where
`structure.md` and a convention file decide the same case differently,
the stack-specific file that applies governs". Two cases turned on it.

- **`web/`.** `docs/convention/stack-nextjs.md` §13 said a feature "may
  not import from `app/` or from another feature", and
  `web/eslint.config.mjs` refused every such import. Against it,
  [structure.md](../structure.md) §Boundaries and dependency direction
  says a boundary reaches a sibling directly through the surface that
  sibling declares
  ([ADR-0047](./0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)).
  Under that rule 8 the stack file won, so ADR-0047 did not hold inside
  `web/`. `npx eslint` with the `7d67c88` config refuses
  `import { listProviders } from "@/features/auth"` placed in
  `web/src/features/orgs/`, which is an import through the declared
  surface.
- **The composition root.** [structure.md](../structure.md) §Where the
  composition root lives puts garam's in `internal/cli`.
  `docs/convention/stack-go.md` §Project envelope puts construction in
  `cmd/<binary>/main.go` and nowhere else. That section cited rule 8 to
  say the stack file governs, and recorded `internal/cli` as a
  divergence (#756).

`garamsh/convention-driven-project`'s PM reported the first case on that
repository's #409. That report is where #1230 comes from. The template
had changed rule 8 the same day.

## Decision

**garam takes in the following template commits, with the deviations
stated.** Each one is named by its merge commit in
`garamsh/convention-driven-project`.

- **`b02fe37` (#377), taken verbatim.** It rewrites rule 8: a
  structural rule yields to a stack file only where that file decides
  the case as its framework imposes it. It also adds to
  `docs/convention/README.md` §Stack-specific the sentence saying what a
  stack file states of a structural rule. This is the change this ADR
  is owed for under rule 4 and
  [ADR-0061](./0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md):
  it decides two cases differently from the record before it.
- **`8e32768` (#409), taken for `stack-nextjs.md` verbatim.** The tiers
  in §13 go, the one rule in them that was not structural moves to §12,
  and §13 is renamed. The commit's `stack-fastapi.md` half has nothing
  to land on, because garam keeps no such file.
- **`dace7e1` (#366), taken verbatim.** Whoever merges ensures the
  squash commit carries a `Co-authored-by:` for each author of the
  commits behind it. `git.md` already exempted that trailer from its
  rule against lines a tool appends, and this turns the exemption into
  an obligation.
- **`2f625b6` (#380), taken verbatim.** `git.md` names the default
  branch instead of `main`. On `garamsh/garam` that branch is `dev`.
- **`db30fcb` (#368) and `dc78d64` (#389), taken verbatim.** These are
  `documentation.md` §GitHub artifacts' rule that a claim names a basis
  that could have contradicted it, and `ci.md`'s rule that a change
  shows the whole check set ran before it landed.

**The two cases in §Context now follow from the new rule 8.**

- **`web/`.** It is governed by `structure.md`, and a feature reaches a
  sibling through that sibling's `index.ts` or `actions.ts`.
  `web/eslint.config.mjs` refuses an import into a sibling's internals
  and admits one through those two files. That is the same zone it
  already drew around `app/`.
- **The composition root.** Go imposes no site for construction:
  `internal/cli` builds and runs. So `structure.md` governs that case,
  `internal/cli` is the composition root by its rule, and the
  divergence record ends.

## What this does not decide

- **Whether `stack-go.md` should name a property rather than a path.**
  That question is still open with the template's convention owner, as
  #756 put it.
- **The template commits between `TEMPLATE_SYNCED` (`6fbfd5d`) and
  `8e32768` that are not named above.** One of them is `3152373`
  (#388), which rewords the sentence `b02fe37` added to the conventions
  index. None of them is taken here. So `TEMPLATE_SYNCED` stays where
  it is: moving it would claim them received.
- **The adoptions into `docs/convention/` on their own.** Under rule 4,
  a rule there earns no ADR. They are named here because ADR-0061 makes
  the adoption, not the rule, the unit.

## Consequences

- **A stack file no longer outranks `structure.md` on a case its
  framework does not impose.** A reviewer who meets the two disagreeing
  asks one thing: does the alternative fail to build or run?
- **`web/` can draw a feature-to-feature edge without an ADR.** The
  edge must go through the declared surface, and the lint rule enforces
  that.
- **This ADR pairs with the index, with [web-app.md](../web-app.md) and
  with [structure.md](../structure.md).** Rule 8 is the index's
  ([ADR-0062](./0062-an-adr-deciding-a-rule-of-the-index-pairs-with-the-index.md)).
  The other two each held a statement this ADR made false: the first
  that `stack-nextjs.md` governs which tier may import which, the
  second that rule 8 puts `stack-go.md` above it.

## Rejected alternatives

- **Keeping the old rule 8 and taking `8e32768` alone.** The `web/`
  contradiction goes, but its cause stays: any later stack-file rule
  would outrank `structure.md` again on a case the framework does not
  impose. That is the condition the template PM named on #409.
- **Taking the new rule 8 and keeping the lint zone.** ADR-0047 would
  hold in the documents and stay overridden in the build. A working
  artifact that enforces a rule no document states is the drift
  [ADR-0058](./0058-a-responsibility-document-routes-to-the-artifact-that-enforces-a-rule.md)
  routes against.
