# ADR 0019: The machine listener carries an OpenAPI specification of its own

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

ADR-0004 made `api/openapi.yaml` the source of truth for the HTTP API
because hand-written routes and hand-written docs drift, and the drift
fails silently. ADR-0017 then added a listener that document does not
describe, and left the question open in its own consequences: "whether
a second surface gets a spec of its own is undecided and the message
endpoints will force it."

The message endpoints are not what arrived first. An operator creating
an agent and obtaining its certificate is, and it is three endpoints
with a client garam does not ship — ADR-0012 turns on a user being able
to bring their own operator, so the contract is read by someone who
cannot read this repository's handlers.

Two further facts decide the shape rather than the question. The
browser-facing spec applies one security scheme to every operation, the
session cookie, and this listener's caller is a client certificate
(ADR-0014). And `web/` generates its API types from
`api/openapi.yaml` under `make web-check-gen`, so a path added there
reaches a client that can never call it.

## Decision

- **`api/machine.yaml` is the source of truth for what `garam serve
  machine` serves**, generated into `api/machine/` by the same
  oapi-codegen the first spec uses and implemented by
  `internal/machineapi`. `make check-gen` gates it the way it gates the
  first.
- **One specification per listener.** The split follows what a caller
  reaches and authenticates as, not what a process contains.
- **The whole listener is in it**, the identity endpoint included. A
  listener described in one file and routed in another is the drift
  ADR-0004 exists to prevent, and a client cannot discover an endpoint
  that appears only in Go.

## Consequences

- Two specifications to keep current, and a reader asking what garam
  serves reads both.
- The browser client's generated types stay free of a surface it cannot
  call, and neither spec carries the other's security scheme.
- The machine listener's contract is publishable to whoever writes an
  operator, which is what ADR-0012's bring-your-own operator needs.
- A third listener repeats this rather than reopening it.
- The identity endpoint's shape is now a specified model rather than a
  handler's anonymous struct, so changing it is a change to a reviewed
  document.

## Rejected alternatives

- **Adding the machine paths to `api/openapi.yaml`.** One document and
  one generated server to wire. It puts two authentication schemes in
  one contract, hands the web client generated types for endpoints it
  can never reach, and hides the process split ADR-0017 bought behind a
  single document — the deployment boundary would be invisible exactly
  where a client looks for it.
- **Hand-written handlers on this listener, documented in prose or not
  at all.** Cheapest today, and it is the drift ADR-0004 already
  rejected: the reason does not weaken because the listener is the
  second one, and an operator written elsewhere would have nothing to
  build against.
- **Deferring until the message endpoints, as ADR-0017 expected.** The
  endpoints that arrived first would be hand-written now and rewritten
  then, and the two shapes would be reconciled in a pull request rather
  than decided here.
