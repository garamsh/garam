# Users

Human identity. Implemented by `internal/user`.

| Field | Meaning |
|-------|---------|
| `grn.GRN` | user identifier (`service_type = "user"`) |
| `nickname` | display name, not enforced unique |
| `roles` | directly assigned roles |
| `policies` | directly assigned policies; not yet evaluated |

A user can belong to multiple orgs; membership is recorded only in the
organizations domain (`Membership` / `org_members`), never duplicated on
the user. Roles on the user are direct grants, collected by the api
server alongside group- and project-derived grants at check time
([authz.md](./authz.md)). The `policies` field is stored but not
evaluated: a policy reaches a subject through an attachment the authz
engine owns (ADR-0008), not through this list.
