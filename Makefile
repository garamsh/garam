# `go run tool@version` takes the toolchain from the tool's own go
# directive, which can be older than this module's and then cannot load
# its packages. The floor is the version go.mod declares; `+auto` still
# upgrades for a tool that needs a newer one.
export GOTOOLCHAIN := go$(shell sed -n 's/^go //p' go.mod)+auto

OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
MOCKERY := go run github.com/vektra/mockery/v3@v3.7.2
BUF := go run github.com/bufbuild/buf/cmd/buf@v1.72.0
GOIMPORTS := go run golang.org/x/tools/cmd/goimports@v0.48.0

# Host port for the dev Postgres container. Override it as an
# environment variable (GARAM_DB_PORT=55440 make db-up e2e): this file
# does not read deploy/local/.env, so a value set only there moves
# compose and not the URL below.
#
# One dev Postgres per machine, not per worktree:
# deploy/local/docker-compose.yml pins `name: garam-local`, so every
# worktree addresses the same compose project and container, and this
# variable moves that one server rather than adding a second.
GARAM_DB_PORT ?= 55432

# The e2e suite truncates every table and migrates from its own
# migrations/, so it gets a database per worktree on that one server.
# The suffix is git's own name for a linked worktree — unique per
# worktree and fixed for its lifetime — and empty in the main checkout,
# which keeps garam_test. The local stack keeps `garam`.
TEST_DB_NAME ?= garam_test$(shell git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')
TEST_DATABASE_URL ?= postgres://garam:garam@localhost:$(GARAM_DB_PORT)/$(TEST_DB_NAME)?sslmode=disable

COMPOSE_LOCAL := docker compose -f deploy/local/docker-compose.yml
PNPM := pnpm --dir web

.PHONY: lint format test build mock gen check-gen e2e db-up db-down backend-up ci
.PHONY: web-install web-lint web-format web-test web-build web-gen web-check-gen web-e2e web-ci

lint:
	go vet ./...
	$(BUF) lint

# gofmt and goimports list offending files but exit 0, so the gate is a
# non-empty-output check.
format:
	@out="$$(gofmt -l .)"; test -z "$$out" || { echo "gofmt: $$out"; exit 1; }
	@out="$$($(GOIMPORTS) -l .)"; test -z "$$out" || { echo "goimports: $$out"; exit 1; }

# go has no negative package pattern, so the sweep is filtered here.
# tests/e2e needs the Postgres db-up starts and fails without it, so test
# covers every other package and names the one it left out. Should the
# filter ever stop matching, the e2e suite is swept back in and fails for
# want of a database rather than skipping quietly.
TEST_PKGS = $(shell go list ./... | grep -v '/tests/e2e$$')

test:
	@echo 'test: $(words $(TEST_PKGS)) packages; tests/e2e not covered here — it needs Postgres (make db-up e2e)'
	@go test $(TEST_PKGS)

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

# check-gen fails when generated code is stale or uncommitted. It is scoped
# to generated paths so unrelated working-tree edits do not fail it.
GEN_PATHS := api/server.gen.go internal/*/db proto/gen internal/*/mocks

# git diff sees tracked paths only, so a generated file nobody added is
# invisible to it. git status reports modified and untracked entries, skips
# ignored ones, and exits 0 either way, so the gate checks for output.
check-gen: gen
	@out="$$(git status --porcelain -- $(GEN_PATHS))"; \
		test -z "$$out" || { echo "check-gen: uncommitted generated output:"; echo "$$out"; exit 1; }

# db-up starts the stack's postgres service alone and makes sure the e2e
# database exists next to `garam`. Postgres has no CREATE DATABASE IF NOT
# EXISTS, so ask before creating. The e2e suite migrates it itself.
db-up:
	$(COMPOSE_LOCAL) up -d --wait postgres
	@$(COMPOSE_LOCAL) exec -T postgres psql -U garam -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$(TEST_DB_NAME)'" | grep -q 1 \
		|| $(COMPOSE_LOCAL) exec -T postgres createdb -U garam $(TEST_DB_NAME)

db-down:
	$(COMPOSE_LOCAL) rm -sf postgres

# backend-up starts the backend `make web-e2e` drives — postgres, the
# one-shot migration, authz and api — from the same compose file, and
# returns once api answers. The profile-gated `web` service stays down:
# the browser suite builds and serves the app itself.
backend-up:
	$(COMPOSE_LOCAL) up -d --wait api

# e2e runs the store round-trips against real Postgres (see db-up).
e2e:
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 ./tests/e2e/...

ci: format lint test build check-gen

# ---------------------------------------------------------------------
# web/ — the Next.js frontend (ADR-0009). It keeps its own toolchain and
# lockfile; `web-ci` is the gate CI runs alongside `ci`.
# ---------------------------------------------------------------------

web-install:
	$(PNPM) install --frozen-lockfile

web-lint:
	$(PNPM) lint
	$(PNPM) typecheck

web-format:
	$(PNPM) format

web-test:
	$(PNPM) test

# `next build` runs with NODE_ENV=production, where env.ts requires
# GARAM_API_URL (the `/api/*` rewrite is baked into the build). Nothing
# calls the backend during the build, so a placeholder is enough.
web-build:
	GARAM_API_URL=http://localhost:8080 $(PNPM) build

# web-gen regenerates the API types the frontend consumes from the
# OpenAPI spec, which stays the contract's source of truth.
web-gen:
	$(PNPM) gen:api

web-check-gen: web-gen
	git diff --exit-code -- web/src/types/api.ts

# web-e2e drives the built app in a real browser against the backend
# `make backend-up` starts. It builds and serves the app itself, so it
# needs GARAM_API_URL — the real api here, not `web-build`'s placeholder
# — and the browser, which Playwright keeps outside the lockfile.
web-e2e:
	$(PNPM) exec playwright install chromium
	GARAM_API_URL=http://localhost:8080 $(PNPM) e2e

web-ci: web-format web-lint web-test web-build web-check-gen
