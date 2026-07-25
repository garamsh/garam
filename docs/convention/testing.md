# Testing Conventions

## Stack

- Runner: stdlib `testing`.
- Assertions: `github.com/stretchr/testify` (`assert` + `require`).
- Mocks: [mockery v3](https://vektra.github.io/mockery/) via
  `.mockery.yaml`, output to `internal/<domain>/mocks/`.
- Test data: inline for small fixtures, `testdata/` for large or
  generated sets.

## Layers

| Layer | Scope | Mocks | Where |
|-------|-------|-------|-------|
| Unit | one function or type | replace at module boundaries (DB, HTTP, time, FS) | `<domain>/<file>_test.go` |
| Integration | modules cooperating | external systems only; SUT is real | `tests/integration/` (planned) |
| E2E | built binary against real infra | none | `e2e/` (planned) |

No testcontainers in integration. Real services are an e2e concern.

## Placement

- Default to external tests (`package authz_test`). Test public
  contracts only.
- Internal tests (`package authz`) only when a real white-box seam
  exists. Still assert on behavior, not on helper shape.

## Naming

`TestFunction_Scenario`. Subtests with `t.Run("case", ...)`.
Table-driven when cases share shape.

## Mocking

Mock at module boundaries. Substitute stateful inputs cleanly:

- time → inject a clock interface
- network → `httptest.NewServer` or a small interface
- filesystem → `t.TempDir()`
- random → seed it

Do not edit generated mocks by hand. Regenerate.

## Behavior over implementation

Assert on outputs and side-effects only — return values, emitted
events, log lines, HTTP responses, DB rows. Never on call order or
unexported helper shape.

## Coverage

Aim at meaningful branches, not 100%. CI doesn't gate on a percentage;
reviewers check that changed branches are exercised.