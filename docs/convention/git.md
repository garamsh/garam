# Git Conventions

How branches, commits, and PR titles are written, what survives a
merge, and how `dev` reaches `main`.

## Branches

- `feat/<short-name>` — new behavior
- `fix/<short-name>` — bug fixes
- `chore/<short-name>` — tooling, config, dependencies
- `docs/<short-name>` — documentation only
- `refactor/<short-name>` — behavior-preserving restructure
- `test/<short-name>` — test-only changes

One task, one branch. Branch from the default branch; never commit to
it directly.

**Exception — Orca worktree branches.** Orca creates the branch and the
worktree before a dispatched worker exists, naming it
`<github-user>/<task>` — for example `sauce-git/console-mutations-110`.
That name stands: the worker works in the worktree in place, and
renaming a branch a dispatch already tracks breaks the link between
them. The prefixes above govern every branch created by hand.

## Commits

A squash merge puts the PR title on the default branch and drops the
authors of the commits behind it. Whoever merges ensures the commit that
reaches the default branch carries a `Co-authored-by:` trailer for each
of them, and confirms it by reading that commit against the commits it
squashed rather than assuming the merge carried them over. A branch's
commit messages are otherwise a working record for the reviewer reading
the branch, not the permanent history: a line a tool appended to a
branch commit is neither required there nor to be removed.

An author is a person. An agent that wrote a change is the instrument of
its author, not a second author. The record that an agent wrote a change
is its pull request, so the commit that reaches the default branch names
that pull request in its subject.

Format: `<type>: <imperative summary>`

- Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`
  - A change that alters what an operation costs without altering what
    it returns is a `fix` when the cost grows with the deployment: the
    answer is right and how it scales is the defect. Speeding up an
    operation already bounded by its own answer is not one.
- Summary: imperative mood, lowercase, no trailing period.
  `fix: reject empty tokens`, not `fixed some bugs`.
- One concern per commit. An "and" in the title is not the test; what
  decides is what the "and" joins:
  - **A list inside one concern.** The rest of the title holds for
    everything joined — one verb over several objects, or several verbs
    over one object.
    `refactor: bring six identifiers, three log calls and one assertion onto the forms stack-go.md states`,
    `feat: grant, see and withdraw consent from the console`. Leave it
    whole; splitting a list produces worse commits, not better ones.
  - **Two concerns.** Each side carries its own verb and its own object,
    and either would stand as the whole title.
    `docs: record the chart version decision and define the ProvisioningState members`.
    Split it.
  - **Which of the two a title is, a reviewer decides and nothing else
    does.** The shapes above are what a writer reads first, and two
    unrelated changes can be written to share a verb. What a reviewer
    reads the diff for is whether one reason put every part of the
    change there; parts that do not share one are two concerns however
    the title is worded.
- Omit the body unless the reason for the change is invisible in the
  diff. A body never restates what changed.
- The commit that reaches the default branch contains only what the
  project put there — no line a tool appended unasked, whichever tool
  appended it and whatever the line says. A `Co-authored-by:` naming an
  author of the squashed commits is not such a line: the project asks
  for it.
- Reversing an earlier decision mid-branch is an ordinary commit
  describing the new state. Earlier commits are not rewritten to hide
  that the decision changed.
- A point about a commit message is fixed in the commit that carries it
  — amend or rebase, then force-push the same branch. A later commit
  cannot remove text from an earlier one, so it cannot carry that fix.
  This is the one case where rewriting a branch commit is right.

## PRs

- Title follows the commit format: `<type>: <imperative summary>`. It
  becomes the commit on the default branch, so it is the permanent
  record of the change.
- A change to a file other projects copy from this one that renames,
  renumbers, or removes a heading or a numbered rule names the old one
  in its pull request title. That title is what reaches a project taking
  the change, and nothing else tells it which of its citations to
  re-check.
- Body follows `.github/PULL_REQUEST_TEMPLATE.md`.
- A promotion is a PR from the default branch into a branch that only
  promotions move. It is merged by fast-forward, never by a squash, a
  rebase or a merge commit: each of those leaves that branch holding a
  commit the default branch never held. Its title becomes no commit;
  the commits it carries keep the titles and trailers their own squash
  merges gave them.
- PRs other than a promotion are squash-merged. Branch commits do not
  appear on the default branch; do not rewrite them to be pretty.

## Promotion

Every PR but the promotion PR targets `dev`. `main` moves only by this
procedure.

- **The PM promotes.** No other role moves `main`.
- **Due** before any release, and at least after each verified
  milestone.
- **The gate:** a PR from `dev` into `main`, which is what starts
  `ci.yml` and `images.yml` on the forge. Every job of both passes on
  the head being promoted. A run that was never dispatched is handled
  as `AGENTS.md` §Roles states.
- **The steps**, with `<n>` the promotion PR and `<head>` the commit
  its runs tested:
  1. Open the PR with base `main` and head `dev`.
  2. Read `<head>` and the checks on it:
     `gh pr view <n> --json headRefOid,statusCheckRollup`. Every
     `ci.yml` and `images.yml` job on `<head>` has passed.
  3. Confirm `main` is still an ancestor of `<head>`:
     `gh api repos/garamsh/garam/compare/main...<head> --jq .behind_by`
     answers `0`.
  4. Move `main` with a non-forced ref update:
     `gh api -X PATCH repos/garamsh/garam/git/refs/heads/main -f sha=<head> -F force=false`.
     This is the fast-forward the promotion is merged by: the forge
     refuses one that is not a fast-forward, and marks the PR merged
     once `main` holds its head.
  5. Read back that `main` is `<head>`:
     `gh api repos/garamsh/garam/git/refs/heads/main --jq .object.sha`.
- **Nothing else moves `main`:** no push, no merge of a PR into `main`
  but step 4's, no other ref update. Branch protection and rulesets are
  unavailable on this repository's plan (the API answered 403 on
  2026-10-08, #1215), so this rule is the whole of `main`'s defence. A
  fix `main` needs reaches it through `dev` and the next promotion.
- **A release tag is cut only on a commit `main` holds.** `release.yml`
  publishes whatever commit a `v*` tag names, so a tag on a commit only
  `dev` holds releases what the gate never passed.
- **A failed run is fixed on `dev`, not on the promotion PR.** No commit
  is pushed to the promotion PR and `main` does not move around it. An
  ordinary PR into `dev` carries the fix; its merge moves the promotion
  PR's head, and the gate is read again on the run that follows. The
  promotion PR stays open across that.
