# Documentation Conventions

What makes documentation valid in this project. Applies to every file under `docs/` and to GitHub artifacts (issues, PRs, comments). Violations are grounds for PR rejection. For review artifacts specifically, `review.md` adds stricter rules.

## Keep it maintainable

- **No ASCII art or text diagrams.** Boxes, arrows, and trees drawn with characters break on every edit. Express structure as bulleted lists of paths with one-sentence descriptions, or link to the code.
- **No implementation detail.** Code is the source of truth for how things work; docs record what exists and why. Do not transcribe function signatures, line-level behavior, or config dumps — they rot.
- **One file, one responsibility.** Split files by topic, not by length. A file that outgrows a single topic does not get a Contents list as a remedy — it gets split by responsibility, with each piece serving one topic.
- **Lists and tables over prose.** A rule scannable in 5 seconds beats a paragraph.

## Keep it current

- A PR that changes structure, workflow, or conventions updates the affected docs in the same PR. Stale docs are worse than missing docs.
- Delete docs that no longer describe anything real. Do not archive.
- Every claim in a doc must be verifiable in the repo. If you cannot point at it, remove it.
- Update an existing document when the topic is already covered there; do not create a parallel document for a new facet of the same topic. Total document volume is a managed cost — a new file earns its place by adding a topic no existing file owns.

## GitHub artifacts

Issues, PRs, and comments are documentation too.

- **Facts only.** No rhetoric, no self-assessment, no inflated language ("perfect", "massive improvement"). State what changed, where, and why.
- **Stay inside the template.** No extra sections beyond the template fields; leave no field empty — write `N/A` with a reason.
- **One comment, one point.** A comment carries a single request, instruction, or question. Ground it by citing a path or a rule, not by arguing.
- **No decoration.** No emojis, badges, or ornamental headers.

## Format

- English, Markdown, sentence-case headings.
- Start each file with one line stating what it governs.
- Use file paths (`internal/authz/check.go`) instead of drawn hierarchies.
- File names: lowercase kebab-case (`runtime-safety.md`). Uppercase is reserved for fixed-entry files tools and humans look for first: `README.md`, `AGENTS.md`, `CLAUDE.md`.
