# Apps

How every system that connects to garam is registered, reviewed,
installed in an organization and authenticated, and what each app's
settings decide
([ADR-0192](./adr/0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)).

**Unbuilt.** No package, table, route or action of the app system
exists yet. What runs today is its predecessors: a connected cluster's
record ([clusters.md](./clusters.md)) and a first-party service's
principal ([machine-identity.md](./machine-identity.md) §cowork's
principal). Each becomes the app system's when the issue below that
names it is built.

## Current decisions

- **Three resources, under `iam`.**
  - An app, `grn:root/iam/app/<slug>` when the platform registers it
    and `grn:<org>/iam/app/<slug>` when an organization does. A slug is
    unique across every owner and never reused.
  - An installation, `grn:<org>/iam/installation/<slug>`: one app in
    one organization, at most one per pair, named by the app's slug,
    carrying its scope and an incarnation.
  - An instance, `grn:<org>/iam/instance/<id>`: one running copy,
    garam minting the identifier, its installation its Cedar parent.
    Its kind is a string garam stores and never reads.
  - "Installation" names garam's app's alone. The per-organization
    record of an external service's app garam brokers (garam-bot ADR
    0038) is an external app connection.
- **An app's record carries every difference between apps**, and no
  path branches on which app it is:
  - visibility, `private` or `public`, and its review state;
  - requested actions, as single action ids, and whether it asks to
    host bots;
  - instances, `none`, `one` or `many` per installation;
  - delivery, `pull` or `webhook`;
  - up to two public app keys;
  - whether its tokens reach the browser-facing API.

  The platform alone writes four more, which are off or empty for an
  app an organization registered: automatic installation, the machine
  operations its tokens reach, the issuer audiences it may obtain and
  whether it introspects its own, and the fixed policies written with
  each installation.
- **Registration and review.** The platform's apps,
  `garam-bot-connector`, `cowork` and `codehub`, are written by a
  migration and the deployment's configuration, and are public. An
  organization's owner or administrator registers, changes and deletes
  its own under `iam:app.create`, `.update` and `.delete`; every member
  reads its own and every public app under `iam:app.read`.
  - A registered app starts `private`, installable only in its own
    organization, and needs no review.
  - Asking for `public` makes it `pending` until a platform
    administrator approves or rejects it by a server command. A public
    app's changed requested actions, bot-hosting request or webhook URL
    is a pending revision, and every installation uses the last
    approved one until it is approved.
  - A public app goes private only while no other organization has it
    installed. Deleting an app, by its owner or by a server command,
    deletes every installation of it.
- **Installation.**
  - The install screen lists each requested action with the
    registry's description of it, and asks for the scope: the whole
    organization or chosen projects. An install accepts the set whole
    and writes one `permit` per action on the installation, its
    resource in that scope.
  - Because it writes `permit`s, `iam:installation.create` and
    `.update` are the owner's; the administrator holds `.delete`, and
    every role `.read`.
  - Bot hosting is shown apart and unselected. Selecting it is a
    second owner write in the same flow, a `permit` of `iam:bot.host`
    on the installation itself.
  - An action an approved revision adds waits for each owner to
    accept it; one it drops is withdrawn at once.
  - An automatically installed app is written with each organization,
    over the whole of it, with its fixed policies, and only a server
    command revokes it.
  - Uninstalling deletes every policy whose principal is the
    installation or one of its instances, whoever wrote it, so a
    reinstall inherits nothing.
- **Authentication is `private_key_jwt` under `machine-token.v2`, two
  ways.**
  - An instance's key: a one-time registration token, a key the
    instance generated, one-hour tokens read on every request, rotation
    under the current key and revocation per instance, as
    [clusters.md](./clusters.md) describes for a cluster today. The
    registration limit becomes `GARAM_INSTANCE_REGISTRATION_LIMIT`.
  - An app key: an assertion naming an installation's GRN, exchanged
    for a token of that installation. A platform app's keys stay in the
    deployment's service keys file; a registered app's owner sets the
    public halves.
- **No app can widen a grant.** No app may request an action that
  grants, delegates or changes membership, roles, invitations,
  policies, consent, a principal's credentials or the organization
  itself. ADR-0192 decision 6 lists today's members, among them the
  group and project membership actions, `iam:org.invite`,
  `iam:operator.delegate`, every policy write and `iam:bot.host`. The
  manifest check keeps a new action of that property unrequestable.
  An instance holds what its installation holds and nothing of its
  own, and no grant crosses the organization.
- **A token reaches** the app system's own machine routes, the machine
  operations its record lists and nothing else on that listener, the
  issuer's exchange and introspection as its record lists them, the
  browser-facing API only where the record says so, and the bot-token
  exchange only for an instance whose installation holds
  `iam:bot.host`. cowork's floor (its own organization, agents only,
  the system `permit` of `iam:message.send`) is its record's fixed
  policies.
- **Delivery.** `pull` reads a per-installation event log by long
  poll, kept 30 days. `webhook` posts each event as a JWS under
  garam's issuer key, `typ` `garam-event+jwt` and `aud` the app's GRN,
  only to an address that resolves outside loopback, private,
  link-local, unique-local and metadata ranges at that attempt, with
  no redirect followed. It is retried for 24 hours and then recorded
  as failed. An app's own protocol, such as the connector's
  declarations feed, stays its own routes.
- **Actions** replace `iam:cluster.*`: `iam:app.*`,
  `iam:installation.*` and `iam:instance.register`, `.read`,
  `.update`, `.reissue` and `.delete`, each named and none granted by
  wildcard. Placing a bot on an instance is the bot domain's
  `iam:bot.place`.

## Rationale

- Why one system and not a kind per connected thing, and why the
  connector is an ordinary app: the owner's direction recorded in
  ADR-0192 §Context.
- Why an installation is named by its app's slug, with a project
  scope rather than more installations: a service derives its
  principal from the organization with no lookup, as ADR-0129's did.
- Why installing is the owner's: an install writes `permit`s
  (ADR-0145 item 4).
- Why public apps are reviewed: the owner named review as part of
  registration (ADR-0192 §Context).
- Why webhooks are signed by the issuer key under a type of their own:
  garam already publishes that key set (ADR-0141), and a distinct
  `typ` and audience keep an event from being taken for a token.

## Open questions

- Not yet built, each its own issue: the core with the connector
  seeded and the app-key exchange (#2081); the bot domain under
  `iam:bot.place` and `iam:bot.host` (#2082); registration by
  organizations, review, owner-set app keys, the events feed and
  webhooks (#2083); the cluster-connection split (#2084); cowork and
  codehub as platform apps (#2085). Until #2081 lands, no route,
  action, table or environment variable is added under a cluster's
  name.
