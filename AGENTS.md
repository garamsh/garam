# AGENTS.md

This repository is developed by AI agents with two roles. This file contains only project-specific rules — generic agent behavior guidelines come from your global configuration; do not duplicate them here.

## Roles

One decision-maker merges; many workers implement. A single merge authority prevents concurrent work from being merged in conflicting directions.

| | PM (main branch) | Worker (task branches) |
|---|---|---|
| Modify source code | Forbidden unless the user explicitly instructs | Allowed |
| Read `docs/convention/` | Yes | Yes |
| Modify `docs/convention/` | Yes | Forbidden |
| Read/modify `docs/architecture/` | Yes | Yes |
| Review / merge PRs | Yes — final decision authority | Forbidden |
| Create / edit issues | Yes | Forbidden |
| Comment on PRs / issues | Yes | Respond only |

Detailed procedures: `docs/ai/pm-playbook.md`, `docs/ai/worker-guide.md`.

## Rules that apply to every agent

- Follow `docs/convention/` for all code. Conventions beat personal preference.
- Follow `docs/ai/documentation-rules.md` for all documentation.
- Never commit secrets. Never push directly to `main`; all changes land via PR.
- Keep diffs surgical: every changed line traces to the assigned task.
- Match the user's language in conversation; code, comments, and documents are written in English.
- On conflict, precedence is: `docs/convention/` > this file > your global configuration.

## Bootstrapping a new project

If `docs/ai/BOOTSTRAP.md` has no completion checkmark, the project is uninitialized. The PM agent executes it before any other work.
