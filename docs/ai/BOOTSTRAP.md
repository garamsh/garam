# BOOTSTRAP.md — Project Initialization

> Status: **COMPLETE (2026-07-25)** — retrofitted onto an existing codebase; conventions and structure were adopted from the harness template after development had already begun.

Executed once by the PM agent on `main` after the project is created from the template. All changes land in a single bootstrap PR. If the project already exists and was not created from the template, start with `docs/ai/adoption.md` instead.

## Steps

1. **Identify the project.** Ask the user (or infer from existing code) the purpose, primary stack, and tooling (package manager, linter, formatter, test runner).
2. **Select stack conventions.** In `docs/convention/`, keep only the `stack-*.md` files matching the project's stack and delete the rest. If no file matches, write a new one following the style of the existing stack files — concise rules, no fluff.
3. **Adapt neutral conventions.** Review the stack-neutral files in `docs/convention/` (everything not prefixed `stack-`) and adjust anything that conflicts with the chosen stack. Do not pad them with restated content.
4. **Extend .gitignore.** Augment the template's base `.gitignore` with the stack's template from the `github/gitignore` collection (e.g. `Node.gitignore`, `Python.gitignore`). Verify the current template content via web fetch — do not write it from memory.
5. **Set up CI plumbing.** Create the `Makefile` and `lefthook.yml` described in `docs/convention/ci.md` with the project's real commands. Add a CI workflow if the project will run automated checks.
6. **Fill in CODEOWNERS.** Replace `@pm` in `.github/CODEOWNERS` with the GitHub user or team acting as PM.
7. **Initialize architecture docs.** Create initial responsibility documents under `docs/architecture/` (one per major domain or concern, following the skeleton in `docs/architecture/README.md`), record any initial design decisions as ADRs, and fill in the index in `docs/architecture/README.md`.
8. **Write the root README.md.** Introduce the project: what it is, who it is for, how to run it. Do not describe internal structure or modules — that belongs to `docs/architecture/`.
9. **Refresh the convention index.** Update `docs/convention/README.md` so the file list matches reality.
10. **Open one PR** titled `chore: bootstrap project conventions` containing all of the above, and merge it.
11. **Mark completion.** Change the status line at the top of this file to `COMPLETE` with the date.

## Rules

- Do not invent conventions beyond the chosen stack's needs; the template defaults are the baseline.
- Do not delete `docs/ai/` — these documents stay for the life of the project.
