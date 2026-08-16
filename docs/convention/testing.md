# Testing Principles

Test layers, mocking, and placement — what each layer proves, what to mock, and where tests live.

## Contents
- Three layers — different goals, different scopes
- Behavior over implementation
- Waiting
- Mocking strategy
- Coverage and naming
- Placement

## Three layers — different goals, different scopes

The concrete in-process client per stack is owned by the project's stack convention file; the Test client column states only the shape each layer requires.

| Layer | Scope | Mocks | Test client | External systems |
|---|---|---|---|---|
| **Unit** | single function/class in isolation | replace at module boundaries (DB, HTTP, queue, time, FS) | direct call | none |
| **Integration** | modules cooperating | external systems mocked or substituted; SUT is real | in-process client | mocked |
| **E2E** | built binary against real infra | nothing | real client | **real** (testcontainers OK) |

The app is *not yet a built binary* in integration. **No testcontainers in integration** — real Postgres for an integration test is e2e.

## Behavior over implementation

Assert on outputs and side-effects only:
- return values
- emitted events, log lines
- HTTP responses (status + body)
- DB rows (from a testcontainer, at the e2e layer)

Do not assert on: call order, unexported helper shape, private type structure, internal refactors. **If an internal refactor forces test updates, the tests were testing implementation.**

The concrete targets for a given stack are owned by the project's stack convention file; the lists above state only the kinds that qualify.

## Waiting

A test that waits on the wrong condition passes for the wrong reason, and the failure surfaces later on a machine with different timing.

- **A step waits on the condition it depends on.** Waiting for something already true proves nothing, and waiting for something unrelated proves less. A step that needs a form to be interactive waits for that, not for a label that was on screen before the step began.
- **The condition waited on and the condition asserted are the same.** A wait that passes while the assertion reads a stale value waited on the wrong thing.
- **Raising a timeout is not a fix.** It only lengthens how long the test tolerates the wrong condition. Find what the step depends on.
- **An intermittent failure is reproduced before it is fixed**, and the fix is shown by the failure rate before against after — not by one green run. The conditions that expose it are not always the loaded ones: a slow machine can hide a race by delaying the thing that would otherwise arrive too early.

## Mocking strategy

- **Unit**: mock any module boundary you control.
- **Integration**: external systems mocked or substituted; SUT itself is real.
- **E2E**: nothing mocked. Real binary, real infrastructure.

### Substitutes for state

Time → inject a clock. Network → substitute. Filesystem → tmpdir. Random → seed.

## Coverage and naming

- Target meaningful branches, not 100%.
- Names read like a spec. Concrete test-name forms follow the project's stack convention file.
- A failing test name should tell you what broke without opening the file.

## Placement

- Unit: co-located with the source it tests.
- Integration: co-located with the boundary it exercises, or in a dedicated integration directory.
- E2E: a dedicated top-level directory, outside the application source tree.

Concrete directory and file names follow the project's stack convention file — the stack file owns placement specifics.