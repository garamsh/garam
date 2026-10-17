# ADR 0098: Garam syncs the conventions template to `b29f0f1`, and `structure.md` carries the composition-site rule

> Status: accepted
> Date: 2026-10-09

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Signs the decision was wrong
- Rejected alternatives

## Context

`Makefile`'s `TEMPLATE_SYNCED` named
`garamsh/convention-driven-project@f7de866`
([ADR-0096](./0096-garam-syncs-the-convention-template-to-f7de866.md)).
Two template commits follow it on the template's `main`:

- **`548895a` (#417), deciding template #415.** The composition-site
  rule leaves `stack-go.md` §Project envelope. It becomes a new section
  of the template's architecture index, §Rules every shape carries,
  which bootstrap copies into `structure.md` and then deletes. The
  bootstrap step in the template's `README.md` says so.
  `stack-go.md` keeps only the Go file that holds the site.
- **`b29f0f1` (#419).** `documentation.md` §Format gains a rule: a
  section whose territory abuts another document's states what it
  states. The template applies it in `stack-go.md`, `stack-nextjs.md`
  and `stack-fastapi.md`.

[README.md](../README.md) rule 4 and
[ADR-0061](./0061-adopting-a-rule-the-template-decided-is-a-decision-taken-here.md)
make the adoption the unit an ADR records. `b29f0f1` lands in
`docs/convention/` alone, which earns no ADR on its own. `548895a` does
not: it moves a structural rule from a convention file into
`structure.md`, and it adds a section to `docs/architecture/README.md`.

Garam bootstrapped long ago, and its `structure.md` already exists. The
template's PM gave the handling for a consumer in that position on
#415, and the PM adopted it on #1246: copy the three bullets into
`structure.md`, decline the section itself as `structures/` is
declined, and record the copy here.

## Decision

**garam takes both commits in `f7de866..b29f0f1`, with the handling
below.** The pull request that adds this ADR carries the per-commit
table. The rules stand in the files they landed in, and this ADR does
not restate them.

**`548895a` (#417) is taken in part, copied in part, and declined in
part.**

- **The `stack-go.md` hunk is taken verbatim.** The composition-site
  bullet goes, and "That site is" becomes "The composition site is",
  so the remaining bullet does not lean on the one removed.
- **The three bullets of §Rules every shape carries are copied into
  [structure.md](../structure.md) §Where the composition root lives.**
  That is what bootstrap does with them, and the bootstrap Garam ran
  predates them.
- **§Rules every shape carries itself is declined.** The template marks
  it bootstrap input that bootstrap deletes once `structure.md` carries
  its bullets. Garam's `structure.md` carries them as of this pull
  request, so the section would arrive already due for deletion. Rule 4
  declines `structures/` on the same ground. The hunk that names the
  section in the template index's `structures/` bullet is declined with
  it: Garam's index has no such bullet.
- **The `README.md` line is declined.** It rewrites the template's
  bootstrap step 4. Garam's `README.md` describes Garam and carries no
  bootstrap steps, so the line has nothing to land on.

**`b29f0f1` (#419) is taken for every file Garam keeps.** Each
application is matched to its section by content, since Garam's stack
files are adapted.

- **`documentation.md` §Format** takes the rule verbatim, after "Start
  each file with one line stating what it governs".
- **`stack-nextjs.md` §7 Errors** and **`stack-go.md` §7's Generated
  mocks** take the template's headings verbatim. Each section here
  holds what its heading names.
- **`stack-go.md` §3 Naming and §4 Error handling** take the template's
  opening sentences verbatim. Garam's §4 differs from the template's,
  through `errorx`, but it still states error forms and which layer
  wraps, logs, recovers or maps one.
- **`stack-go.md` §Project envelope takes the template's sentence with
  one clause added.** Garam's section also lists the seven directories
  this project adds to the layout. The template's sentence names only
  the composition site, `internal/` and `pkg/`, so verbatim it would
  misstate the section's scope, which is what the rule exists to state.
- **The `stack-fastapi.md` half has nothing to land on.** Garam keeps
  no such file.

**`TEMPLATE_SYNCED` moves to `b29f0f1`.** Both commits after
`f7de866` are taken, copied, or declined with a reason.

## What this does not decide

- **Which other sections of Garam's documents abut another document's
  territory.** The template applied the new rule in seven places, and
  this sync takes those. The rule binds the pull request that writes a
  section, so a section here meets it when it is next written.
- **The two local positions
  [ADR-0096](./0096-garam-syncs-the-convention-template-to-f7de866.md)
  keeps.** Neither commit touches `stack-go.md` §1 or the
  checked-against headers.

## Consequences

- **The composition-site rule is `structure.md`'s, and `stack-go.md`
  states only its Go path.**
  [ADR-0094](./0094-structural-rules-yield-only-to-what-a-framework-imposes.md)
  found `internal/cli` to be "the composition site that
  `stack-go.md` §Project envelope itself provides", so that the two
  files decided the case the same way. That consequence is restated
  here. Under rule 6 the rule now lives in `structure.md`, and
  `stack-go.md` spells it in Go. The one case that file still decides is
  where the site goes when a test has to reach it. Go imposes that case,
  since only a test in its own directory can import a main package, so
  rule 8 leaves it to the stack file. `internal/cli` remains the site,
  for the reason ADR-0094 gave: `tests/integration` reaches it through
  `cli.NewRootCommand()`.
- **A reviewer cites the rule against a Go change by `structure.md`.**
  A unit that constructs its own dependencies breaches
  `structure.md` §Where the composition root lives. Before this sync,
  the citation was `stack-go.md` §Project envelope.
- **This ADR pairs with [structure.md](../structure.md) and with the
  index.** The first now carries the rule, and the second lists this
  ADR.

## Signs the decision was wrong

- A later template commit changes the wording of
  §Rules every shape carries, and nobody here notices, because Garam
  holds the bullets in `structure.md` and not in the section the
  template edits. If that happens, a sync needs to read that section's
  history as well as the files Garam keeps.
- A reviewer cites `stack-go.md` to refuse a Go unit that constructs
  its own dependency, and cannot find the rule there. That would mean
  the route from `stack-go.md` to `structure.md` is too weak for a Go
  author to find.
- The added clause in §Project envelope's opening sentence is the only
  difference a later sync of that section meets, and it cannot tell
  whether the clause is a local position. If that happens, the
  adaptation belongs in a note beside the section rather than here.

## Rejected alternatives

- **Taking §Rules every shape carries into the index as well.** The
  rule would then sit in both the index and `structure.md`, and rule 6
  places it in `structure.md` alone.
- **Taking the §Project envelope sentence verbatim.** It would state a
  narrower scope than the section holds, so it would breach the rule
  the same commit adds.
- **Leaving the rule in `stack-go.md` and declining `548895a`.** That
  would keep a structural rule in a convention file, which rule 6
  places in `structure.md`, and it would leave Garam holding a
  position the template owner decided against on #415.
