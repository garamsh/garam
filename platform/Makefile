# platform — the Go module behind the api, authz and machine processes
# (ADR-0125). Its verbs are every module's (#1661); `make -C platform
# ci` is its fast gate, and `stack-up suites` the half that needs
# Docker.

include ../tooling/make/go.mk
include ../tooling/make/stack.mk

.PHONY: ci format lint test build gen check-gen stack-up suites integration e2e workload-integration standalone-e2e mutation

# A third tool directive on the terms go.mk states, and the only one no
# whole-set name reaches: `mutation` is invoked by hand and by nothing
# else, for the reasons stated at that target (#875).
GREMLINS := go tool gremlins

# What buf lint will read, counted on lint's line. buf itself fails on a
# module holding no .proto file; the guard below is for an ls-files that
# comes back empty, which would otherwise print a zero nobody checked.
PROTO_FILES = $(shell $(BUF) ls-files)

ci: format lint test build check-gen

format:
	$(go-format)

lint:
	@echo 'lint: $(words $(PROTO_FILES)) proto files (buf lint); go vet counted on its own line below'
	@test -n '$(PROTO_FILES)' || { echo 'lint: nothing to lint — the buf module matched no input'; exit 1; }
	$(go-vet)
	@$(BUF) lint

test:
	$(go-test)
	@echo 'test: tests/integration and tests/e2e not covered here — they need Postgres (make -C platform stack-up suites); tests/workload and tests/standalone need a Docker daemon and the pinned images (make -C platform workload-integration, make -C platform standalone-e2e)'

build:
	$(go-build)

# gen regenerates everything derived from a source of truth: the
# OpenAPI servers, sqlc queries, protobuf code, and mocks. The client of
# the browser API is lib/platform-sdk-go's, and the commands derived
# from it platform-cli's (ADR-0132).
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
GEN_PATHS := $(shell { sed -n 's|^output: ||p' api/oapi-codegen.yaml api/machine-codegen.yaml; \
	sed -n 's|^        out: ||p' sqlc.yaml; \
	sed -n 's|^    out: ||p' buf.gen.yaml; \
	sed -n 's|^  $(shell sed -n 's/^module //p' go.mod)/\(.*\):$$|\1/mocks|p' .mockery.yaml; } | sort -u)

check-gen:
	$(go-check-gen)

# The stack the suites below run against: postgres, the openbao
# `integration` signs against and the rustfs it shares with cowork's.
stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local db-up

suites: integration e2e

INTEGRATION_PKGS = $(call go-test-pkgs,./tests/integration/...)
INTEGRATION_TEST_FILES = $(call go-test-files,./tests/integration/...)
E2E_PKGS = $(call go-test-pkgs,./tests/e2e/...)
E2E_TEST_FILES = $(call go-test-files,./tests/e2e/...)

# integration runs the store round-trips and the services over them in
# process, against real Postgres, and the Transit custodian against the
# dev OpenBao (see deploy/local's db-up).
integration:
	@echo 'integration: $(words $(INTEGRATION_PKGS)) packages, $(words $(INTEGRATION_TEST_FILES)) test files, against $(TEST_DB_NAME) and the openbao at $(OPENBAO_HOST_ADDR)'
	@test -n '$(INTEGRATION_PKGS)' || { echo 'integration: nothing to run — no package under ./tests/integration/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "integration: no postgres published on this host — run make -C platform stack-up" >&2; exit 1; }
	@test -n '$(OPENBAO_HOST_ADDR)' && test -n '$(OPENBAO_CONTAINER)' || { echo "integration: no openbao published on this host — run make -C platform stack-up" >&2; exit 1; }
	GARAM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' GARAM_TEST_OPENBAO_ADDR='https://$(OPENBAO_HOST_ADDR)' \
		GARAM_TEST_OPENBAO_CONTAINER='$(OPENBAO_CONTAINER)' \
		go test -count=1 $(INTEGRATION_PKGS)

# e2e runs the built binary as the processes a deployment runs, against
# the same Postgres.
e2e:
	@echo 'e2e: $(words $(E2E_PKGS)) packages, $(words $(E2E_TEST_FILES)) test files, against $(TEST_DB_NAME)'
	@test -n '$(E2E_PKGS)' || { echo 'e2e: nothing to run — no package under ./tests/e2e/... holds a test file'; exit 1; }
	@test -n '$(TEST_DATABASE_URL)' || { echo "e2e: no postgres published on this host — run make -C platform stack-up" >&2; exit 1; }
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
WORKLOAD_PKGS = $(call go-test-pkgs,./tests/workload/...)
WORKLOAD_TEST_FILES = $(call go-test-files,./tests/workload/...)

workload-integration:
	@echo 'workload-integration: $(words $(WORKLOAD_PKGS)) packages, $(words $(WORKLOAD_TEST_FILES)) test files, against $(GARAM_TEST_DOCKER_HOST)'
	@test -n '$(WORKLOAD_PKGS)' || { echo 'workload-integration: nothing to run — no package under ./tests/workload/... holds a test file'; exit 1; }
	go test -count=1 -timeout 30m $(WORKLOAD_PKGS)

# standalone-e2e runs a standalone host end to end (ADR-0085, #1180):
# the shipped binary prepares, enrolls and runs the supervisor against
# garam's own image, over a database tests/testprincipal creates on the
# Postgres stack-up starts, with a real model answering. Like
# workload-integration it is in no whole-set name and in no workflow,
# for the same images and a model credential besides. Beside that
# target's GARAM_TEST_DOCKER_HOST and Sherlock images it reads
# GARAM_TEST_GARAM_IMAGE (this tree's image-build output, by ID),
# GARAM_TEST_TEI_IMAGE and GARAM_TEST_MODEL_ENV_FILE, an owner-only
# KEY=VALUE file; the server and its container come from compose.
STANDALONE_PKGS = $(call go-test-pkgs,./tests/standalone/...)
STANDALONE_TEST_FILES = $(call go-test-files,./tests/standalone/...)

standalone-e2e:
	@echo 'standalone-e2e: $(words $(STANDALONE_PKGS)) packages, $(words $(STANDALONE_TEST_FILES)) test files, against $(GARAM_TEST_DOCKER_HOST)'
	@test -n '$(STANDALONE_PKGS)' || { echo 'standalone-e2e: nothing to run — no package under ./tests/standalone/... holds a test file'; exit 1; }
	@test -n '$(DB_HOST_ADDR)' || { echo "standalone-e2e: no postgres published on this host — run make -C platform stack-up" >&2; exit 1; }
	GARAM_TEST_SERVER_URL='postgres://garam:garam@$(DB_HOST_ADDR)/postgres?sslmode=disable' \
		GARAM_TEST_POSTGRES_CONTAINER="$$($(COMPOSE_LOCAL) ps -q postgres)" \
		go test -count=1 -timeout 60m $(STANDALONE_PKGS)

# mutation runs gremlins over internal/ and reports the mutants no test
# noticed; docs/architecture/README.md §Operation records why it is in
# no workflow and what the coefficient and TMPDIR cost (#875). The
# default coefficient times every mutant out and gremlins still exits 0,
# so the counts are read from its report, removed first since it writes
# none when it finds nothing, and zero tested fails. Point TMPDIR at a
# disk: each worker copies the module.
MUTATION_COEFFICIENT := 300
MUTATION_REPORT := mutation.json

mutation:
	@echo 'mutation: internal/, coefficient $(MUTATION_COEFFICIENT), report $(MUTATION_REPORT)'
	@rm -f '$(MUTATION_REPORT)'
	@$(GREMLINS) unleash --timeout-coefficient $(MUTATION_COEFFICIENT) -o '$(CURDIR)/$(MUTATION_REPORT)' internal
	@killed="$$(sed -n 's/.*"mutants_killed":\([0-9]*\).*/\1/p' '$(MUTATION_REPORT)')"; \
		lived="$$(sed -n 's/.*"mutants_lived":\([0-9]*\).*/\1/p' '$(MUTATION_REPORT)')"; \
		efficacy="$$(sed -n 's/.*"test_efficacy":\([0-9.]*\).*/\1/p' '$(MUTATION_REPORT)')"; \
		test -n "$$killed" && test -n "$$lived" && test -n "$$efficacy" \
			|| { echo 'mutation: $(MUTATION_REPORT) carried no counts — the parse above stopped matching'; exit 1; }; \
		echo "mutation: $$killed killed, $$lived survived, efficacy $$efficacy%"; \
		test "$$((killed + lived))" -gt 0 \
			|| { echo 'mutation: no mutant was tested — every one timed out or none was found, and neither is a result'; exit 1; }
