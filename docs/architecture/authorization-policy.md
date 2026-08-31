# Authorization Policy

The rules that decide what a subject may do, and the engine that
decides. Implemented by `internal/authz`, served over gRPC (ADR-0003
stage 2) as the IAM-style model ADR-0002 chose.

| Type | Responsibility |
|------|----------------|
| `Statement` / `PolicyDocument` | IAM-style rules: `Effect`, `Action[]`, `Resource[]` patterns |
| `StatementEvaluator` | native in-process `Evaluator`: explicit Deny > Allow > default Deny |
| `GRPCServer` | Check/Explain over `proto/garam/authz/v1` |
| `Policy` / `PolicyVersion` | a versioned org rule set and its immutable revisions |
| `PolicyAttachment` | binds a policy to the user or group it applies to (ADR-0008) |
| `Store` / `PostgresStore` | persists policies, their append-only versions, and attachments |
| `PolicyCache` | an org's active policies and attachments, dropped on write and on `NOTIFY` |
| `PolicyGRPCServer` | the policy lifecycle over `proto/garam/authz/v1` |

The api server authorizes through the gRPC client and manages policies
through the `PolicyManager` seam over the same connection
([authz.md](./authz.md) §Managing policies over HTTP); the engine
matches strings and imports no domain. What it does not take on trust
is the organization a question is asked in: it learns the subject's
memberships and refuses a question outside the object's organization
itself (ADR-0038). Lifecycle, caching, and
invalidation are described in [authz.md](./authz.md) §Policy
persistence.
