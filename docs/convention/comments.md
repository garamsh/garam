# Comment Conventions

## When to comment

Add a comment where it helps a reader. Skip where the code is
self-evident. When in doubt, prefer to omit; an absent comment is
easier to add later than an absent explanation is to remember.

This governs inline comments. The doc comment an exported name
carries is required whether or not the name looks self-evident —
see below.

## Brevity

When you do comment, write the shortest version that conveys the
essential point. One short sentence beats a paragraph.

Long comments drift out of sync with the code they explain; short ones
age well. If a comment needs more than 2–3 sentences, refactor the
code first — extract the explanation into a doc comment on a named
function or type where readers can find it.

## Voice

Write comments in declarative or imperative mood, stating facts:
`returns the cached token`, `retries once on timeout`. Never narrate
("this will now…"), apologize ("unfortunately we have to…"), or
justify the author's choices ("I chose this because…") — rationale
belongs in the commit message or an ADR.

## Inline vs doc comments

**Inline** (inside function bodies) — earn their place when they flag a
non-obvious invariant, an ordering constraint, or an external
constraint a future reader would otherwise re-derive incorrectly. They
go above the line, not at the end.

**Doc comments** (above types, functions, modules) — required for every
exported name. Go style: the comment starts with the declared name.

```go
// AssignRole attaches role r to subject on scope.
func AssignRole(...) error
```

## Forbidden

- **Paraphrasing** (`// increment counter` above `i++`)
- **Narrating** (`// loop through users` above a for-loop)
- **Section dividers** (`// constructor` / `// getters` / `// setters`)
- **Wall-of-text** above a function — one line at the top of the body,
  or a doc comment on the type/module, is enough

## TODOs

`// TODO(name): …` with an owner (GitHub handle or email). `FIXME` /
`XXX` decay — prefer TODO.

## Language

English. All comments, identifiers, and docs.
