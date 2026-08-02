# Adoption — Applying and Updating the Harness Template

How an agent brings the harness template into its own project and keeps it current. Executed by the PM agent; lands via PR.

## Principles

- **The harness repo is a read-only source.** The project keeps its own origin and history; the harness remote is only a file supplier. No merges, no shared history.
- **Verbatim first.** Take template file contents as-is. Modify only where the project genuinely requires it (paths, stack, ownership) — and record every adaptation in the PR body.
- **Selective, not wholesale.** Take only the files the project needs. Conventions especially: only the relevant `stack-*.md` and the neutral files the project will actually enforce.
- **The project owns its file tree.** The template never requires deleting or reorganizing existing files. Adding and managing files is the project's own decision.

## Reading the source

```bash
git remote add harness <template-repo-url>   # skip if already added
git fetch harness                            # updates refs/remotes/harness/* only
```

- Read a file without touching the worktree: `git show harness/main:<path>`.
- Copy a file verbatim into the worktree: `git checkout harness/main -- <path>`.
- No remote wanted: `git clone --depth 1 <url> /tmp/harness` and read from there instead. Re-add the remote when updating.

## First-time application

Pre-flight: `git status` clean; work on a `chore/adopt-harness` branch.

1. **Choose the set.** Core: `AGENTS.md`, `CLAUDE.md`, `docs/ai/`, the agent adapters the project uses (`.opencode/agents/`, `.claude/agents/`). Conventions: `docs/convention/` neutral files the project will enforce + only the matching `stack-*.md`. Skip the rest — including the harness `README.md` (the project keeps its own).
2. **Copy verbatim** per the commands above. If `docs/` or `.github/` already exists, copy only the missing subpaths; never overwrite existing project files.
3. **Adapt the touch points.** `.gitignore`: append missing lines, keep existing ones. `CODEOWNERS`: replace `@pm`. Anything else: verbatim.
4. **Verify**: `git status` shows only the intended paths; no project file was modified unexpectedly.
5. **Continue with `docs/ai/BOOTSTRAP.md` from step 1** — survey the existing code to pick stacks, and write architecture documents from the existing modules. Adapt the project README; do not replace it.

## Updating to a newer harness version

1. `git fetch harness`. Compare per file: `git diff main harness/main -- <path>`, or read the incoming version with `git show harness/main:<path>`.
2. Classify before adopting:

| Category | Paths | Action |
|---|---|---|
| Take verbatim | `docs/ai/`, agent adapters, `.github/` templates | `git checkout harness/main -- <path>` |
| Compare hunks | neutral `docs/convention/` files, `AGENTS.md` | adopt improvements, keep deliberate local changes |
| Never touch | `docs/architecture/`, `README.md`, `stack-*.md`, source | project-owned |

3. Open one PR titled `chore: sync harness template updates`. Body lists what was adopted, adapted, and deliberately skipped — skipped items are not re-proposed on later syncs.
4. After merging, invoke the QA agent once to verify the updated docs reference no missing files.

## Rules

- Never apply or update during active feature work — wait for a quiet `main`.
- A file the project deleted stays deleted; the template does not resurrect it.
- If the project drifted far from the template, re-run the relevant BOOTSTRAP steps instead of hunk-by-hunk merging.
