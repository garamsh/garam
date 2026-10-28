# Rust — Architecture & Style Conventions

> How a Rust service is laid out and where each kind of code belongs:
> the workspace, crate boundaries, naming, errors, logging, comments,
> tests, dependencies.
>
> Checked against rustc and cargo 1.99.1 with their rustfmt and
> clippy. A claim below that names no version holds for these.

## Contents
- 0. Names the tools impose
- 1. Workspace layout
- 2. Crate boundary
- 3. Naming
- 4. Error handling
- 5. Logging & observability
- 6. Comments & docs
- 7. Testing
- 8. Dependencies
- 9. Verification commands

## 0. Names the tools impose

Cargo requires these names, and they keep them where it requires
them: `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml`,
`clippy.toml`, a crate's `src/`, `src/lib.rs`, `src/main.rs`,
`tests/` and `benches/`, and `build.rs`.

## 1. Workspace layout

A Rust service is one Cargo workspace in its service folder:

```
<service>/
├─ Cargo.toml            # [workspace]: members, versions, lints
├─ Cargo.lock            # committed
├─ rust-toolchain.toml   # the exact toolchain
├─ clippy.toml
├─ crates/
│  ├─ <responsibility>/  # a library crate: Cargo.toml, src/lib.rs
│  └─ server/            # the binary crate: src/main.rs
├─ proto/                # the contract the service serves, when gRPC
└─ migrations/           # the service's own database
```

- **One crate per responsibility**, under `crates/`, named for it
  (`crates/decision/`, `crates/grant/`). The workspace's `members` is
  `["crates/*"]`, which takes every folder there as a member.
- **One binary crate composes the rest.** It wires the adapters into
  the ports, serves the contract and owns `main`. No library crate
  depends on it.
- **`rust-toolchain.toml` pins an exact version** (`channel =
  "1.99.1"`, with `rustfmt` and `clippy` as components). Every cargo
  invocation inside the service runs that toolchain, so a local run
  and CI build with the same compiler and lints. The container image's
  base tag names the same version.
- **The workspace declares `edition` and `rust-version` once** in
  `[workspace.package]`, and each crate takes them with
  `edition.workspace = true` and `rust-version.workspace = true`.

## 2. Crate boundary

- **A crate uses only what its `Cargo.toml` names.** The dependency
  table is the boundary: a crate not listed cannot be imported, so the
  direction between crates is enforced at build time and read from
  the manifests rather than from the code.
- **Cargo rejects a cycle** between workspace crates as
  `cyclic package dependency`, before anything compiles.
- **A port is a trait in the crate that needs it.** The crate that
  needs a store or a lookup declares the trait in a file named for it
  (`src/policy_source.rs` declares `trait PolicySource`). The crate
  that provides it implements the trait, and the binary crate joins
  the two.
- **An adapter is a module named for its technology**, beside the
  domain code it serves: `src/postgres.rs`, or `src/postgres/` with
  one file per concern once it holds more than one.
- **Only `pub` crosses a crate.** What another crate does not need is
  `pub(crate)` or private.

## 3. Naming

- **Crates:** kebab-case, the folder's name (`access-report`). Code
  imports a crate with its hyphens as underscores
  (`use access_report::…`); that spelling is Cargo's.
- **Modules and files:** `snake_case`, named for a concept
  (`linked_policy.rs`).
- **Types and traits:** `UpperCamelCase`. A trait for a capability is
  named for it (`PolicySource`, `EntityStore`).
- **Functions, methods, variables:** `snake_case`. Constants and
  statics: `SCREAMING_SNAKE_CASE`.
- **Acronyms in `UpperCamelCase` are one word:** `Grn`, `Http`,
  `GrpcServer`, not `GRN`.
- **Conversions follow the standard prefixes:** `as_` borrows, `to_`
  clones or computes, `into_` consumes; `try_` returns a `Result`.

## 4. Error handling

- **Each library crate declares its own error `enum`** with
  `thiserror`, one variant per failure a caller branches on. A crate
  never returns another crate's error type through its public API; it
  wraps it in a variant of its own (`#[from]` or `#[source]`).
- **The binary crate translates errors to the transport in one
  place.** Every handler returns the domain error, and one function
  maps each variant to a gRPC status code. The codes are the
  platform's error kinds, so no kind is invented here.
- **No `unwrap` or `expect` outside tests.** The workspace denies
  `clippy::unwrap_used` and `clippy::expect_used`, and `clippy.toml`
  sets `allow-unwrap-in-tests` and `allow-expect-in-tests`. A value
  that cannot be absent is returned as an error that says why.
- **A panic is a violated invariant**, never a failure a caller
  handles. `unsafe` code is forbidden for the workspace
  (`[workspace.lints.rust] unsafe_code = "forbid"`).
- **Don't log and return.** Return, and let the binary's boundary log
  once.

## 5. Logging & observability

- **`tracing`**, with structured fields (`tracing::info!(org = %org,
  "…")`), and one subscriber installed by the binary.
- **Never log** secrets, keys, tokens or a full request body.

## 6. Comments & docs

- **Each library crate opens `src/lib.rs` with `//!`** naming the
  crate's responsibility in a sentence or two.
- **`///` on every `pub` item** a second crate uses.
- `cargo doc --workspace --no-deps` builds them.

## 7. Testing

**Runner:** the built-in test harness, run by `cargo test`.

- **Unit tests** sit in the file under test, in `#[cfg(test)] mod
  tests`, and reach private items when they need to.
- **Integration tests** sit in the `tests/` folder of the crate they
  exercise and reach it only through its public API. Tests that wire
  the whole service sit in the binary crate's `tests/`.
- **Real services** come from `deploy/local/docker-compose.yml`, as
  every suite in this repository that needs one.
- **Test names** state the behaviour: `fn
  forbid_overrides_a_permit_on_the_same_request()`.
- `cargo test` prints `ok. 0 passed` for a crate holding no tests and
  exits 0, so the test entry point counts the tests
  (`cargo test --workspace -- --list`) and fails where the count is
  zero.

## 8. Dependencies

- **Versions are declared once**, in `[workspace.dependencies]`, and
  a crate takes one with `name.workspace = true`.
- **Lints are declared once**, in `[workspace.lints]`, and every crate
  takes them with `[lints] workspace = true`.
- **`Cargo.lock` is committed**, for the library crates too, since
  the workspace ships a binary.
- `cargo update -p <crate>` moves one dependency; a lockfile change no
  manifest change explains is not committed.

## 9. Verification commands

These are the commands that sit behind the project's entry-point
names, run from the service folder.

| Task | Command |
|------|---------|
| Build | `cargo build --workspace --all-targets --locked` |
| Test | `cargo test --workspace --locked` |
| Test (one) | `cargo test -p <crate> <name>` |
| Lint | `cargo clippy --workspace --all-targets --locked -- -D warnings` |
| Format check | `cargo fmt --all --check` |
| Format apply | `cargo fmt --all` |
| Docs | `cargo doc --workspace --no-deps --locked` |

`--locked` refuses to write `Cargo.lock`, so a build that would
change it fails here and in CI alike.
