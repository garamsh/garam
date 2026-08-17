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

# Where the running api is published on this host, asked of compose at
# the moment it is used. GARAM_LOCAL_API_PORT moves that port, from
# deploy/local/.env as readily as from the environment, and a second
# copy of it here would be the copy that goes stale. Empty when nothing
# is published, which `web-e2e` reports rather than dials.
API_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port api 8080 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')

.PHONY: lint format test build mock gen check-gen e2e db-up db-down backend-up ci
.PHONY: web-install web-lint web-format web-test web-build web-gen web-check-gen web-e2e web-ci
.PHONY: minikube-up minikube-down minikube-start minikube-images minikube-db
.PHONY: minikube-secret minikube-migrate minikube-apps minikube-rebuild minikube-forward

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
	$(OAPI_CODEGEN) --config api/machine-codegen.yaml api/machine.yaml
	$(SQLC) generate
	$(MOCKERY)
	$(BUF) generate
	$(GOIMPORTS) -w proto/gen

# check-gen fails when generated code is stale or uncommitted. It is scoped
# to generated paths so unrelated working-tree edits do not fail it.
GEN_PATHS := api/server.gen.go api/machine/server.gen.go internal/*/db proto/gen internal/*/mocks

# git diff sees tracked paths only, so a generated file nobody added is
# invisible to it. git status reports modified and untracked entries, skips
# ignored ones, and exits 0 either way, so the gate checks for output.
#
# An unmatched pathspec is silent too, so a GEN_PATHS entry naming output
# that has moved or gone would read as a clean tree. ls fails on it first,
# and reports the count it did inspect.
check-gen: gen
	@paths="$$(ls -d $(GEN_PATHS))" && echo "check-gen: $$(echo "$$paths" | wc -l) generated paths inspected"
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
#
# It builds first, so what starts is the working tree rather than
# whatever was built last — the migrations included, since they travel
# in the same image. Nothing else here carries a source change: an
# image that already exists satisfies compose, which starts the stale
# one and reports it healthy. The build is cached, so an unchanged tree
# costs seconds; the containers are recreated either way, because the
# image id moves on every export whether or not a layer did.
backend-up:
	$(COMPOSE_LOCAL) up -d --build --wait api

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
	@test -n "$(API_HOST_ADDR)" || { echo "web-e2e: no api published on this host — run make backend-up" >&2; exit 1; }
	$(PNPM) exec playwright install chromium
	GARAM_API_URL=http://$(API_HOST_ADDR) $(PNPM) e2e

web-ci: web-format web-lint web-test web-build web-check-gen

# ---------------------------------------------------------------------
# deploy/local/minikube — the whole stack on a local Kubernetes cluster.
# Development only: every image is built into that cluster's own daemon
# and nothing is pushed or pulled, because no node here holds a registry
# credential (ADR-0020). What reconciles a real cluster is not this.
# ---------------------------------------------------------------------

# The local cluster keeps its credentials beside its own state instead of
# in ~/.kube/config, so bringing it up never writes into a kubeconfig
# holding credentials for a real cluster. An exported KUBECONFIG wins, so
# `KUBECONFIG=~/.kube/config make minikube-up` merges the context into
# your own.
KUBECONFIG ?= $(HOME)/.minikube/kubeconfig
export KUBECONFIG

# Every command names the context, so nothing in this file can reach a
# cluster that is not the local one, whichever kubeconfig it reads.
KUBECTL_MINIKUBE := kubectl --context minikube
KUBECTL_LOCAL := $(KUBECTL_MINIKUBE) --namespace garam-local
MINIKUBE_DIR := deploy/local/minikube

# The operator, pinned to the commit that carries the manifest: a release
# asset can be replaced under its tag and a commit cannot. v1.30.0 is the
# current stable release and is the first patched for every advisory
# github.com/cloudnative-pg/cloudnative-pg publishes, the newest of which
# is GHSA-7qwx-x8ff-3px9.
CNPG_MANIFEST := https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/4b5e244a7d031f67e025c83c1555e7726ecbbfa1/releases/cnpg-1.30.0.yaml

minikube-up: minikube-start minikube-db minikube-secret minikube-rebuild

# minikube-rebuild is everything a code change touches: the images, the
# migrations the new code expects, and the processes that run it. The
# bring-up above composes it rather than restating its steps, so the two
# cannot come apart.
minikube-rebuild: minikube-images minikube-migrate minikube-apps

# minikube-start brings up the cluster and the Postgres operator, and is
# a no-op on both when they are already there. The CRDs are past the
# annotation size a client-side apply writes, so the apply is server-side.
minikube-start:
	minikube status >/dev/null 2>&1 || minikube start --driver=docker
	$(KUBECTL_MINIKUBE) apply --server-side -f $(CNPG_MANIFEST)
	$(KUBECTL_MINIKUBE) -n cnpg-system rollout status deployment/cnpg-controller-manager --timeout=300s

# minikube-images builds both images (ADR-0020) into the cluster's own
# daemon, which docker-env points the client at for the length of the
# recipe. Nothing is pushed, and the manifests pull Never, so what runs
# is what was built here. Both Dockerfiles sit outside the context they
# build — `minikube image build` uploads the context and cannot reach
# them — and this is the same pair of contexts the compose file uses.
minikube-images:
	eval "$$(minikube docker-env --shell bash)"; \
		docker build -t garam:local -f deploy/local/Dockerfile . && \
		docker build -t garam-web:local -f deploy/local/Dockerfile.web web

# minikube-db creates the namespace everything else lands in and the
# Postgres cluster CNPG reconciles, and returns once it is reachable.
# The operator reports the cluster ready before its Service carries a
# routable endpoint, and everything downstream connects through that
# Service — so the second wait is on the endpoint the migration
# actually dials, not on the condition next to it.
minikube-db:
	$(KUBECTL_MINIKUBE) apply -f $(MINIKUBE_DIR)/namespace.yaml
	$(KUBECTL_MINIKUBE) apply -f $(MINIKUBE_DIR)/postgres.yaml
	$(KUBECTL_LOCAL) wait --for=condition=Ready cluster/postgres --timeout=300s
	$(KUBECTL_LOCAL) wait --for=jsonpath='{.endpoints[0].conditions.ready}'=true endpointslice -l kubernetes.io/service-name=postgres-rw --timeout=120s

minikube-secret:
	$(MINIKUBE_DIR)/machine-key-material.sh

# minikube-migrate applies pending migrations and waits for them. A Job's
# pod template is immutable, so the finished one is deleted rather than
# updated.
minikube-migrate:
	$(KUBECTL_LOCAL) delete job migrate --ignore-not-found
	$(KUBECTL_LOCAL) apply -f $(MINIKUBE_DIR)/migrate-job.yaml
	$(KUBECTL_LOCAL) wait --for=condition=complete job/migrate --timeout=300s

# minikube-apps starts the three serve processes and the web app, and
# reports what is running rather than that it finished. Each pod
# template is stamped with the id of the image it runs, so a rebuilt
# image is a changed Deployment the apply rolls and an unrebuilt one
# restarts nothing — which a mutable tag alone cannot tell apart.
minikube-apps:
	@eval "$$(minikube docker-env --shell bash)"; \
		id="$$(docker image inspect --format '{{.Id}}' garam:local)" && \
		web_id="$$(docker image inspect --format '{{.Id}}' garam-web:local)" && \
		echo "minikube-apps: garam:local=$$id garam-web:local=$$web_id" && \
		sed -e "s|GARAM_IMAGE_ID|$$id|" -e "s|GARAM_WEB_IMAGE_ID|$$web_id|" $(MINIKUBE_DIR)/garam.yaml \
		| $(KUBECTL_LOCAL) apply -f -
	@for d in authz api machine web; do $(KUBECTL_LOCAL) rollout status deployment/$$d --timeout=300s; done
	@$(KUBECTL_LOCAL) get pods

# minikube-forward is how the web app is reached: the Services are
# cluster-internal and there is no ingress, so nothing leaves the machine
# without this. It holds the terminal until interrupted.
minikube-forward:
	$(KUBECTL_LOCAL) port-forward service/web 3000:3000

# minikube-down removes the stack, its database volume and its key
# material, and leaves the cluster and the operator up.
minikube-down:
	$(KUBECTL_MINIKUBE) delete -f $(MINIKUBE_DIR)/namespace.yaml --ignore-not-found
