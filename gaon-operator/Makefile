# gaon-operator — the Go module behind the gaon operator (ADR-0151).
# Its verbs are every module's (#1661); `make -C gaon-operator ci` is
# its gate. Nothing in it needs Docker.

include ../tooling/make/go.mk

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
