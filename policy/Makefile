# policy — the Rust workspace behind the policy service (ADR-0130). Its
# verbs are every module's (#1661); `make -C policy ci` is its fast
# gate, which needs no Docker, and `stack-up suites` the half that does.
# With nothing generated into the tree, it defines no `gen`.

include ../tooling/make/go.mk
include ../tooling/make/stack.mk

.PHONY: ci format lint test build stack-up suites integration

ci: format lint test build

# What each verb covers, counted on its own line and failing on zero:
# cargo given an empty workspace, and buf given no file, exit 0 reading
# exactly like a run that did the work.
RUST_FILES = $(shell git ls-files --cached --others --exclude-standard -- '*.rs')
CRATES = $(wildcard crates/*/Cargo.toml)
PROTO_FILES = $(shell git ls-files --cached --others --exclude-standard -- '*.proto')

format:
	@echo 'format: $(words $(RUST_FILES)) Rust files, listed by git (cargo fmt)'
	@test -n '$(RUST_FILES)' || { echo 'format: nothing to check — git listed no Rust file'; exit 1; }
	cargo fmt --all --check

lint:
	@echo 'lint: $(words $(CRATES)) crates under crates/ (clippy, cargo doc), $(words $(PROTO_FILES)) proto files, listed by git (buf lint)'
	@test -n '$(CRATES)' && test -n '$(PROTO_FILES)' || { echo 'lint: nothing to lint — the counts above name which'; exit 1; }
	cargo clippy --workspace --all-targets --locked -- -D warnings
	RUSTDOCFLAGS='-D warnings' cargo doc --workspace --no-deps --locked
	$(BUF) lint

# `cargo test` prints `ok. 0 passed` and exits 0 for a crate holding no
# tests, so the tests are listed and counted first. A test marked
# `#[ignore]` needs Postgres and is left to `integration`.
test:
	@echo 'test: the #[ignore]d store tests not covered here — they need Postgres (make -C policy stack-up suites)'
	@n="$$(cargo test --workspace --locked -- --list | grep -c ': test$$' || true)"; \
		echo "test: $$n tests, listed by cargo test --workspace -- --list"; \
		test "$$n" -gt 0 || { echo 'test: nothing to run — the listing above held no test'; exit 1; }
	cargo test --workspace --locked

build:
	@echo 'build: $(words $(CRATES)) crates under crates/'
	@test -n '$(CRATES)' || { echo 'build: nothing to build — crates/ holds no crate'; exit 1; }
	cargo build --workspace --all-targets --locked

# The dev Postgres, which holds policy's databases beside the others'.
# `#[sqlx::test]` connects to the URL below and creates and drops one
# database per test on that server.
stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local db-up

POLICY_TEST_SERVER_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/postgres)

suites: integration

integration:
	@test -n '$(POLICY_TEST_SERVER_URL)' || { echo "integration: no postgres published on this host — run make -C policy stack-up" >&2; exit 1; }
	@n="$$(cargo test --workspace --locked -- --list --ignored | grep -c ': test$$' || true)"; \
		echo "integration: $$n tests, listed by cargo test --workspace -- --list --ignored, against the postgres at $(POLICY_TEST_SERVER_URL)"; \
		test "$$n" -gt 0 || { echo 'integration: nothing to run — the listing above held no test'; exit 1; }
	DATABASE_URL='$(POLICY_TEST_SERVER_URL)' cargo test --workspace --locked -- --ignored
