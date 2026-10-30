# policy

The policy service's Cargo workspace (ADR-0130, ADR-0137). It holds the
contract, the vocabulary, the system tier and the crate that decides.
Its binary and its database are not built yet (#1689), so nothing calls
it.

- `proto/garam/policy/v1/` — the gRPC contract in AuthZEN's shape.
- `schema/garam.cedarschema` — the entity types, actions and attributes
  a decision can name.
- `system-policies/` — the system tier: tenant isolation, an agent's
  sending, and the role templates. One policy per file, and the file's
  stem is the id a decision's reasons name. Their tests sit in
  `crates/decision/tests/`.
- `crates/decision/` — decides a request against the system tier, an
  organization's policies and the entities they read.

`make -C policy ci` is the fast gate. `docs/convention/stack-rust.md`
governs the workspace.
