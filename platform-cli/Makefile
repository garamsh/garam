# platform-cli — the platform's client CLI, the `garam` a person signs
# in with (ADR-0120, ADR-0132). Its verbs are every module's (#1661);
# `make -C platform-cli ci` is its gate. Its suite against the platform
# runs in the platform's, which builds this binary.

include ../tooling/make/go.mk

.PHONY: ci format lint test build gen check-gen

ci: format lint test build check-gen

format:
	$(go-format)

lint:
	$(go-vet)

test:
	$(go-test)

build:
	$(go-build)

# gen regenerates the commands cligen derives from the platform's
# browser API (ADR-0122).
gen:
	go run ./internal/cli/cligen

# The output cligen owns, read from its source, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell sed -n 's|^\toutPath *= "\(.*\)"$$|\1|p' internal/cli/cligen/main.go)

check-gen:
	$(go-check-gen)
