# cowork — the Go module behind the cowork server (ADR-0127). Its verbs
# are every module's (#1661); `make -C cowork ci` is its fast gate, and
# `stack-up suites` the half that needs Docker.

include ../tooling/make/go.mk
include ../tooling/make/stack.mk

.PHONY: ci format lint test build gen check-gen stack-up suites integration

ci: format lint test build check-gen

format:
	$(go-format)

lint:
	$(go-vet)

test:
	$(go-test)
	@echo 'test: tests/integration not covered here — it needs Postgres and RustFS (make -C cowork stack-up suites)'

build:
	$(go-build)

# gen regenerates the OpenAPI server, the sqlc queries, and the Yjs
# bundle cowork/engine builds into internal/.
gen:
	$(OAPI_CODEGEN) --config api/oapi-codegen.yaml api/openapi.yaml
	$(SQLC) generate
	@$(MAKE) --no-print-directory -C engine build

# The output each generator owns, read from its config, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell { sed -n 's|^output: ||p' api/oapi-codegen.yaml; \
	sed -n 's|^        out: ||p' sqlc.yaml; \
	sed -n 's|.*outfile: "\.\./\(.*\)".*|\1|p' engine/build.mjs; } | sort -u)

check-gen:
	$(go-check-gen)

# The stack the suite below runs against: cowork's database on the dev
# Postgres, and the RustFS its blob store is tested against.
stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local db-up

suites: integration

INTEGRATION_PKGS = $(call go-test-pkgs,./tests/integration/...)
INTEGRATION_TEST_FILES = $(call go-test-files,./tests/integration/...)

integration:
	@echo 'integration: $(words $(INTEGRATION_PKGS)) packages, $(words $(INTEGRATION_TEST_FILES)) test files, against $(COWORK_TEST_DB_NAME) and the rustfs at $(COWORK_S3_HOST_ADDR)'
	@test -n '$(INTEGRATION_PKGS)' || { echo 'integration: nothing to run — no package under ./tests/integration/... holds a test file'; exit 1; }
	@test -n '$(COWORK_TEST_DATABASE_URL)' || { echo "integration: no postgres published on this host — run make -C cowork stack-up" >&2; exit 1; }
	@test -n '$(COWORK_S3_HOST_ADDR)' || { echo "integration: no rustfs published on this host — run make -C cowork stack-up" >&2; exit 1; }
	COWORK_TEST_DATABASE_URL='$(COWORK_TEST_DATABASE_URL)' \
		COWORK_TEST_S3_ENDPOINT='http://$(COWORK_S3_HOST_ADDR)' COWORK_TEST_S3_ACCESS_KEY='$(COWORK_S3_ACCESS_KEY)' \
		COWORK_TEST_S3_SECRET_KEY='$(COWORK_S3_SECRET_KEY)' \
		go test -count=1 $(INTEGRATION_PKGS)
