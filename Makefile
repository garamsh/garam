# The versions this repository declares its two toolchains at, read from
# the file that declares each. `check-toolchain` holds the container
# bases against them.
GO_DIRECTIVE := $(shell sed -n 's/^go //p' go.mod)
NODE_VERSION := $(shell cat web/.nvmrc)

# `go run tool@version` takes the toolchain from the tool's own go
# directive, which can be older than this module's and then cannot load
# its packages. The floor is the version go.mod declares; `+auto` still
# upgrades for a tool that needs a newer one.
export GOTOOLCHAIN := go$(GO_DIRECTIVE)+auto

# Tool directives in go.mod: go.sum records each version and
# Dependabot's gomod ecosystem proposes its bumps, and
# `.github/dependabot.yml` names what it does not cover. Each writes
# output this repository commits, so a move — a bump, or a floor another
# dependency raises — lands with the `gen` that follows it or
# `check-gen` fails.
OAPI_CODEGEN := go tool oapi-codegen
SQLC := go tool sqlc
MOCKERY := go tool mockery
BUF := go tool buf
GOIMPORTS := go tool goimports

# Two more tool directives, pinned and moved on the terms the block
# above states and named beside it in `.github/dependabot.yml`. Neither
# writes output this repository commits, so a bump of either lands on
# its own and `check-gen` says nothing about it. They read what nothing
# else here reads: release.yml, whose first execution is the first tag,
# and the shell scripts, one of which no Make target calls (#840).
ACTIONLINT := go tool actionlint
SHFMT := go tool shfmt

# A third on the same terms, and the only tool directive here no
# whole-set name reaches: `mutation` is invoked by hand and by nothing
# else, for the reasons stated at that target (#875).
GREMLINS := go tool gremlins

# helm is not a tool directive: helm.sh/helm/v4 requires a
# google.golang.org/protobuf newer than protoc-gen-go's, and merging its
# graph into this module's raises that floor and restamps every
# generated .pb.go header. Nothing parses this line for a pin, so no bot
# proposes a bump and no scheduled review is named; naming no mover of
# its own, it is the PM's to move under AGENTS.md §Roles. It writes no
# output this repository commits, so nothing here fails when it goes
# stale.
HELM := go run helm.sh/helm/v4/cmd/helm@v4.2.4

# One test database per worktree, on the one dev Postgres every
# worktree shares (compose pins `name: garam-local`). The suffix is
# git's name for a linked worktree and empty in the main checkout,
# which keeps garam_test. Both Go suites migrate it and the integration
# suite truncates every table, so no worktree shares another's.
TEST_DB_NAME ?= garam_test$(shell git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')
export TEST_DB_NAME

# IMAGE_BUILD_CACHE=gha has `backend-up` and `image-build` read and
# write their layers in the GitHub Actions cache, which only a CI run
# holds (#1519). Empty, the default, builds as before.
IMAGE_BUILD_CACHE ?=
ifneq ($(filter-out gha,$(IMAGE_BUILD_CACHE)),)
$(error IMAGE_BUILD_CACHE is '$(IMAGE_BUILD_CACHE)', and gha is the one cache it can name)
endif

COMPOSE_LOCAL := docker compose -f deploy/local/docker-compose.yml$(if $(IMAGE_BUILD_CACHE), -f deploy/local/docker-compose.gha-cache.yml)
export COMPOSE_LOCAL
PNPM := pnpm --dir web

# Where the api and the dev Postgres are published on this host, asked
# of compose at the moment each is used. GARAM_LOCAL_API_PORT and
# GARAM_DB_PORT move them, from deploy/local/.env as readily as from the
# environment, and a second copy of either here would be the copy that
# goes stale. Each is empty when nothing is published, which `web-e2e`,
# `integration` and `e2e` report rather than dial.
API_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port api 8080 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
DB_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port postgres 5432 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
# The dev OpenBao `integration` signs against (ADR-0104), rewritten to
# localhost because its TLS certificate names that; GARAM_OPENBAO_PORT
# moves it. The container is named so the suite can read the CA and
# root token the dev server generated.
OPENBAO_HOST_ADDR = $(shell $(COMPOSE_LOCAL) port openbao 8200 2>/dev/null | sed 's/^0\.0\.0\.0/localhost/')
OPENBAO_CONTAINER = $(shell $(COMPOSE_LOCAL) ps -q openbao 2>/dev/null)

# Empty rather than half a URL when no Postgres is published, so
# `integration` and `e2e` have one thing to test. Set it in the
# environment to run them against a database outside compose.
TEST_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/$(TEST_DB_NAME)?sslmode=disable)

# The Playwright suite seeds sessions for the api it drives, so its URL
# names the stack's own `garam` rather than a database of its own. Empty
# and overridable on the same terms as the line above.
WEB_E2E_DATABASE_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/garam?sslmode=disable)

.PHONY: lint format test build gen check-gen check-toolchain integration e2e workload-integration standalone-e2e mutation db-up db-down db-prune backend-up ci template-diff search
.PHONY: chart chart-lint chart-render chart-identity chart-names chart-rewrite chart-guards chart-security chart-package
.PHONY: web-install web-lint web-format web-test web-build web-gen web-check-gen web-e2e web-image-e2e web-ci
.PHONY: image-build test-image test-image-e2e test-image-push test-image-pin
.PHONY: minikube-up minikube-down minikube-start minikube-images minikube-db
.PHONY: minikube-secret minikube-migrate minikube-apps minikube-rebuild minikube-forward minikube-context

# The module's packages, listed once so the targets below report the set
# they covered rather than name a ./... pattern whose match nobody sees.
# An empty list is never passed on, and each target says so itself
# rather than resting on what a tool does with no argument: `go test`
# given none reads the current directory, which fails only while no .go
# file sits at the module root — a doc.go there is an ordinary idiom and
# would make an empty list a run of zero tests exiting 0 (#870).
GO_SELECT = go list ./...
GO_PKGS = $(shell $(GO_SELECT))

# The one way a Go entry point reports its packages: the count beside
# the selector that chose them, failing on zero (stack-go.md §9).
# $(call go-packages,<entry point>,<selector>,<packages>)
define go-packages
@echo "$(1): $(words $(3)) packages from $(2)"
@test -n '$(strip $(3))' || { echo "$(1): nothing to run — $(2) selected no package"; exit 1; }
endef

# What buf lint will read, counted on lint's line. buf itself fails on a
# module holding no .proto file; the guard below is for an ls-files that
# comes back empty, which would otherwise print a zero nobody checked.
PROTO_FILES = $(shell $(BUF) ls-files)

# What actionlint and shfmt will read, counted on lint's line too.
# actionlint given no path finds the workflows itself and shfmt given a
# directory walks it, and either sweep coming back empty exits zero
# reading exactly like the run that read every file — so the paths are
# listed here and an empty list fails below. The shell sweep takes two
# roots: `scripts/`, and the repository root, where the template ships
# `check-contents-list.sh` and `check-heading-prefixes.sh` and where
# they stay so template-diff can measure each against the path it comes
# from.
WORKFLOW_FILES = $(shell find .github/workflows -type f \( -name '*.yml' -o -name '*.yaml' \))
SHELL_SCRIPTS = $(shell find . -maxdepth 1 -type f -name '*.sh') $(shell find scripts -type f -name '*.sh')

# actionlint runs with shellcheck and pyflakes named off, so it is one
# check on every machine, and its ref-keyed checks reach no action
# pinned by full SHA (#848, #854, #858). shfmt runs as a POSIX parser
# with output discarded: it fails a syntax error or a bashism the POSIX
# grammar cannot hold, never a layout (#904). The two Markdown scripts
# count their own files and fail on zero (#1468).
lint:
	@echo 'lint: $(words $(PROTO_FILES)) proto files (buf lint), $(words $(WORKFLOW_FILES)) workflows (actionlint), $(words $(SHELL_SCRIPTS)) shell scripts (shfmt); go vet counted on its own line below, Markdown by check-contents-list.sh and check-heading-prefixes.sh'
	@test -n '$(PROTO_FILES)' && test -n '$(WORKFLOW_FILES)' && test -n '$(SHELL_SCRIPTS)' \
		|| { echo 'lint: nothing to lint — the buf module, .github/workflows or the shell sweep matched no input; the counts above name which'; exit 1; }
	$(call go-packages,lint (go vet),$(GO_SELECT),$(GO_PKGS))
	@go vet $(GO_PKGS)
	@$(BUF) lint
	@$(ACTIONLINT) -shellcheck= -pyflakes= $(WORKFLOW_FILES)
	@$(SHFMT) -ln posix $(SHELL_SCRIPTS) >/dev/null
	@./check-contents-list.sh
	@./check-heading-prefixes.sh

# gofmt and goimports list offending files but exit 0, so the gate is a
# non-empty-output check; a file they cannot read or parse, a tracked
# file deleted from the working tree among them, makes xargs exit 123
# and fails the gate too. The set is git's: the tracked files CI checks
# out plus untracked ones not ignored. Given an empty set, xargs still
# runs each tool once on /dev/null and it reports nothing, so the files
# are counted first and a count of zero fails.
FORMAT_SELECT = git ls-files --cached --others --exclude-standard -z -- '*.go'
FORMAT_COUNT = $(shell $(FORMAT_SELECT) | tr -cd '\0' | wc -c)

format:
	@echo 'format: $(FORMAT_COUNT) Go files (gofmt, goimports)'
	@test '$(FORMAT_COUNT)' -gt 0 || { echo 'format: nothing to check — the sweep matched no Go file'; exit 1; }
	@out="$$($(FORMAT_SELECT) | xargs -0 gofmt -l)" || { echo "gofmt: failed on the error above"; exit 1; }; test -z "$$out" || { echo "gofmt: $$out"; exit 1; }
	@out="$$($(FORMAT_SELECT) | xargs -0 $(GOIMPORTS) -l)" || { echo "goimports: failed on the error above"; exit 1; }; test -z "$$out" || { echo "goimports: $$out"; exit 1; }

# go has no negative package pattern, so the sweep is filtered here and
# the four suites needing Postgres or a Docker daemon are named instead;
# a filter that stops matching sweeps its suite back in to fail rather
# than skip. The two shell tests need no Docker:
# scripts/image-build-test.sh (ADR-0071, ADR-0097) and
# scripts/image-test-ecr-test.sh (ADR-0093).
TEST_EXCLUDE = %/tests/integration %/tests/e2e %/tests/workload %/tests/standalone
TEST_SELECT = $(GO_SELECT) minus $(TEST_EXCLUDE)
TEST_PKGS = $(filter-out $(TEST_EXCLUDE),$(GO_PKGS))

test:
	$(call go-packages,test,$(TEST_SELECT),$(TEST_PKGS))
	@echo 'test: tests/integration and tests/e2e not covered here — they need Postgres (make db-up integration e2e); tests/workload and tests/standalone need a Docker daemon and the pinned images (make workload-integration, make standalone-e2e)'
	@go test -count=1 $(TEST_PKGS)
	@scripts/image-build-test.sh
	@scripts/image-test-ecr-test.sh

# `go build ./...` exits 0 having done nothing should that pattern ever
# resolve to no package, so build counts the packages it selects first.
# It runs the pattern rather than the list: go build skips a test-only
# package swept in by ./... and fails on one named directly.
build:
	$(call go-packages,build,$(GO_SELECT),$(GO_PKGS))
	@go build ./...

# gen regenerates everything derived from a source of truth: the
# OpenAPI server, sqlc queries, protobuf code, and mocks.
gen:
	$(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
	$(OAPI_CODEGEN) --config api/machine-codegen.yaml api/machine.yaml
	$(SQLC) generate
	$(MOCKERY)
	$(BUF) generate
	$(GOIMPORTS) -w proto/gen

# The output each generator owns, read from its config, since no
# generator answers "what do I own" and none deletes what it stops
# producing. The paths are literal rather than globs: a glob expands
# against the working tree, so a deleted generated directory would fall
# outside it and read as clean (#360).
GEN_PATHS := $(shell { sed -n 's/^output: //p' api/oapi-codegen.yaml api/machine-codegen.yaml; \
	sed -n 's/^        out: //p' sqlc.yaml; \
	sed -n 's/^    out: //p' buf.gen.yaml; \
	sed -n 's|^  $(shell sed -n 's/^module //p' go.mod)/\(.*\):$$|\1/mocks|p' .mockery.yaml; } | sort -u)

# check-gen fails on owned output that is modified, untracked or
# deleted, and on tracked output carrying the "Code generated ... DO
# NOT EDIT." marker that no owned path claims (#175, #360). The shape
# checks run before gen, which would otherwise regenerate a deleted
# path before any status could see it.
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

# The pairs check-toolchain holds together, written as the site
# declaring a version, that version, the Dockerfile, and the image whose
# tag has to carry it. `.github/dependabot.yml`'s docker entry moves the
# FROM line alone, so each pair is a half a bump can arrive without.
#
# Both versions reach the script through the environment and never
# through the command text: each is read out of a file in the tree,
# which ci.md §Security baseline keeps out of a command line.
export TOOLCHAIN_PAIRS = \
	web/.nvmrc,$(NODE_VERSION),deploy/local/Dockerfile.web,node \
	go.mod,$(GO_DIRECTIVE),deploy/local/Dockerfile,golang \
	go.mod,$(GO_DIRECTIVE),tests/testprincipal/Dockerfile,golang

check-toolchain:
	@scripts/check-toolchain.sh

# The template commit this repository has taken every improvement from,
# up to. Nothing can derive it, since an adapted file differs from the
# template by design, so the pull request that takes template content
# moves it in a diff a reviewer reads (#775, #777).
TEMPLATE_SYNCED := 3d0c87aaf8d363fc304df45a588928febc97097a

template-diff:
	@TEMPLATE_SYNCED='$(TEMPLATE_SYNCED)' scripts/template-diff.sh

# Q reaches awk through the environment and never through the command
# text, so a phrase holding a quote is searched for rather than run.
export SEARCH_Q = $(Q)

search:
	@scripts/search.sh

# db-up starts postgres and the openbao `integration` signs against, and
# creates the test database beside `garam` if absent: Postgres has no
# CREATE DATABASE IF NOT EXISTS, so it asks psql first. Output and exit
# status are read apart, because one non-zero status cannot tell a
# failed query from a clean negative (#344).
db-up:
	$(COMPOSE_LOCAL) up -d --wait postgres openbao
	@present="$$($(COMPOSE_LOCAL) exec -T postgres psql -U garam -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$(TEST_DB_NAME)'")" \
		|| { echo "db-up: could not ask postgres whether $(TEST_DB_NAME) exists" >&2; exit 1; }; \
		test -n "$$present" || $(COMPOSE_LOCAL) exec -T postgres createdb -U garam $(TEST_DB_NAME)

db-down:
	$(COMPOSE_LOCAL) rm -sf postgres openbao

# db-prune drops the per-worktree databases db-up created for worktrees
# that are gone. It is in no whole-set name and no workflow: a runner
# starts empty, and the target is destructive on a developer machine.
# The script holds the keep set and the controls it rests on (#943).
db-prune:
	@scripts/db-prune.sh

# backend-up starts what `make web-e2e` drives — postgres, the one-shot
# migration, authz and api — and returns once api answers; the
# profile-gated `web` service stays down. It builds first, migrations
# included, because compose starts an existing image stale and reports
# it healthy (#297).
backend-up:
	$(COMPOSE_LOCAL) up -d --build --wait api

# Only packages holding a test file are listed, because `go test` exits
# 0 with `[no test files]` on one holding none — a build tag that
# stopped matching reads like the run that did the work. That condition
# then empties a list and fails below.
INTEGRATION_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/integration/...)
INTEGRATION_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/integration/...)
E2E_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/e2e/...)
E2E_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/e2e/...)

# integration runs the store round-trips and the services over them in
# process, against real Postgres, and the Transit custodian against the
# dev OpenBao (see db-up).
integration:
	@echo 'integration: $(words $(INTEGRATION_PKGS)) packages, $(words $(INTEGRATION_TEST_FILES)) test files, against $(TEST_DB_NAME) and the openbao at $(OPENBAO_HOST_ADDR)'
	@test -n '$(INTEGRATION_PKGS)' || { echo 'integration: nothing to run — no package under ./tests/integration/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "integration: no postgres published on this host — run make db-up" >&2; exit 1; }
	@test -n '$(OPENBAO_HOST_ADDR)' && test -n '$(OPENBAO_CONTAINER)' || { echo "integration: no openbao published on this host — run make db-up" >&2; exit 1; }
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' GARAM_TEST_OPENBAO_ADDR='https://$(OPENBAO_HOST_ADDR)' \
		GARAM_TEST_OPENBAO_CONTAINER='$(OPENBAO_CONTAINER)' go test -count=1 $(INTEGRATION_PKGS)

# e2e runs the built binary as the processes a deployment runs, against
# the same Postgres (see db-up).
e2e:
	@echo 'e2e: $(words $(E2E_PKGS)) packages, $(words $(E2E_TEST_FILES)) test files, against $(TEST_DB_NAME)'
	@test -n '$(E2E_PKGS)' || { echo 'e2e: nothing to run — no package under ./tests/e2e/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make db-up" >&2; exit 1; }
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 $(E2E_PKGS)

# The Sherlock release the real-runtime suites run against, by tag and
# digest, recorded here alone (#1220). v0.9.0 keeps v0.8.0's ingress
# that verifies garam's signature (ADR-0101), its issuer configuration
# and owner-only state files (sherlock#1025), and its contracts
# (sherlock#1110); it adds `sherlock workspace read|write|list|exec`
# and `sherlock workspace mcp`, and a plain `sherlock workspace` now
# refuses a positional argument (#1568). The PM moves both images
# together to a published release (AGENTS.md §Roles). Either
# GARAM_TEST_*_IMAGE in the environment overrides it.
SHERLOCK_REGISTRY := 486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam
GARAM_TEST_RUNTIME_IMAGE ?= $(SHERLOCK_REGISTRY)/sherlock-agent:0.9.0@sha256:1bb1ee0b5464b8d166c2948f3feae0dd217649a9e94cdb7655175c8789ecae2a
GARAM_TEST_WORKSPACE_IMAGE ?= $(SHERLOCK_REGISTRY)/sherlock-workspace:0.9.0@sha256:da953edcd972aa325a37bce801de324a4c4ac576077807af13724041dce984f6
export GARAM_TEST_RUNTIME_IMAGE GARAM_TEST_WORKSPACE_IMAGE

# workload-integration runs a standalone host's supervisor against a
# real Docker daemon and the images it runs (ADR-0085). It is in no
# whole-set name and in no workflow: the pinned Sherlock images are
# pulled from a registry CI holds no credential for.
# GARAM_TEST_DOCKER_HOST names the daemon, a rootless one's unix://
# socket; GARAM_TEST_RUNTIME_IMAGE and GARAM_TEST_WORKSPACE_IMAGE
# default to the Sherlock pin above, and GARAM_TEST_ADAPTER_IMAGE names
# this tree's garam image by digest or ID.
WORKLOAD_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/workload/...)
WORKLOAD_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/workload/...)

workload-integration:
	@echo 'workload-integration: $(words $(WORKLOAD_PKGS)) packages, $(words $(WORKLOAD_TEST_FILES)) test files, against $(GARAM_TEST_DOCKER_HOST)'
	@test -n '$(WORKLOAD_PKGS)' || { echo 'workload-integration: nothing to run — no package under ./tests/workload/... holds a test file'; exit 1; }
	go test -count=1 -timeout 30m $(WORKLOAD_PKGS)

# standalone-e2e runs a standalone host end to end (ADR-0085, #1180):
# the shipped binary prepares, enrolls and runs the supervisor against
# garam's own image, over a database tests/testprincipal creates on the
# Postgres db-up starts, with a real model answering. Like
# workload-integration it is in no whole-set name and in no workflow,
# for the same images and a model credential besides. Beside that
# target's GARAM_TEST_DOCKER_HOST and Sherlock images it reads
# GARAM_TEST_GARAM_IMAGE (this tree's image-build output, by ID),
# GARAM_TEST_TEI_IMAGE and GARAM_TEST_MODEL_ENV_FILE, an owner-only
# KEY=VALUE file; the server and its container come from compose.
STANDALONE_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/standalone/...)
STANDALONE_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/standalone/...)

standalone-e2e:
	@echo 'standalone-e2e: $(words $(STANDALONE_PKGS)) packages, $(words $(STANDALONE_TEST_FILES)) test files, against $(GARAM_TEST_DOCKER_HOST)'
	@test -n '$(STANDALONE_PKGS)' || { echo 'standalone-e2e: nothing to run — no package under ./tests/standalone/... holds a test file'; exit 1; }
	@test -n '$(DB_HOST_ADDR)' || { echo "standalone-e2e: no postgres published on this host — run make db-up" >&2; exit 1; }
	GARAM_TEST_SERVER_URL='postgres://garam:garam@$(DB_HOST_ADDR)/postgres?sslmode=disable' \
		GARAM_TEST_POSTGRES_CONTAINER="$$($(COMPOSE_LOCAL) ps -q postgres)" \
		go test -count=1 -timeout 60m $(STANDALONE_PKGS)

# mutation runs gremlins over internal/ and reports the mutants no test
# noticed; docs/architecture/README.md §Operation records why it is in
# no workflow and what the coefficient and TMPDIR cost (#875). The
# default coefficient times every mutant out and gremlins still exits
# 0, so the counts are read from its report, removed first since it
# writes none when it finds nothing, and zero tested fails. Point TMPDIR
# at a disk: each worker copies the module, web/ builds included.
MUTATION_COEFFICIENT := 300
MUTATION_REPORT := mutation.json

mutation:
	@echo 'mutation: internal/, coefficient $(MUTATION_COEFFICIENT), report $(MUTATION_REPORT)'
	@rm -f '$(MUTATION_REPORT)'
	@$(GREMLINS) unleash --timeout-coefficient $(MUTATION_COEFFICIENT) -o '$(MUTATION_REPORT)' internal
	@killed="$$(sed -n 's/.*"mutants_killed":\([0-9]*\).*/\1/p' '$(MUTATION_REPORT)')"; \
		lived="$$(sed -n 's/.*"mutants_lived":\([0-9]*\).*/\1/p' '$(MUTATION_REPORT)')"; \
		efficacy="$$(sed -n 's/.*"test_efficacy":\([0-9.]*\).*/\1/p' '$(MUTATION_REPORT)')"; \
		test -n "$$killed" && test -n "$$lived" && test -n "$$efficacy" \
			|| { echo 'mutation: $(MUTATION_REPORT) carried no counts — the parse above stopped matching'; exit 1; }; \
		echo "mutation: $$killed killed, $$lived survived, efficacy $$efficacy%"; \
		test "$$((killed + lived))" -gt 0 \
			|| { echo 'mutation: no mutant was tested — every one timed out or none was found, and neither is a result'; exit 1; }

# The whole set, both toolchains, under one name (#789). `web-ci` runs
# last so the cheaper Go half fails first, and it puts pnpm and the node
# version web/.nvmrc names on every contributor's path.
ci: format lint test build check-gen check-toolchain chart web-ci

# ---------------------------------------------------------------------
# web/ — the Next.js frontend (ADR-0009). It keeps its own toolchain and
# lockfile; `web-ci` is the half of `ci` that covers it.
# ---------------------------------------------------------------------

web-install:
	$(PNPM) install --frozen-lockfile

# eslint prints nothing and exits 0 both when it covered the tree and
# when its ignore patterns left it a fraction of it, so what it covered
# is read out of its own JSON report: one entry per file it linted. A
# report naming no file fails the target below rather than reporting a
# count nobody checked. One pass writes the report, and the same report
# is printed through eslint's own stylish formatter, so the count and
# the lint result come from one run whose exit status the target keeps
# (#1528).
WEB_LINT_REPORT = const [file, status] = process.argv.slice(1); \
	const text = require("fs").readFileSync(file, "utf8"); \
	if (!text) { console.error("web-lint: eslint exited " + status + " and wrote no report"); process.exit(1); } \
	const results = JSON.parse(text); \
	console.log("web-lint: " + results.length + " files (eslint)"); \
	if (!results.length) { console.error("web-lint: nothing to lint — the eslint report named no file"); process.exit(1); } \
	new (require("eslint").ESLint)().loadFormatter("stylish").then((f) => process.stdout.write(f.format(results)));

# tsc reads its inputs from tsconfig.json rather than from eslint's
# ignore patterns, so the TS18003 it raises on its own empty input says
# nothing about what eslint read. The count above is the eslint half
# reporting for itself.
web-lint:
	@report=$$(mktemp) && trap 'rm -f "$$report"' EXIT; \
		status=0; $(PNPM) exec eslint --format json --output-file "$$report" || status=$$?; \
		$(PNPM) exec node -e '$(WEB_LINT_REPORT)' "$$report" "$$status" || exit 1; \
		exit "$$status"
	$(PNPM) typecheck

# prettier reports no count and exits 0 when its ignore files leave it
# nothing, so what it opens is asked of prettier one path at a time. Its
# debug log is not that ask: prettier colours the `[debug]` prefix
# whenever CI is set in the environment, and those escape sequences left
# the scrape that read it matching nothing on every runner (#822).
#
# The candidates are what git has under web/ — tracked, plus untracked
# files no ignore file excludes — the set prettier's own default
# --ignore-path is aligned to.
WEB_FORMAT_CANDIDATES = $(shell git -C web ls-files --cached --others --exclude-standard)

# getFileInfo is prettier's documented answer for one path: `ignored`
# applies both ignore files, and a null `inferredParser` is a file it
# skips, so an ignore file that swallowed every source cannot pass on a
# count of skipped files (#828). Each path is quoted for the App
# Router's parentheses and brackets, --silent keeps pnpm's install
# lines out of the paths, and the exit status is read because a crash
# prints nothing.
WEB_FORMAT_FILES = $(shell $(PNPM) --silent exec node -e 'const p=require("prettier");(async()=>{for(const f of process.argv.slice(1)){const i=await p.getFileInfo(f,{ignorePath:[".gitignore",".prettierignore"]});if(!i.ignored&&i.inferredParser)console.log(f)}})()' $(patsubst %,'%',$(WEB_FORMAT_CANDIDATES)))$(if $(filter-out 0,$(.SHELLSTATUS)),$(error web-format: prettier could not be asked which files it checks — the getFileInfo run above exited $(.SHELLSTATUS)))

web-format:
	@echo 'web-format: $(words $(WEB_FORMAT_FILES)) files (prettier)'
	@test -n '$(WEB_FORMAT_CANDIDATES)' || { echo 'web-format: nothing to check — git lists no file under web/'; exit 1; }
	@test -n '$(WEB_FORMAT_FILES)' || { echo 'web-format: nothing to check — prettier called all $(words $(WEB_FORMAT_CANDIDATES)) of them ignored or unparseable'; exit 1; }
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

# image-build builds the two images a release publishes, each from what
# .github/workflows/release.yml says it is built from; nothing else
# builds either (#760, #850). IMAGE_REGISTRY makes it a development
# build under `garam-dev/` in that registry, tagged IMAGE_TAG or HEAD's
# first twelve hex digits (ADR-0071, ADR-0097); the script refuses any
# other tag and pushes nothing.
IMAGE_BUILD_ARGS = $(if $(IMAGE_REGISTRY),--registry '$(IMAGE_REGISTRY)') $(if $(IMAGE_TAG),--tag '$(IMAGE_TAG)') $(if $(IMAGE_BUILD_CACHE),--cache '$(IMAGE_BUILD_CACHE)')

image-build:
	scripts/image-build.sh $(IMAGE_BUILD_ARGS)

# What image-build tagged the web image, asked of the script that tags
# it by the build context, so a rename in release.yml cannot leave this
# naming an image an earlier run built (#870). Empty when the release
# builds none from the web context, which the target reports.
WEB_IMAGE = $(shell scripts/image-build.sh --names $(IMAGE_BUILD_ARGS) | sed -n 's|^web ||p')

# web-image-e2e checks the built image rather than the app: that the
# standalone server starts, answers both probes and reaches the baked
# `/api/*` target. Its subject is the web image image-build produced,
# carrying the GARAM_API_URL a release bakes. GARAM_WEB_IMAGE names one
# to check instead, which is how an image pulled from ECR by digest is
# verified, and then nothing is built here.
web-image-e2e: $(if $(GARAM_WEB_IMAGE),,image-build)
	@image='$(or $(GARAM_WEB_IMAGE),$(WEB_IMAGE))'; \
		test -n "$$image" || { echo 'web-image-e2e: nothing names the image to check — .github/workflows/release.yml builds none from the web context'; exit 1; }; \
		GARAM_WEB_IMAGE="$$image" tests/image/web.sh

# test-image builds the test image another repository's CI copies
# garam, testprincipal and the migrations out of (ADR-0092), tagged
# with HEAD's full hash under TEST_IMAGE_REPOSITORY; the script refuses
# a working tree that differs from HEAD. test-image-e2e checks it as
# that consumer reads it. Both need Docker, so neither is in `ci`.
TEST_IMAGE_REPOSITORY ?= garam-test

test-image:
	scripts/image-test.sh '$(TEST_IMAGE_REPOSITORY)'

test-image-e2e: test-image
	tests/image/test-image.sh '$(TEST_IMAGE_REPOSITORY):$(shell git rev-parse HEAD)'

# An operator pushes the test image to ECR and moves a consumer's pin
# under their own AWS credentials (ADR-0093), so neither target is in
# `ci`. test-image-push checks HEAD as test-image-e2e does and pushes it
# under its first twelve hex digits (ADR-0071); test-image-pin moves
# `pinned-$(CONSUMER)` to the image of $(COMMIT). Both scripts hold the
# repository, so no make variable can redirect them.
test-image-push:
	scripts/image-test-push.sh

test-image-pin:
	scripts/image-test-pin.sh '$(COMMIT)' '$(CONSUMER)'

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

# Every environment variable garam reads, from its declarations: the
# envName block in internal/cli, and web/src/env.ts parsed for its own
# shape. The guards chart-render runs first keep the block equal to the
# reads (#734), and both sides read one character class, since a name
# outside it drops out of both sets and passes unseen (#864).
CHART_GO_ENV = $(shell grep -rhoE 'envName = "[A-Z][A-Z0-9_]*"' --include=*.go internal cmd | sed 's/.*"\([A-Z][A-Z0-9_]*\)"/\1/' | sort -u)
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

chart: chart-lint chart-render chart-identity chart-names chart-rewrite chart-guards chart-security chart-package

chart-lint:
	@echo 'chart-lint: $(words $(CHART_TEMPLATES)) templates, $(words $(CHART_VALUES)) value files (helm lint --strict)'
	@test -n '$(CHART_TEMPLATES)' || { echo 'chart-lint: nothing to lint — $(CHART_DIR)/templates holds no template'; exit 1; }
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
		|| { echo 'chart-render: the source sweep read no name — the envName block or the env.ts parse is broken'; exit 1; }
	@# What makes the block above the whole of what the Go source reads:
	@# every read reaches os.Getenv, and os.Getenv is called once, inside
	@# envOr. A second read is a name this check cannot see.
	@reads="$$(grep -rhoE 'os\.(Getenv|LookupEnv|Environ)\(' --include=*.go --exclude=*_test.go internal cmd | wc -l)"; \
		test "$$reads" -eq 1 \
		|| { echo "chart-render: $$reads reads of the environment under internal|cmd, expected the one envOr holds — a read outside it is a name this check cannot see"; exit 1; }
	@# And every name reaching envOr is declared rather than written at
	@# the call, so no reader can name a variable this check does not
	@# enumerate. An env-shaped literal passed to any call is that.
	@stray="$$(grep -rnE '[A-Za-z_][A-Za-z0-9_]*\("(GARAM|GOOGLE)_[A-Z_]+"' --include=*.go --exclude=*_test.go internal cmd || true)"; \
		test -z "$$stray" \
		|| { echo 'chart-render: an environment name is written at a call site rather than declared in internal/cli, so this check cannot enumerate it:'; echo "$$stray"; exit 1; }
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

# chart-names renders under the longest release name Helm accepts and
# holds every object name to 63 characters, which an apply enforces and
# a render does not (#725, #732). `^  name: ` is an object's own name,
# and each render lands in a file before it is parsed, so a failed
# render fails set -e rather than contributing no name (#864).
CHART_LONG_RELEASE := garam-production-eu-west-1-primary-tenant-alpha-01234

chart-names:
	@echo 'chart-names: $(words $(CHART_VALUES)) value files rendered as $(CHART_LONG_RELEASE) (release length $(shell printf %s '$(CHART_LONG_RELEASE)' | wc -c), Helm caps it at 53) — object names only, $(CHART_DIR)/README.md §What the checks are complete over'
	@test -n '$(CHART_VALUES)' || { echo 'chart-names: nothing to render — $(CHART_DIR)/ci holds no value file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(CHART_VALUES); do \
			$(HELM) template $(CHART_LONG_RELEASE) $(CHART_DIR) --values "$$v" > "$$out/render"; \
			sed -n 's/^  name: \(.*\)$$/\1/p' "$$out/render" >> "$$out/names"; \
		done; \
		total="$$(sort -u "$$out/names" | wc -l)"; \
		test "$$total" -gt 0 || { echo 'chart-names: the render named no object — the name parse is broken'; exit 1; }; \
		over="$$(awk 'length($$0) > 63 { print length($$0)" "$$0 }' "$$out/names" | sort -u)"; \
		test -z "$$over" || { echo 'chart-names: over the 63-character limit, which an apply refuses and a render does not:'; echo "$$over"; exit 1; }; \
		echo "chart-names: $$total distinct object names, longest $$(awk '{ if (length($$0) > m) m = length($$0) } END { print m }' "$$out/names"), none over 63"

# chart-rewrite holds the `/api/*` target a release bakes into the web
# image against the api Service rendered under release `garam`, two
# facts nothing else reads together (#861). The render under
# $(CHART_LONG_RELEASE) is the control and must differ, or the equality
# proves nothing (ADR-0056). scripts/image-build.sh reads the same file
# for what the release builds.
CHART_BAKED_API_URL = $(shell sed -n 's/^ *GARAM_API_URL=\(.*\)$$/\1/p' .github/workflows/release.yml)

chart-rewrite:
	@echo 'chart-rewrite: $(CHART_BAKED_API_URL), baked by .github/workflows/release.yml — one URL against the api Service, $(CHART_DIR)/README.md §What the checks are complete over'
	@test '$(words $(CHART_BAKED_API_URL))' = 1 \
		|| { echo 'chart-rewrite: .github/workflows/release.yml bakes $(words $(CHART_BAKED_API_URL)) GARAM_API_URL values, expected the one a release is bound to — the parse is broken'; exit 1; }
	@set -e; \
		apiurl() { $(HELM) template "$$1" $(CHART_DIR) --values $(CHART_DEFAULT_VALUES) | awk ' \
			function flush() { if (kind == "Service" && comp == "api") printf "http://%s:%s\n", name, port } \
			/^---$$/ { flush(); kind = ""; comp = ""; name = ""; port = ""; next } \
			/^kind: / { kind = $$2 } \
			/^  name: / { if (name == "") name = $$2 } \
			/app.kubernetes.io\/component: / { if (comp == "") comp = $$2 } \
			$$1 == "port:" { if (port == "") port = $$2 } \
			END { flush() }'; }; \
		rendered="$$(apiurl garam)"; \
		test -n "$$rendered" && test "$$(printf '%s\n' "$$rendered" | wc -l)" -eq 1 \
			|| { echo "chart-rewrite: the render named no single api Service — the parse is broken:"; echo "$$rendered"; exit 1; }; \
		test "$$rendered" = '$(CHART_BAKED_API_URL)' \
			|| { echo "chart-rewrite: the release bakes $(CHART_BAKED_API_URL) and release garam renders $$rendered — a published web image reaches a name this chart does not render, and nothing after the install says so"; exit 1; }; \
		control="$$(apiurl $(CHART_LONG_RELEASE))"; \
		test "$$control" != '$(CHART_BAKED_API_URL)' \
			|| { echo "chart-rewrite: release $(CHART_LONG_RELEASE) renders $$control as well, so the equality above holds under every release name and this check reads nothing"; exit 1; }; \
		echo "chart-rewrite: release garam renders $$rendered; release $(CHART_LONG_RELEASE) renders $$control, which no published web image reaches"

# chart-guards renders each values file under ci/refused/ and holds that
# helm refuses it with `execution error at (<template>): <value> `,
# naming the value the file is named after (#726, #740). The message is
# matched rather than the status or the file's path, which carries the
# same string (#864). The last render is the control:
# minimal-values.yaml must still render.
CHART_REFUSED = $(wildcard $(CHART_DIR)/ci/refused/*.yaml)

chart-guards:
	@echo 'chart-guards: $(words $(CHART_REFUSED)) values files that must be refused, each naming the value it is named after'
	@test -n '$(CHART_REFUSED)' || { echo 'chart-guards: nothing to check — $(CHART_DIR)/ci/refused holds no values file'; exit 1; }
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		for v in $(CHART_REFUSED); do \
			want="$$(basename "$$v" .yaml)"; \
			if $(HELM) template garam $(CHART_DIR) --values $(CHART_DIR)/ci/minimal-values.yaml --values "$$v" >/dev/null 2>"$$out/err"; then \
				echo "chart-guards: $$v rendered, and the guard for $$want did not fire"; exit 1; \
			fi; \
			key="$$(printf %s "$$want" | sed 's/\./\\./g')"; \
			grep -qE "execution error at \([^)]*\): $$key( |\$$)" "$$out/err" \
				|| { echo "chart-guards: $$v was refused, but not by the guard for $$want — a refusal for another reason, or one whose message does not open with the value:"; cat "$$out/err"; exit 1; }; \
		done; \
		$(HELM) template garam $(CHART_DIR) --values $(CHART_DIR)/ci/minimal-values.yaml >/dev/null \
			|| { echo 'chart-guards: the control render was refused too — the guards refuse a chart that should install'; exit 1; }; \
		echo "chart-guards: $(words $(CHART_REFUSED)) refused as named, and minimal-values.yaml still renders"

chart-security:
	@CHART_DIR='$(CHART_DIR)' HELM='$(HELM)' \
		CHART_IDENTITIES='$(CHART_IDENTITIES)' CHART_VALUES='$(CHART_VALUES)' \
		scripts/chart-security.sh

# chart-package searches the artifact `helm package` produces, which no
# other check opens, for a resource name set as a key: no figure is one
# garam measured (deployment-contract.md §Not established). Its zero
# guard counts lines setting `resources:`, not lines holding the word,
# which this chart's README carries in prose (#582, #864).
chart-package:
	@echo 'chart-package: $(words $(CHART_RESOURCE_NAMES)) resource names searched for as keys in the packaged artifact — resource quantities only, $(CHART_DIR)/README.md §What the checks are complete over'
	@set -e; \
		out="$$(mktemp -d)"; trap 'rm -rf "$$out"' EXIT; \
		$(HELM) package $(CHART_DIR) --destination "$$out" >/dev/null; \
		tgz="$$(echo "$$out"/*.tgz)"; \
		mkdir "$$out/unpacked"; tar -xzf "$$tgz" -C "$$out/unpacked"; \
		cd "$$out/unpacked"; \
		files="$$(find . -type f | wc -l)"; \
		named="$$(grep -rnE '^[[:space:]]*resources:' . | wc -l)"; \
		test "$$files" -gt 0 && test "$$named" -gt 0 \
			|| { echo 'chart-package: the unpacked artifact sets no resources key — the package or the search read nothing'; exit 1; }; \
		re="$$(echo '$(CHART_RESOURCE_NAMES)' | tr ' ' '|')"; \
		found="$$(grep -rnE "\b($$re):" . || true)"; \
		test -z "$$found" || { echo "chart-package: the artifact sets a resource name, and no figure is published for any garam process (docs/architecture/deployment-contract.md §Not established):"; echo "$$found"; exit 1; }; \
		echo "chart-package: $$(basename "$$tgz") — $$files files, $$named lines setting a resources key, 0 setting one of the names"

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

# The CloudNativePG operator, pinned to the commit carrying the
# manifest; deploy/local/minikube/README.md §What runs records why
# v1.30.0 and where its advisories are re-checked. Nothing proposes a
# bump, so it is the PM's to move (AGENTS.md §Roles).
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
