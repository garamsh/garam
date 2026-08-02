# Testing Conventions

## Contents

- Three layers — unit / integration / e2e
- Behavior over implementation
- Mocking strategy
- Coverage and naming
- Placement

## Three layers — different goals, different scopes

| Layer | Scope | Mocks | Test client | External systems |
|---|---|---|---|---|
| **Unit** | single function/type in isolation | replace at module boundaries (DB, HTTP, time, FS) | direct call | none |
| **Integration** | modules cooperating | external systems substituted; SUT is real | `httptest.NewServer` | mocked |
| **E2E** | built binary against real infra | nothing | real client | **real** — dockerised Postgres (`make db-up`), gated on `GAON_TEST_DATABASE_URL` |

The app is *not yet a built binary* in integration. **No testcontainers
in integration** — real Postgres for an integration test is e2e.

## Behavior over implementation

Assert on outputs and side-effects only:

- return values
- emitted events, log lines
- HTTP responses (status + body)
- DB rows (at the e2e layer)

Do not assert on: call order, unexported helper shape, private type
structure, internal refactors. **If a refactor forces test updates, the
tests were testing implementation.**

## Mocking strategy

- **Unit**: mock any module boundary you control.
- **Integration**: external systems mocked or substituted; SUT itself is real.
- **E2E**: nothing mocked. Real binary, real infrastructure.

Mocks are generated with **mockery v3** via `.mockery.yaml`, output to
`internal/<domain>/mocks/` as importable packages (never `_test.go`
files — Go cannot import those). Regenerate; never hand-edit.
`make check-gen` fails CI on stale output.

### Substitutes for state

Time → inject a clock. Network → `httptest.NewServer`. Filesystem →
`t.TempDir()`. Random → seed it.

## Coverage and naming

- Target meaningful branches, not 100%. CI does not gate on a
  percentage; reviewers check that changed branches are exercised.
- `TestFunction_Scenario`; subtests via `t.Run("case", ...)`.
  Table-driven when cases share shape.
- Assertions: `testify/assert` + `testify/require`.
- A failing test name should tell you what broke without opening the file.

## Placement

- Unit: co-located with source. Default to **external test packages**
  (`package authz_test`) — black-box through the public contract.
  Internal tests only with a genuine white-box seam; still assert on
  behavior.
- Integration: `tests/integration/` (not yet used).
- E2E: `tests/e2e/`. Never inside `internal/`.
