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
| `Service` | registers a connector, exchanges its assertion for a token, rotates its key, and holds its declarations poll until a change, a wake or the wait |
| `DeclaredObject` | an object DECL-7 to DECL-9 admit, kept to DECL-7's members, with the kind, namespace and name two live declarations never share |
| `Declaration` | one declaration placed on a record: its identifier, generation and object, or none once tombstoned |
| `DeclarationStore` | places, tombstones, reads and lists a record's declarations, and answers a page of its log after a cursor; `Declare` checks a write before it reaches the store |
| `Wakes` | the polls a process holds, by record, which a write wakes |
| `Report` | a connector's report as garam keeps it: the members the interface names, every `spec` and `status` kept as sent and never read |
| `ReportStore` | which identifiers a record's declarations hold, the record's kept report, replaced only by one observed later, and its latest contact |
| `KeptReport` | what garam holds of a record's reports: its latest contact and its latest report, each absent where there is none |

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
  on an action of its own (ADR-0184 decision 9, and ADR-0187 decision
  1 for declarations): `iam:cluster.read` for the list and a read,
  `iam:cluster.register` to create, `iam:cluster.update` to rename and
  describe, `iam:cluster.delete` to delete, `iam:cluster.reissue` to
  answer a new registration token and to revoke a key, and
  `iam:cluster.declare` to place, change and tombstone a declaration.
  Listing the keys, reading the declarations and reading the report
  take `iam:cluster.read`.
- **Which roles hold each**, decided where it was built (#2045, and
  #2048 for `.declare`): every role holds `iam:cluster.read`; the
  owner and the administrator hold `iam:cluster.register`, `.update`,
  `.reissue`, `.delete` and `.declare`. Each is
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
  the store deletes its row before the key is read. An expired hash
  is deleted on the next mint or spend of any cluster's token, so with
  no traffic one lingers, granting nothing. A record deleted between
  the spend and the bind is refused as an unknown token is.
- **Registration is throttled per source address before the token is
  read** (REG-8): `GARAM_CLUSTER_REGISTRATION_LIMIT` a minute, ten
  unless set, past which a request is answered 429 and spends
  nothing. The binary refuses `0`, so no deployment runs without the
  bound, and a conformance run raises it
  (`deploy/charts/garam/README.md` is authoritative for the value).
  A listener with no token endpoint configured serves no
  registration, since it would have no `tokenUrl` to answer.
- **A registration binds the public half of an RSA key of 3072 to 4096
  bits** (REG-1, REG-2). REG-1 sets the floor, and the cap is garam's,
  the operator's key bound (`operator.MaxKeyBits`). A JWK naming a
  private member (`d`, `p`, `q`, `dp`, `dq`, `qi`, `oth`), another
  algorithm or another use is refused. The key becomes the record's
  only live key: every key held before is withdrawn, with its tokens
  (REG-6).
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
  names is an RFC 8693 exchange at the machine listener's token
  endpoint, with the connector's token as the actor, for a bot a live
  `Bot` declaration places on the record (#2049). ADR-0189's "garam's
  audience" is that endpoint's URL, the `aud` AUTH-1's assertion
  carries
  ([ADR-0189](./adr/0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)
  decision 2, [machine-identity.md](./machine-identity.md) §A declared
  bot's token).
- **A rotation needs the current key's authorization and the
  successor's proof** (ROT-1, ROT-2), verified before the store is
  asked, so the bearer token alone rotates nothing. Each cluster's
  rotations are bounded, ten a minute, before any signature is
  verified, and past it answered 429 `throttled`. A listener whose
  token URL does not end in `/oauth/token` serves no rotation, and
  answers it 503 rather than `invalid_proof`. Both JWSs' jtis
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
  the record cascades to its keys and tokens, and to its declarations
  and their log, all but the identifiers. `listClusterKeys`
  answers each key held, with its state.
- **An organization places declarations on a record, and its
  connector long-polls them**
  ([ADR-0187](./adr/0187-a-declaration-placed-on-a-cluster-is-written-under-iam-cluster-declare-into-a-log-per-record-the-connector-long-polls.md)).
  The browser routes are `listClusterDeclarations`,
  `createClusterDeclaration` (garam draws the identifier),
  `getClusterDeclaration`, `putClusterDeclaration` (a new identifier,
  or the next generation of a live one) and `deleteClusterDeclaration`,
  which tombstones. `platform/api/openapi.yaml` §cluster is
  authoritative for them. Each write is the next generation, and a
  tombstone is a delete at the next generation carrying the identifier
  alone.
- **A declared object is refused at the write unless the connector
  would admit it**, and is stored as admitted:
  - interface 1's own parser, `manifest.Parse` of
    `github.com/garamsh/garam-bot/api` at the version `platform/go.mod`
    pins, admits it, which refuses an unknown field, an image (DECL-9)
    and a secret named other than as `<secret-name>/<key>` (DECL-8);
  - it has a namespace;
  - it is at most 64 KiB once kept;
  - DECL-7's members alone are kept, numbers as written.

  A refusal of the first two is answered 422 naming the field.
  `cluster.ParseDeclaredObject` is the check. Two live
  declarations of a record never name one object, and
  `platform/migrations/000085_cluster_declarations.up.sql`
  §cluster_declarations_live_object_idx holds that too. A declaration
  of a kind, namespace and name the record's kept report shows held by
  a cluster-owned object is refused 409, whether it is new or a next
  generation moved onto that name. That refusal is a courtesy, read in
  the write's transaction: the connector's `conflict` is what holds
  ([ADR-0188](./adr/0188-garam-keeps-a-connected-clusters-latest-report-as-untrusted-display-data-and-its-last-contact-from-its-token.md)
  decision 6).
- **An identifier names one declaration, once, across every record.**
  It is 1 to 128 letters, digits, `.`, `_` or `-`, and one garam draws
  is `decl-` and sixteen hex digits. A tombstoned declaration's
  identifier is refused 409, as is one placed on another record, and
  `cluster_declaration_ids` keeps each one after its record and its
  organization are gone.
- **Each record keeps a log of its changes, at positions drawn while
  the write holds the record's row**, so they commit in order. The
  long poll is `watchDeclarations` on the machine listener:
  - with no cursor it answers the full set of live declarations as
    applies, paged, then the log cursor at the position the set began
    at;
  - after a cursor it answers the changes in write order, a hundred
    to a page with `more` where more remain;
  - an empty page is held up to `wait` seconds, fifty unless asked,
    and a full set is answered at once.

  Cursors are `c.<n>` and `f.<base>.<sent>`; `cluster.ParseCursor` is
  authoritative for them. Each cluster's polls are bounded, 300 a
  minute, and past it answered 429 `throttled`. A record deleted while
  a poll is held is answered 401.
- **A change is kept 30 days** (DECL-6). Each write purges its
  record's older ones and moves the purged position to the last
  deleted, and a cursor at or before that position is answered 410
  `cursor_expired`. A full set is never purged.
- **A write wakes the polls held on its record on every `serve
  machine` process** through Postgres, as a watch crosses processes
  ([ADR-0027](./adr/0027-a-watch-crosses-processes-through-postgres.md)).
  The write's transaction notifies `garam_cluster_declarations` with
  the record's GRN. `clusterpg.ListenDeclarations` holds one connection
  per process, and a held poll reads again every five seconds besides,
  so a notification lost to a reconnect costs at most that long.
- **A record keeps its latest report, as untrusted display data**
  (ADR-0188). `report` on the machine listener answers 204, and
  replaces the kept report only with one observed later. Before it is
  kept, garam drops:
  - each outcome and object naming an identifier no declaration of
    the record holds, live or tombstoned;
  - each garam-owned object whose marker names another record.

  A Secret, a ConfigMap or any kind not declared is refused 400
  `invalid_request`, and a body over 4 MiB 413 `report_too_large`, the
  one body the listener bounds past its 1 MiB. Each cluster's reports
  are bounded, 120 a minute. Every `spec`, `status` and `message` is
  kept as sent and never read. `getClusterReport` answers the kept
  report under `iam:cluster.read`, for a console to render as text.
  `platform/migrations/000086_cluster_reports.up.sql` holds the rows.
- **`lastContactAt` is the time a record's token last resolved**, to
  within a second (ADR-0188 decision 4). The query that resolves a
  cluster's token records it and skips the write while the kept time
  is under a second old, so a busy record writes once a second.
- **The wire is the interface's own specification, vendored**
  ([ADR-0186](./adr/0186-the-cluster-connection-routes-are-served-from-the-interfaces-own-specification-vendored-at-a-pinned-commit.md)).
  `platform/api/cluster-connection/v1alpha1/openapi.yaml` is
  authoritative for the shapes, and garam-bot-operator's `README.md`
  beside the original for the behaviour. The pin is
  `CLUSTER_CONNECTION_PIN` in `platform/Makefile`. garam serves every
  route the interface defines.

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
- Why placing a declaration is an action of its own rather than
  `iam:cluster.update`, why an identifier is never placed twice, and
  why the wake goes through Postgres: ADR-0187.
- Why one report per record, why a tombstoned declaration's
  identifier still counts as placed, and why `lastContactAt` is
  written at most once a second: ADR-0188.
- Why every route is decided on the organization and the roles mirror
  the operator's: standing a machine identity up or taking it away is
  the administrative act registering and deleting an operator already
  are (ADR-0045, ADR-0070), and the PM settled the fork that way on
  #2045.

## Open questions

- Not yet built, each its own issue: the server conformance suite in
  CI (#2047), which waits for garam-bot-operator#439 to build it. The
  fixture the suite will run, `platform/tests/clusterfixture` (its
  `README.md` is the contract), carries every subcommand the suite
  names.
- The per-address bounds read the machine listener's TCP peer, per
  replica, so behind a balancer that rewrites the source one address
  stands for every connector
  ([deployment-contract.md](./deployment-contract.md) §Replicas,
  spreading and rollout). Reading a forwarded address waits for a
  deployment that needs it.
- A rotation from a key already retiring answers that key's earlier
  deadline, which can be under ROT-3's 24 hours; the interface's
  answer for that case is garam-bot-operator#451's.
- `garam_machine_token_exchanges_total` counts a cluster's exchange
  under the method `operator_private_key_jwt`, since the method is
  read off the route and a cluster's exchange shares the operator's.
  Telling them apart needs the label read off `client_id`, which the
  counter does not parse.
- The console pages for a cluster, built once the routes above are,
  in one issue filed after #2050.
