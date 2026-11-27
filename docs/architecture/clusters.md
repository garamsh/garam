# Clusters

What an organization records about a cluster it connected: a user's
own cluster, EKS or any conformant one, whose connector dials garam
and authenticates as that record
([ADR-0184](./adr/0184-a-connected-cluster-is-a-machine-identity-that-registers-once-with-a-one-time-token-and-a-key-it-generates.md)).
The record is an instance of the organization's installation of the
`garam-bot-connector` app, `grn:<org>/iam/instance/<id>`, and what it
authenticates with is every instance's, in `platform/internal/app`
([apps.md](./apps.md)), superseding ADR-0184 decisions 1, 2 and 9
([ADR-0192](./adr/0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)).
What garam declares for it, what it reports and its bots' tokens are
the bot domain's, `platform/internal/bot` ([bots.md](./bots.md)). The
wire between garam and the connector is
`garamsh/garam-bot-operator`'s interface
`interfaces/cluster-connection/v1alpha1/`, whose server garam owes and
serves from `platform/internal/machineapi` §ClusterConnection.

No package is a cluster's own: this document holds what is true of an
instance because it is a connected cluster, and the terms the
app system's registration, exchange and rotation hold for every
instance, first met here.

## Current decisions

- **A connected cluster is an instance of `garam-bot-connector`,
  `grn:<org>/iam/instance/<id>`, in one organization** (ADR-0192
  decisions 1 and 11). garam mints the identifier, sixteen hex digits
  from a cryptographically secure source, because the connector learns
  its identity at registration and is configured with none before it.
  The organization installs the app once, and each cluster is an
  instance of that installation, which `garam-bot-connector`'s
  `instances` setting of `many` admits. The record, its routes, the
  actions they are decided on and the roles that hold them are
  [apps.md](./apps.md)'s, and every instance's registration, exchange,
  rotation and revocation are what the bullets below describe for a
  cluster.
- **Every cluster garam reaches is such a record**, the one garam's
  own deployment runs on included, and garam issues no PEM for a
  cluster
  ([ADR-0185](./adr/0185-every-cluster-garam-reaches-aurora-included-connects-by-the-connector-and-its-one-time-token.md)).
- **A registration token is answered once, and stored only as a
  hash** (ADR-0184 decision 3). It is 256 bits from a cryptographically
  secure source behind `garam_crt_`, answered by `createInstance` and
  by `reissueInstanceRegistrationToken`, under `Cache-Control:
  no-store`. A record holds one at a time: minting another replaces
  it. It is refused an hour after it was minted (REG-5), and it is
  spent when it is presented, however the registration fares (REG-3):
  the store deletes its row before the key is read. An expired hash
  is deleted on the next mint or spend of any instance's token, so with
  no traffic one lingers, granting nothing. A record deleted between
  the spend and the bind is refused as an unknown token is.
- **Registration is throttled per source address before the token is
  read** (REG-8): `GARAM_INSTANCE_REGISTRATION_LIMIT` a minute, ten
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
  record's live keys (AUTH-1). The exchange picks the instance's
  profile by `client_id`'s GRN type, and an instance's token names no
  audience.
  A token is `garam_ct_` and 256 bits, valid for an hour, stored only
  as a hash, and read on every request joined to the record and the
  key it was exchanged under, so a deleted record or a withdrawn key
  refuses it on the next request (AUTH-2). The listener's token
  resolution is `platform/internal/cli/machine_token.go`
  §identityTokens.
- **A cluster's token reaches the cluster-connection routes alone**
  (AUTH-4): `machineapi.ConfineApps` refuses an instance's token on
  every route of `platform/api/machine.yaml` its app's machine
  operations do not list, and garam-bot-connector lists none of them.
  The declarations and reports routes refuse with 401 a token of an
  instance whose app does not list them, and any other caller's; the
  registration and the rotation are the app system's own, which every
  instance reaches. The bot-token mint AUTH-4 also names is the bot
  domain's exchange ([bots.md](./bots.md)).
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
  (ADR-0184 decision 7): `revokeInstanceKey` withdraws one key and its
  tokens, a registration withdraws every key held before, and deleting
  the record, or the installation it is an instance of, cascades to its
  keys and tokens, and to what the bot domain keeps for it
  ([bots.md](./bots.md)). `listInstanceKeys` answers each key held,
  with its state.
- **`lastContactAt` is the time a record's token last resolved**, to
  within a second (ADR-0188 decision 4). The query that resolves an
  instance's token, in `platform/internal/app/postgres`, records it in
  `instance_contacts` and skips the write while the kept time is under
  a second old, so a busy record writes once a second.
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
- Why `lastContactAt` is written at most once a second: ADR-0188.
- Why the record is an app's instance: ADR-0192 §Context.

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
- `garam_machine_token_exchanges_total` counts an instance's exchange
  under the method `operator_private_key_jwt`, since the method is
  read off the route and an instance's exchange shares the operator's.
  Telling them apart needs the label read off `client_id`, which the
  counter does not parse.
