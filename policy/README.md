# policy

The policy service's Cargo workspace (ADR-0130, ADR-0137, ADR-0138). It
decides over its own stores and authenticates its callers. No image
runs it yet, and nothing calls it (#1689).

- `proto/garam/policy/v1/` — the gRPC contract in AuthZEN's shape.
- `schema/garam.cedarschema` — the entity types, actions and attributes
  a decision can name.
- `system-policies/` — the system tier: tenant isolation, an agent's
  sending, and the role templates. One policy per file, and the file's
  stem is the id a decision's reasons name. Their tests sit in
  `crates/decision/tests/`.
- `migrations/` — the service's own database.
- `crates/decision/` — decides a request against the system tier, an
  organization's policies and the entities they read, and declares the
  ports it reads them through.
- `crates/grant/` — role assignments and guardrails: schema-validated
  writes and append-only versions.
- `crates/entity/` — the containment and membership decisions read, and
  the sequenced write owners reach it by.
- `crates/access-report/` — the policies naming a subject or a resource.
- `crates/server/` — the binary, `policy`. `policy serve` serves the
  contract, authenticates callers and joins the adapters to the ports,
  reading `POLICY_ADDR`, `POLICY_DATABASE_URL`, `POLICY_TOKEN_ISSUER`
  and `POLICY_JWKS_FILE`. `policy migrate` applies the migrations and
  exits, reading `POLICY_DATABASE_URL`.

`make -C policy ci` is the fast gate, and `make -C policy stack-up
suites` runs the store tests against Postgres.
`docs/convention/stack-rust.md` governs the workspace.
