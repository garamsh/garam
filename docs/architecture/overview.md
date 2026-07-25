# Architecture Overview

gaon is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

## In scope

- Identities for users and agents, including organization membership.
- Org / group / project structure.
- Authorization decisions, expressed as IAM-style policy documents.
- One identifier system across every resource (GRN).

## Out of scope (today)

- Workflow execution engine, scheduler, queue.
- Notifications or messaging.
- Persistent store implementations (only interfaces today).

## Module boundaries

```
github.com/garamsh/gaon
├── cmd/gaon/     # single cobra CLI: serve api | serve authz | migrate
├── proto/        # .proto sources, buf codegen
├── internal/
│   ├── grn/      # identifier — shared primitive
│   ├── errorx/   # typed errors — shared primitive
│   ├── cli/      # cobra commands + wiring
│   ├── api/      # OpenAPI HTTP server (generated handlers)
│   ├── authn/    # OIDC provider abstraction, sessions
│   ├── authz/    # policy engine: model, stores, evaluator, gRPC server
│   ├── org/      # organizations, groups, projects, invitations
│   ├── user/     # human identities
│   └── agent/    # autonomous identities (planning)
└── (docs/)
```

`grn` and `errorx` are the only shared packages. `api` and `authz`
never import each other — the api server reaches the engine over gRPC
(ADR-0003). Cross-cutting promotion only when ≥3 domains share the
concern.

## Core abstractions

| Type | Package | Responsibility |
|------|---------|----------------|
| `grn.GRN` | `internal/grn` | resource identifier (value type) |
| `errorx.Error` | `internal/errorx` | typed error with kind |
| `user.User` | `internal/user` | human identity |
| `agent.Agent` | `internal/agent` | autonomous identity |
| `org.Organization` | `internal/org` | top-level tenant |
| `org.Group` | `internal/org` | user/agent bundle |
| `org.Project` | `internal/org` | unit of work |
| `authz.Subject` | `internal/authz` | actor interface |
| `authz.Object` | `internal/authz` | target interface |
| `authz.Evaluator` | `internal/authz` | policy evaluator (native, IAM-style) |
| `authz.Checker` | `internal/authz` | role enrich + evaluate |
| `authz.Manager` | `internal/authz` | grant/revoke + audit |

## Decisions

| Decision | Reason | Where |
|----------|--------|-------|
| GRN org/project = owning resource identifiers | Tenancy is structural; reserved `root` owns organizations. | [grn-model.md](./grn-model.md), [ADR-0001](./adr/0001-grn-segments.md) |
| Policies are IAM-style documents, native evaluator | Deterministic deny-overrides; no runtime dependency. | [authz.md](./authz.md), [ADR-0002](./adr/0002-iam-style-policy-model.md) |
| Single cobra CLI, independent api/authz packages | One binary, two processes; engine reached over gRPC only. | [ADR-0003](./adr/0003-single-cli-topology.md) |
| `Evaluator` is an interface | Swap in-process / gRPC-client without touching call sites. | [authz.md](./authz.md) |
| Store interfaces live next to their domain | Implementation swap at the package boundary. | [domains.md](./domains.md) |

## Non-functional goals

- **Deterministic decisions** — `Manager.Explain` can reproduce why a
  request was allowed or denied.
- **Transport-agnostic errors** — `errorx` flows over HTTP, gRPC, CLI
  without translation at the source.
- **Testable in isolation** — every domain exposes an interface; unit
  tests substitute at module boundaries (mocks, in-memory test doubles
  where they exist).
- **Storage-agnostic** — Postgres, SQLite, Redis plug into the same
  `Store` interface without domain code changes.