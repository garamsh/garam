# GRN Model

Garam Resource Names — one identifier for every resource in gaon.
Implementation: [`internal/grn/grn.go`](../../internal/grn/grn.go).

## Format

```
grn:<organization>:<project>:<service_type>:<identifier>
```

Five colon-separated segments. Anything that does not start with
`grn:` (e.g. `arn:...`) is not a GRN.

| Segment | Meaning | Constraint |
|---------|---------|------------|
| `organization` | company or tenant | fixed to `garam` for now |
| `project` | unit of work | non-empty string |
| `service_type` | resource kind (`user`, `org`, …) | see catalogue below |
| `identifier` | unique id within the type | non-empty string |

## service_type catalogue

Update both this table and [`../convention/naming.md`](../convention/naming.md)
when adding or renaming a type.

| service_type | Domain | Example |
|--------------|--------|---------|
| `user` | person identity | `grn:garam:default:user:alice` |
| `agent` | autonomous actor | `grn:garam:default:agent:bot-1` |
| `org` | organization | `grn:garam:default:org:acme` |
| `group` | user/agent bundle | `grn:garam:default:group:eng` |
| `project` | unit of work | `grn:garam:default:project:frontend` |
| `doc` | permissioned document (planned) | `grn:garam:default:doc:readme` |

## identifier

Lowercase letters, digits, `-`. Length ≤ 64. Slug over UUID. Never
expose a transient ID where a stable one is expected.

## Why `garam` is fixed

The Go module path (`github.com/garamsh/gaon`) and the company name
align, so the org segment doubles as a company marker. Multitenancy
is a separate decision and would force a config-driven segment.

## Rego package is `gaon.authz`

The data identifier uses `garam`; the policy language namespace uses
`gaon`. They are intentionally different:

- Data: `grn:garam:...`
- Policy package: `package gaon.authz`
- Policy query: `data.gaon.authz.allow`

GRN points at the data's origin. The Rego package points at the
code's home. See [authz.md](./authz.md) §3.

## API

```go
g := grn.New("garam", "default", "user", "alice")
s := g.String()                 // "grn:garam:default:user:alice"

g, err := grn.Parse(s)
err := g.Validate()
if g.IsZero() { ... }
```

`GRN` is a value type. Two structs with equal fields are equal.