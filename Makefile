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

# One dev Postgres per machine, not per worktree:
# deploy/local/docker-compose.yml pins `name: garam-local`, so every
# worktree addresses the same compose project and container.
#
# The integration suite truncates every table and both Go suites
# migrate from the same migrations/, so they share a database per
# worktree on that one server. The suffix is git's own name for a
# linked worktree — unique per worktree and fixed for its lifetime —
# and empty in the main checkout, which keeps garam_test. The local
# stack keeps `garam`.
TEST_DB_NAME ?= garam_test$(shell git rev-parse --git-dir | sed -n 's|.*/worktrees/|_|p')
export TEST_DB_NAME

COMPOSE_LOCAL := docker compose -f deploy/local/docker-compose.yml
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
GO_PKGS = $(shell go list ./...)

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
# `check-contents-list.sh` and where it stays so template-diff can
# measure it against the path it comes from.
WORKFLOW_FILES = $(shell find .github/workflows -type f \( -name '*.yml' -o -name '*.yaml' \))
SHELL_SCRIPTS = $(shell find . -maxdepth 1 -type f -name '*.sh') $(shell find scripts -type f -name '*.sh')

# actionlint reads a `run:` block with shellcheck, and a python one with
# pyflakes, when it finds them on PATH, and drops the rule where it does
# not — reporting that in -verbose and nowhere else. The runner image
# ships shellcheck and a contributor's machine mostly does not, so left
# inferred this is not the same check in the two places. Both are named
# off, so the absence is a decision rather than a property of the
# machine, and what it leaves uncovered is recorded on #840.
#
# Three of its checks are keyed on `owner/repo@ref` and hold only for a
# ref its bundled database names, and that database is keyed on
# major-version tags: whether a `with:` key is an input the action
# declares, whether the action's runner is too old, and the expression
# rule's typing of `steps.<id>.outputs.<name>`, which falls back to
# `map[string]string` otherwise. Neither the full commit SHA
# docs/convention/ci.md §Reference implementation requires nor the exact
# tag recorded beside it is such a ref, so all three reach no action
# here, release.yml's four `steps.*.outputs.*` references included
# (#854).
# What reaches every workflow whatever the ref is the rest of the
# expression, `needs:` and shell-name checking.
#
# shfmt is a formatter and this repository's shell layout is not the one
# it prints, so it is run as the POSIX parser it is built on: output
# discarded, exit status read. That status answers whether the file
# parses and not whether it is formatted — a file diverging from shfmt's
# layout passes, a syntax error fails, and a bashism fails only where
# the POSIX grammar has no shape for it: `arr=(a b)` and `function f`
# fail, while `[[ ]]`, `source` and `local` parse as ordinary commands
# and pass. `-ln posix` fixes the dialect every script here declares
# rather than taking it from each shebang, so a script that later says
# bash fails here instead of being read as bash.
#
# check-contents-list.sh reads the Markdown, against
# docs/convention/documentation.md §Format. It selects its own files and
# prints that selector with the count it produced, and fails when the
# count is zero, so the line below states no Markdown count — a second
# selector here would be one more thing to keep equal to the script's.
lint:
	@echo 'lint: $(words $(GO_PKGS)) packages (go vet), $(words $(PROTO_FILES)) proto files (buf lint), $(words $(WORKFLOW_FILES)) workflows (actionlint), $(words $(SHELL_SCRIPTS)) shell scripts (shfmt); Markdown counted by check-contents-list.sh below'
	@test -n '$(GO_PKGS)' && test -n '$(PROTO_FILES)' && test -n '$(WORKFLOW_FILES)' && test -n '$(SHELL_SCRIPTS)' \
		|| { echo 'lint: nothing to lint — go list, the buf module, .github/workflows or the shell sweep matched no input; the counts above name which'; exit 1; }
	@go vet $(GO_PKGS)
	@$(BUF) lint
	@$(ACTIONLINT) -shellcheck= -pyflakes= $(WORKFLOW_FILES)
	@$(SHFMT) -ln posix $(SHELL_SCRIPTS) >/dev/null
	@./check-contents-list.sh

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
# tests/integration and tests/e2e each need the Postgres db-up starts
# and fail without it, so test covers every other package and names the
# two it left out. Should either filter stop matching, that suite is
# swept back in and fails for want of a database rather than skipping
# quietly.
#
# Two tests are outside Go, each needing git or nothing and no Docker,
# and each printing its own count: scripts/image-build-test.sh, which
# tags a development image-build accepts (ADR-0071) and under which
# namespace it names them (ADR-0097), and
# scripts/image-test-ecr-test.sh, what test-image-push and
# test-image-pin refuse before they reach AWS (ADR-0093).
TEST_PKGS = $(filter-out %/tests/integration %/tests/e2e %/tests/workload %/tests/standalone,$(GO_PKGS))

test:
	@echo 'test: $(words $(TEST_PKGS)) packages; tests/integration and tests/e2e not covered here — they need Postgres (make db-up integration e2e); tests/workload and tests/standalone need a Docker daemon and the pinned images (make workload-integration, make standalone-e2e)'
	@test -n '$(TEST_PKGS)' || { echo 'test: nothing to test — the package list is empty, or the filter above matched all of it'; exit 1; }
	@go test -count=1 $(TEST_PKGS)
	@scripts/image-build-test.sh
	@scripts/image-test-ecr-test.sh

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
# up to. Nothing derives it: an adapted file differs from the template by
# design, so no comparison separates an improvement this project declined
# from one it never saw, and where the two share no blob anywhere in
# either history — docs/convention/stack-go.md, the file #775 was opened
# for — there is nothing to compare at all. The pull request that takes
# template content moves it. Left behind it names commits already
# received, which is loud; moved past them it goes quiet, so it moves in
# a diff a reviewer reads rather than in a computation nobody sees.
TEMPLATE_SYNCED := ac201ad0aec045b5bf5fa8fb15b857ded621831e

template-diff:
	@TEMPLATE_SYNCED='$(TEMPLATE_SYNCED)' scripts/template-diff.sh

# Q reaches awk through the environment and never through the command
# text, so a phrase holding a quote is searched for rather than run.
export SEARCH_Q = $(Q)

search:
	@scripts/search.sh

# db-up starts the stack's postgres service alone and makes sure the test
# database exists next to `garam`. Postgres has no CREATE DATABASE IF NOT
# EXISTS, so ask before creating. Each Go suite migrates it itself.
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

# db-prune drops the per-worktree databases db-up created for worktrees
# that are gone. It is in no whole-set name and in no workflow: a runner
# starts empty and its databases go with it, so nothing there
# accumulates and there would be nothing for a run to drop. It is
# destructive on a developer machine, which is the other reason a person
# invokes it.
#
# COMPOSE_LOCAL and TEST_DB_NAME are exported above; the script holds
# what it keeps, the controls the keep set rests on, and the counts.
db-prune:
	@scripts/db-prune.sh

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

# What each of the two suites against real Postgres covers, listed once
# so the targets report the set they ran rather than name a ./... pattern
# whose match nobody sees. `go test` prints `[no test files]` and exits 0
# for a package holding none — a build tag that stopped matching, a
# renamed file — which reads exactly like the run that did the work. Only
# packages holding a test file are listed, so that condition empties a
# list and fails below instead of reaching go test.
INTEGRATION_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/integration/...)
INTEGRATION_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/integration/...)
E2E_PKGS = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./tests/e2e/...)
E2E_TEST_FILES = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' ./tests/e2e/...)

# integration runs the store round-trips and the services over them in
# process, against real Postgres (see db-up).
integration:
	@echo 'integration: $(words $(INTEGRATION_PKGS)) packages, $(words $(INTEGRATION_TEST_FILES)) test files, against $(TEST_DB_NAME)'
	@test -n '$(INTEGRATION_PKGS)' || { echo 'integration: nothing to run — no package under ./tests/integration/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "integration: no postgres published on this host — run make db-up" >&2; exit 1; }
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 $(INTEGRATION_PKGS)

# e2e runs the built binary as the processes a deployment runs, against
# the same Postgres (see db-up).
e2e:
	@echo 'e2e: $(words $(E2E_PKGS)) packages, $(words $(E2E_TEST_FILES)) test files, against $(TEST_DB_NAME)'
	@test -n '$(E2E_PKGS)' || { echo 'e2e: nothing to run — no package under ./tests/e2e/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make db-up" >&2; exit 1; }
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 $(E2E_PKGS)

# The Sherlock release the real-runtime suites below run against, by
# tag and digest, and the one place it is recorded (#1220). v0.5.0
# carries v0.4.0's ingress, which verifies garam's signature on a
# message (ADR-0101), and its issuer configuration. The PM moves it,
# as AGENTS.md §Roles has the PM move a pinned dependency that names no
# other mover: both images together, to a release garamsh/sherlock
# published. A GARAM_TEST_RUNTIME_IMAGE or GARAM_TEST_WORKSPACE_IMAGE
# in the environment still overrides it.
SHERLOCK_REGISTRY := 486152169996.dkr.ecr.ap-northeast-2.amazonaws.com/garam
GARAM_TEST_RUNTIME_IMAGE ?= $(SHERLOCK_REGISTRY)/sherlock-agent:0.5.0@sha256:f902ac3e8a9e2c051057ff5ee333b5fe6d5ddfce740b450a4530e41f38f1ec03
GARAM_TEST_WORKSPACE_IMAGE ?= $(SHERLOCK_REGISTRY)/sherlock-workspace:0.5.0@sha256:ade684b626e40d65ccc33156f6709ca3de30210a8dd8557f79f692d765f4853b
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

# mutation asks of every line what the hunt behind #875 asked of a few
# by hand: whether the check at that line can fail. gremlins rewrites
# one operator at a time and reports the mutants no test noticed.
#
# It is in no whole-set name and in no workflow. A full run over
# internal/ takes minutes of every core it is given, `internal/` holds
# every package the module ships so a path filter would exclude little,
# and what the run buys is an audit of the suite rather than a gate on a
# change. docs/architecture/README.md §Operation records that decision;
# this target is what a contributor runs when the question is whether a
# test could have failed.
#
# The coefficient is a ratio and not a duration: gremlins allows each
# mutant's `go test` the coverage run's own duration times this number.
# That duration is whatever Go's test cache made it — two runs behind
# #875 measured it at 5.6 seconds and at 0.49 seconds — so 300 buys half
# an hour per mutant in the first case and two and a half minutes in the
# second. The smaller is enough for every mutant here that is not a
# genuine hang. The default of 3 buys a second and a half either way,
# which is less than compiling one package takes, so every mutant times
# out and gremlins prints `Test efficacy: 0.00%` and exits 0 having
# tested nothing.
#
# A long budget is paid for only by a mutant that hangs, which holds a
# worker for the whole of it. Those two runs spent the same 36 minutes
# of CPU and took 7 and 32 minutes of wall time, and the nine mutants
# that hang in delivery/gateway.go are the whole difference.
#
# A run where every mutant timed out is the one docs/convention/ci.md
# §One entry point per task says has to be told apart from the one that
# did the work, and the counts below are what tell them apart. They are
# read from the report gremlins writes rather than from its stream,
# which says the same thing on both runs. The report is json.Marshal
# output, so each key is followed directly by its value; a parse that
# comes back empty fails the target rather than printing a zero nobody
# checked.
#
# The report is removed first because gremlins writes none at all where
# it found nothing to run — it prints `No results to report.` and exits
# 0 — and a report left by the run before would then be read as this
# one's.
#
# gremlins 0.6.0 copies the whole module directory into TMPDIR once per
# worker (`internal/engine/workdir`), and a worker is a core by default.
# The Go tree is four megabytes; the `web/node_modules` and `web/.next`
# that `make web-install` and `make web-build` leave behind are another
# 856, so a run on twelve cores after `make ci` writes ten gigabytes —
# to a tmpfs, and so to RAM, wherever TMPDIR is one. Point TMPDIR at a
# disk, or run this on a tree that has not built the frontend.
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
# applies .gitignore and .prettierignore as that default does, and a
# null `inferredParser` is a file prettier opens and skips for want of a
# parser. Without that second half an ignore file that swallowed every
# source would still pass on a count the skipped files held above zero.
# The exit status is read because a crash prints nothing, and an empty
# list below has to mean an empty tree and nothing else.
#
# --silent because pnpm prints the install it verifies before `exec` on
# stdout, and those lines would be counted as paths. Each path is quoted
# because the App Router puts parentheses and brackets in these names.
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
# .github/workflows/release.yml says it is built from — the script reads
# that file rather than restating it. Nothing else builds either: `build`
# and `web-build` prove the Go module and the web app build, which is
# not that the images do (#760).
#
# IMAGE_REGISTRY makes it a development build (ADR-0071): the release's
# repositories under `garam-dev/` in that registry (ADR-0097), tagged
# IMAGE_TAG or, when that is empty, the first twelve hex digits of HEAD.
# The script refuses a bare release version or any other tag before it
# builds, and pushes nothing.
IMAGE_BUILD_ARGS = $(if $(IMAGE_REGISTRY),--registry '$(IMAGE_REGISTRY)') $(if $(IMAGE_TAG),--tag '$(IMAGE_TAG)')

image-build:
	scripts/image-build.sh $(IMAGE_BUILD_ARGS)

# What image-build tagged the web image, asked of the script that tags
# it. The repository comes from .github/workflows/release.yml's `tags:`,
# and a second copy here was the copy that went stale: a rename in the
# release left this naming an image an earlier run had built, which the
# target then checked instead of the one it had just built (#870).
#
# Keyed on the build context, which is what makes an image the web
# app's. Empty when the release builds none from it, which the target
# reports rather than run against nothing.
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

# An operator pushes the test image to its ECR repository and moves a
# consumer's pin there, under their own AWS credentials (ADR-0093): no
# workflow holds one for it. test-image-push builds and checks HEAD as
# test-image-e2e does and pushes it under HEAD's first twelve hex
# digits (ADR-0071's commit-derived tag); test-image-pin moves
# `pinned-$(CONSUMER)` to the image of $(COMMIT).
# Neither is in `ci`, which holds no AWS credential. Both scripts hold
# the repository themselves, so no make variable can redirect them.
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

# Every environment variable garam reads, derived from the two files that
# read them rather than listed here. The Go side is the envName block in
# internal/cli: envOr wraps the module's only os.Getenv call and takes a
# name declared there, so this reads the declarations rather than the
# spelling of the calls — a wrapper around envOr no longer shrinks what
# this check covers, which is what a call-site pattern could not promise
# (#734). The two guards chart-render runs first are what keep the block
# equal to the reads. web/src/env.ts is the web app's only reader and is
# parsed for its own shape.
#
# Both sides read the same character class. A class the sweep does not
# accept drops the name out of the enumerated set silently: the chart
# does not set it either, so the comparison below finds nothing to
# report and passes on a name neither side ever saw (#864).
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

# chart-package reads the artifact `helm package` produces, which the
# three targets above never open: everything not excluded ships, `ci/`
# included, so a file reaches a consumer without passing through any of
# them. A resource name set there reads as a figure garam measured, and
# none is (docs/architecture/deployment-contract.md §Not established).
# Its zero-guard counts lines that set a `resources:` key: counting any
# line holding the word counted this README's own prose, so an artifact
# carrying no workload at all still reported a search that read
# something (#864).
#
# chart-names renders under the longest release name Helm accepts and
# holds every object name to 63 characters. Rendering cannot see this
# class on its own: `helm template` produced a 67-character Service
# name and only an apply reported it (#725), and the two release names
# the checks above render under are too short to reach the boundary.
#
# 63 binds for three different reasons — a Service name is a DNS-1035
# label, a Job's name becomes its pods' `job-name` label value, and a
# Deployment name reaches pod hostnames — so the check is on the name
# rather than on the kind.
#
# `^  name: ` is the object's own name at exactly two spaces: a
# container, an env entry and a secretKeyRef all sit deeper.
#
# Each render lands in a file before it is parsed: piped into sed only
# sed's status reaches set -e, so a render that failed contributed no
# name and the other value file kept the count above zero (#864).
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
# image against the api Service this chart renders under the release
# name `garam`. Both are true and each is written where the other is not
# read: the target is a build argument in a workflow, the Service name
# comes out of a template, and until #861 no check opened two of them at
# once. Renaming either leaves every published image reaching a name
# nothing renders, and the failure is a browser request that 404s —
# every object Ready, no probe on that path (ADR-0023), no log line.
#
# The second render is the control. It is the release name chart-names
# proves the chart survives, and the URL it names is one no published
# image reaches (ADR-0056), so it is also where the constraint this
# check records is visible: equality under `garam` proves nothing unless
# the two renders differ.
#
# The build argument is read here and enumerated in
# scripts/image-build.sh, which asks a different question of the same
# file — what the release builds, rather than what one image is bound to.
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

# chart-guards renders each values file under $(CHART_DIR)/ci/refused/
# and holds that the render is refused, naming the value the file is
# named after. A value documented as guarded and not guarded reads as
# protection and is worse than none (#726), and rendering under the
# valid values above cannot see one: the render succeeds, and what is
# missing arrives at runtime or not at all.
#
# The message is checked, not only the exit status. A render refused
# for an unrelated reason is not this guard firing, and would pass a
# check that read the status alone. What the message must carry is
# `execution error at (<template>): <value> ` — helm's own form for a
# template that stopped itself, opening with the value the guard names.
# Searching the whole of helm's output for the value instead matched
# the values file's path, which carries the same string, so any failure
# that named the file passed (#864).
#
# The control is the last line: the values every other check renders
# must still render. Without it a chart that refused everything would
# pass every case above.
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

# The operator, pinned to the commit that carries the manifest: a release
# asset can be replaced under its tag and a commit cannot. v1.30.0 is the
# current stable release and the first patched for all four advisories
# github.com/cloudnative-pg/cloudnative-pg publishes; GHSA-7qwx-x8ff-3px9
# is what puts that floor at 1.30.0 rather than 1.29.2, and the GitHub
# Advisory Database does not carry it — re-check the four at the
# project's own advisories. Nothing proposes a bump for this pin either,
# and no scheduled review is named for it; naming no mover of its own,
# it is the PM's to move under AGENTS.md §Roles.
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
