# What every Go module's Makefile shares (#1661): the toolchain floor,
# the tool directives, and the one way a Go entry point selects and
# reports what it covers. A module's Makefile includes this file and
# runs its verbs from its own folder, so `./...` is that module and no
# other: go stops a pattern at a nested module's go.mod.

REPO_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/../..)

# The version the repository declares its Go toolchain at, read from
# the file that declares it. `go run tool@version` takes the toolchain
# from the tool's own go directive, which can be older than this
# module's and then cannot load its packages. The floor is the version
# platform/go.mod declares; `+auto` still upgrades for a tool that
# needs a newer one.
GO_DIRECTIVE := $(shell sed -n 's/^go //p' $(REPO_ROOT)/platform/go.mod)
export GOTOOLCHAIN := go$(GO_DIRECTIVE)+auto

# Tool directives in platform/go.mod, which go.work makes every
# module's: its go.sum records each version and Dependabot's gomod
# ecosystem proposes its bumps, and `.github/dependabot.yml` names what
# it does not cover. Each writes output this repository commits, so a
# move — a bump, or a floor another dependency raises — lands with the
# `gen` that follows it or `check-gen` fails.
OAPI_CODEGEN := go tool oapi-codegen
SQLC := go tool sqlc
MOCKERY := go tool mockery
BUF := go tool buf
GOIMPORTS := go tool goimports

# The module's packages, listed once so the verbs report the set they
# covered rather than name a ./... pattern whose match nobody sees. An
# empty list is never passed on, and each verb says so itself rather
# than resting on what a tool does with no argument: `go test` given
# none reads the current directory, which fails only while no .go file
# sits at the module root — a doc.go there is an ordinary idiom and
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

# The Go modules nested inside this one, which `./...` stops at and the
# format sweep below leaves to their own Makefile, as git pathspecs.
GO_NESTED = $(patsubst $(CURDIR)/%,':!:%',$(filter $(CURDIR)/%,$(shell go list -m -f '{{.Dir}}')))

# gofmt and goimports list offending files but exit 0, so the gate is a
# non-empty-output check; a file they cannot read or parse, a tracked
# file deleted from the working tree among them, makes xargs exit 123
# and fails the gate too. The set is git's: the tracked files CI checks
# out plus untracked ones not ignored, under this module and outside a
# nested one. Given an empty set, xargs still runs each tool once on
# /dev/null and it reports nothing, so the files are counted first and a
# count of zero fails.
FORMAT_SELECT = git ls-files --cached --others --exclude-standard -z -- '*.go' $(GO_NESTED)
FORMAT_COUNT = $(shell $(FORMAT_SELECT) | tr -cd '\0' | wc -c)

define go-format
@echo 'format: $(FORMAT_COUNT) Go files (gofmt, goimports)'
@test '$(FORMAT_COUNT)' -gt 0 || { echo 'format: nothing to check — the sweep matched no Go file'; exit 1; }
@out="$$($(FORMAT_SELECT) | xargs -0 gofmt -l)" || { echo "gofmt: failed on the error above"; exit 1; }; test -z "$$out" || { echo "gofmt: $$out"; exit 1; }
@out="$$($(FORMAT_SELECT) | xargs -0 $(GOIMPORTS) -l)" || { echo "goimports: failed on the error above"; exit 1; }; test -z "$$out" || { echo "goimports: $$out"; exit 1; }
endef

define go-vet
$(call go-packages,lint (go vet),$(GO_SELECT),$(GO_PKGS))
@go vet $(GO_PKGS)
endef

# go has no negative package pattern, so the sweep is filtered here and
# the suites needing Postgres or a Docker daemon are named instead; a
# filter that stops matching sweeps its suite back in to fail rather
# than skip.
TEST_EXCLUDE = %/tests/integration %/tests/e2e %/tests/workload %/tests/standalone
TEST_SELECT = $(GO_SELECT) minus $(TEST_EXCLUDE)
TEST_PKGS = $(filter-out $(TEST_EXCLUDE),$(GO_PKGS))

define go-test
$(call go-packages,test,$(TEST_SELECT),$(TEST_PKGS))
@go test -count=1 $(TEST_PKGS)
endef

# `go build ./...` exits 0 having done nothing should that pattern ever
# resolve to no package, so build counts the packages it selects first.
# It runs the pattern rather than the list: go build skips a test-only
# package swept in by ./... and fails on one named directly.
define go-build
$(call go-packages,build,$(GO_SELECT),$(GO_PKGS))
@go build ./...
endef

# Only packages holding a test file are listed, because `go test` exits
# 0 with `[no test files]` on one holding none — a build tag that
# stopped matching reads like the run that did the work. That condition
# then empties a list and fails at the verb that reads it.
# $(call go-test-pkgs,<pattern>) and $(call go-test-files,<pattern>)
go-test-pkgs = $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' $(1))
go-test-files = $(shell go list -f '{{range .TestGoFiles}}{{.}} {{end}}{{range .XTestGoFiles}}{{.}} {{end}}' $(1))

# check-gen fails on owned output that is modified, untracked or
# deleted, and on tracked output under this module carrying the "Code
# generated ... DO NOT EDIT." marker that no owned path claims (#175,
# #360). The shape checks run before gen, which would otherwise
# regenerate a deleted path before any status could see it. The module
# names its owned paths in GEN_PATHS, each relative to its folder.
define go-check-gen
@test -n '$(GEN_PATHS)' || { echo "check-gen: generator configs yielded no output paths — the parse above is broken"; exit 1; }
@out="$$(git ls-files --deleted -- $(GEN_PATHS))"; \
	test -z "$$out" || { echo "check-gen: generated output deleted in the working tree:"; echo "$$out"; exit 1; }
@generated="$$(git grep --cached -l -E '^// Code generated .* DO NOT EDIT' -- . $(GO_NESTED) || true)"; \
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
endef
