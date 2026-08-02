# Naming Conventions

## Project vs company vs module

- Company: `garam`
- Project / module name: `gaon`
- Service codename: `gaon`
- Go module path: `github.com/garamsh/gaon`

Do not say "garam project" — that mixes the company with the product.

## GRN

`grn:<organization>:<project>:<service_type>:<identifier>`. Full
format: [`../architecture/grn-model.md`](../architecture/grn-model.md).

Organization and project segments identify the owning resources;
`root` is reserved as the owner of organizations themselves. See
[grn-model.md](../architecture/grn-model.md).

### service_type catalogue

Add or change entries in both the catalogue below and the same table
in [`grn-model.md`](../architecture/grn-model.md).

| service_type | Meaning | Code |
|--------------|---------|------|
| `user` | person | `internal/user` |
| `agent` | autonomous actor | `internal/agent` |
| `org` | organization | `internal/org` |
| `group` | user/agent bundle | `internal/org` |
| `project` | unit of work | `internal/org` |
| `policy` | stored policy document | `internal/authz` |
| `doc` | permissioned document (planned) | TBD |

### identifier

Lowercase letters, digits, `-`. Length ≤ 64. Prefer slug over UUID.

## Acronyms

Uniform case. `GRN`, `ID`, `URL`, `HTTP`, `JSON`, `OPA`, `SQL`,
`Rego`. Mixed case (`HttpClient`) is wrong.

## Go identifiers

- Verb + noun: `AssignRole`, `ParseToken`.
- Booleans: `Is*`, `Has*`, `Allow*`.
- Getters: `Name()`, not `GetName()`.
- Enum constants carry the type: `DecisionAllow`, `ActionGrant`,
  `KindNotFound`.
- Error sentinels: `Err*` prefix + domain noun (`ErrUserNotFound`).

## File names

Lowercase + underscore (`grn.go`, `authz_test.go`,
`manager_memory.go`). Files map to a concept, not a role.

## Directory names

Singular by default. Plural only for true collections.