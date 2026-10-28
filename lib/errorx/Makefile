# lib/errorx — the typed errors and their kinds every Go module returns
# (ADR-0003, ADR-0132). Its verbs are every module's (#1661);
# `make -C lib/errorx ci` is its gate.

include ../../tooling/make/go.mk

.PHONY: ci format lint test build

ci: format lint test build

format:
	$(go-format)

lint:
	$(go-vet)

test:
	$(go-test)

build:
	$(go-build)
