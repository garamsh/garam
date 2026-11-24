# Clusters

What an organization records about a cluster it connected: a user's
own cluster, EKS or any conformant one, whose connector dials garam
and authenticates as that record
([ADR-0184](./adr/0184-a-connected-cluster-is-a-machine-identity-that-registers-once-with-a-one-time-token-and-a-key-it-generates.md)).
Implemented by `platform/internal/cluster`, which persists through
`platform/internal/cluster/postgres`. The wire between garam and the
connector is `garamsh/garam-bot-operator`'s interface
`interfaces/cluster-connection/v1alpha1/`, whose server garam owes and
serves from `platform/internal/machineapi` §ClusterConnection.

| Type | Responsibility |
|------|----------------|
| `Cluster` | an organization's record of a connected cluster under a GRN garam minted: what the organization calls it and says it is for, who created it and when |
| `DetailsPatch` | a parsed name and description, each absent where the caller said nothing: what a creation starts with and what an update changes |
| `Store` | creates one, answers one by GRN, lists an organization's own, changes its name and description, and deletes one |
| `PublicKey` | the public half of a connector's RSA key, parsed from the JWK it sent, with its RFC 7638 thumbprint as `kid` |
| `Key` | a key the record holds, live or withdrawn, and the row an exchange or a rotation verified with |
| `CredentialStore` | what a record authenticates with: the hash of its registration token, its keys, the hashes of its tokens and the jtis its signed requests spent |
| `Service` | registers a connector, exchanges its assertion for a token, and rotates its key |

## Current decisions

- **A connected cluster is `grn:<org>/iam/cluster/<id>`, in one
  organization** (ADR-0184 decisions 1 and 2). garam mints the
  identifier, sixteen hex digits from a cryptographically secure
  source, because the connector learns its identity at registration
  and is configured with none before it. The ownership is held twice:
  `cluster.GRNOf` derives the GRN, and
  `platform/migrations/000082_clusters.up.sql` §clusters_grn_ownership
  refuses a row whose GRN names another organization or another type.
- **Every cluster garam reaches is such a record**, the one garam's
  own deployment runs on included, and garam issues no PEM for a
  cluster
  ([ADR-0185](./adr/0185-every-cluster-garam-reaches-aurora-included-connects-by-the-connector-and-its-one-time-token.md)).
- **The record carries what ADR-0184 decision 8 lists of it and
  nothing else**: the GRN, the organization, the name, the
  description, who created it and when. A name is not unique and not
  an identity, and one left out of a creation is the identifier. The
  bounds are `platform/api/openapi.yaml` §ClusterName and
  §ClusterDescription, which is authoritative for them, and 000082
  holds the same ones. The record follows its organization out.
- **The surface is the browser-facing listener, on the organization's
  own paths**: `createCluster`, `listClusters`, `getCluster`,
  `updateCluster` and `deleteCluster`. `platform/api/openapi.yaml`
  §cluster is authoritative for them.
- **Every cluster route is decided on the organization's GRN**, each
  on an action of its own (ADR-0184 decision 9): `iam:cluster.read`
  for the list and a read, `iam:cluster.register` to create,
  `iam:cluster.update` to rename and describe, `iam:cluster.delete`
  to delete, `iam:cluster.reissue` to answer a new registration token
  and to revoke a key. Listing the keys takes `iam:cluster.read`.
- **Which roles hold each**, decided where it was built (#2045): every
  role holds `iam:cluster.read`; the owner and the administrator hold
  `iam:cluster.register`, `.update`, `.reissue` and `.delete`. Each is
  named and none is granted as `iam:cluster.*`, for the operator's
  reason: a wildcard grants in advance an action nobody has decided.
  `platform/internal/org/role.go` §machineActions and
  `policy/schema/iam.cedarschema` are authoritative for the grants,
  and a test holds the two together.
- **`Iam::Cluster` is declared with `@caller("none")`**, so the GRN
  type exists in `lib/grn`'s catalogue and nothing feeds a cluster to
  the policy service: no decision names one while every route is
  decided on its organization.
- **A registration token is answered once, and stored only as a
  hash** (ADR-0184 decision 3). It is 256 bits from a cryptographically
  secure source behind `garam_crt_`, answered by `createCluster` and
  by `reissueClusterRegistrationToken`, under `Cache-Control:
  no-store`. A record holds one at a time: minting another replaces
  it. It is refused an hour after it was minted (REG-5), and it is
  spent when it is presented, however the registration fares (REG-3):
  the store deletes its row before the key is read.
- **Registration is throttled per source address before the token is
  read** (REG-8): `GARAM_CLUSTER_REGISTRATION_LIMIT` a minute, ten
  unless set, past which a request is answered 429 and spends
  nothing. The binary refuses `0`, so no deployment runs without the
  bound, and a conformance run raises it
  (`deploy/charts/garam/README.md` is authoritative for the value).
  A listener with no token endpoint configured serves no
  registration, since it would have no `tokenUrl` to answer.
- **A registration binds the public half of an RSA key of 3072 to 4096
  bits** (REG-1, REG-2). A JWK naming a private member (`d`, `p`, `q`,
  `dp`, `dq`, `qi`, `oth`), another algorithm or another use is
  refused. The key becomes the record's only live key: every key held
  before is withdrawn, with its tokens (REG-6).
- **The connector's assertion is exchanged on `/oauth/token`**, under
  the operator's `private_key_jwt` profile, verified against the
  record's live keys (AUTH-1). The exchange picks the cluster's profile
  by `client_id`'s GRN type, and a cluster's token names no audience.
  A token is `garam_ct_` and 256 bits, valid for an hour, stored only
  as a hash, and read on every request joined to the record and the
  key it was exchanged under, so a deleted record or a withdrawn key
  refuses it on the next request (AUTH-2). The listener's token
  resolution is `platform/internal/cli/machine_token.go`
  §identityTokens.
- **A cluster's token reaches the cluster-connection routes alone**
  (AUTH-4): `machineapi.ConfineClusters` refuses it on every route of
  `platform/api/machine.yaml`, and the cluster-connection routes refuse
  any other caller's token with 401. The bot-token mint AUTH-4 also
  names is #2049's.
- **A rotation needs the current key's authorization and the
  successor's proof** (ROT-1, ROT-2), verified before the store is
  asked, so the bearer token alone rotates nothing. Both JWSs' jtis
  are spent in the transaction that adds the key. The predecessor
  retires 24 hours after the rotation, and both are live until then
  (ROT-3). A record holds two live keys at most: a third is refused
  409 `too_many_keys`. A rotation to a key a rotation made live, while
  it is live, is answered as the first was, from the key it succeeded,
  which the key's row names. A rotation to any other key the record
  has held is refused 400 `invalid_request`, the key registration
  bound included (ROT-4).
- **An organization withdraws a key, re-registers or deletes the
  record**, and each takes effect on the connector's next request
  (ADR-0184 decision 7): `revokeClusterKey` withdraws one key and its
  tokens, a registration withdraws every key held before, and deleting
  the record cascades to its keys and tokens. `listClusterKeys`
  answers each key held, with its state.
- **The wire is the interface's own specification, vendored**
  ([ADR-0186](./adr/0186-the-cluster-connection-routes-are-served-from-the-interfaces-own-specification-vendored-at-a-pinned-commit.md)).
  `platform/api/cluster-connection/v1alpha1/openapi.yaml` is
  authoritative for the shapes, and garam-bot-operator's `README.md`
  beside the original for the behaviour. The pin is
  `CLUSTER_CONNECTION_PIN` in `platform/Makefile`. A route the
  interface defines and garam has not built answers 501 with the code
  `internal`, which the connector backs off from (T-6).

## Rationale

- Why a cluster is its own machine identity and not an operator, and
  why its key never leaves the cluster: ADR-0184. Why aurora's cluster
  takes the same path: the owner's direction recorded in ADR-0185.
- Why the token is spent before the key is read, and the throttle
  answers before the token is: a token presented is its one attempt,
  and a 429 that spent one would cost the connector its registration
  for a limit it could not see (REG-3, REG-8).
- Why the wire is vendored rather than restated in
  `platform/api/machine.yaml`: a second copy of the interface's contract
  would drift from the one its conformance suite tests (ADR-0186).
- Why every route is decided on the organization and the roles mirror
  the operator's: standing a machine identity up or taking it away is
  the administrative act registering and deleting an operator already
  are (ADR-0045, ADR-0070), and the PM settled the fork that way on
  #2045.

## Open questions

- Not yet built, each its own issue: the server conformance suite in
  CI (#2047), which waits for garam-bot-operator#439 to build it;
  declarations placed on a cluster (#2048); the bot-token mint for a
  cluster (#2049); reports (#2050). The fixture the suite will run is
  built for what the server already serves,
  `platform/tests/clusterfixture` (its `README.md` is the contract),
  and each issue adds its own subcommands.
- `garam_machine_token_exchanges_total` counts a cluster's exchange
  under the method `operator_private_key_jwt`, since the method is
  read off the route and a cluster's exchange shares the operator's.
  Telling them apart needs the label read off `client_id`, which the
  counter does not parse.
- The console pages for a cluster, built once the routes above are,
  in one issue filed after #2050.
