# Users

Human identity. Implemented by `internal/user`.

**The `policies` field is built in part.** A user's directly assigned
policies are stored and exposed by the aggregate (ADR-0051), and
nothing collects them into a decision: a policy reaches a subject
through an attachment the authz engine owns (ADR-0008), not through
this list.

| Field | Meaning |
|-------|---------|
| `grn.GRN` | user identifier (`service_type = "user"`) |
| `nickname` | display name, not enforced unique |
| `roles` | directly assigned roles |
| `policies` | directly assigned policies |

A user can belong to multiple orgs; membership is recorded only in the
organizations domain (`Membership` / `org_members`), never duplicated on
the user. Roles on the user are direct grants, collected by the api
server alongside group- and project-derived grants at check time
([authz.md](./authz.md)).
