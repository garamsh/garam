# ADR 0192: Every system that connects to garam is an app installed in an organization, and each running copy is an instance with its own key

> Status: accepted
> Date: 2026-11-25

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2079, for which the Garam PM reserved this number. The
owner's direction is `garamsh/discussions` discussion 6, comments
19863476 and 19863532 (the app system), 19863598 (the connector
registers through it, and what differs between apps is settings) and
19863634 (registration by organizations and third parties is designed
now rather than later). The decisions #2079 lists were settled before
drafting, and its inventory comment of 2026-11-24 maps what this
changes. garam is read at `1b8f3fe5cafaccf0821c1add828eccb6ecb601df`,
and a claim below that names no revision holds for it.

## Context

- **garam grows one identity per kind of thing that connects.**
  - A connected cluster is `grn:<org>/iam/cluster/<id>`, with its own
    record, one-time registration token, keys, rotation, routes,
    actions, tables and environment variable (ADR-0184, ADR-0185,
    [clusters.md](../clusters.md)).
  - cowork and codehub each act in every organization as
    `grn:<org>/iam/service/<name>`, proving one deployment key
    (ADR-0129, [machine-identity.md](../machine-identity.md)
    §cowork's principal and §codehub's principal).
  - A PC, a naru host and a deployment target were next in line, each
    as another kind (#2079 §Why).
- **The owner's direction** (comments 19863476 and 19863532): every
  external system connected to garam is an app, registered through
  one generic system as GitHub Apps are. An app is registered, then
  installed in an organization with permissions from garam's IAM, and
  each copy of it that runs registers on site with a one-time token
  and a key it makes. garam core models no kind. Bots stay IAM
  principals and are not apps. An external service, GitHub among
  them, is used through its own app system, and the brokering of
  those (garam-bot ADR 0038) is unchanged.
- **The connector is an ordinary app** (comment 19863598), as GitHub's
  Actions Runner Controller authenticates as a GitHub App and
  registers runners through it. What differs between apps is their
  settings and permissions:
  - how many instances an installation holds, each with its own key
    and revoked alone;
  - whether events reach the app by a webhook garam pushes or by a
    feed its instances pull;
  - whether it may host bots, which an organization grants explicitly.
- **Registration is open now** (comment 19863634): an organization
  registers its own apps, and an app may be offered to other
  organizations, so a third party is an organization whose app is
  public.
- **What the record already holds that this reuses.**
  - A GRN names every resource; `root` owns what no organization owns;
    the platform's types sit under `iam` (ADR-0142,
    [grn-model.md](../grn-model.md) §Ownership).
  - One Cedar policy decides, in the policy service, over the actions
    each manifest declares (ADR-0130, ADR-0143, ADR-0145). Writing a
    `permit` is the owner's alone (ADR-0145 item 4).
  - garam is an OIDC issuer with a published key set (ADR-0141).
  - A per-record change log with positions and a held long poll
    (ADR-0187 decisions 2, 3 and 6).
- **Nothing is live to migrate for clusters.** aurora connects only at
  its end-of-build registration step, and no other cluster is
  registered (#2079 §Decisions). The service principals are live:
  migrations `000073` and `000074` wrote one per organization.

## Decision

**An app is registered once, by the platform or by an organization.
An organization installs it, and the install writes the app's
requested actions as Cedar policies on the installation. Each running
copy of it is an instance of that installation, registered with a
one-time token and a key it generated. Every difference between apps
is a setting on the app's record that every path reads. No path
branches on which app, or on an instance's kind.**

1. **Three resources, under `iam`.**
   - **An app**: `grn:root/iam/app/<slug>` when the platform registers
     it, `grn:<org>/iam/app/<slug>` when an organization does. The slug
     is unique across every app of every owner, 3 to 39 lowercase
     letters, digits and `-`, and is never reused once taken.
   - **An installation**: `grn:<org>/iam/installation/<slug>`, one app
     installed in one organization, at most one per app and
     organization, named by the app's slug so a service derives it
     from the organization with no lookup. Its record names the app's
     GRN and an incarnation, so a reinstall revives nothing an earlier
     one held.
   - **An instance**: `grn:<org>/iam/instance/<id>`, one running copy
     of an installation's app. garam mints the identifier, sixteen hex
     digits, and answers it at registration. Its record names its
     installation, which is its Cedar parent.
   - An instance carries a **kind**, a string of at most 64 characters
     the instance or the organization sets. garam stores and displays
     it and never reads it. Where a protocol names the connected
     record, the word is `instance`.
2. **What an app's record carries.** Every app's, whoever registered
   it:
   - its slug, name, description and owner;
   - its **visibility**: `private`, installable only in its owning
     organization, or `public`, installable in any. A platform app is
     public;
   - its **requested actions**: a set of actions the manifests already
     declare (decision 6), and whether it asks to host bots;
   - its **instances**: `none`, `one` or `many` per installation;
   - its **delivery**: `pull` or `webhook`, and for `webhook` an `https`
     URL;
   - its **app keys**: at most two public RSA JWKs (decision 5);
   - whether its tokens reach the browser-facing API (decision 7).

   Two settings only the platform writes, and a registered app's are
   always off: **automatic installation** in every organization with
   a fixed floor (decision 4), and **machine operations**, the list of
   operations on the machine listener beyond the app system's own that
   its tokens reach (decision 7). Who writes a setting is a rule on the
   setting, not a test of the app.
3. **Registering an app.**
   - The platform's apps are written by a migration and the
     deployment's configuration, and no route writes one. They are
     `garam-bot-connector`, `cowork` and `codehub`.
   - An organization's owner or administrator registers one under
     `iam:app.create`, decided on the organization, and changes or
     deletes it under `iam:app.update` and `iam:app.delete`, decided on
     the app. Every member reads its organization's apps and every
     public app under `iam:app.read`.
   - Making a public app private is refused 409 while another
     organization holds an installation of it.
   - Deleting an app deletes every installation of it, in every
     organization, with their policies, instances, keys and tokens.
     Every token of it is refused from the next request.
   - The deployment deletes any app by a server command, whoever
     registered it, with the same effect.
4. **Installing an app.**
   - **The install shows what it grants before it writes.** The
     console lists each requested action with the description the
     registry serves for it (`GET /registry`), the app's owner and
     visibility, its delivery, its instance setting, and, apart and
     unselected, the request to host bots.
   - **An install accepts the requested set whole.** It writes one
     `permit` per action, its principal the installation and its
     resource the installing organization, and nothing outside that
     organization. Narrowing it afterwards is an ordinary `forbid` on
     the installation.
   - **Installing writes `permit`s, so it is the owner's**:
     `iam:installation.create` and `iam:installation.update` are held
     by the owner, as `iam:permit.write` is (ADR-0145 item 4). The
     administrator holds `iam:installation.delete`, which only takes
     grants away, and every role holds `iam:installation.read`.
   - **Hosting bots is granted on its own**, by the owner under
     `iam:installation.update`, as a `permit` of `iam:bot.host` on the
     installation. No install writes it, and an app that did not ask
     for it cannot be granted it.
   - **A changed request waits for the owner.** When an app's
     requested set gains an action, each installation keeps the grants
     it accepted until its owner accepts the new set under
     `iam:installation.update`. An action the app drops is withdrawn
     from every installation at once.
   - **An automatically installed app** is written with each new
     organization and by a migration for every existing one, with the
     fixed floor its record names. No route installs or deletes it.
     The deployment revokes and re-issues one organization's
     installation by a server command, as ADR-0129 decision 7 does
     for a service principal.
   - **Uninstalling** under `iam:installation.delete` deletes the
     installation with its policies, instances, keys and tokens, and
     each is refused from the next request.
5. **Two ways to authenticate, both `private_key_jwt` under
   `machine-token.v2`.**
   - **An instance's key.** ADR-0184 decisions 3 to 8 hold for every
     instance of every app, with "cluster" read as "instance":
     - a one-time registration token answered when a member creates
       the instance record under `iam:instance.register` or reissues
       it under `iam:instance.reissue`;
     - registration with the public half of a key the instance
       generated, throttled per source address by
       `GARAM_INSTANCE_REGISTRATION_LIMIT` before the token is read;
     - one-hour opaque tokens read on every request beside the record
       and the key;
     - rotation under the current key's signature;
     - revocation by withdrawing a key, reissuing, or deleting the
       instance, each alone.

     An app whose instance setting is `none` has no instance
     routes, and `one` refuses a second record 409.
   - **An app key.** An app holding app keys exchanges an assertion
     signed by one of them, with `client_id`, `iss` and `sub` the
     installation's GRN, for a token of that installation, as
     ADR-0129 decision 3 decides for a service principal. A platform
     app's keys are in the deployment's service keys file, as today.
     A registered app's owner sets its public JWKs under
     `iam:app.update`; garam never holds the private half. At most two
     are live, for a rotation: add the successor, prove an exchange,
     withdraw the old. Withdrawing a key refuses its tokens from the
     next request.
6. **Permissions are the actions garam already has.**
   - An app requests actions the manifests declare, and no new
     vocabulary. An action is requestable when its manifest lists
     `Iam::Installation` and `Iam::Instance` among its principals. A
     manifest adds them to an action to make it so.
   - **No app can widen its own grant.** `iam:app.*`,
     `iam:installation.*`, `iam:instance.*`, `iam:policy.*`,
     `iam:permit.write`, `iam:consent.*` and `iam:bot.host` are never
     requestable, so an installation's principal cannot change what it
     or any principal holds.
   - **The installation is the principal of the grants, and an
     instance is in it.** A token from an app key is decided with the
     installation as principal, and an instance's with the instance,
     whose parent is its installation, so every instance holds what
     the installation does and nothing of its own.
   - An installation's grants never reach another organization, and no
     consent relation (ADR-0035) carries one across.
7. **What a token reaches, read off the app's record.**
   - Every app's token reaches the app system's machine routes:
     registration, the exchange, rotation and its events (decision
     8).
   - The machine operations the record lists, and nothing else on the
     machine listener. This replaces `machineapi.ConfineClusters` and
     ADR-0129 decision 5's three operations with one confinement read
     off the record. The connector's list is the cluster-connection
     routes; cowork's and codehub's are what ADR-0129 and
     [machine-identity.md](../machine-identity.md) §codehub's principal
     give them.
   - The browser-facing API only where the record says so, each
     operation decided by Cedar as for any principal. cowork's and
     codehub's records do not, which keeps ADR-0129 decision 3's
     audience.
   - The bot-token exchange, only for an instance whose installation
     holds `iam:bot.host` (decision 10).
8. **Delivery is a setting, carried by the app system.**
   - **Events** are what garam tells an installation: the app system's
     own (an installation created, its grants changed, an instance
     registered or deleted, a key withdrawn, the installation
     deleted), and those a domain raises for an installation's
     instances. Each names the installation's GRN, and the instance's
     where there is one.
   - **`pull`**: each installation keeps a log of its events at
     positions drawn one at a time, kept 30 days, read by a long poll
     on the machine listener with any of the installation's tokens,
     on ADR-0187 decisions 2, 3 and 6's terms.
   - **`webhook`**: garam posts each event to the app's URL as a JWS
     signed by its issuer key (ADR-0141), so a receiver verifies it
     against garam's published key set and holds no shared secret.
     Each carries a delivery identifier for the receiver to drop a
     repeat, and is retried with backoff for 24 hours, then recorded
     as failed where the app's owner reads it.
   - **A protocol of an app's own stays its own.** The connector's
     declarations feed and its reports are the cluster-connection
     interface's routes and are not events; its delivery is `pull`.
9. **Actions.** `iam:cluster.*` is replaced:
   - `iam:app.create`, `.read`, `.update` and `.delete`;
   - `iam:installation.create`, `.read`, `.update` and `.delete`;
   - `iam:instance.register`, `.read`, `.update`, `.reissue` and
     `.delete`, decided on the instance's installation, the owner and
     the administrator holding all but `.read`, which every role
     holds, as ADR-0184 decision 9's did.

   Each is named and none is granted by wildcard, for the operator's
   reason. The routes follow: `/orgs/{org}/apps`,
   `/orgs/{org}/installations` and
   `/orgs/{org}/installations/{installation}/instances`.
10. **Placing a bot is the bot domain's act, not the app system's.**
    - `iam:cluster.declare` becomes `iam:bot.place`, decided on the
      target instance and held by the owner and the administrator.
      An instance whose installation does not hold `iam:bot.host` is
      refused as a target.
    - Declarations, their log, reports and last contact belong to
      garam's bot domain, keyed by the instance's GRN. ADR-0187
      decisions 2 to 7 and ADR-0188 stand there.
    - A declared bot's token (ADR-0189 decision 2) is exchanged by an
      instance whose installation holds `iam:bot.host`, for a bot a
      live placement puts on that instance. `act.sub` is the
      instance's GRN.

## Consequences

- **What this supersedes and amends.**
  - ADR-0184 decisions 1, 2 and 9 are superseded by decisions 1 and 9:
    the cluster record becomes an instance. Its decisions 3 to 8 and
    10 stand, read for every instance (decision 5).
  - ADR-0187 decision 1 is superseded by decision 10. Its other
    decisions stand, in the bot domain.
  - ADR-0185 is amended: every connector garam reaches, aurora's and
    each deployment target's included, is an instance of
    `garam-bot-connector` registered through the app system.
  - ADR-0189 is amended: the actor of a declared bot's token is an
    instance whose installation holds `iam:bot.host`.
  - ADR-0129 is amended: cowork's and codehub's principal becomes
    their automatic installation, `grn:<org>/iam/installation/cowork`
    and `…/codehub`, its deployment key their app key and its floor
    their record's machine operations. One per organization, the
    deployment's revocation and the machine-listener-only audience
    stand.
- **Building it is four steps after this one** (#2079 §Order): the
  app system's core with the connector seeded as an app (A); the bot
  domain taking declarations, reports and the mint under
  `iam:bot.place` (B); organizations and third parties registering
  apps, with app keys, webhook delivery and the install screen; and
  the interface split with garam-bot#1205, then cowork and codehub as
  platform apps. The service principals' GRNs are renamed by the
  last; the cluster's tables, routes and environment variable by A,
  with nothing live to move.
- **Until A lands** no route, action, table or environment variable
  is added under a cluster's name, and the cluster-connection
  interface's `v1alpha1` paths are served unchanged after it: they
  are `self`-relative and name no kind.
- **Mirrors are owed elsewhere**: garam-bot ADR 0036 decision 5 (a
  place's kind becomes an instance's opaque kind) and
  garam-bot-operator ADR 0088 decisions 1, 8 and 9. garam-bot#1214
  holds `instance` as the kind-free word.
- **An organization can now connect a system of its own**, and offer
  it to others, through the path garam's own apps take, and a
  third party's app reaches only what an owner accepted on the
  install screen.
- **Platform apps were the first scope and are not a special case.**
  The model is the same for an app the platform registered and one an
  organization did: an installation names its app by GRN and an
  instance names its installation, so neither changed when
  registration by organizations was taken into scope, and the
  platform's apps differ only in the two settings decision 2 reserves.
- **The operator and supervisor records stay** for the callers that
  use them. Retiring them is a later decision (#2079 §Order, step 6).
- **A public app is installed without review.** What stands between a
  public app and an organization is the owner's accepting its
  requested set, the floor of decision 6, and the platform's power to
  delete any app by a server command.

## Signs the decision was wrong

- A setting is added that one app alone reads, or a path tests an
  app's slug, which would show the kinds coming back as settings.
- Owners accept a requested set whole and then write `forbid`s for
  part of it as routine, which would show an install needs to grant
  less than the app asked.
- An app needs per-instance grants, which would show an instance is a
  principal of its own and not one of its installation.
- A third party's public app is installed and abuses what it was
  granted before the platform notices, which would show public apps
  need review before they are installable.
- The event log or webhook retries load the database or the egress
  more than the delivery is worth.

## Rejected alternatives

- **A kind per connected system**, the path from cluster to PC to naru
  host. Each adds a record, routes, actions and tables that duplicate
  the last (comment 19863476).
- **A new permission vocabulary for apps**, as GitHub's per-app
  scopes. garam's actions already name what a principal may do, and a
  second vocabulary would need its own mapping onto them.
- **`grn:<org>/iam/installation/<i>/instance/<id>`.** #2079 settled
  the flat form, and the installation is held as the record's parent
  instead, which Cedar reads the same way.
- **`bot:placement.write`.** A GRN service segment is a service's token
  audience and caller name (grn-model.md §Ownership); the bot domain
  is neither, and ADR-0189 put the bot's types under `iam`.
- **A shared secret to sign webhooks.** garam already publishes an
  issuer key set, and a secret would be another credential per app
  for garam to hold.
- **The administrator installing.** An install writes `permit`s, which
  no role below the owner writes.
