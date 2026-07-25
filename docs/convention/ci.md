# CI Conventions

## Principle

Local commands and CI commands are the same commands. `make ci` runs
locally and in GitHub Actions with no drift.

## Local gate

No git hooks are installed. Run `make ci` before pushing; CI runs the
same target and rejects what local runs missed.

## Make targets

```makefile
.PHONY: lint format test build mock ci

lint:
	go vet ./...

format:
	gofmt -l .
	go run golang.org/x/tools/cmd/goimports@v0.48.0 -l .

test:
	go test ./...

build:
	go build ./...

mock:
	mockery

gen:
	# regenerate oapi-codegen, sqlc, mockery output (pinned tool versions)

check-gen: gen
	# fail when committed generated code is stale

db-up / db-down:
	# dockerised dev Postgres

e2e:
	# store round-trips against real Postgres (needs db-up)

ci: format lint test build check-gen
```

## GitHub Actions

`.github/workflows/ci.yml` runs `make ci` on every push and PR. It
uses the same setup action and the same inputs as documented.

- Pin actions to a SHA or tag, not `@main`.
- Verify the pinned version's `action.yml` before adoption
  (`webfetch`, not memory).
- For non-trivial third-party actions, check the GitHub Advisory
  Database.

## Security baseline

- `permissions: read-all` at the workflow top; grant `write` per job.
- No plaintext secrets. Use repo/environment secrets or OIDC.
- Pass untrusted input via `env:`, never interpolated into `run:`.
- Mask values derived from secrets with `::add-mask::`.

## Anti-patterns

- Inline `run:` blocks where an action already exists.
- A `make` target that drifts from CI.
- Mutable refs (`@main`).
- Documentation inputs that don't match the action's own `action.yml`.