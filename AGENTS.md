# AGENTS.md

This repository is developed by AI agents with three roles. This file contains only project-specific rules — generic agent behavior guidelines come from your global configuration; do not duplicate them here.

## Roles

One decision-maker merges; many workers implement; QA hunts for problems but fixes nothing. A single merge authority prevents concurrent work from being merged in conflicting directions.

| | PM (main branch) | Worker (task branches) | QA (read-only) |
|---|---|---|---|
| Modify source code | Forbidden unless the user explicitly instructs | Allowed | Forbidden |
| Run tests / builds | Forbidden | Allowed | Allowed — never commits |
| Read `docs/convention/` | Yes | Yes | Yes |
| Modify `docs/convention/` | Yes | Forbidden | Forbidden |
| Read `docs/architecture/` | Yes | Yes | Yes |
| Modify `docs/architecture/` | Yes | Allowed | Forbidden |
| Review / merge PRs | Yes — final decision authority | Forbidden | Forbidden |
| Create issues | Yes | Forbidden | Yes — with evidence |
| Edit / close issues | Yes | Forbidden | Forbidden |
| Comment on PRs / issues | Yes | Respond only | Forbidden |
| Delete remote branches | Merged or confirmed-stale only | Forbidden | Forbidden |

A session is bound to one role at launch with the tool's own `--agent <role>` flag; the role cannot be switched mid-session. Role adapters live in `.claude/agents/` and `.opencode/agents/`.

Detailed procedures: `docs/ai/pm-guide.md`, `docs/ai/worker-guide.md`, `docs/ai/qa-guide.md`. The PM dispatches workers into orca worktrees per `docs/ai/orca-dispatch.md`.

## Rules that apply to every agent

- Follow `docs/convention/` for all code. Conventions beat personal preference.
- Follow `docs/convention/documentation.md` for all documentation.
- Never commit secrets. Never push directly to `main`; all changes land via PR.
- Keep diffs surgical: every changed line traces to the assigned task.
- Match the user's language in conversation; code, comments, and documents are written in English.
- On conflict, precedence is: `docs/convention/` > this file > your global configuration.

## Bootstrapping a new project

If `docs/ai/BOOTSTRAP.md` has no completion checkmark, the project is uninitialized. The PM agent executes it before any other work.
