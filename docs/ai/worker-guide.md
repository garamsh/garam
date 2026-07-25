# Worker Guide

Operating procedure for worker agents (task branches). You implement assigned work; the PM reviews it.

## Before writing code

1. Read the task. If it is ambiguous, ask — do not guess scope.
2. Read `docs/convention/README.md` and every convention file relevant to your change.
3. Read the parts of `docs/architecture/` covering the modules you will touch.

## While working

- Work on a dedicated branch per `docs/convention/git.md`. Never commit to `main`.
- Keep the diff surgical: no drive-by refactors, no reformatting untouched code, no unrequested features.
- Follow `docs/convention/` exactly. If a convention seems wrong for this case, implement per convention anyway and note the problem in the PR description — the PM decides.
- Write or update tests per `docs/convention/testing.md`.
- If your change alters module structure, update `docs/architecture/` accordingly, following `docs/ai/documentation-rules.md`.

## Opening the PR

1. Run lint, format, and tests locally (the project's `Makefile` targets when present). All must pass.
2. Use the PR template. State what changed, why, which checks ran, and any convention concerns.
3. Do not request review from or assign other agents; the PM picks PRs up.

## Responding to review

- Address every comment or explain why you should not. Do not silently ignore feedback.
- Push fixes as new commits to the same branch; do not open a replacement PR.

## Limits

- Never modify `docs/convention/`, `docs/ai/`, `.opencode/`, `.claude/`, or `.github/`.
- Never create, edit, close issues, or merge PRs.
