# Conventions

Rules every contributor follows. PR review checks against this index. The PM owns these files; workers read and apply them.

## Stack-specific (Go)

- [go.md](./go.md) — layout, packages, naming, dependencies
- [errors.md](./errors.md) — error kinds, sentinels, wrapping
- [naming.md](./naming.md) — identifiers, GRN, abbreviations

## Stack-neutral

- [comments.md](./comments.md) — docstrings, inline comments, TODO
- [style.md](./style.md) — naming and pattern consistency in code
- [simplicity.md](./simplicity.md) — clarity over complexity; consolidation judgment
- [runtime-safety.md](./runtime-safety.md) — types, boundaries, error handling
- [testing.md](./testing.md) — test layers, naming, mocking
- [git.md](./git.md) — branches, commits, PR titles, merging
- [review.md](./review.md) — what makes a review valid; how authors respond
- [documentation.md](./documentation.md) — what makes documentation valid
- [planning.md](./planning.md) — PLAN.md structure for non-trivial work
- [ci.md](./ci.md) — local ↔ CI parity, pre-commit hooks

## Independence

Each convention file is self-contained: reading it alone is enough to apply its rules. Cross-references may mark scope boundaries but never hold required content. No rule appears in two files.

## Precedence

1. The stack-specific files, when one applies.
2. The stack-neutral files.
3. This index.

On conflict, the more specific rule wins. If existing code contradicts a
rule, follow the code and record the deviation in `plans/`. Report
conflicts to the PM in the PR description — do not resolve them yourself.
