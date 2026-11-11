# lib/policy-sdk-go — the Go client of the policy service's gRPC API,
# generated from policy/proto (ADR-0130, ADR-0140). Its verbs are every
# module's (#1661); `make -C lib/policy-sdk-go ci` is its gate. It holds
# no test of its own: platform's tests call it.

include ../../tooling/make/go.mk

.PHONY: ci format lint build gen check-gen

ci: format lint build check-gen

format:
	$(go-format)

lint:
	$(go-vet)

build:
	$(go-build)

# policy/ owns the contract, so the input is its buf module and this
# module owns no .proto file.
gen:
	$(BUF) generate ../../policy --template buf.gen.yaml
	$(GOIMPORTS) -w gen

# The output the generator owns, read from its config, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell sed -n 's|^    out: ||p' buf.gen.yaml | sort -u)

check-gen:
	$(go-check-gen)
