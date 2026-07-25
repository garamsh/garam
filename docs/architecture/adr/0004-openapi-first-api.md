# ADR 0004: OpenAPI spec is the HTTP API source of truth

> Status: accepted
> Date: 2026-07-25

## Context

The MVP adds an HTTP API. Hand-written routes and hand-written docs
drift apart; drift between spec and behavior is a recurring source of
client bugs.

## Decision

`api/openapi.yaml` (OpenAPI 3.1) is written and reviewed before
handler code. oapi-codegen generates the `ServerInterface` and models
(stdlib `net/http`, no router dependency) into `api/server.gen.go`;
`internal/api` implements the interface. `make check-gen`
regenerates in CI and fails on drift. The same mechanism covers sqlc
and mockery output.

## Consequences

- Endpoint shape changes start in the spec, where review is cheap.
- Handlers cannot diverge from the spec's paths, parameters, or
  models — the compiler and CI catch it.
- Generated code is committed, so builds need no codegen tools; only
  `make gen` / `make check-gen` runs them (pinned via `go run`).
