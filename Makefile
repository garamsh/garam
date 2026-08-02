OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
MOCKERY := go run github.com/vektra/mockery/v3@v3.7.2
BUF := go run github.com/bufbuild/buf/cmd/buf@v1.72.0
GOIMPORTS := go run golang.org/x/tools/cmd/goimports@v0.48.0

# Host port for the dev Postgres container. Override per worktree
# (e.g. GARAM_DB_PORT=55440 make db-up e2e) to run worktrees in parallel.
GARAM_DB_PORT ?= 55432
TEST_DATABASE_URL ?= postgres://garam:garam@localhost:$(GARAM_DB_PORT)/garam?sslmode=disable

.PHONY: lint format test build mock gen check-gen e2e db-up db-down ci

lint:
	go vet ./...
	$(BUF) lint

# gofmt and goimports list offending files but exit 0, so the gate is a
# non-empty-output check.
format:
	@out="$$(gofmt -l .)"; test -z "$$out" || { echo "gofmt: $$out"; exit 1; }
	@out="$$($(GOIMPORTS) -l .)"; test -z "$$out" || { echo "goimports: $$out"; exit 1; }

test:
	go test ./...

build:
	go build ./...

mock:
	$(MOCKERY)

# gen regenerates everything derived from a source of truth: the
# OpenAPI server, sqlc queries, protobuf code, and mocks.
gen:
	$(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
	$(SQLC) generate
	$(MOCKERY)
	$(BUF) generate
	$(GOIMPORTS) -w proto/gen

# check-gen fails when committed generated code is stale. The diff is
# scoped to generated paths so unrelated working-tree edits do not fail it.
GEN_PATHS := api/server.gen.go internal/*/db proto/gen internal/*/mocks

check-gen: gen
	git diff --exit-code -- $(GEN_PATHS)

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

# e2e runs the store round-trips against real Postgres (see db-up).
e2e:
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 ./tests/e2e/...

ci: format lint test build check-gen
