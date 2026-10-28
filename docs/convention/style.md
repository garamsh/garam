# Style Conventions

Consistency in how code is written. A formatter owns layout; these rules
own what formatters cannot judge.

## Naming consistency

These rules state which names must agree, not their concrete form:
casing, affixes, a file name's spelling, or the names a stack bans.

- **Same concept, same name.** Pick one term per domain concept and use
  it everywhere. Mixing `user` and `member` for one thing is a defect,
  not a choice.
- **Same kind, same shape.** Functions doing similar work follow the
  same verb pattern (`getUser`, `getOrder` — not `getUser`,
  `fetchOrder`).
- **Shared concept, named file.** When two helpers share a concept,
  give the concept a name and name their file for it. The path then
  says what the file does, not that it holds helpers.
- **A path names its responsibility, at every depth.** A reader — a
  person or an agent — tells what a folder or a file is responsible
  for from its path alone: `decision/`, `grant/`, `outbox.rs`,
  `operator-table.tsx`.
- **No catch-all name.** `common`, `shared`, `util`, `utils`,
  `helpers`, `misc`, `core` and `base` name no responsibility, so
  anything lands in them; none is a folder or a file name. A unit with
  no nameable responsibility is two units.
- **A folder grouping packages is not a catch-all.** `crates/` says
  what it holds, a set of packages, and each package in it is named
  for its own responsibility (`crates/decision/`).
- **A name a tool imposes is the tool's.** A folder or file the tool
  requires by that name keeps it, and only where the tool requires it;
  the stack file names its tool's.

## Pattern consistency

- **Same problem, same solution shape.** When an earlier module solved a
  problem one way, later modules solve it that way too. Introduce a new
  pattern only when the existing one demonstrably cannot solve the case
  — and say so in the PR description.
- **Copy the local idiom.** New code mimics the conventions of the
  module it lands in.

## Priority

Module's existing conventions > project conventions > personal taste.
"My way is better" is never a valid reason for divergence; propose the
change instead.

## Boundaries

- No style-only diffs. Restyling existing code to your taste is a scope
  violation; changing the established style project-wide is a `proposal`
  issue, not a drive-by edit.
