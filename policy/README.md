# policy

The policy service's Cargo workspace (ADR-0130, ADR-0137, ADR-0138,
ADR-0140). It decides over its own stores and authenticates its callers,
and an owner writes its entities over `ApplyEntityChanges`. The local
stack runs its image under the `policy` profile, and nothing calls it
yet (#1689).

- `proto/garam/policy/v1/` — the gRPC contract in AuthZEN's shape.
- `schema/<service>.cedarschema` — a service's manifest (ADR-0143):
  one namespace, `Iam` or `Cowork`, annotated with `@service` on the
  namespace and `@grn` and `@caller` on each entity type. Together they
  are the entity types, actions and attributes a decision can name, and
  a type or an action is named by its qualified form,
  `Iam::Org` and `iam:org-read`. `make -C policy gen` writes
  `lib/grn/catalogue_gen.go` from them, and `make -C policy check-gen`
  fails, naming what disagrees, when the committed catalogue is stale,
  a manifest breaks a rule every manifest holds, an `@caller` is
  neither `none` nor a `Caller`, a `Caller` is named by no manifest, or
  an audience of the platform names no manifest service and is neither
  `policy` nor `codehub`.
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

## Callers

- **A caller is a service this list names, read from its token's
  `sub`.** Any other `sub` is refused with `PERMISSION_DENIED`, however
  well its token verifies. `Caller` in `crates/server/src/caller.rs` is
  that list. Which types a caller writes is read from the manifests'
  `@caller` annotations, and a test holds each to that list: every
  entity type names a `Caller`'s `sub`, or `none`.

| `sub` | Writes | Asks |
|-------|--------|------|
| `platform` | the types whose `@caller` is `platform`: `Iam::Org`, `Iam::OrgMembers`, `Iam::Team`, `Iam::Project`, `Iam::User`, `Iam::Agent`, `Iam::Operator` | anything |

- **No caller writes the rest.** `@caller("none")` holds
  `Cowork::Channel`, `Cowork::Page`, `Cowork::Task` and
  `Cowork::Artifact`, which wait for
  cowork as a caller, and `Iam::Policy`, `Iam::Supervisor` and
  `Iam::Service`.
- **An entity type and an action are written qualified.** A type is
  `Iam::Org`, and an action `iam:org-read`, which is
  `Iam::Action::"org-read"`. The unqualified form is refused. The
  platform's feed sends `Iam::` types from the migration that qualified
  the outbox, but the platform's role table and its calls still write
  `<type>:<verb>`, so nothing it asks of this service yet names an
  action this service accepts (ADR-0143 item 4, not built).
- **A type the caller does not own refuses the whole
  `ApplyEntityChanges`.** Any other fault refuses that entry, named by
  its index in the response, and the rest are written. A request holds
  at most 1000 changes, an id holds at most 512 bytes, a type 64 and a
  change 1000 parents.
- **Only the type of the entity written is checked** against the
  caller, not the types of its `org`, `members` and `parents`. Slice 5
  decides that with the `cowork` row.
- **The caller's address is `GARAM_POLICY_ADDR`,** unset until the
  platform's delivery exists. `POLICY_ADDR` is where this service
  listens.

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
