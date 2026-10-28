# lib/grn — Garam Resource Names, which identify a resource inside
# garam (ADR-0132). Its verbs are every module's (#1661);
# `make -C lib/grn ci` is its gate.

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
