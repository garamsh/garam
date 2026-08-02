# GRN Model

Garam Resource Names — one identifier for every resource in garam.
Implementation: [`internal/grn/grn.go`](../../internal/grn/grn.go).

## Format

```
grn:<organization>:<project>:<service_type>:<identifier>
```

Five colon-separated segments. Anything that does not start with
`grn:` (e.g. `arn:...`) is not a GRN. The `grn` prefix already names
the vendor (Garam); no segment repeats it.

| Segment | Meaning | Constraint |
|---------|---------|------------|
| `organization` | owning Organization identifier | slug; `root` is reserved |
| `project` | owning Project identifier | slug; `default` when org-level |
| `service_type` | resource kind (`user`, `org`, …) | see catalogue below |
| `identifier` | unique id within the type | slug, see below |

## Ownership

The organization and project segments identify the resources that own
the addressed resource — tenancy is structural, not a flag.

- `grn:acme:frontend:user:alice` — user alice, owned by project
  `frontend` of org `acme`.
- `grn:acme:default:agent:bot-1` — org-level resource: project segment
  is `default`.
- `grn:root:default:org:acme` — organizations are owned by the
  reserved root owner `root`. `root` may not be used as a real
  organization identifier.

Segment values (organization, project, identifier) follow the
identifier rules below.

## service_type catalogue

Update both this table and [`../convention/naming.md`](../convention/naming.md)
when adding or renaming a type.

| service_type | Domain | Example |
|--------------|--------|---------|
| `user` | person identity | `grn:acme:default:user:alice` |
| `agent` | autonomous actor | `grn:acme:default:agent:bot-1` |
| `org` | organization | `grn:root:default:org:acme` |
| `group` | user/agent bundle | `grn:acme:default:group:eng` |
| `project` | unit of work | `grn:acme:default:project:frontend` |
| `policy` | stored policy document | `grn:acme:default:policy:a1b2c3d4` |
| `doc` | permissioned document (planned) | `grn:acme:frontend:doc:readme` |

## identifier

Lowercase letters, digits, `-`. Length ≤ 64. Slug over UUID. Never
expose a transient ID where a stable one is expected.

## Rego package note (historical)

An earlier design compiled Rego policies under `package garam.authz`.
That direction is superseded by IAM-style policy documents
([ADR-0002](./adr/0002-iam-style-policy-model.md)); no Rego package
exists.

## API

```go
g := grn.New("acme", "frontend", "user", "alice")
s := g.String()                 // "grn:acme:frontend:user:alice"

g, err := grn.Parse(s)
err := g.Validate()
if g.IsZero() { ... }
```

`GRN` is a value type. Two structs with equal fields are equal.
