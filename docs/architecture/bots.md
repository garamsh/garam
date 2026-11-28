# Bots

garam's bot domain: what garam declares for an app's instances to
apply, what they report back, and the tokens a connector exchanges
for the bots placed on its instance, each keyed by the instance's GRN
([ADR-0192](./adr/0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)
decision 10). It is `platform/internal/bot`, which persists through
`platform/internal/bot/postgres`. The instance itself, its
registration, its keys and its last contact are the app system's
([apps.md](./apps.md)); a connected cluster is one such instance
([clusters.md](./clusters.md)). The wire between garam and the
connector is `garamsh/garam-bot-operator`'s interface
`interfaces/cluster-connection/v1alpha1/`, served from
`platform/internal/machineapi` §ClusterConnection.

| Type | Responsibility |
|------|----------------|
| `Service` | holds an instance's declarations poll until a change, a wake or the wait, keeps its report, mints a declared bot's token for its connector, and resolves one presented to a route |
| `BotTokenStore` | records a bot token for a live `Bot` declaration of the instance, under the key its connector's token rests on and bound to the key of the bot's process, and resolves a live one with that key |
| `DeclaredObject` | an object DECL-7 to DECL-9 admit, kept to DECL-7's members, with the kind, namespace and name two live declarations never share |
| `Declaration` | one declaration placed on an instance: its identifier, generation and object, or none once tombstoned |
| `DeclarationStore` | places, tombstones, reads and lists an instance's declarations, and answers a page of its log after a cursor; `Declare` checks a write before it reaches the store |
| `Wakes` | the polls a process holds, by instance, which a write wakes |
| `Report` | a connector's report as garam keeps it: the members the interface names, every `spec` and `status` kept as sent and never read |
| `ReportStore` | which identifiers an instance's declarations hold, and the instance's kept report, replaced only by one observed later |
| `KeptReport` | what garam holds of an instance's reports: its latest, absent where there is none |

## Current decisions

- **Placing a declaration is `iam:bot.place` on the target instance**,
  held by the owner and the administrator (ADR-0192 decision 10,
  superseding ADR-0187 decision 1), and reading the declarations and
  the report is `iam:instance.read` on the installation.
  `platform/internal/org/role.go` §machineActions and
  `policy/schema/iam.cedarschema` are authoritative for the grants.
- **`iam:cluster.declare` is withdrawn without a mapping.** No action
  takes its place for a grant an organization wrote, so policy
  migration `policy/migrations/000004_cluster_declare_withdrawn.up.sql`
  deletes every stored permit any version of which names it, one
  naming other actions beside it included: a version is never
  rewritten, and deleting a permit only takes grants away. Each
  removal is recorded in the organization's audit as an
  `iam:policy.delete` the service made. A grant of it to someone
  outside the owner and the administrator lapses with it, and its
  organization writes the grant again as `iam:bot.place`. A guardrail
  naming it stays: a forbid of an action no manifest declares decides
  nothing, and deleting one naming another action beside it would
  lift that action's deny.
- **Only an instance whose installation hosts bots is a target or an
  actor** (ADR-0192 decision 10). Each declaration write asks the
  policy service `iam:bot.host` with the instance's installation as
  principal and as resource (`bot.Hosts`), past the service's cache,
  and a refusal is answered 403. The bot-token exchange asks the same
  of the actor's installation, and a refusal is answered 400
  `invalid_request`. An installation whose app does not ask to host
  bots is refused at both, whatever a permit grants. Reads, the long
  poll and the report are not gated. The owner grants it
  ([apps.md](./apps.md) §Current decisions, bot hosting).
- **A withdrawal of bot hosting stops what comes next, not what is
  placed.** It deletes the grant and then every bot token minted for
  an instance of the installation, so a token minted before it is held
  by nothing after it. The grant is the policy service's and the
  tokens this database's, so the two are not one transaction. The
  declarations already placed stay until the organization tombstones
  them: the connector goes on reading them on its long poll and
  applying them, and its reports are kept, while a write placing or
  changing one is answered 403 and an exchange for a bot's token 400
  `invalid_request`.
- **An organization places declarations on an instance, and its
  connector long-polls them**
  ([ADR-0187](./adr/0187-a-declaration-placed-on-a-cluster-is-written-under-iam-cluster-declare-into-a-log-per-record-the-connector-long-polls.md)).
  The browser routes are `listInstanceDeclarations`,
  `createInstanceDeclaration` (garam draws the identifier),
  `getInstanceDeclaration`, `putInstanceDeclaration` (a new identifier,
  or the next generation of a live one) and
  `deleteInstanceDeclaration`, which tombstones, each under the
  instance's path. `platform/api/openapi.yaml` §instance is
  authoritative for them. Each write is the next generation, and a
  tombstone is a delete at the next generation carrying the identifier
  alone. Deleting the instance, or the installation it is an instance
  of, cascades to its declarations and their log, all but the
  identifiers.
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
  `bot.ParseDeclaredObject` is the check. Two live declarations of an
  instance never name one object, and the index
  `bot_declarations_live_object_idx` holds that too (created by
  `platform/migrations/000085_cluster_declarations.up.sql` and renamed
  by `000091_bot_domain.up.sql`). A declaration of a kind, namespace
  and name the instance's kept report shows held by a cluster-owned
  object is refused 409, whether it is new or a next
  generation moved onto that name. That refusal is a courtesy, read in
  the write's transaction: the connector's `conflict` is what holds
  ([ADR-0188](./adr/0188-garam-keeps-a-connected-clusters-latest-report-as-untrusted-display-data-and-its-last-contact-from-its-token.md)
  decision 6).
- **An identifier names one declaration, once, across every
  instance.** It is 1 to 128 letters, digits, `.`, `_` or `-`, and one
  garam draws is `decl-` and sixteen hex digits. A tombstoned
  declaration's identifier is refused 409, as is one placed on another
  instance, and `bot_declaration_ids` keeps each one after its
  instance and its organization are gone.
- **Each instance keeps a log of its changes, at positions drawn while
  the write holds the instance's row**, so they commit in order. The
  long poll is `watchDeclarations` on the machine listener:
  - with no cursor it answers the full set of live declarations as
    applies, paged, then the log cursor at the position the set began
    at;
  - after a cursor it answers the changes in write order, a hundred
    to a page with `more` where more remain;
  - an empty page is held up to `wait` seconds, fifty unless asked,
    and a full set is answered at once.

  Cursors are `c.<n>` and `f.<base>.<sent>`; `bot.ParseCursor` is
  authoritative for them. Each instance's polls are bounded, 300 a
  minute, and past it answered 429 `throttled`. An instance deleted
  while a poll is held is answered 401.
- **A change is kept 30 days** (DECL-6). Each write purges its
  instance's older ones and moves the purged position to the last
  deleted, and a cursor at or before that position is answered 410
  `cursor_expired`. A full set is never purged.
- **A write wakes the polls held on its instance on every `serve
  machine` process** through Postgres, as a watch crosses processes
  ([ADR-0027](./adr/0027-a-watch-crosses-processes-through-postgres.md)).
  The write's transaction notifies `garam_bot_declarations` with the
  instance's GRN. `botpg.ListenDeclarations` holds one connection per
  process, and a held poll reads again every five seconds besides, so
  a notification lost to a reconnect costs at most that long.
- **An instance keeps its latest report, as untrusted display data**
  (ADR-0188). `report` on the machine listener answers 204, and
  replaces the kept report only with one observed later. Before it is
  kept, garam drops:
  - each outcome and object naming an identifier no declaration of
    the instance holds, live or tombstoned;
  - each garam-owned object whose marker names another instance.

  A Secret, a ConfigMap or any kind not declared is refused 400
  `invalid_request`, and a body over 4 MiB 413 `report_too_large`, the
  one body the listener bounds past its 1 MiB. Each instance's reports
  are bounded, 120 a minute. Every `spec`, `status` and `message` is
  kept as sent and never read. `getInstanceReport` answers the kept
  report under `iam:instance.read`, for a console to render as text,
  with the instance's last contact read from the app system. The two
  are separate reads of two stores, not one snapshot, so a report and
  a contact written between them can be answered from different
  moments; ADR-0188 makes both display data.
  `platform/migrations/000091_bot_domain.up.sql` holds the rows'
  current names.
- **A placed bot's token is minted by an RFC 8693 exchange** at the
  machine listener's token endpoint, with the connector's token as the
  actor, for a bot a live `Bot` declaration places on the actor's
  instance (#2049). ADR-0189's "garam's audience" is that endpoint's
  URL. Each instance's exchanges are bounded, and a withdrawn instance
  key takes the bot tokens minted under it, through
  `platform/migrations/000091_bot_domain.up.sql`
  §instance_key_withdrawn
  ([ADR-0189](./adr/0189-a-bot-garam-declares-is-an-iam-principal-whose-connector-exchanges-for-its-token-and-one-cedar-policy-decides-what-it-may-do.md)
  decision 2, [machine-identity.md](./machine-identity.md) §A declared
  bot's token, [agents.md](./agents.md) §A bot garam declares).
- **A bot's token is bound to its process's key.** The exchange's
  transport verifies the DPoP proof the bot's process signed and hands
  the domain its key's thumbprint, which `MintBotToken` refuses to be
  without and keeps as the token's `cnf.jkt`
  ([ADR-0191](./adr/0191-every-token-garam-issues-to-a-bots-process-is-bound-to-that-processs-key-by-dpop.md),
  [domains.md](./domains.md) §A token bound to its holder's key).

## Rationale

- Why placing a bot is the bot domain's act, decided on the instance,
  and only on an instance whose installation hosts bots: ADR-0192
  decision 10 and §Context. Why `iam:bot.host` lists the installation
  as its principal though no app may request it: ADR-0193.
- Why placing a declaration is an action of its own rather than
  `iam:instance.update`, why an identifier is never placed twice, and
  why the wake goes through Postgres: ADR-0187.
- Why one report per instance, and why a tombstoned declaration's
  identifier still counts as placed: ADR-0188.
- Why a bot is a principal of its own and its token is exchanged by
  the connector: ADR-0189.

## Open questions

- The console pages for an instance's declarations and report
  (#2082).
