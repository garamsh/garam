# Git Conventions

How branches, commits, and PR titles are written, and what survives a merge.

## Branches

- `feat/<short-name>` — new behavior
- `fix/<short-name>` — bug fixes
- `chore/<short-name>` — tooling, config, dependencies
- `docs/<short-name>` — documentation only
- `refactor/<short-name>` — behavior-preserving restructure
- `test/<short-name>` — test-only changes

One task, one branch. Branch from `main`; never commit to `main` directly.

**Exception — Orca worktree branches.** Orca creates the branch and the worktree before a dispatched worker exists, naming it `<github-user>/<task>` — for example `sauce-git/console-mutations-110`. That name stands: the worker works in the worktree in place, and renaming a branch a dispatch already tracks breaks the link between them. The prefixes above govern every branch created by hand.

## Commits

Only the PR title reaches `main`, together with the trailer naming the squashed commits' authors. A branch's commit messages are otherwise a working record for the reviewer reading the branch, not the permanent history.

Format: `<type>: <imperative summary>`

- Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`
- Summary: imperative mood, lowercase, no trailing period. `fix: reject empty tokens`, not `fixed some bugs`.
- One concern per commit. An "and" in the title is not the test; what decides is what the "and" joins:
  - **A list inside one concern.** The rest of the title holds for everything joined — one verb over several objects, or several verbs over one object. `refactor: bring six identifiers, three log calls and one assertion onto the forms stack-go.md states`, `feat: grant, see and withdraw consent from the console`. Leave it whole; splitting a list produces worse commits, not better ones.
  - **Two concerns.** Each side carries its own verb and its own object, and either would stand as the whole title. `docs: record the chart version decision and define the ProvisioningState members`. Split it.
  - **Which of the two a title is, a reviewer decides and nothing else does.** The shapes above are what a writer reads first, and two unrelated changes can be written to share a verb. What a reviewer reads the diff for is whether one reason put every part of the change there; parts that do not share one are two concerns however the title is worded.
- Omit the body unless the reason for the change is invisible in the diff. A body never restates what changed.
- The message contains only what the project put there — no line a tool appended unasked. A `Co-authored-by:` naming an author of the squashed commits is not such a line: squash drops those authors and the trailer is where they survive.
- Reversing an earlier decision mid-branch is an ordinary commit describing the new state. Earlier commits are not rewritten to hide that the decision changed.
- A point about a commit message is fixed in the commit that carries it — amend or rebase, then force-push the same branch. A later commit cannot remove text from an earlier one, so it cannot carry that fix. This is the one case where rewriting a branch commit is right.

## PRs

- Title follows the commit format: `<type>: <imperative summary>`. It becomes the commit on `main`, so it is the permanent record of the change.
- Body follows `.github/PULL_REQUEST_TEMPLATE.md`.
- PRs are squash-merged. Branch commits do not appear on `main`; do not rewrite them to be pretty.
