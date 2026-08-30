# Go — Architecture & Style Conventions

> How a Go module is laid out and where each kind of code belongs:
> directories, naming, errors, logging, comments, tests, imports.
>
> Checked against Go 1.21 and mockery 3. A claim below that names no
> version holds for these.

## Contents
- 0. Folder & file naming — strict
- 1. Directory layout
- 2. Module / package boundary
- 3. Naming
- 4. Error handling
- 5. Logging & observability
- 6. Comments & docs
- 7. Testing
- 8. Imports & dependencies
- 9. Verification commands

## 0. Folder & file naming — strict

Names describe **what they own**. **Banned at any level:**
`model.go`, `utils/`, `helpers/`, `common/`, `ext/`, `adapter/`,
`driver/`, `platform/`, `infra/`, `kit/`, `repo/`.

If two helpers share a concept, **give the concept a name**:
`password_hashing.go`, `format_currency.go`. The path tells you what
the file does.

(Standard practice in the Go community: package names after the
concept they own, not the role they play — `auth` over
`auth_utils`. `spf13/cobra` and `go-kit/kit` both follow this.)

## 1. Directory layout

See §0 for the banned-name list.

### Layout A — small service (default)

- `cmd/<binary>/main.go` — the binary's entry point.
- `internal/<domain>/<domain>.go` — the aggregate: domain types, DTOs
  and sentinel errors (`User`, `CreateUserInput`, `ErrUserNotFound`).
- `internal/<domain>/service.go` — what the domain offers: `type
  Service interface { ... }`, unexported `type service struct { ... }`,
  `NewService(...) Service`. A domain owes one once it has behaviour of
  its own to offer. Holding types and persisting them is not that, and
  neither is translating a request, composing commands or wiring a
  runtime.
- `internal/<domain>/<name>.go` — one dependency interface the domain
  needs, named for it: `repository.go` declares `type Repository
  interface`, `mailer.go` declares `type Mailer interface`.
- `internal/<domain>/<verb>.go` — one file per verb (`create.go`,
  `update.go`, `query.go`); method bodies split by responsibility.
- `internal/<domain>/service_test.go` — the service's tests (§7).
- `internal/<domain>/<name>/` — the implementations of that dependency
  interface, one file each (`repository/postgres.go`,
  `repository/memory.go`); the in-memory one serves tests and dev.
- `internal/<crosscutting>/` — a shared concern extracted below the
  domains, named by what it is.
- `go.mod`, `go.sum` — module root.

**Rules for `internal/<domain>/`:**

- Split into `<verb>.go` only when the service has more than one
  verb. A single-method service keeps that method in `service.go`.
- **Every dependency interface gets its own file**, named for the
  interface: `internal/<domain>/repository.go` declares `type
  Repository interface`. No condition and no threshold — the side that
  needs the behaviour declares the contract, and one file holds one
  contract.
- **Its implementations go in `internal/<domain>/<name>/`**, a package
  named for the interface rather than for the technology behind it.
  Two implementations of one interface are two files in that one
  package.

**One file vs several** inside `internal/<domain>/<name>/`:

- **One file per implementation** (`postgres.go`, `memory.go`) when it
  is ≤ ~300 LoC and has no private helpers worth isolating.
- **Several files for one implementation** (`postgres.go`,
  `postgres_queries.go`) when it is > ~300 LoC or owns private
  helpers / connection-pool / per-SQL constants.

### Layout B — domain-rich service (5k–30k LoC)

Layout A's shape, with verb files grouped by responsibility and impl
packages split across several files:

- `internal/<domain>/lifecycle.go`, `internal/<domain>/billing.go`,
  `internal/<domain>/permissions.go` — method bodies grouped by
  responsibility (create/activate/deactivate, charge/refund,
  authorize/deny).
- `internal/<domain>/repository/postgres.go`,
  `internal/<domain>/repository/postgres_queries.go` — one
  implementation too big for one file.
- `internal/<domain>/repository/memory.go` — the in-memory
  implementation, in the same package.
- Every other domain repeats the shape.

A cross-cutting concern moves to its own `internal/<thing>/` package
past ~300 LoC.

### Layout C — library

- `<pkg>.go`, `<pkg>_test.go`, `go.mod`, `README.md` — all at the
  module root. `command.go`, `args.go` — one file per thing it owns.

### Root-level files

A root-level file exists when the module has that concern, and it is
named after the concern:

- `errors.go` — sentinels shared across domains, where the module has
  no package for them. This project keeps them in `internal/errorx`
  (§4). Domain sentinels stay in `internal/<domain>/<domain>.go`.
- `config.go` — the module's configuration type and its loading.
- `logger.go` — only when logger setup goes past `slog.Default()`
  (§5). A project that calls `slog.Default()` directly has no
  root-level logger file.
- `httpserver.go` — server construction and route wiring, when the
  module serves HTTP.

Sizing, for each of them:

- **Single file (`errors.go`, `logger.go`, `config.go`, …)** when ≤
  ~300 LoC. Filename = what's inside.
- **Promotion to `internal/<thing>/`** when > ~300 LoC or owns private
  helpers. Folder name describes what's inside
  (`internal/logger/`, `internal/httpserver/`, `internal/config/`);
  the §0 banned-name list applies.

Multiple root-level files are fine: `errors.go` + `logger.go` +
`config.go`, each named after its concern.

### Project envelope

- `cmd/<binary>/main.go` is the **only place** that constructs concrete types and passes them to interfaces. Keep it thin.
- `internal/` is enforced by the Go toolchain. Use it for everything not explicitly public.
- `pkg/` is for code other modules import. Most services don't need it.

This project adds seven directories the layout above does not name.
Each bullet names the ADR that owns its directory where one does;
`migrations/`, `tests/` and `docs/` are owned by none.

- `api/` — the OpenAPI specifications and the servers generated from
  them. There are two, one per surface: `openapi.yaml` for the browser
  (ADR-0004) and `machine.yaml` for the machine listener (ADR-0019).
- `proto/` — `.proto` sources and generated code (ADR-0003).
- `migrations/` — SQL migrations; the schema's source of truth. The Go
  file beside them tests the migrations and declares no schema.
- `tests/` — black-box tests: `tests/e2e/` against real infrastructure,
  `tests/image/` against the built image.
- `web/` — the frontend, which is not Go, so this file does not govern
  it (ADR-0009).
- `deploy/` — `local/` is what is needed to run the built images
  locally, and `charts/garam/` is the chart that describes a garam
  deployment (ADR-0042, superseding ADR-0020). garam publishes two
  images and a contract; the chart now stands beside them, so what a
  cluster deploys does live here. `deploy/charts/garam/README.md`
  describes what it renders.
- `docs/` — the conventions, the architecture documents and the plans;
  `docs/README.md` is the index.

Not every package under `internal/` is a domain. The boundary binds a
test as it binds shipped code: one surface's test package does not
import another surface.

## 2. Module / package boundary

§1 owns the per-domain file inventory. This section states which Go
file declares and which depends.

- `internal/<domain>/<domain>.go`, `internal/<domain>/service.go` and
  `internal/<domain>/<name>.go` declare the types, sentinels, `Service`
  interface and dependency interfaces the rest of the domain is written
  against.
- `internal/<domain>/<verb>.go` and the implementations in
  `internal/<domain>/<name>/` depend on those declarations.
- `mocks/` — generated by mockery (§7) for the interfaces its config
  names.
- **The implementation lives in a sub-package of the package that
  declares the interface.** `internal/<domain>/<name>/` may import
  `internal/<domain>` for the types in the interface's signatures; the
  reverse import never happens.
- **A consumer imports the producer's interface, not its concrete
  type.** `order` imports `billing.Charger`; the compiler does not
  enforce this, so it is a review matter.
- Within a domain and across domains those two rules point opposite
  ways, for a reason: a domain declares the behaviour it needs and
  publishes the behaviour it offers, so a need is declared beside its
  consumer and an offer beside its producer.
- Go rejects an import cycle at compile time but does not detect a
  `type → struct → type` cycle. Those are found by reading.
- **A boundary that erases a static type is read back with the comma-ok
  form.** `context.Value` returns `any`, and probing whether a request
  satisfies an interface has no other syntax. `v, ok := x.(T)` with an
  explicit branch for `!ok` is the accepted form; a bare `x.(T)` is not,
  because it panics instead of handling the case.

## 3. Naming

- **Packages:** single word, lowercase, no underscores. Singular for
  one kind of thing (`user`, `order`); pluralize only for genuine
  collections (`errors`, `flags`).
- **Types:** `MixedCaps`, no underscores (`UserService`).
- **Interfaces:** the behaviour required, in the agent form of its verb
  (`Storer`, `Mailer`, `Validator`). A dependency that is a thing rather
  than an action keeps the thing's name — `Repository`, `Clock`,
  `Session`; a `Model` does not become a `Modeller`. Where that name is
  also one of the domain's verbs, the agent form breaks the tie: a
  `Store` verb, a `Storer` interface, one file each (§1).
- **Functions / methods:** `MixedCaps`, verb-noun (`GetUser`,
  `ParseToken`).
- **Constants:** `MixedCaps` (not `MAX_SIZE`). Group in `const ( ... )`.
- **Variables:** short in small scopes, `MixedCaps` for package-level.
- **Acronyms:** all-caps for the common form, consistent case
  (`HTTPClient`, `URLParser`, `ID`).
- **Receivers:** short, consistent across methods of the same type
  (`s *Service`).
- **Initialisms:** `URL`, `ID`, `HTTP`, `JSON`, `XML`, `API`, `SQL`,
  `GRN`, `OPA` — always uppercase or lowercase, never mixed. `Rego` is
  a word, not an initialism.
- **Booleans:** `Is*`, `Has*`, `Allow*`.
- **Accessors:** `Name()`, never `GetName()`. The verb-noun form above
  is for functions that do work; an accessor that only returns a field
  is named after the field.
- **Enum constants carry the type:** `DecisionAllow`, `ActionGrant`,
  `KindNotFound`.
- **Error sentinels:** `Err` prefix plus a domain noun
  (`ErrUserNotFound`).
- **File names:** lowercase with underscores (`grn.go`, `authz_test.go`,
  `manager_memory.go`). A file is named after a concept, not a role.

## 4. Error handling

- Errors are values. Use `error` interface, never panics in libraries.
- An HTTP server still holds a recover middleware: `net/http` recovers
  a handler's panic and logs the stack itself, but aborts the response
  instead of answering — the client gets a closed connection, or an
  HTTP/2 `RST_STREAM` — so the middleware exists to reply 500.
- Don't log and return — pick one (usually return; let the caller
  decide to log).
- Wrap at boundaries (network, IO, external calls), not on every line.

Every error carries a kind, through `internal/errorx`. ADR-0003 decides
this: the api and authz processes talk over gRPC, and gRPC status codes
carry `errorx` kinds without translation. Stdlib sentinels lose the kind
at that boundary, so they are not used here.

- `errorx.New(kind, msg)` — a new error with a fixed kind.
- `errorx.Wrap(err, msg, kind...)` — attach context. Falls back to the
  chain's kind when no override is given.
- `errorx.IsKind(err, kind)` — kind check across the chain.
- Domain sentinels are declared once in the owning package
  (`var ErrUserNotFound = errorx.New(errorx.KindNotFound, "user not found")`);
  shared ones live in `internal/errorx`.
- `errors.New` and `fmt.Errorf("…: %w", …)` lose or drift the kind, and
  `errors.Is` does not read it. Do not mix them with `errorx`.
- Do not add kinds. Map at the transport boundary if you need more.

## 5. Logging & observability

- Stdlib `log/slog` (Go 1.21+). Prefer it over `log` and third-party
  loggers for new code.
- Levels: `Debug`, `Info`, `Warn`, `Error`.
- Required fields for HTTP / RPC handlers: `trace_id` (from
  OpenTelemetry's request context — falls back to a
  locally-generated UUID if no span exists) and `user_id`
  / `account_id` when the principal is known. The older
  `request_id` field still appears in many log pipelines but is
  really a stand-in for `trace_id`; new code emits the latter.
- Emit the operation name and key parameters as well.
- **Never** log secrets, passwords, tokens, full request bodies that
  may contain PII.
- Metrics: `prometheus/client_golang`. Define the registry once.
- Tracing: OpenTelemetry (`go.opentelemetry.io/otel`).

## 6. Comments & docs

This section states the Go comment form only.

- Doc comments begin with the name being declared.
- Long descriptions belong in `service.go` as a doc comment on the
  service struct.

## 7. Testing

This section states the Go test placement, tooling and test-name
form.

**Runner:** stdlib `testing`, files end with `_test.go`.
**Assertions:** `testify/assert` + `testify/require` (default unless
project says otherwise).

**Organization:**

- **External tests** (`package user_test`): next to source. Reach the
  unit under test through its exported API. **Default to this.**
- **Internal tests** (`package user`): same directory and package as
  the code under test. Use only when you genuinely need a white-box
  seam (uncommon), or when the package cannot be imported — `package
  main` cannot, so a test of one is internal by necessity rather than
  by choice.
- **`tests/` at module root:** integration / E2E tests that wire
  multiple domains. Separate binary.
- **In-process integration test client:** `httptest.NewServer`.
- **E2E tooling:** the binary under test is what `go build` produces;
  real services come from containers. This project brings them up from
  `deploy/local/docker-compose.yml` rather than from
  `testcontainers-go`, so one compose project serves the dev stack and
  both test suites.

Within `internal/<domain>/`:

- Tests for `<group>.go` live in `<group>_test.go`.
- Table-driven subtests: `tests := []struct{ name string; ... }{...}`,
  run through `t.Run("case name", ...)`.
- Test names: `TestFunctionName` or `TestFunctionName_Scenario`.
- Benchmarks: `func BenchmarkXxx(b *testing.B)`.

### Generated mocks

Where the project generates mocks, generate them with
**[mockery v3](https://vektra.github.io/mockery/)**.

- **Config:** `.mockery.yml` at the module root — or `.mockery.yaml`,
  which mockery reads equally. One or the other, not both. It declares
  which interfaces to mock, output directory, package names,
  per-interface overrides. The leading dot is load-bearing: mockery
  v3.7.2 discovers only the dotted names, and an undotted file is
  reachable only through `--config`.
- **Generated location:** declared in that file by `dir`, the output
  directory, with `filename` for the file itself — at the top level,
  or per interface in that interface's `config:` block under
  `interfaces:`. Default: `mocks/<package>/<Interface>.go` at module
  root. A dependency interface is scoped to the one domain that
  declares it, so `dir: internal/<domain>/mocks/` fits it too. Pick
  one convention per project.
- **Generation:** `mockery` (reads config) or `go generate ./...`
  when interfaces carry `//go:generate mockery` directives. Pick one.
- **In tests:** the generated mock satisfies the interface; pass it
  as a constructor argument (`NewService(repo, mailer, logger)`).

When a top-level domain depends on another top-level's interface, the
test for the consumer uses the consumer-side mock (generated from the
interface declared in the **producer's** top level).

## 8. Imports & dependencies

Three groups, separated by blank lines:

1. Standard library
2. Third-party
3. Internal module

`goimports -local <module path>` produces the third group. Plain
`goimports` emits two — standard library and everything else — so the
`-local` flag is what separates the internal module.
Use `go mod tidy` after every change. Don't commit `go.sum` updates
you don't recognize. `go vet ./...`. `golangci-lint run` if
configured.

## 9. Verification commands

These are the commands that sit behind the project's entry-point
names.

`go test` prints only `ok <pkg>` for a package that passes and discards
the rest of its output; the full output appears on failure, or under
`-v`. So anything a test binary prints about what it covered is missing
from exactly the run that needed it. An entry point that has something
to report prints it itself, before invoking `go test`.

| Task | Command |
|------|---------|
| Build | `go build ./...` |
| Test | `go test ./...` |
| Test (one) | `go test -run TestName ./pkg/...` |
| Vet | `go vet ./...` |
| Format check | `gofmt -l .` |
| Format apply | `gofmt -w .` |
| Module tidy | `go mod tidy` |
| Lint (if configured) | `golangci-lint run` |
| Coverage | `go test -cover ./...` |
| Generate (mockery) | `mockery` (config) or `go generate ./...` |

Every tool the module invokes is built with a toolchain at least the
module's `go` directive. `go install tool@version` and `go run
tool@version` take the toolchain from the tool's own `go.mod`, not from
this module — mockery 3.7.3 declares `go 1.25.5` — so a tool whose
directive is older yields a binary that refuses to load the module's
packages, fatally and at package-load time. Set that floor once for the
module rather than per tool: `GOTOOLCHAIN` with an `+auto` suffix sets a
minimum without stopping a tool that needs more.

A task runner (Taskfile, Mage, Make) wraps these under the entry-point
names; the underlying go commands stay the same.

