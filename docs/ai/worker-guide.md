# Worker Guide

Operating procedure for worker agents (task branches). You implement assigned work; the PM reviews it.

## Before writing code

1. Read the task. If it is ambiguous, ask — do not guess scope.
2. Read `docs/convention/README.md` and every convention file relevant to your change.
3. Read `docs/architecture/README.md` for the index, then the responsibility documents covering the modules you will touch. Read ADRs only when you need the context behind a decision.

## While working

- Work on a dedicated branch per `docs/convention/git.md`. Never commit to `main`. When the PM dispatches you into an orca worktree (`docs/ai/orca-dispatch.md`), the branch and worktree already exist — work in place; do not create another.
- Keep the diff surgical: no drive-by refactors, no reformatting untouched code, no unrequested features.
- Follow `docs/convention/` exactly. If a convention seems wrong for this case, implement per convention anyway and note the problem in the PR description — the PM decides.
- Write or update tests per `docs/convention/testing.md`.
- If your change alters module structure, update `docs/architecture/` accordingly, following `docs/convention/documentation.md`.

## Opening the PR

1. Run lint, format, and tests locally (the project's `Makefile` targets when present). All must pass.
2. Use the PR template. State what changed, why, which checks ran, and any convention concerns.
3. List the convention documents you actually read and applied (`Conventions consulted` field). Do not claim a document you did not read — the PM cross-checks claims against the diff.
4. Do not request review from or assign other agents; the PM picks PRs up.

## Responding to review

Follow the response rules in `docs/convention/review.md`: address or rebut every point, push fixes to the same branch.

## Reporting status

- At meaningful checkpoints, update your Orca card: `worktree set --worktree active --comment "<short status>" --workspace-status in-progress` — and `in-review` once the PR is open.
- If you were dispatched via Orca orchestration, report `worker_done` exactly once when a round completes, using the task and dispatch IDs from the injected preamble. Never report completion in prose only.
- Follow-up rounds arrive as a new injected dispatch or a prompt pointing at an issue/PR comment. Read the comment thread before resuming.

## Limits

Your permissions and prohibitions are defined by your agent adapter and the role matrix in `AGENTS.md` — they are not restated here.
