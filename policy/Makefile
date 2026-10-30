# policy — the Rust workspace behind the policy service (ADR-0130). Its
# verbs are every module's (#1661); `make -C policy ci` is its fast
# gate, which needs no Docker. No suite exists yet, so it defines no
# `stack-up` or `suites`, and with nothing generated, no `gen`.

include ../tooling/make/go.mk

.PHONY: ci format lint test build

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
# tests, so the tests are listed and counted first.
test:
	@n="$$(cargo test --workspace --locked -- --list | grep -c ': test$$' || true)"; \
		echo "test: $$n tests, listed by cargo test --workspace -- --list"; \
		test "$$n" -gt 0 || { echo 'test: nothing to run — the listing above held no test'; exit 1; }
	cargo test --workspace --locked

build:
	@echo 'build: $(words $(CRATES)) crates under crates/'
	@test -n '$(CRATES)' || { echo 'build: nothing to build — crates/ holds no crate'; exit 1; }
	cargo build --workspace --all-targets --locked
