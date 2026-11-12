# policy — the Rust workspace behind the policy service (ADR-0130). Its
# verbs are every module's (#1661); `make -C policy ci` is its fast
# gate, which needs no Docker, and `stack-up suites` the half that does.
# Its `gen` writes the GRN catalogue lib/grn commits, from the
# manifests under schema/ (ADR-0143 item 2), and `check-gen` is the one
# command that fails when the manifests and what is committed disagree.

include ../tooling/make/go.mk
include ../tooling/make/stack.mk

.PHONY: ci format lint test build gen check-gen stack-up suites integration

ci: format lint test build check-gen

# The one reader of the manifests is crates/manifest; the example in
# crates/server runs its rules and the closed list of callers, and
# writes the catalogue only when none is broken.
CATALOGUE = ../lib/grn/catalogue_gen.go

gen:
	cargo run -q --locked -p server --example manifests -- $(CATALOGUE) schema
	gofmt -w $(CATALOGUE)

# check-gen fails, naming what disagrees, when a manifest breaks a rule
# every manifest holds, when an `@caller` is neither `none` nor a
# `Caller`, when a `Caller` is named by no manifest, when the committed
# catalogue is not what the manifests generate, and when an audience of
# the platform names no manifest service and is neither policy nor
# codehub. The last is a Go test, and `go test` exits 0 on a package
# where the named test does not exist, so its run is counted.
check-gen:
	@test -f '$(CATALOGUE)' || { echo 'check-gen: $(CATALOGUE) is missing — run make -C policy gen'; exit 1; }
	@$(MAKE) --no-print-directory gen
	@out="$$(git status --porcelain -- $(CATALOGUE))"; \
		test -z "$$out" || { echo "check-gen: the committed catalogue is not what the manifests generate — run make -C policy gen and commit:"; echo "$$out"; git diff -- $(CATALOGUE) | head -40; exit 1; }
	@echo 'check-gen: $(CATALOGUE) is what the manifests generate'
	@out="$$(cd ../platform && go test ./internal/identity -run TestAudiencesNameAManifestServiceOrAnException -count=1 -v 2>&1)"; \
		status=$$?; echo "$$out" | grep -v '^$$'; test $$status -eq 0 || { echo 'check-gen: an audience disagrees with the manifests'; exit 1; }; \
		echo "$$out" | grep -q -- '--- PASS: TestAudiencesNameAManifestServiceOrAnException' || { echo 'check-gen: the audience test did not run'; exit 1; }

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
# `#[ignore]` needs Postgres or cvc5 and is left to `integration`.
test:
	@echo 'test: the #[ignore]d store tests and analysis not covered here — they need Postgres and cvc5 (make -C policy stack-up suites)'
	@n="$$(cargo test --workspace --locked -- --list | grep -c ': test$$' || true)"; \
		echo "test: $$n tests, listed by cargo test --workspace -- --list"; \
		test "$$n" -gt 0 || { echo 'test: nothing to run — the listing above held no test'; exit 1; }
	cargo test --workspace --locked

build:
	@echo 'build: $(words $(CRATES)) crates under crates/'
	@test -n '$(CRATES)' || { echo 'build: nothing to build — crates/ holds no crate'; exit 1; }
	cargo build --workspace --all-targets --locked

# The dev Postgres, which holds policy's databases beside the others'.
# `#[sqlx::test]` connects to the URL below and creates one database
# per test on that server, named `_sqlx_test_<n>`. It drops each on a
# pass and leaves a failed test's behind.
stack-up:
	@$(MAKE) --no-print-directory -C ../deploy/local db-up

POLICY_TEST_SERVER_URL ?= $(if $(DB_HOST_ADDR),postgres://garam:garam@$(DB_HOST_ADDR)/postgres)

# The solver Cedar's analysis asks (crates/decision/tests/analysis.rs,
# ADR-0130 §5): the release cedar-policy-symcc 0.8.0's README names, a
# single static binary, pinned by the SHA-256 of the archive the cvc5
# project publishes for each architecture. Nothing parses these lines
# for a pin, so no bot proposes a bump and no scheduled review is
# named; naming no mover of its own, it is the PM's to move under
# AGENTS.md §Roles, read against that README whenever
# cedar-policy-symcc moves. It lands under target/, once per checkout.
CVC5_VERSION := 1.3.1
CVC5_SHA256_x86_64 := 1a1cda20d2df4938fa4944a69f33ddc9172e319ece0eed0aa09c4d7abede3ed1
CVC5_SHA256_arm64 := fe2b661834a82fd8830f7a757c340f0e20041fa41e19b038fa02ace0eaf1c6f2
CVC5_ARCH := $(subst aarch64,arm64,$(shell uname -m))
CVC5 := $(abspath target/cvc5-$(CVC5_VERSION)/cvc5)

$(CVC5):
	@test -n '$(CVC5_SHA256_$(CVC5_ARCH))' || { echo 'cvc5: no pin for $(CVC5_ARCH)'; exit 1; }
	@mkdir -p $(@D)
	curl -fsSL --retry 3 -o $@.zip https://github.com/cvc5/cvc5/releases/download/cvc5-$(CVC5_VERSION)/cvc5-Linux-$(CVC5_ARCH)-static.zip
	echo '$(CVC5_SHA256_$(CVC5_ARCH))  $@.zip' | sha256sum -c -
	unzip -q -j -o $@.zip '*/bin/cvc5' -d $(@D)
	rm $@.zip

suites: integration

integration: $(CVC5)
	@test -n '$(POLICY_TEST_SERVER_URL)' || { echo "integration: no postgres published on this host — run make -C policy stack-up" >&2; exit 1; }
	@n="$$(cargo test --workspace --locked -- --list --ignored | grep -c ': test$$' || true)"; \
		echo "integration: $$n tests, listed by cargo test --workspace -- --list --ignored, against the postgres at $(POLICY_TEST_SERVER_URL) and cvc5 $(CVC5_VERSION)"; \
		test "$$n" -gt 0 || { echo 'integration: nothing to run — the listing above held no test'; exit 1; }
	DATABASE_URL='$(POLICY_TEST_SERVER_URL)' CVC5='$(CVC5)' cargo test --workspace --locked -- --ignored
