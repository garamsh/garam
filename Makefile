OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
MOCKERY := go run github.com/vektra/mockery/v3@v3.7.2

TEST_DATABASE_URL ?= postgres://gaon:gaon@localhost:55432/gaon?sslmode=disable

.PHONY: lint format test build mock gen check-gen e2e db-up db-down ci

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
	$(MOCKERY)

# gen regenerates everything derived from a source of truth: the
# OpenAPI server, sqlc queries, and mocks.
gen:
	$(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
	$(SQLC) generate
	$(MOCKERY)

# check-gen fails when committed generated code is stale.
check-gen: gen
	git diff --exit-code

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

# e2e runs the store round-trips against real Postgres (see db-up).
e2e:
	GAON_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 ./e2e/...

ci: format lint test build check-gen
