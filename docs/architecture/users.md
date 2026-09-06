# Users

Human identity. Implemented by `internal/user`, whose PostgreSQL
store is `internal/user/postgres`.

**The `Policies` field is built in part.** A user's directly assigned
policies are stored and carried on the aggregate, and nothing collects
them into a decision: a policy reaches a subject through an attachment
the authz engine owns (ADR-0008), not through this list.

`User`'s fields are exported and it has no accessors: every one of them
is a value the store writes and reads back, and the aggregate holds no
invariant over any of them
([ADR-0069](./adr/0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md),
superseding ADR-0051's accessor for this aggregate). `user.New`
remains, because it stamps `CreatedAt` and `UpdatedAt` from one instant
rather than copying what it was passed.

| Field | Meaning |
|-------|---------|
| `GRN` | user identifier (`service_type = "user"`) |
| `Nickname` | display name, not enforced unique |
| `Roles` | directly assigned roles |
| `Policies` | directly assigned policies |
| `CreatedAt`, `UpdatedAt` | insert and last-update instants |

A user can belong to multiple orgs; membership is recorded only in the
organizations domain (`Membership` / `org_members`), never duplicated on
the user. Roles on the user are direct grants, collected by the api
server alongside group- and project-derived grants at check time
([authz.md](./authz.md)).
