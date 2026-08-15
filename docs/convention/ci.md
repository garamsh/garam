# CI Conventions

## Contents

- Core principle
- Local gate
- Make targets
- Workflow authoring
- Verify an action before adopting
- Security baseline
- Anti-patterns

## Core principle

Local commands and CI commands must be the same commands. `make ci`
runs locally and in GitHub Actions with no drift. **Never pick an
action or tool version from memory** — verify the current stable
version via webfetch first.

## Local gate

No git hooks are installed (lefthook evaluated and not adopted). Run
`make ci` before pushing; CI runs the same target and rejects what
local runs missed.

## Make targets

The living list is the `Makefile`. Shape:

```makefile
lint:     go vet ./... && buf lint
format:   gofmt / goimports (pinned via go run)
test:     go test ./...
build:    go build ./...
mock:     mockery
gen:      regenerate oapi-codegen, sqlc, buf, mockery (pinned tools)
check-gen: fail when committed generated code is stale
db-up / db-down: dockerised dev Postgres, one per machine; GARAM_DB_PORT
          moves that one server's host port (default 55432)
e2e:      store round-trips against real Postgres (needs db-up); runs
          against its own database, never two worktrees at once
ci:       format lint test build check-gen
```

## Workflow authoring

For each CI step:

1. **Check for an existing action** before writing inline `run:` blocks.
2. **Verify it** per the next section — version, inputs, usage.
3. **Use it as documented**, or write inline `run:` if no action exists.

`.github/workflows/ci.yml` runs `make ci` on every push and PR. No
parallel sets of commands: CI invokes Make targets, nothing else.

## Verify an action before adopting

Before pinning any action, `webfetch` its repo to determine:

1. **Current stable version** — the version an agent remembers may be
   deprecated or nonexistent.
2. **Exact inputs the version accepts** — verify against the version's
   own `action.yml` or README.
3. **Exact usage pattern** — follow the action's docs.

For non-trivial third-party actions, also check the GitHub Advisory
Database for known vulnerabilities in the candidate version.

## Security baseline

- **Minimal permissions**: `permissions: read-all` at the workflow top;
  grant `write` per job.
- **No plain-text secrets**: repo or environment secrets; OIDC for
  cloud deploys.
- **Avoid script injection**: pass untrusted input via `env:`, never
  interpolated into `run:`.
- **Mask derived values**: `::add-mask::` for values derived from
  secrets that may appear in logs.

## Anti-patterns

- Inline `run:` blocks for what an action already does.
- A `make` target that drifts from CI — local and CI must match.
- Pinning to mutable refs (`@main`). Use a SHA or tag.
- Long shell logic inside a `make` target — extract to `scripts/`.
- Reusing a workflow without auditing its contents — treat it like any
  other dependency.
