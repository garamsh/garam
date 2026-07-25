# Git Conventions

How branches, commits, and PR titles are written.

## Branches

- `feat/<short-name>` — new behavior
- `fix/<short-name>` — bug fixes
- `chore/<short-name>` — tooling, config, dependencies
- `docs/<short-name>` — documentation only

One task, one branch. Branch from `main`; never commit to `main` directly.

## Commits

Format: `<type>: <imperative summary>`

- Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`
- Summary: imperative mood, lowercase, no trailing period. `fix: reject empty tokens`, not `fixed some bugs`.
- One concern per commit. If a change needs "and" to describe it, split it.

## PRs

- Title follows the commit format: `<type>: <imperative summary>`.
- Body follows `.github/PULL_REQUEST_TEMPLATE.md`.
- PRs are squash-merged; the squashed commit message follows the PR title. Keep the branch's intermediate commit history clean enough to review, but do not rewrite it to be pretty.
