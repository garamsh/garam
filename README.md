# garam

Workflow management core: identity, organization, and authorization
for people and autonomous agents collaborating on shared work.

Documentation lives in [`docs/`](./docs/).

- [docs/README.md](./docs/README.md) — start here
- [docs/architecture/overview.md](./docs/architecture/overview.md) —
  what garam is and is not
- [docs/plans/roadmap.md](./docs/plans/roadmap.md) — where it is going
- [docs/convention/](./docs/convention/) — how we write code here

## Module path

`github.com/garamsh/garam`

## Build and test

```sh
go build ./...
go test  ./...
```

CI and local run the same commands (`make ci`). See
[docs/convention/ci.md](./docs/convention/ci.md).

## Development model

This repository is developed by AI agents under three roles (PM,
worker, QA). See [AGENTS.md](./AGENTS.md) for the role matrix and rules.