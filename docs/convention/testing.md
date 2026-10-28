# Testing Principles

Test layers, mocking, and placement — what each layer proves, what to
mock, and where tests live.

## Contents
- Three layers — different goals, different scopes
- Behavior over implementation
- Waiting
- Failing
- Mocking strategy
- Coverage and naming
- Placement

## Three layers — different goals, different scopes

This section states what each layer exercises and the shape it requires,
not the concrete client, build or wiring.

| Layer | Scope | Mocks | Test client | External systems |
|---|---|---|---|---|
| **Unit** | single function/class in isolation | replace at module boundaries you control (DB, HTTP, queue, time, FS) | direct call | none |
| **Integration** | modules cooperating | SUT is real | in-process client | substituted, or real; the rules below decide which |
| **E2E** | built binary against real infra | nothing | real client | **real** |

What separates the layers is the subject: integration exercises real
modules in process, e2e exercises what the build produces.

**No testcontainers in integration.** Reaching for a container to get a
real service **behind the code under test** means the test wanted the
deployed system. A suite of in-process modules stays an integration test
however real its Postgres, so substitute the service — renaming the
suite e2e leaves the project with nothing exercising the built artifact,
which is what the e2e row is for. Where the deployed system is what you
meant, build it and test that.

The exception is a dependency no in-process substitute can stand in for,
because the code exists to interact with it: a controller's API server,
not a database. Run that as a fixture and the test is still integration,
because the subject is still in-process modules.

**This project's integration layer runs against the Postgres
`make db-up` starts, because the store implementations are the subject
of those suites.** There, substituting the service means substituting
the store: the stand-in goes where the code under test is rather than
behind it, and the suite is left exercising the stand-in. The e2e row's
obligation is met separately, by `platform/tests/e2e/`, which drives the
built binary and fails without a store (#900).

**A test constructs what it puts under test** — the unit and whatever
stands in for its dependencies, or the server its client is pointed at.
What it does not construct is the shipped graph, the real
implementations wired together as the application ships them: where
that graph is the subject, the test calls the place that assembles it
for the application. A copy assembled inside the test goes on passing
after the shipped wiring breaks.

## Behavior over implementation

The lists below state only the kinds of target that qualify.

Assert on outputs and side-effects only:
- return values
- emitted events, log lines
- HTTP responses (status + body)
- DB rows — migration bookkeeping and schema catalog included, where the
  subject is what changes the schema

Do not assert on: call order, unexported helper shape, private type
structure, a connection's session state, internal refactors. **If an
internal refactor forces test updates, the tests were testing
implementation.**

## Waiting

A test that waits on the wrong condition passes for the wrong reason,
and the failure surfaces later on a machine with different timing.

- **A step waits on the condition it depends on.** Waiting for something
  already true proves nothing, and waiting for something unrelated
  proves less. A step that needs a form to be interactive waits for
  that, not for a label that was on screen before the step began.
- **The condition waited on and the condition asserted are the same.** A
  wait that passes while the assertion reads a stale value waited on the
  wrong thing.
- **Raising a timeout is not a fix.** It only lengthens how long the
  test tolerates the wrong condition. Find what the step depends on.
- **An intermittent failure is reproduced before it is fixed**, and the
  fix is shown by the failure rate before against after. A run count is
  not a rate: a handful of green runs is consistent with every failure
  rate small enough to be worth chasing, and a defect at one in a
  thousand is invisible to ten. The conditions that expose it are not
  always the loaded ones either — a slow machine can hide a race by
  delaying the thing that would otherwise arrive too early.

## Failing

A test that cannot fail is not evidence. It passes on the day the
behaviour is deleted, and it reads exactly like one that would have
caught it.

- **A test is shown to fail before it is trusted.** Remove the behaviour
  it names, watch it break, put the behaviour back. An assertion that
  survives that is measuring something else, and the pull request says
  which tests were seen to fail.
- **A test asserting a refusal carries a control that isolates the
  mechanism.** A rejection holds for reasons that have nothing to do
  with the one under test — two unrelated keys fail to match as readily
  as two wrongly separated ones — and a control chosen only to pass
  proves no more than the refusal did. The control is whatever fails
  when something other than the mechanism is doing the refusing: the
  accepted case that stops being accepted without it, or — where the
  mechanism accepts nothing of its own, as with a check that only
  narrows — an input everything around it accepts. Assert both in the
  same test, so they are read together.
- **A test verifies that its setup produced the state it asked for.** A
  setup call reports success for writing what the environment allowed,
  not what the test requested, and the difference is silent. Prefer a
  call the environment cannot alter; where none exists, read the state
  back and assert it before asserting behaviour on it. Otherwise a case
  that degenerated into another case still runs, still passes, and
  reports the assertion it never tested. Where the degenerate form is
  the control's own state, the control cannot see it.
- **Assert the property, not the absence of its consequence.** A field
  that is false and a field that is absent are different states, and
  most assertions cannot tell them apart. Only one of them is the
  property.

## Mocking strategy

This section states when a generated mock earns its place and what kind
of substitute stands in for state, not the tool or mechanism that does
it. What to mock at each layer is in
§Three layers — different goals, different scopes' Mocks column.

- **A generated mock exists only where a test consumes it.** An
  interface earns its entry in the generator's configuration when the
  first test needs a double, and loses it when the last one goes. A mock
  nothing calls is generated code standing in for nothing.

### Substitutes for state

Time → inject a clock. Network → substitute. Filesystem → tmpdir. Random
→ seed.

## Coverage and naming

Naming here covers what a test name conveys, not its concrete form.

- Target meaningful branches, not 100%.
- Names read like a spec.
- A failing test name should tell you what broke without opening the
  file.

## Placement

The list below states the kind of location each layer takes, not its
directory or file names.

- Unit: co-located with the source it tests.
- Integration: co-located with the boundary it exercises, or in a
  dedicated integration directory.
- E2E: a dedicated top-level directory, outside the application source
  tree.