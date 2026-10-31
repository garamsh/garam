# policy

The policy service's Cargo workspace (ADR-0130, ADR-0137, ADR-0138). It
decides over its own stores and authenticates its callers. The local
stack runs its image under the `policy` profile, and nothing calls it
yet (#1689).

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
  reading `POLICY_ADDR`, `POLICY_HEALTH_ADDR`, `POLICY_DATABASE_URL`,
  `POLICY_TOKEN_ISSUER`, `POLICY_JWKS_FILE`, `POLICY_TLS_CERT_FILE` and
  `POLICY_TLS_KEY_FILE`. `policy migrate` applies the migrations and
  exits, reading `POLICY_DATABASE_URL`. `policy probe <url>` is what a
  healthcheck runs on an image with no shell.

## Transport

- **The contract is served over TLS and in no other way.** Every setting
  `policy serve` reads is required, so with either half of the pair
  unset it refuses to start, and no setting turns TLS off: a bearer
  token rides this connection (ADR-0138), and ADR-0125 rules out network
  position. The local stack is no exception, since `policy-tls` writes
  it a throwaway pair.
- **The pair takes the machine listener's terms.** The key file is
  readable by its owner alone, and its owner is the process's user
  (`deployment-contract.md`). It is read at start, so a rotation is a
  rollout, as ADR-0138 states of the verifying keys; the machine
  listener's re-read on each handshake is not built here.
- **`/healthz` and `/readyz` answer on `POLICY_HEALTH_ADDR`, in
  plaintext.** It is a port of its own, as `GARAM_MACHINE_HEALTH_ADDR`
  is, and carries no credential. `/healthz` reads nothing outside the
  process, and `/readyz` answers while the database does.
- **Postgres TLS is `rustls` on `ring`,** the provider the gRPC listener
  already uses, so the binary links one TLS stack and needs no OpenSSL
  on a scratch image. `POLICY_DATABASE_URL`'s `sslmode` decides whether
  a connection is encrypted and what it verifies. `sslrootcert` names
  the database's root, and without one it verifies against the system's
  roots, which the image ships.

## Analysis

`crates/decision/tests/analysis.rs` proves properties of the system
tier with `cedar-policy-symcc` and cvc5, for every request and entity
store the schema admits (ADR-0130 §5). `policy/Makefile` pins the
solver and fetches it under `target/`.

`make -C policy ci` is the fast gate, and `make -C policy stack-up
suites` runs the store tests against Postgres and the analysis.
`docs/convention/stack-rust.md` governs the workspace.
