# `go run tool@version` takes the toolchain from the tool's own go
# directive, which can be older than this module's and then cannot load
# its packages. The floor is the version go.mod declares; `+auto` still
# upgrades for a tool that needs a newer one.
export GOTOOLCHAIN := go$(shell sed -n 's/^go //p' go.mod)+auto

# No Dependabot ecosystem parses a Makefile, so nothing proposes a bump
# for the six generator pins below: each moves when a contributor moves
# it. No owner and no scheduled review is named for them either, so they
# have no mover of any form docs/convention/ci.md §Verify admits. Five
# of them write output this repository commits, so a bump lands with the
# `gen` that follows it or `check-gen` fails.
OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
MOCKERY := go run github.com/vektra/mockery/v3@v3.7.2
BUF := go run github.com/bufbuild/buf/cmd/buf@v1.72.0
GOIMPORTS := go run golang.org/x/tools/cmd/goimports@v0.48.0
HELM := go run helm.sh/helm/v4/cmd/helm@v4.2.4

# One dev Postgres per machine, not per worktree:
# deploy/local/docker-compose.yml pins `name: garam-local`, so every
# worktree addresses the same compose project and container.
#
# The e2e suite truncates every table and migrates from its own
# migrations/, so it gets a database per worktree on that one server.
# The suffix is git's own name for a linked worktree — unique per
# worktree and fixed for its lifetime — and empty in the main checkout,
# which keeps garam_test. The local stack keeps `garam`.
TEST_DB_NAME ?= garam_test$(shell git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')

COMPOSE_LOCAL := docker compose -f deploy/local/docker-compose.yml
PNPM := pnpm --dir web

# Where the api and the dev Postgres are published on this host, asked
# of compose at the moment each is used. GARAM_LOCAL_API_PORT and
# GARAM_DB_PORT move them, from deploy/local/.env as readily as from the
# environment, and a second copy of either here would be the copy that
# goes stale. Each is empty when nothing is published, which `web-e2e`
# and `e2e` report rather than dial.
API_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port api 8080 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
DB_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port postgres 5432 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')

# Empty rather than half a URL when no Postgres is published, so `e2e`
# has one thing to test. Set it in the environment to run the suite
# against a database outside compose.
TEST_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/$(TEST_DB_NAME)?sslmode=disable)

# The Playwright suite seeds sessions for the api it drives, so its URL
# names the stack's own `garam` rather than a database of its own. Empty
# and overridable on the same terms as the line above.
WEB_E2E_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/garam?sslmode=disable)

.PHONY: lint format test build gen check-gen e2e db-up db-down backend-up ci template-diff
.PHONY: chart chart-lint chart-render chart-identity chart-package
.PHONY: web-install web-lint web-format web-test web-build web-gen web-check-gen web-e2e web-image-e2e web-ci
.PHONY: minikube-up minikube-down minikube-start minikube-images minikube-db
.PHONY: minikube-secret minikube-migrate minikube-apps minikube-rebuild minikube-forward minikube-context

# The module's packages, listed once so the targets below report the set
# they covered rather than name a ./... pattern whose match nobody sees.
# An empty list is never passed on: given no argument at all these tools
# read the current directory, so each target fails on one instead.
GO_PKGS = $(shell go list ./...)

# What buf lint will read, counted on lint's line. buf itself fails on a
# module holding no .proto file; the guard below is for an ls-files that
# comes back empty, which would otherwise print a zero nobody checked.
PROTO_FILES = $(shell $(BUF) ls-files)

lint:
	@echo 'lint: $(words $(GO_PKGS)) packages (go vet), $(words $(PROTO_FILES)) proto files (buf lint)'
	@test -n '$(GO_PKGS)' && test -n '$(PROTO_FILES)' || { echo 'lint: nothing to lint — go list or the buf module matched no input'; exit 1; }
	@go vet $(GO_PKGS)
	@$(BUF) lint

# gofmt and goimports list offending files but exit 0, so the gate is a
# non-empty-output check. Walking `.` is just as silent when it reaches
# no file, so the files are listed here and counted; an empty list fails
# rather than reaching a tool that, given no file, formats stdin and
# reports nothing.
FORMAT_FILES = $(shell find . -type f -name '*.go' -not -path '*/.*')

format:
	@echo 'format: $(words $(FORMAT_FILES)) Go files (gofmt, goimports)'
	@test -n '$(FORMAT_FILES)' || { echo 'format: nothing to check — the sweep matched no Go file'; exit 1; }
	@out="$$(gofmt -l $(FORMAT_FILES))"; test -z "$$out" || { echo "gofmt: $$out"; exit 1; }
	@out="$$($(GOIMPORTS) -l $(FORMAT_FILES))"; test -z "$$out" || { echo "goimports: $$out"; exit 1; }

# go has no negative package pattern, so the sweep is filtered here.
# tests/e2e needs the Postgres db-up starts and fails without it, so test
# covers every other package and names the one it left out. Should the
# filter ever stop matching, the e2e suite is swept back in and fails for
# want of a database rather than skipping quietly.
TEST_PKGS = $(filter-out %/tests/e2e,$(GO_PKGS))

test:
	@echo 'test: $(words $(TEST_PKGS)) packages; tests/e2e not covered here — it needs Postgres (make db-up e2e)'
	@go test $(TEST_PKGS)

# `go build ./...` exits 0 having done nothing should that pattern ever
# resolve to no package, so build names what it compiles too. It skips a
# test-only package swept in by ./... and fails on one named directly,
# so the list holds those with a non-test file; go test covers the rest.
BUILD_PKGS = $(shell go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./...)

build:
	@echo 'build: $(words $(BUILD_PKGS)) packages'
	@test -n '$(BUILD_PKGS)' || { echo 'build: nothing to build — no package has a non-test Go file'; exit 1; }
	@go build $(BUILD_PKGS)

# gen regenerates everything derived from a source of truth: the
# OpenAPI server, sqlc queries, protobuf code, and mocks.
gen:
	$(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
	$(OAPI_CODEGEN) --config api/machine-codegen.yaml api/machine.yaml
	$(SQLC) generate
	$(MOCKERY)
	$(BUF) generate
	$(GOIMPORTS) -w proto/gen

# check-gen fails when the committed generated output and what the
# generators are configured to produce disagree, in either direction:
# output a generator still owns is modified, uncommitted, or deleted;
# or tracked output no generator owns is left behind. No generator
# deletes output it stops producing, so a removed config entry leaves
# its files committed, and a status check over the owned set says
# nothing about them.
#
# GEN_PATHS is the owned set, and the generator configs are its only
# source of truth — no generator answers "what do I own". Each states
# it differently: oapi-codegen names `output:` files, sqlc and buf name
# `out:` directories, and mockery owns a mocks/ directory beside each
# package it lists (its dir template is {{.InterfaceDir}}/mocks). The
# paths are literal, not globs: a glob expands against the working tree
# when the recipe runs, so a deleted generated directory falls outside
# it and a pending deletion reads as a clean tree. A parse that stops
# matching a config shrinks the owned set, which the ownership check
# below turns into a failure rather than a pass.
GEN_PATHS := $(shell { sed -n 's/^output: //p' api/oapi-codegen.yaml api/machine-codegen.yaml; \
	sed -n 's/^        out: //p' sqlc.yaml; \
	sed -n 's/^    out: //p' buf.gen.yaml; \
	sed -n 's|^  $(shell sed -n 's/^module //p' go.mod)/\(.*\):$$|\1/mocks|p' .mockery.yaml; } | sort -u)

# git diff sees tracked paths only, so a generated file nobody added is
# invisible to it. git status reports modified and untracked entries, skips
# ignored ones, and exits 0 either way, so the gate checks for output.
#
# The checks against the owned set's shape run before gen, which would
# otherwise heal them silently: a deleted owned path regenerates before
# any status check could see it. The owned set cannot see output no
# generator owns, but a generated file declares itself — every
# generator here writes Go's "// Code generated ... DO NOT EDIT."
# marker — so a tracked file carrying it that no owned path claims is
# stale output. The scan reads the index, so output deleted in the
# working tree but not yet staged is still seen and still fails.
check-gen:
	@test -n '$(GEN_PATHS)' || { echo "check-gen: generator configs yielded no output paths — the parse above is broken"; exit 1; }
	@out="$$(git ls-files --deleted -- $(GEN_PATHS))"; \
		test -z "$$out" || { echo "check-gen: generated output deleted in the working tree:"; echo "$$out"; exit 1; }
	@generated="$$(git grep --cached -l -E '^// Code generated .* DO NOT EDIT' || true)"; \
		test -n "$$generated" || { echo "check-gen: found no generated files — the marker scan is broken"; exit 1; }; \
		orphans="$$(printf '%s\n' "$$generated" | sed 's|$$|/|' | grep -vF $$(printf '%s/\n' $(GEN_PATHS) | sed 's/^/-e /') || true)"; \
		test -z "$$orphans" || { echo "check-gen: tracked generated output no generator is configured to produce — commit its removal or restore the config entry:"; printf '%s\n' "$$orphans" | sed 's|/$$||'; exit 1; }; \
		echo "check-gen: $$(printf '%s\n' "$$generated" | wc -l) generated files, all owned by a generator config"
	@$(MAKE) --no-print-directory gen
	@# An owned path gen did not create means the parse and the configs
	@# disagree; ls fails on it rather than reading it as a clean tree.
	@paths="$$(ls -d $(GEN_PATHS))" && echo "check-gen: $$(echo "$$paths" | wc -l) generated paths inspected"
	@out="$$(git status --porcelain -- $(GEN_PATHS))"; \
		test -z "$$out" || { echo "check-gen: uncommitted generated output:"; echo "$$out"; exit 1; }

# template-diff reports where every path the conventions template tracks
# stands here — the question #294 wants answered by a run, not a memory.
# Both of its inputs are derived, so neither can go stale: the path set
# is the template's own tree, and a path's base is the newest template
# commit whose blob equals the one HEAD holds. No such commit means the
# content was never the template's — a local adaptation, or a file the
# two repositories only share the name of — and no revision describes
# that, so the path is named and left there. A path absent here is
# reported rather than skipped, which is how a directory the template
# grows arrives without an edit to this file.
template-diff:
	@git fetch template --quiet
	@paths="$$(git ls-tree -r --name-only template/main)"; \
		test -n "$$paths" || { echo 'template-diff: template/main tracks no path — the tree listing is broken'; exit 1; }; \
		echo "template-diff: $$(printf '%s\n' "$$paths" | wc -l) paths tracked by template/main"; \
		for p in $$paths; do \
			ours="$$(git rev-parse -q --verify "HEAD:$$p")" || { printf '  absent here  %s\n' "$$p"; continue; }; \
			base="$$(git log --format='%H' --no-renames --raw --abbrev=40 template/main -- "$$p" \
				| awk -v ours="$$ours" 'NF==1{c=$$0} $$4==ours{print c; exit}')"; \
			test -n "$$base" || { printf '  adapted      %s\n' "$$p"; continue; }; \
			since="$$(git log --oneline --no-decorate "$$base..template/main" -- "$$p")"; \
			test -n "$$since" || { printf '  current      %s  %s\n' "$$p" "$$(git rev-parse --short "$$base")"; continue; }; \
			printf '  behind       %s  ours matches %s\n' "$$p" "$$(git rev-parse --short "$$base")"; \
			printf '%s\n' "$$since" | sed 's/^/                 /'; \
		done

# db-up starts the stack's postgres service alone and makes sure the e2e
# database exists next to `garam`. Postgres has no CREATE DATABASE IF NOT
# EXISTS, so ask before creating. The e2e suite migrates it itself.
#
# Whether the database exists is psql's output; whether there is an answer
# at all is its exit status. The two are read separately, because one
# non-zero status cannot tell a failed query from a clean negative.
db-up:
	$(COMPOSE_LOCAL) up -d --wait postgres
	@present="$$($(COMPOSE_LOCAL) exec -T postgres psql -U garam -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$(TEST_DB_NAME)'")" \
		|| { echo "db-up: could not ask postgres whether $(TEST_DB_NAME) exists" >&2; exit 1; }; \
		test -n "$$present" || $(COMPOSE_LOCAL) exec -T postgres createdb -U garam $(TEST_DB_NAME)

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

# What the e2e suite covers, listed once so the target reports the set it
# ran rather than name a ./... pattern whose match nobody sees. `go test`
# prints `[no test files]` and exits 0 for a package holding none — a
# build tag that stopped matching, a renamed file — which reads exactly
# like the run that did the work. Only packages holding a test file are
# listed, so that condition empties the list and fails below instead of
# reaching go test.
E2E_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/e2e/...)
E2E_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/e2e/...)

# e2e runs the store round-trips against real Postgres (see db-up).
e2e:
	@echo 'e2e: $(words $(E2E_PKGS)) packages, $(words $(E2E_TEST_FILES)) test files, against $(TEST_DB_NAME)'
	@test -n '$(E2E_PKGS)' || { echo 'e2e: nothing to run — no package under ./tests/e2e/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make db-up" >&2; exit 1; }
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 $(E2E_PKGS)

ci: format lint test build check-gen chart

# ---------------------------------------------------------------------
# web/ — the Next.js frontend (ADR-0009). It keeps its own toolchain and
# lockfile; `web-ci` is the gate CI runs alongside `ci`.
# ---------------------------------------------------------------------

web-install:
	$(PNPM) install --frozen-lockfile

# eslint prints nothing and exits 0 both when it covered the tree and
# when its ignore patterns left it a fraction of it, so what it covered
# is read out of its own JSON report: one entry per file it linted. A
# report naming no file fails the target below rather than reporting a
# count nobody checked.
WEB_LINT_FILES = $(shell { $(PNPM) exec eslint --format json | $(PNPM) exec node -e 'for(const r of JSON.parse(require("fs").readFileSync(0,"utf8")))console.log(r.filePath)'; } 2>/dev/null)

# tsc reads its inputs from tsconfig.json rather than from eslint's
# ignore patterns, so the TS18003 it raises on its own empty input says
# nothing about what eslint read. The count above is the eslint half
# reporting for itself.
web-lint:
	@echo 'web-lint: $(words $(WEB_LINT_FILES)) files (eslint)'
	@test -n '$(WEB_LINT_FILES)' || { echo 'web-lint: nothing to lint — the eslint report named no file; $(PNPM) lint reports why'; exit 1; }
	$(PNPM) lint
	$(PNPM) typecheck

# prettier reports no count and exits 0 when its ignore files leave it
# nothing, so what it opened is read out of its debug log: one
# `resolve config from` line per file. That log is not part of what
# prettier promises, so a parse yielding no line fails the target below
# rather than reporting a number nobody checked.
WEB_FORMAT_LOG_FILES = $(shell $(PNPM) exec prettier . --check --log-level debug 2>&1 | sed -n "s|^\[debug\] resolve config from '\(.*\)'$$|\1|p")

# Config resolution runs before parser inference, so that log also names
# files prettier then skips for want of a parser. getFileInfo is
# prettier's own answer to which of them it would have checked, and
# without it an ignore file that swallowed every source would still pass
# on a count the skipped files held above zero. Each path is quoted
# because the App Router puts parentheses and brackets in these names.
WEB_FORMAT_FILES = $(shell $(PNPM) exec node -e 'const p=require("prettier");(async()=>{for(const f of process.argv.slice(1))if((await p.getFileInfo(f)).inferredParser)console.log(f)})()' $(patsubst %,'%',$(WEB_FORMAT_LOG_FILES)))

web-format:
	@echo 'web-format: $(words $(WEB_FORMAT_FILES)) files (prettier)'
	@test -n '$(WEB_FORMAT_LOG_FILES)' || { echo 'web-format: nothing parsed — the prettier debug log named no file'; exit 1; }
	@test -n '$(WEB_FORMAT_FILES)' || { echo 'web-format: nothing to check — the ignore files left prettier no file it can parse'; exit 1; }
	@$(PNPM) format

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

# What gen:api writes, read from the script that writes it: package.json
# owns the -o path, and a second copy here is the copy that goes stale.
# The script runs in web/, so the path it names is relative to it.
WEB_GEN_PATHS = $(shell sed -n 's|.*"gen:api": ".* -o \([^" ]*\)".*|web/\1|p' web/package.json)

# A git diff pathspec matching no tracked file exits 0 having compared
# nothing, so what git tracks under the owned path is counted first: a
# moved -o or a removed file fails here instead of passing forever.
web-check-gen: web-gen
	@echo 'web-check-gen: $(words $(WEB_GEN_PATHS)) generated paths compared'
	@test -n '$(WEB_GEN_PATHS)' && git ls-files --error-unmatch -- $(WEB_GEN_PATHS) >/dev/null 2>&1 \
		|| { echo 'web-check-gen: nothing to compare — package.json names no gen:api output, or git tracks none of "$(WEB_GEN_PATHS)"'; exit 1; }
	@git diff --exit-code -- $(WEB_GEN_PATHS)

# web-e2e drives the built app in a real browser against the backend
# `make backend-up` starts. It builds and serves the app itself, so it
# needs GARAM_API_URL — the real api here, not `web-build`'s placeholder
# — the URL its seed script writes sessions through, and the browser,
# which Playwright keeps outside the lockfile.
web-e2e:
	@test -n "$(API_HOST_ADDR)" || { echo "web-e2e: no api published on this host — run make backend-up" >&2; exit 1; }
	@test -n '$(WEB_E2E_DATABASE_URL)' || { echo "web-e2e: no postgres published on this host — run make backend-up" >&2; exit 1; }
	$(PNPM) exec playwright install chromium
	GARAM_API_URL=http://$(API_HOST_ADDR) GARAM_E2E_DATABASE_URL='$(WEB_E2E_DATABASE_URL)' $(PNPM) e2e

# web-image-e2e checks the built image rather than the app: that the
# standalone server starts, answers both probes and reaches the baked
# `/api/*` target. It builds the image it checks; GARAM_WEB_IMAGE names
# one to check instead, which is how an image pulled from ECR by digest
# is verified.
web-image-e2e:
	tests/image/web.sh

web-ci: web-format web-lint web-test web-build web-check-gen

# ---------------------------------------------------------------------
# deploy/charts/garam — the chart that describes a garam deployment
# (ADR-0042). `chart` is the whole check and `make ci` runs it; both
# halves are named tasks of their own. helm is run through the module
# pin at the top of this file rather than off the path, so a contributor
# installs nothing and every run takes one version.
# ---------------------------------------------------------------------

CHART_DIR := deploy/charts/garam
CHART_TEMPLATES = $(wildcard $(CHART_DIR)/templates/*.yaml)

# One render case per value file: the minimal one takes every default and
# the full one sets every optional value, so only their union reaches the
# templates' one branch.
CHART_VALUES = $(wildcard $(CHART_DIR)/ci/*-values.yaml)

# Every environment variable garam reads, derived from the two files that
# read them rather than listed here: envOr wraps the Go module's only
# os.Getenv call, and web/src/env.ts is the web app's only reader. The
# call's opening parenthesis is matched as `.` because make counts the
# parentheses inside $(shell ...) and an unbalanced one ends it early.
CHART_GO_ENV = $(shell grep -rho 'envOr."[A-Z_]*"' --include=*.go internal cmd | sed 's/.*"\([A-Z_]*\)"/\1/' | sort -u)
CHART_WEB_ENV = $(shell sed -n 's/^ *\([A-Z][A-Z0-9_]*\): process\.env\.\1,$$/\1/p' web/src/env.ts)

# What the chart declares it does not set, read from the table that
# carries the reasons. A renamed heading empties this and fails the
# target below rather than exempting nothing quietly.
CHART_UNSET_ENV = $(shell awk '/^\#\# /{s=0} /^\#\# Not set by this chart/{s=1} s' $(CHART_DIR)/README.md \
	| sed -n 's/^| `\([A-Z][A-Z0-9_]*\)`.*/\1/p')

# Every workload the chart renders, the kind it renders as, and the value
# that names the account it runs as — read from the README table a
# consumer places its own names through. A renamed heading empties this
# and fails the target below rather than checking nothing.
CHART_IDENTITIES = $(shell awk '/^\#\# /{s=0} /^\#\# Workload identities/{s=1} s' $(CHART_DIR)/README.md \
	| sed -n 's/^| `\([a-z0-9-]*\)` | \([A-Za-z]*\) | `\([a-zA-Z0-9.]*\)` |.*/\1=\2=\3/p')

# The value file that names no account, so its render is what a
# deployment supplying only what the chart requires receives.
CHART_DEFAULT_VALUES = $(CHART_DIR)/ci/minimal-values.yaml

# The resource names the packaged artifact may not carry as a key,
# listed so the count the target prints is the set it searched for. Two
# names, which $(CHART_DIR)/README.md §What the checks are complete over
# records as the whole of what that target reaches.
CHART_RESOURCE_NAMES := cpu memory

chart: chart-lint chart-render chart-identity chart-package

chart-lint:
	@echo 'chart-lint: $(words $(CHART_TEMPLATES)) templates, $(words $(CHART_VALUES)) value files (helm lint --strict)'
	@test -n '$(CHART_VALUES)' || { echo 'chart-lint: nothing to lint — $(CHART_DIR)/ci holds no value file'; exit 1; }
	@set -e; for v in $(CHART_VALUES); do $(HELM) lint --strict --quiet $(CHART_DIR) --values "$$v"; done

# chart-render renders every value file and compares the environment
# names the render carries against the ones the source reads. Nothing
# else keeps the chart honest: a manifest that renders is not a manifest
# that matches the code. Every container, port and volume name in the
# chart is lowercase, so an all-caps `- name:` in the render is an
# environment variable.
chart-render:
	@echo 'chart-render: $(words $(CHART_GO_ENV)) Go and $(words $(CHART_WEB_ENV)) web environment names read by the source, $(words $(CHART_UNSET_ENV)) declared unset — environment names only, $(CHART_DIR)/README.md §What the checks are complete over'
	@test -n '$(CHART_GO_ENV)' && test -n '$(CHART_WEB_ENV)' \
		|| { echo 'chart-render: the source sweep read no name — the envOr or env.ts parse is broken'; exit 1; }
	@test -n '$(CHART_UNSET_ENV)' \
		|| { echo 'chart-render: $(CHART_DIR)/README.md §Not set by this chart named no variable — the parse is broken'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(CHART_VALUES); do $(HELM) template garam $(CHART_DIR) --values "$$v" > "$$out/$$(basename "$$v")"; done; \
		echo "chart-render: $$(cat "$$out"/*-values.yaml | grep -c '^kind:') documents rendered"; \
		sed -n 's/^ *- name: \([A-Z][A-Z0-9_]*\)$$/\1/p' "$$out"/*-values.yaml | sort -u > "$$out/rendered"; \
		printf '%s\n' $(CHART_UNSET_ENV) | sort -u > "$$out/unset"; \
		printf '%s\n' $(CHART_GO_ENV) $(CHART_WEB_ENV) | sort -u > "$$out/source"; \
		both="$$(comm -12 "$$out/rendered" "$$out/unset")"; \
		test -z "$$both" || { echo "chart-render: rendered and declared unset at once:"; echo "$$both"; exit 1; }; \
		sort -u "$$out/rendered" "$$out/unset" > "$$out/covered"; \
		diff "$$out/source" "$$out/covered" \
			|| { echo 'chart-render: < read by the source, neither rendered nor declared unset; > covered by the chart, read by nothing'; exit 1; }

# chart-identity compares the identity every rendered workload runs as
# against the table the README declares it under, under two release
# names. An account name is the subject another repository binds a role
# to (ADR-0043): a name this chart composed would move with the release
# name, the credential would stop arriving at runtime, and one render
# cannot see it. Keyed on the component label, which no release name
# enters, so the two renders compare directly.
chart-identity:
	@echo 'chart-identity: $(words $(CHART_IDENTITIES)) workloads declared by $(CHART_DIR)/README.md §Workload identities — pod identity only, §What the checks are complete over'
	@test -n '$(CHART_IDENTITIES)' \
		|| { echo 'chart-identity: $(CHART_DIR)/README.md §Workload identities declared no workload — the parse is broken'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		identity() { rel="$$1"; shift; $(HELM) template "$$rel" $(CHART_DIR) "$$@" > "$$out/raw"; awk ' \
			function flush() { \
				if (kind == "Deployment" || kind == "Job") printf "workload %s=%s runs as %s\n", comp, kind, \
					(fields > 1 ? fields " serviceAccountName fields at once" : (sa == "" ? "the namespace default" : sa)); \
				else if (kind == "ServiceAccount") printf "account %s\n", name } \
			/^---$$/ { flush(); kind = ""; comp = ""; name = ""; sa = ""; fields = 0; next } \
			/^kind: / { kind = $$2 } \
			/^  name: / { if (name == "") name = $$2 } \
			/app.kubernetes.io\/component: / { if (comp == "") comp = $$2 } \
			/^ *serviceAccountName: / { sa = $$2; fields++ } \
			END { flush() }' "$$out/raw" | sort; }; \
		printf '%s\n' $(CHART_IDENTITIES) | cut -d= -f1,2 | sort -u > "$$out/declared"; \
		for v in $(CHART_VALUES); do \
			n="$$(basename "$$v")"; \
			identity garam --values "$$v" > "$$out/garam"; \
			identity prod-garam --values "$$v" > "$$out/prod-garam"; \
			echo "chart-identity: $$n — $$(grep -c '^workload ' "$$out/garam" || true) workloads, $$(grep -c '^account ' "$$out/garam" || true) accounts"; \
			diff "$$out/garam" "$$out/prod-garam" \
				|| { echo "chart-identity: $$n: < release garam, > release prod-garam — an identity moved with the release name, and so did the subject another repository binds a role to"; exit 1; }; \
			sed -n 's/^workload \([a-zA-Z0-9=-]*\) runs as .*/\1/p' "$$out/garam" | sort -u > "$$out/rendered"; \
			diff "$$out/declared" "$$out/rendered" \
				|| { echo "chart-identity: $$n: < workload=kind declared in the README, > rendered by the chart"; exit 1; }; \
			sed -n 's/^account //p' "$$out/garam" | sort -u > "$$out/accounts"; \
			sed -n 's/^workload .* runs as //p' "$$out/garam" | sort -u > "$$out/bound"; \
			unused="$$(comm -23 "$$out/accounts" "$$out/bound")"; \
			test -z "$$unused" || { echo "chart-identity: $$n renders an account no workload runs as — a subject the next reader takes for one that was needed, and one an annotation sends a consumer to a role for:"; echo "$$unused"; exit 1; }; \
		done; \
		probe=""; for d in $(CHART_IDENTITIES); do probe="$$probe --set $${d##*=}=probe-$${d%%=*}"; done; \
		identity garam --values $(CHART_DEFAULT_VALUES) $$probe > "$$out/probe"; \
		for d in $(CHART_IDENTITIES); do \
			grep -q " runs as probe-$${d%%=*}$$" "$$out/probe" \
				|| { echo "chart-identity: §Workload identities names $${d##*=} for $${d%%=*}, and setting it named no account — the value is not the one the chart reads, or it is not read verbatim"; exit 1; }; \
		done; \
		identity garam --values $(CHART_DEFAULT_VALUES) > "$$out/default"; \
		defaulted="$$(grep '^account ' "$$out/default" || true; grep '^workload ' "$$out/default" | grep -v 'runs as the namespace default' || true)"; \
		test -z "$$defaulted" || { echo "chart-identity: $$(basename $(CHART_DEFAULT_VALUES)) names no account and the chart supplied one anyway — the default is none:"; echo "$$defaulted"; exit 1; }; \
		echo "chart-identity: $$(basename $(CHART_DEFAULT_VALUES)) — no name given, so no account created and every workload on the namespace default"; \
		for d in $(CHART_IDENTITIES); do p="$${d##*=}"; \
			if $(HELM) template garam $(CHART_DIR) --values $(CHART_DEFAULT_VALUES) --set "$${p%.name}.create=true" >/dev/null 2>&1; then \
				echo "chart-identity: $${p%.name}.create=true rendered with no $$p — the chart composed a name, and a composed name moves the subject another repository binds a role to"; exit 1; fi; \
		done

# chart-package reads the artifact `helm package` produces, which the
# three targets above never open: everything not excluded ships, `ci/`
# included, so a file reaches a consumer without passing through any of
# them. A resource name set there reads as a figure garam measured, and
# none is (docs/architecture/deployment-contract.md §Not established).
chart-package:
	@echo 'chart-package: $(words $(CHART_RESOURCE_NAMES)) resource names searched for as keys in the packaged artifact — resource quantities only, $(CHART_DIR)/README.md §What the checks are complete over'
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		$(HELM) package $(CHART_DIR) --destination "$$out" >/dev/null; \
		tgz="$$(echo "$$out"/*.tgz)"; \
		mkdir "$$out/unpacked"; tar -xzf "$$tgz" -C "$$out/unpacked"; \
		cd "$$out/unpacked"; \
		files="$$(find . -type f | wc -l)"; \
		named="$$(grep -rn resources . | wc -l)"; \
		test "$$files" -gt 0 && test "$$named" -gt 0 \
			|| { echo 'chart-package: the unpacked artifact named resources nowhere — the package or the search read nothing'; exit 1; }; \
		re="$$(echo '$(CHART_RESOURCE_NAMES)' | tr ' ' '|')"; \
		found="$$(grep -rnE "\b($$re):" . || true)"; \
		test -z "$$found" || { echo "chart-package: the artifact sets a resource name, and no figure is published for any garam process (docs/architecture/deployment-contract.md §Not established):"; echo "$$found"; exit 1; }; \
		echo "chart-package: $$(basename "$$tgz") — $$files files, $$named lines naming resources, 0 setting one of the names"

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
# current stable release and the first patched for all four advisories
# github.com/cloudnative-pg/cloudnative-pg publishes; GHSA-7qwx-x8ff-3px9
# is what puts that floor at 1.30.0 rather than 1.29.2, and the GitHub
# Advisory Database does not carry it — re-check the four at the
# project's own advisories. Nothing proposes a bump for this pin either,
# and no owner and no scheduled review is named for it, so it has no
# mover of any form docs/convention/ci.md §Verify admits.
#
# The database's Postgres major is no longer this pin's to move:
# deploy/local/minikube/postgres.yaml names its own image rather than
# taking the default this operator version carries.
CNPG_MANIFEST := https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/4b5e244a7d031f67e025c83c1555e7726ecbbfa1/releases/cnpg-1.30.0.yaml

minikube-up: minikube-start minikube-db minikube-secret minikube-rebuild

# minikube-rebuild is everything a code change touches: the images, the
# migrations the new code expects, and the processes that run it. The
# bring-up above composes it rather than restating its steps, so the two
# cannot come apart.
minikube-rebuild: minikube-images minikube-migrate minikube-apps

# Every kubectl recipe needs the apiserver reachable through the project
# kubeconfig, and `minikube status` does not answer that: a cluster
# started outside make writes the new apiserver port into ~/.kube/config
# and leaves this file stale behind a Running process, which status's
# exit code also reports as failure. Ask the file itself and repair it
# with update-context; boot only when the apiserver process is down.
minikube-context:
	@$(KUBECTL_MINIKUBE) get --raw=/healthz >/dev/null 2>&1 || { \
		test "$$(minikube status --format '{{.APIServer}}' 2>/dev/null | head -n 1)" = Running \
			|| minikube start --driver=docker; \
		minikube update-context; \
	}

# minikube-start brings up the cluster and the Postgres operator, and is
# a no-op on both when they are already there. The CRDs are past the
# annotation size a client-side apply writes, so the apply is server-side.
minikube-start: minikube-context
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
minikube-db: minikube-context
	$(KUBECTL_MINIKUBE) apply -f $(MINIKUBE_DIR)/namespace.yaml
	$(KUBECTL_MINIKUBE) apply -f $(MINIKUBE_DIR)/postgres.yaml
	$(KUBECTL_LOCAL) wait --for=condition=Ready cluster/postgres --timeout=300s
	$(KUBECTL_LOCAL) wait --for=jsonpath='{.endpoints[0].conditions.ready}'=true endpointslice -l kubernetes.io/service-name=postgres-rw --timeout=120s

minikube-secret: minikube-context
	$(MINIKUBE_DIR)/machine-key-material.sh

# minikube-migrate applies pending migrations and waits for them. A Job's
# pod template is immutable, so the finished one is deleted rather than
# updated.
minikube-migrate: minikube-context
	$(KUBECTL_LOCAL) delete job migrate --ignore-not-found
	$(KUBECTL_LOCAL) apply -f $(MINIKUBE_DIR)/migrate-job.yaml
	$(KUBECTL_LOCAL) wait --for=condition=complete job/migrate --timeout=300s

# minikube-apps starts the three serve processes and the web app, and
# reports what is running rather than that it finished. Each pod
# template is stamped with the id of the image it runs, so a rebuilt
# image is a changed Deployment the apply rolls and an unrebuilt one
# restarts nothing — which a mutable tag alone cannot tell apart.
minikube-apps: minikube-context
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
minikube-forward: minikube-context
	$(KUBECTL_LOCAL) port-forward service/web 3000:3000

# minikube-down removes the stack, its database volume and its key
# material, and leaves the cluster and the operator up.
minikube-down:
	$(KUBECTL_MINIKUBE) delete -f $(MINIKUBE_DIR)/namespace.yaml --ignore-not-found
