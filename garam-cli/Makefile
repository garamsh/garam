# garam-cli — the one client CLI, the `garam` a person signs in with,
# carrying every service's client commands (ADR-0120, ADR-0133). Its
# verbs are every module's (#1661); `make -C garam-cli ci` is its gate.
# Its suites run in the platform's and cowork's, which build this
# binary.

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
# browser API (ADR-0122) and cowork's API (ADR-0133).
gen:
	go run ./internal/cli/cligen

# The output cligen owns, read from its source, on the terms
# platform/Makefile states for its own.
GEN_PATHS := $(shell sed -n 's|^\t[a-z]*OutPath *= "\(.*\)"$$|\1|p' internal/cli/cligen/main.go)

check-gen:
	$(go-check-gen)
