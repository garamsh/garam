# lib/clisession — a person's CLI session with garam's platform, which
# cowork's CLI holds (ADR-0132). Its verbs are every module's (#1661);
# `make -C lib/clisession ci` is its gate.

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
