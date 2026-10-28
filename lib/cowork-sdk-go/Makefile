# lib/cowork-sdk-go — the Go client of cowork's HTTP API, generated
# from cowork/api/openapi.yaml (ADR-0133). Its verbs are every module's
# (#1661); `make -C lib/cowork-sdk-go ci` is its gate. It holds no test
# of its own: garam-cli's tests call it.

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
	$(OAPI_CODEGEN) --config oapi-codegen.yaml ../../cowork/api/openapi.yaml

# The output the generator owns, read from its config, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell sed -n 's|^output: ||p' oapi-codegen.yaml)

check-gen:
	$(go-check-gen)
