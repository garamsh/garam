# policy

The policy service's Cargo workspace (ADR-0130, ADR-0137, ADR-0138,
ADR-0140). It decides over its own stores and authenticates its callers,
and an owner writes its entities over `ApplyEntityChanges`. The local
stack runs its image under the `policy` profile, and nothing calls it
yet (#1689).

- `proto/garam/policy/v1/` — the gRPC contract in AuthZEN's shape.
- `schema/<service>.cedarschema` — a service's manifest (ADR-0143):
  one namespace, `Iam`, `Cowork` or `Codehub`, annotated with
  `@service` on the namespace and `@grn` and `@caller` on each entity
  type. Together they
  are the entity types, actions and attributes a decision can name, and
  a type or an action is named by its qualified form,
  `Iam::Org` and `iam:org.read`. `make -C policy gen` writes
  `lib/grn/catalogue_gen.go` from them, and `make -C policy check-gen`
  fails, naming what disagrees, when the committed catalogue is stale,
  a manifest breaks a rule every manifest holds, an `@caller` is
  neither `none` nor a `Caller`, a `Caller` is named by no manifest, or
  an audience of the platform names no manifest service and is not
  `policy`.
- `system-policies/` — the system tier: tenant isolation and its one
  exception, an agent's sending, a hosted operator's ten delegated
  operations (`operator-<type>-<verb>`), and the role templates. One
  policy per file, and the file's
  stem is the id a decision's reasons name. Their tests sit in
  `crates/decision/tests/`.
- `migrations/` — the service's own database.
- `crates/decision/` — decides a request against the system tier, an
  organization's policies and the entities they read, and declares the
  ports it reads them through.
- `crates/grant/` — role links, guardrails and `permit`s:
  schema-validated writes, append-only versions, and the reads of a
  policy and of an organization's policies.
- `crates/audit/` — the record of every policy write and every denial
  (ADR-0148): a write is recorded in the transaction that makes it, a
  denial collapses with its hour's identical ones and an organization
  keeps at most 10,000 live ones, and a denial is removed 90 days after
  it was last made, by an hourly sweep and by the read. `grant` records
  its writes through it, and `ListAuditRecords` reads them for the
  platform on `iam:audit.read`.
- `crates/entity/` — the containment and membership decisions read, and
  the sequenced write owners reach it by.
- `crates/access-report/` — the policies naming a subject or a resource.
- `crates/server/` — the binary, `policy`. `policy serve` serves the
  contract and AuthZEN's HTTP binding, authenticates callers and joins
  the adapters to the ports,
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
  entity type names a `Caller`'s name, or `none`.
- **A `sub` names a caller in one of two forms.** The platform is the
  bare `platform`. Any other caller is a service principal's GRN,
  `grn:<org>/iam/service/<name>`, where `<name>` is the caller's name
  in the table and `<org>` a slug other than `root`: the caller is then
  bound to that organization. The bare name of a bound caller, a
  principal named `platform`, a GRN of another service or type, and a
  name outside the list are each an unknown caller.

| `sub` | Writes | Asks |
|-------|--------|------|
| `platform` | the types whose `@caller` is `platform`: `Iam::Org`, `Iam::OrgMembers`, `Iam::Team`, `Iam::Project`, `Iam::User`, `Iam::Agent`, `Iam::Operator` | anything |
| `grn:<org>/iam/service/cowork` | the types whose `@caller` is `cowork`: `Cowork::Channel`, `Cowork::Page`, `Cowork::Task`, `Cowork::Artifact`, into `<org>` alone | `cowork:` actions on resources of `<org>` |
| `grn:<org>/iam/service/codehub` | the types whose `@caller` is `codehub`: `Codehub::Repo`, into `<org>` alone | `codehub:` actions on resources of `<org>` |

- **No caller writes the rest.** `@caller("none")` holds `Iam::Policy`,
  `Iam::Supervisor` and `Iam::Service`.
- **A caller bound to an organization writes only into it.** Every
  entity of its `ApplyEntityChanges` is of a type it owns, has a GRN
  beginning `grn:<org>/`, names `<org>`'s own `Iam::Org` as its `org`,
  and has `members` and parents of `<org>`: an `Iam::Org` is of the
  organization its id names, anything else by the organization its GRN
  names. A change that breaks one refuses the whole request with
  `PERMISSION_DENIED`, as a type the caller does not own does.
- **A caller bound to an organization asks only for its own.** The
  action of an `Evaluate` or of an entry of `Evaluations` is of its
  service (`cowork:...`) and the resource is of its organization;
  anything else fails the call with `PERMISSION_DENIED`. The subject is
  not restricted: tenant isolation decides it (ADR-0137).
- **An entity type and an action are written qualified.** A type is
  `Iam::Org`, and an action `iam:org.read`, which is
  `Iam::Action::"org.read"`. The unqualified form is refused. An
  action id is `<type>.<verb>`, and a `<type>.*` group holds exactly
  that type's actions, so `iam:agent.*` does not reach
  `iam:agent-template.read` (ADR-0145). The platform's role table and
  its calls write the dot form, the names the manifests declare
  (ADR-0143 item 4, ADR-0145).
- **The platform alone writes a policy, over gRPC.** `CreatePolicy`,
  `AppendPolicyVersion`, `ActivatePolicy`, `RollBackPolicy`,
  `DeletePolicy`, `GetPolicy` and `ListPolicies` refuse a scoped
  caller whole. A request names its writer, and the service evaluates
  the writer's action on the organization before it stores. A writer
  changes a role link only where it holds every role any version of it
  links, so a policy that ever linked an owner is an owner's to
  change. A writer lacking `iam:policy.read` is refused a write the
  same way whether or not the policy exists. Activation takes the
  version the service checked and is refused if the default has moved.
  With no writer, only a role link written or removed from a
  membership is accepted, and the identifiers opening `link-owner-`,
  `link-admin-` or `link-member-` are that write's alone: a writer
  changes nothing under one, and the platform makes effective or
  removes only a policy every version of which is that member's link
  (ADR-0161).
- **A `permit` never reaches an operator.** A decision for an
  `Iam::Operator` composes the organization's guardrails and none of
  its `permit`s, whatever a `permit` says.
- **Removing a `permit` asks `iam:policy.delete`, not
  `iam:permit.write`.** Removal narrows, and an administrator can
  already forbid what a `permit` allows with a guardrail.
- **A write is two steps.** The service evaluates the writer, then
  stores. A writer whose role ended between them completes one write.
- **An organization's `permit` is written by an owner and scoped to
  its organization.** Writing one is `iam:permit.write`, held by the
  `owner` role alone. A `PermitRight` is the allow of that evaluation
  for one organization and one writer, and a store given one made on
  another organization refuses it (ADR-0144 item 7, ADR-0145 item 4).
- **A type the caller does not own refuses the whole
  `ApplyEntityChanges`.** Any other fault refuses that entry, named by
  its index in the response, and the rest are written. A request holds
  at most 1000 changes, an id holds at most 512 bytes, a type 64 and a
  change 1000 parents.
- **Two types carry one attribute each.** An `EntityUpsert` of an
  `Iam::Org` may name `consents`, the organizations it has granted its
  half of a message toward (ADR-0147), and one of an `Iam::Operator`
  may name `operations`, the actions its current delegation holds as
  wire-form names, `iam:agent.activate` (ADR-0146). An entity of another
  type carrying either, and an operation no manifest declares with
  `Iam::Operator` as a principal, refuse that entry, and each holds at
  most 1000. `Iam::Org` and `Iam::Operator` are the platform's, so a
  scoped caller writing either refuses its whole request, and neither
  attribute can be written by a service that is not the platform. An
  operator is written in its organization's `Iam::OrgMembers` and
  `Iam::Org`, as a user is, and the platform's feed does not yet write
  that parent or either attribute.
- **Tenant isolation has one exception, a message both sides consent
  to.** A `message.send` from an agent to an agent lifts the
  isolation `forbid` when each of their organizations lists the other
  in `consents`, and no other action, principal or resource type does.
  The send still needs the `permit` of `agent-message-send`, and the
  engine also reads the sender organization's guardrails (its `forbid`s
  alone), named `<org>/<id>` so that two organizations' ids never
  collide.
- **A hosted operator holds what its `operations` list names.** One
  system policy per delegable operation permits an `Iam::Operator`
  of the resource's organization whose `operations` holds it, and a
  guardrail forbids it as it forbids any principal. `Iam::Operator` is
  a principal of those ten actions and of no other.
- **The platform's writes are checked by type alone.** The types of
  its `org`, `members` and `parents` are not judged against it, since
  it writes every organization.
- **The caller's address is `GARAM_POLICY_ADDR`,** unset until the
  platform's delivery exists. `POLICY_ADDR` is where this service
  listens.

## Transport

- **The contract and its HTTP binding share one listener,
  `POLICY_ADDR`.** The TLS handshake offers `h2` and `http/1.1`, so a
  gRPC client and a client with no HTTP/2 reach the same port under the
  same certificate, and no setting is added for either.
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

## AuthZEN binding

The service serves OpenID AuthZEN Authorization API 1.0 beside its gRPC
contract, as HTTP/1.1 and JSON on the same listener (sections 5, 6, 7,
9 and 10 of the specification). The bearer token is verified, and the
caller bound, as `Callers` states for gRPC, and both transports reach
one implementation of the decision and of the entity write.

| Route | Does |
|-------|------|
| `POST /access/v1/evaluation` | `Evaluate` |
| `POST /access/v1/evaluations` | `Evaluations` |
| `GET /.well-known/authzen-configuration` | names the two endpoints above; asks for no token |
| `POST /garam/policy/v1/entity-changes` | `ApplyEntityChanges` |

- **Bodies are the specification's.** A request carries `subject`,
  `action`, `resource` and `context`, and the answer is
  `{"decision": bool}` with the gRPC reasons under
  `context.reasons`, each a `policy_id` or an `error`. Fields the
  binding does not know are ignored. A `subject` or an `action`
  carrying `properties` is a malformed body, since a decision reads
  those grounds for itself.
- **The type `grn` stands for the id's own type** (ADR-0149 item 2),
  for a subject, a resource and the entities of an entity feed. The
  entity type is the one whose `@grn` is the id's last type segment,
  in the manifest of the id's service, so
  `grn:acme/codehub/repo/4821` is `Codehub::Repo`. A Cedar type name
  is accepted as before, `Iam::OrgMembers` having no `@grn` and so
  being named by its Cedar type alone. An id no manifest declares, a
  string that is no GRN, or one with a type segment without its id
  is a 400, and refuses the whole request, as a malformed body does.
  A `#locator` is dropped to find the type and the id is passed on
  unchanged. gRPC has no such type: its `type` is always a Cedar type
  name.
- **A context is what the action's schema declares.** `context` of an
  evaluation, over gRPC as over HTTP, reaches the decision, and the
  schema refuses a key or a type the action does not declare: a
  single evaluation fails `INVALID_ARGUMENT` (400), and an entry of a
  batch is denied with the fault as its reason. `codehub:repo.read`
  and `repo.write` declare `path`, a string, and a question about the
  whole repository omits it, so a rule on it names `context has path`
  first. A resource carrying `properties` is still refused.
- **Evaluations are decided one by one, in order.** Top-level
  `subject`, `action`, `resource` and `context` are the defaults of an
  entry that omits them, and entries that share a resource type and id
  are never merged. Only the `execute_all` semantic is served, named
  or by default, and any other `evaluations_semantic` is a 400.
- **The entity-changes body is the JSON form of
  `ApplyEntityChangesRequest`,** and its answer that of the response,
  under the same bounds, the same per-entry faults and the same caller
  checks. A change holds an `upsert` or a `delete`, not both, and a
  `sequence` is a string or a number.
- **The metadata names the endpoints under the host the client
  reached.** A request that names no host is a 400.
- **`X-Request-ID` is echoed where it is sent,** on a refusal as on an
  answer.
- **A refusal is the gRPC status's message, in plain text, under the
  status this table gives it.** A denial is no refusal: it is a 200
  with `decision: false`.

| gRPC status | HTTP status | When |
|-------------|-------------|------|
| `UNAUTHENTICATED` | 401 | no token, or a token that does not verify |
| `PERMISSION_DENIED` | 403 | a `sub` the service does not serve, a scoped caller asking or writing outside what it is bound to, a type it does not own |
| `INVALID_ARGUMENT` | 400 | a malformed body, a missing field, a GRN no manifest declares, a context the schema refuses, a request over its bound, an unserved semantic |
| `UNAVAILABLE` | 503 | a store that cannot be read or written |
| any other | 500 | the service could not decide |

The token is checked before the body is read, so a request with neither
is a 401.

## Analysis

`crates/decision/tests/analysis.rs` proves properties of the system
tier with `cedar-policy-symcc` and cvc5, for every request and entity
store the schema admits (ADR-0130 §5). `policy/Makefile` pins the
solver and fetches it under `target/`. They hold that:

- no system policy and no link can error on a request the schema
  admits;
- with every link and a `permit` on everything, a request reaches a
  resource only for a member of its organization, or as an agent's
  `message.send` to an agent of an organization that consents to each
  other, and a second proof names that send as the only request
  members alone do not cover;
- the tier alone allows an agent's sending and an operator's ten
  delegable operations, and denies every other request.

`make -C policy ci` is the fast gate, and `make -C policy stack-up
suites` runs the store tests against Postgres and the analysis.
`docs/convention/stack-rust.md` governs the workspace.
