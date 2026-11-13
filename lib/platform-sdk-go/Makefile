# lib/platform-sdk-go — the Go client of the platform's browser API,
# generated from platform/api/openapi.yaml (ADR-0132), and, in its
# `machine` package, of the machine API from platform/api/machine.yaml.
# Its verbs are every module's (#1661); `make -C lib/platform-sdk-go ci`
# is its gate. garam-cli's tests call the browser client; the machine
# client's own test sits in machine/.

include ../../tooling/make/go.mk

.PHONY: ci format lint test build gen check-gen

ci: format lint test build check-gen

format:
	$(go-format)

lint:
	$(go-vet)
	$(go-tidy-check)

test:
	$(go-test)

build:
	$(go-build)

gen:
	$(OAPI_CODEGEN) --config oapi-codegen.yaml ../../platform/api/openapi.yaml
	$(OAPI_CODEGEN) --config machine-codegen.yaml ../../platform/api/machine.yaml

# The output the generator owns, read from its config, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell sed -n 's|^output: ||p' oapi-codegen.yaml machine-codegen.yaml)

check-gen:
	$(go-check-gen)
