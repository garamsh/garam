# lib/platform-sdk-go — the Go client of the platform's browser API,
# generated from platform/api/openapi.yaml (ADR-0132). Its verbs are
# every module's (#1661); `make -C lib/platform-sdk-go ci` is its gate.
# It holds no test of its own: platform-cli's tests call it.

include ../../tooling/make/go.mk

.PHONY: ci format lint build gen check-gen

ci: format lint build check-gen

format:
	$(go-format)

lint:
	$(go-vet)

build:
	$(go-build)

gen:
	$(OAPI_CODEGEN) --config oapi-codegen.yaml ../../platform/api/openapi.yaml

# The output the generator owns, read from its config, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell sed -n 's|^output: ||p' oapi-codegen.yaml)

check-gen:
	$(go-check-gen)
