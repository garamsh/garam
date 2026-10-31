# tooling — the repository's own scripts, and the checks over what no
# module owns: the workflows, the shell scripts, and the toolchain pairs
# a Dockerfile and a module declare between them. Its verbs are every
# module's (#1661); `make -C tooling ci` is its gate. Every script here
# reads paths from the repository root, so each recipe runs there.

include make/go.mk

.PHONY: ci lint test check-toolchain

# Two more tool directives, pinned and moved on the terms go.mk states
# and named beside them in `.github/dependabot.yml`. Neither writes
# output this repository commits, so a bump of either lands on its own
# and `check-gen` says nothing about it. They read what nothing else
# here reads: release.yml, whose first execution is the first tag, and
# the shell scripts, one of which no Make target calls (#840).
ACTIONLINT := go tool actionlint
SHFMT := go tool shfmt

ci: lint test check-toolchain

# What actionlint and shfmt will read, counted on lint's line.
# actionlint given no path finds the workflows itself and shfmt given a
# directory walks it, and either sweep coming back empty exits zero
# reading exactly like the run that read every file — so the paths are
# listed here and an empty list fails below. The shell sweep takes two
# roots: `tooling/` and the repository root.
WORKFLOW_FILES = $(shell cd $(REPO_ROOT) && find .github/workflows -type f \( -name '*.yml' -o -name '*.yaml' \))
SHELL_SCRIPTS = $(shell cd $(REPO_ROOT) && { find . -maxdepth 1 -type f -name '*.sh'; find tooling -type f -name '*.sh'; })

# actionlint runs with shellcheck and pyflakes named off, so it is one
# check on every machine, and its ref-keyed checks reach no action
# pinned by full SHA (#848, #854, #858). shfmt runs as a POSIX parser
# with output discarded: it fails a syntax error or a bashism the POSIX
# grammar cannot hold, never a layout (#904).
lint:
	@echo 'lint: $(words $(WORKFLOW_FILES)) workflows (actionlint), $(words $(SHELL_SCRIPTS)) shell scripts (shfmt)'
	@test -n '$(WORKFLOW_FILES)' && test -n '$(SHELL_SCRIPTS)' \
		|| { echo 'lint: nothing to lint — .github/workflows or the shell sweep matched no input; the counts above name which'; exit 1; }
	@cd $(REPO_ROOT) && $(ACTIONLINT) -shellcheck= -pyflakes= $(WORKFLOW_FILES)
	@cd $(REPO_ROOT) && $(SHFMT) -ln posix $(SHELL_SCRIPTS) >/dev/null

# The script tests, none of which needs Docker:
# tooling/image-build-test.sh (ADR-0071, ADR-0134),
# tooling/image-test-ecr-test.sh (ADR-0093) and
# tooling/ci/affected-test.sh (#1661).
test:
	@cd $(REPO_ROOT) && tooling/image-build-test.sh
	@cd $(REPO_ROOT) && tooling/image-test-ecr-test.sh
	@cd $(REPO_ROOT) && tooling/ci/affected-test.sh

# The pairs check-toolchain holds together, written as the site
# declaring a version, that version, the Dockerfile, and the image whose
# tag has to carry it. `.github/dependabot.yml`'s docker entry moves the
# FROM line alone, so each pair is a half a bump can arrive without.
#
# Both versions reach the script through the environment and never
# through the command text: each is read out of a file in the tree,
# which ci.md §Security baseline keeps out of a command line.
NODE_VERSION := $(shell cat $(REPO_ROOT)/platform-web/.nvmrc)
COWORK_GO_DIRECTIVE := $(shell sed -n 's/^go //p' $(REPO_ROOT)/cowork/go.mod)
POLICY_RUST_CHANNEL := $(shell sed -n 's/^channel = "\(.*\)"$$/\1/p' $(REPO_ROOT)/policy/rust-toolchain.toml)

export TOOLCHAIN_PAIRS = \
	platform-web/.nvmrc,$(NODE_VERSION),deploy/local/Dockerfile.web,node \
	platform/go.mod,$(GO_DIRECTIVE),deploy/local/Dockerfile,golang \
	cowork/go.mod,$(COWORK_GO_DIRECTIVE),deploy/local/Dockerfile.cowork,golang \
	policy/rust-toolchain.toml,$(POLICY_RUST_CHANNEL),deploy/local/Dockerfile.policy,rust \
	platform/go.mod,$(GO_DIRECTIVE),platform/tests/testprincipal/Dockerfile,golang

check-toolchain:
	@cd $(REPO_ROOT) && tooling/check-toolchain.sh
