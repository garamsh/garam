# Architecture Overview

gaon is the core of a workflow management product: identity,
organization, and authorization for people and autonomous agents that
collaborate on a shared plan.

## In scope

- Identities for users and agents, including organization membership.
- Org / group / project structure.
- Authorization decisions, expressed as Rego policies.
- One identifier system across every resource (GRN).

## Out of scope (today)

- Workflow execution engine, scheduler, queue.
- Notifications or messaging.
- HTTP / RPC transport.
- Persistent store implementations (only interfaces today).

## Module boundaries

```
github.com/garamsh/gaon
├── internal/
│   ├── grn/        # identifier — every domain depends on it
│   ├── errorx/     # typed errors — every domain depends on it
│   ├── org/        # organizations, groups, projects
│   ├── user/       # human identities
│   ├── agent/      # autonomous identities
│   └── authz/      # subject/object, roles, policies, decisions
└── (cmd/, docs/)
```

`grn` and `errorx` sit at the inner ring. Nothing else imports each
other; cross-cutting promotion only happens when ≥3 domains share the
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
| `authz.Evaluator` | `internal/authz` | policy evaluator (Rego) |
| `authz.Checker` | `internal/authz` | role enrich + evaluate |
| `authz.Manager` | `internal/authz` | grant/revoke + audit |

## Decisions

| Decision | Reason | Where |
|----------|--------|-------|
| GRN org/project = owning resource identifiers | Tenancy is structural; reserved `root` owns organizations. | [grn-model.md](./grn-model.md), [ADR-0001](./adr/0001-grn-segments.md) |
| Rego package = `gaon.authz` | Policy namespace follows the service codename, not the org segment. | [authz.md](./authz.md) |
| `Evaluator` is an interface | Swap Rego / in-process / hardcoded without touching call sites. | [authz.md](./authz.md) |
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