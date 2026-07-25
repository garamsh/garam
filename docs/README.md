# gaon — docs

Start here. Every document is in English, Markdown, and lives next to
the code it describes.

## Conventions

Rules every contributor follows. PR review checks against these.

- [convention/README.md](./convention/README.md) — index
- [go.md](./convention/go.md)
- [errors.md](./convention/errors.md)
- [naming.md](./convention/naming.md)
- [comments.md](./convention/comments.md)
- [style.md](./convention/style.md)
- [runtime-safety.md](./convention/runtime-safety.md)
- [testing.md](./convention/testing.md)
- [git.md](./convention/git.md)
- [review.md](./convention/review.md)
- [documentation.md](./convention/documentation.md)
- [planning.md](./convention/planning.md)
- [ci.md](./convention/ci.md)

## Architecture

The system's shape and the decisions behind it.

- [architecture/README.md](./architecture/README.md) — index
- [architecture/overview.md](./architecture/overview.md)
- [architecture/domains.md](./architecture/domains.md)
- [architecture/grn-model.md](./architecture/grn-model.md)
- [architecture/authz.md](./architecture/authz.md)
- [architecture/adr/](./architecture/adr/) — decision records

## Plans

Active work, milestones, and current state.

- [plans/README.md](./plans/README.md)
- [plans/roadmap.md](./plans/roadmap.md)
- [plans/current-state.md](./plans/current-state.md)

## For agents

Operating procedures for AI agents working on this repository.

- [ai/pm-guide.md](./ai/pm-guide.md) — PM agent procedure
- [ai/worker-guide.md](./ai/worker-guide.md) — worker agent procedure
- [ai/qa-guide.md](./ai/qa-guide.md) — QA agent procedure
- [ai/BOOTSTRAP.md](./ai/BOOTSTRAP.md) — one-time project initialization
  (complete)

## Contributing

1. Read the convention directory end-to-end before the first PR.
2. Keep `architecture/` in sync with code changes.
3. New work that lasts more than a session gets a plan under `plans/`.
4. Local commands and CI commands are the same commands — see
   [ci.md](./convention/ci.md).
