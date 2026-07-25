# Comment Conventions

## When

Skip comments when the code is self-evident. Prefer absence over noise.
Add a comment where it would otherwise take the reader a minute to
re-derive.

**Required docstring:** every exported type, function, method, constant.
**Helpful inline:** non-obvious invariant, ordering constraint, external
constraint, deliberate trade-off.

## How short

One sentence usually. Past two or three sentences, refactor the code
and move the explanation to a docstring on a named function or type.

## Voice

Write comments in declarative or imperative mood, stating facts:
`returns the cached token`, `retries once on timeout`. Never narrate
("this will now…"), apologize ("unfortunately we have to…"), or
justify the author's choices ("I chose this because…") — rationale
belongs in the commit message or an ADR.

## Format

- Doc comments start with the declared name.

  ```go
  // AssignRole attaches role r to subject on scope.
  func AssignRole(...) error

  // Subject is anything that can act.
  type Subject interface { ... }
  ```

- One line when possible. Continuation lines are indented.

  ```go
  // NewChecker wires a Checker to the given role store and evaluator.
  // Both are required.
  func NewChecker(...)
  ```

- Inline comments go above the line, not at the end.

## Forbidden

- Paraphrasing the next line.
- Narrating a loop or branch.
- Section dividers (`// constructor`, `// getters`).
- Walls of text above a function.

## TODO

`// TODO(<owner>): <text>` — owner is a GitHub handle or email.
`FIXME` and `XXX` are discouraged; convert or fix on sight.

Open TODOs are tracked in the backlog section of
[`docs/plans/current-state.md`](../plans/current-state.md).

## Language

English. All comments, identifiers, and docs. Localisation is a
per-project exception, not the default.