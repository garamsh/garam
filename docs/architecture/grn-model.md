# GRN Model

Garam Resource Names — one identifier for every resource in garam.
Implementation: [`internal/grn/grn.go`](../../internal/grn/grn.go).

## Contents
- Format
- Ownership
- service_type catalogue
- identifier
- Rego package note (historical)
- API

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
| `service_type` | resource kind (`user`, `org`, …) | slug, from the catalogue below |
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

This table is the catalogue. Adding or renaming a type changes it here,
and nowhere else.

| service_type | Domain | Example |
|--------------|--------|---------|
| `user` | person identity | `grn:acme:default:user:alice` |
| `agent` | autonomous actor | `grn:acme:default:agent:bot-1` |
| `org` | organization | `grn:root:default:org:acme` |
| `group` | user/agent bundle | `grn:acme:default:group:eng` |
| `project` | unit of work | `grn:acme:default:project:frontend` |
| `operator` | provisions agents from outside (ADR-0012); registered by its organization, which supplies the identifier (ADR-0045) | `grn:acme:default:operator:field-one` |
| `supervisor` | runs one agent on a standalone host; garam mints the identifier and registers it for that agent on an administrator's approval (ADR-0082) | `grn:acme:default:supervisor:0a1b2c3d4e5f6071` |
| `policy` | stored policy document | `grn:acme:default:policy:a1b2c3d4` |
| `agent-template` | starting point an organization kept for defining agents; a definition copied its values once (ADR-0075), and it is retired read-only (ADR-0088) | `grn:acme:default:agent-template:7c1e2a9b3d4f5a60` |
| `doc` | permissioned document (planned) | `grn:acme:frontend:doc:readme` |
| `secret` | reference to a value an external store holds, never the value (planned, ADR-0036) | `grn:acme:default:secret:model-api-key` |

## identifier

Lowercase letters, digits, `-`. Length ≤ 64. Slug over UUID. Never
expose a transient ID where a stable one is expected.

One prefix is reserved. A `user` identifier beginning `local-` names an
account garam minted from a username and a password, and no sign-in
provider's subject may claim one: a provider that asserts such a
subject is refused rather than handed the account it names
([ADR-0041](./adr/0041-a-password-is-garams-own-credential-verified-by-argon2id.md)).
The prefix is what keeps the two kinds of account in disjoint spaces,
so neither can ever resolve to the other's row.

The same character rule binds all four segments, `service_type`
included, and `grn.Validate` enforces it. A segment that can hold a
colon re-associates the rest on the way back in: `Parse` splits on
every colon, so a GRN carrying one names a different resource than the
one written, in a different organization. The catalogue constrains
which service types exist; it is the character rule that keeps a GRN
meaning one thing.

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
