# Simplicity Conventions

Clear, direct code that does the job — over clever or elaborate code. Complexity must earn its place.

## Core principle

Every abstraction names a real, repeated problem. An abstraction that hides no complexity behind it — or that hides a problem that occurs once — is itself complexity. Remove it.

## Rules

- **No speculative abstraction.** Do not build for imagined future needs: no single-use abstractions, no configuration options nobody asked for, no indirection layers for one caller. Add the abstraction when the second real case arrives, not before.
- **Consolidate with judgment.** When two features look similar: first verify whether they truly differ. If a unified module is more intuitive and does not need flags and conditionals everywhere, unify them. If unification requires branching at every turn, they are different in essence — keep them separate. A wrong abstraction is worse than honest duplication.
- **Clarity is testable.** If a competent engineer cannot understand a unit in a few seconds of reading, restructure it before shipping. Nesting depth, indirection hops, and the number of concepts a reader must hold at once are all reviewable.
- **Delete, don't wrap.** Prefer deleting dead code and dead paths over layering compatibility shims on top of them.

## Anti-patterns

- Wrapper or helper layers that pass everything through without adding meaning.
- "Generic" utilities with parameters for cases that do not exist.
- Frameworks inside the project: plugin systems, registries, and dispatch tables built before a second user exists.
- Deep call chains where each hop only forwards arguments.
