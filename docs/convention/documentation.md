# Documentation Conventions

What makes documentation valid in this project. Applies to every file under `docs/` and to GitHub artifacts (issues, PRs, comments). Violations are grounds for PR rejection. For review artifacts specifically, `review.md` adds stricter rules.

## Layout

- `README.md` — index of everything below.
- `convention/` — rules every contributor follows; PM-owned.
- `architecture/` — current system shape and the decisions behind it; `adr/` holds one file per decision.
- `plans/` — active work, then archived; `archive/` keeps completed plans for reference.
- `ai/` — agent-only procedures; never deleted.

Roles:

- **convention/** — "we write code like this." Enforced in review.
- **architecture/** — "the system looks like this, and here is why." Decisions rarely change; new ADRs are appended.
- **plans/** — "we are working on this now." Moved to `archive/` when the work ships or is dropped.
- **ai/** — operating procedures for agents (bootstrap, PM/worker/QA guides, this file's predecessor).

## Keep it maintainable

- **No ASCII art or text diagrams.** Boxes, arrows, and trees drawn with characters break on every edit. Express structure as bulleted lists of paths with one-sentence descriptions, or link to the code.
- **No implementation detail.** Code is the source of truth for how things work; docs record what exists and why. Do not transcribe function signatures, line-level behavior, or config dumps — they rot.
- **One file, one responsibility.** Split files by topic, not by length. A long file is fine while every section serves its single topic; such files keep a Contents list at the top.
- **Lists and tables over prose.** A rule scannable in 5 seconds beats a paragraph.

## Keep it current

- A PR that changes structure, workflow, or conventions updates the affected docs in the same PR. Stale docs are worse than missing docs.
- Delete docs that no longer describe anything real. Do not archive.
- Every claim in a doc must be verifiable in the repo. If you cannot point at it, remove it.
- Adding a file under `docs/` means adding it to the nearest `README.md` index in the same PR.

## Retention

- Completed plans in `plans/archive/` stay for reference; drop a year after they were moved there, not a year after the work shipped.
- `convention/` and `architecture/` files are not deleted when superseded; mark them at the top with `> DEPRECATED:` and a pointer to the replacement.

## GitHub artifacts

Issues, PRs, and comments are documentation too.

- **Facts only.** No rhetoric, no self-assessment, no inflated language ("perfect", "massive improvement"). State what changed, where, and why.
- **Stay inside the template.** No extra sections beyond the template fields; leave no field empty — write `N/A` with a reason.
- **One comment, one point.** A comment carries a single request, instruction, or question. Ground it by citing a path or a rule, not by arguing.
- **No decoration.** No emojis, badges, or ornamental headers.

## Format

- English, Markdown (GFM compatible), sentence-case headings.
- Start each file with one line stating what it governs.
- Direct tone: "We do X." Concrete examples beat abstract advice.
- Code blocks carry a language tag.
- Use file paths (`internal/authz/check.go`) instead of drawn hierarchies.
- File names: lowercase kebab-case (`runtime-safety.md`). Uppercase is reserved for fixed-entry files tools and humans look for first: `README.md`, `AGENTS.md`, `CLAUDE.md`.
