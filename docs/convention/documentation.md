# Documentation Conventions

What makes documentation valid in this project. Applies to every
Markdown document in this repository, and to GitHub artifacts (issues,
PRs, comments). Violations are grounds for PR rejection.

## Keep it maintainable

- **No ASCII art or text diagrams.** Boxes, arrows, and trees drawn with
  characters break on every edit. Express structure as bulleted lists of
  paths with one-sentence descriptions, or link to the code.
- **No implementation detail.** Code is the source of truth for how
  things work; docs record what exists and why. Do not transcribe
  function signatures, line-level behavior, or config dumps — they rot.
- **One file, one responsibility.** Split files by topic, not by length.
  A file that outgrows a single topic does not get a Contents list as a
  remedy — it gets split by responsibility, with each piece serving one
  topic.
- **Lists and tables over prose.** A rule scannable in 5 seconds beats a
  paragraph.

## Keep it current

- A PR that changes structure, workflow, or conventions updates the
  affected docs in the same PR. Stale docs are worse than missing docs.
- Delete docs that no longer describe anything real. Do not archive.
- Every claim about this repository must be verifiable in it. If you
  cannot point at it, remove it.
- A claim about anything outside it names what it holds for, precisely
  enough that a reader can check it there: a release where the project
  publishes them, a commit where it does not. `deprecated in v2` can be
  checked against the library and `deprecated` cannot; a path and a line
  number with neither looks precise and goes on looking valid after the
  line moves.
- A file may state once at the top what it was checked against, and then
  every claim in it holds for that. That statement records a check; it
  promises nothing about later versions. A reader past it holds claims
  nobody checked there, and moving the statement means re-checking the
  claims under it rather than editing the number.
- Where the claim is about a package's health rather than a release
  (`unmaintained`, `legacy`), do not rest a rule on it: name what to use
  instead, which does not expire.
- A claim about the project that will carry this document cannot be
  checked from here, so it is written as an obligation and not as a
  description. "`findOneByOrFail` throws, which the global filter maps"
  asserts a filter this file has never seen; "`findOneByOrFail` throws;
  map it in the filter at X" is a rule the reader can act on and a
  reviewer can look for.
- Update an existing document when the topic is already covered there;
  do not create a parallel document for a new facet of the same topic.
  Total document volume is a managed cost — a new file earns its place
  by adding a topic no existing file owns.

## GitHub artifacts

These are documentation too.

- **Facts only.** No rhetoric, no self-assessment, no inflated language
  ("perfect", "massive improvement"). State what changed, where, and
  why.
- **Stay inside the template.** No extra sections beyond the template
  fields; leave no field empty — write `N/A` with a reason.
- **A closing keyword precedes each issue number.**
  `Closes #1, closes #2` links both; `Closes #1 and #2` links the first,
  and `Issue #1` links neither.
- **One comment, one point.** A comment carries a single request,
  instruction, or question. Ground it by citing a path or a rule, not by
  arguing.

## Format

Every rule in this file binds the pull request that writes a line rather
than the line that stands, so a document no pull request may write again
is held to these rules as they stood when it was written and to nothing
added since.

- English and Markdown. The H1 takes the case of what it is — a name in
  title case, an exact file or repository name as spelled, a statement
  in sentence case (an ADR's H1 states its decision). Every heading
  below it is sentence case.
- **Hard-wrap at 72 columns.** A paragraph is a run of lines that each
  break at or before column 72, never one long line: the line is what a
  diff, a `git blame` and a review citation address, and a paragraph
  written as one line is a change nobody can point inside of. A heading
  and a table row are each one line by construction. A list entry is
  neither: broken, it is still that entry, so it is a paragraph — its
  marker occupies the first line's opening columns and the lines under
  it indent to where that marker's text begins. Nesting changes that
  indent and nothing else. A line passes 72 only where what carries it
  there cannot be broken without becoming something else: a URL, a
  path, or a verbatim quotation of a commit title, a declaration, a
  command. A wrapped commit title is not that commit's title. A quoted
  sentence is not such a case and neither is prose — both read the same
  either side of a break, so a long line of either is a violation
  however awkwardly it breaks. A column is a character, not a byte: an
  em dash is one column and three bytes. Counting the lines past 72 is
  not this rule's check — every line the exception permits is one of
  them, so that count reports violations in a file that has none and
  diverges from the rule exactly where the exception applies.
  `scripts/check-line-width.sh` counts what the rule forbids.
- Start each file with one line stating what it governs.
- A `## Contents` list indexes a file's `##` headings, so how many of
  them there are decides whether it carries one and the file's length
  does not: more than eight, it opens with the list; three or fewer, it
  carries none; between the two, the writer decides. A Contents list is
  never the remedy for a file that has outgrown one topic — see §Keep it
  maintainable.
- A file whose headings come from a template owes no Contents list,
  however many it has. Those headings are the same in every file written
  from that template, so listing them tells a reader only what the
  template already told them.
- **No decoration.** No emojis, badges, or ornamental headers. Nothing
  these rules reach is held to a looser standard: a check-mark glyph in
  a table is decoration where `yes` is a word.
- Use file paths (`src/auth/service.ts`) instead of drawn hierarchies.
- File names: lowercase kebab-case (`runtime-safety.md`). Uppercase is
  reserved for files a tool or a reader looks for by an exact name it
  did not choose: `README.md`, `AGENTS.md`, `CLAUDE.md`, and the names
  GitHub requires under `.github/`.
