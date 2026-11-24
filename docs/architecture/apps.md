# Apps

How every system that connects to garam is registered, installed in an
organization and authenticated, and what each app's settings decide
([ADR-0192](./adr/0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)).

**Unbuilt.** No package, table, route or action of the app system
exists yet. What runs today is its predecessors: a connected cluster's
record ([clusters.md](./clusters.md)) and a first-party service's
principal ([machine-identity.md](./machine-identity.md) §cowork's
principal). Each becomes the app system's when the step of #2079
§Order that names it is built.

## Current decisions

- **Three resources, under `iam`.**
  - An app, `grn:root/iam/app/<slug>` when the platform registers it
    and `grn:<org>/iam/app/<slug>` when an organization does. A slug is
    unique across every owner and never reused.
  - An installation, `grn:<org>/iam/installation/<slug>`: one app in
    one organization, at most one per pair, named by the app's slug
    and carrying an incarnation.
  - An instance, `grn:<org>/iam/instance/<id>`: one running copy,
    garam minting the identifier, its installation its Cedar parent.
    Its kind is a string garam stores and never reads.
- **An app's record carries every difference between apps**, and no
  path branches on which app it is:
  - visibility, `private` or `public`;
  - requested actions, and whether it asks to host bots;
  - instances, `none`, `one` or `many` per installation;
  - delivery, `pull` or `webhook`;
  - up to two public app keys;
  - whether its tokens reach the browser-facing API.

  Automatic installation and the machine-listener operations its
  tokens reach are written by the platform alone, and are off for an
  app an organization registered.
- **Registration.** The platform's apps, `garam-bot-connector`,
  `cowork` and `codehub`, are written by a migration and the
  deployment's configuration. An organization's owner or administrator
  registers, changes and deletes its own under `iam:app.create`,
  `.update` and `.delete`, and every member reads its own and every
  public app under `iam:app.read`. A public app goes private only
  while no other organization has it installed. Deleting an app,
  by its owner or by the deployment's server command, deletes every
  installation of it.
- **Installation.** The install screen lists each requested action
  with the registry's description of it, and the request to host bots
  apart and unselected. An install accepts the set whole and writes one
  `permit` per action on the installation, confined to the installing
  organization. Because it writes `permit`s, `iam:installation.create`
  and `.update` are the owner's; the administrator holds `.delete`,
  and every role `.read`. `iam:bot.host` is granted only by the owner,
  on its own. An action an app adds waits for each owner to accept it;
  one it drops is withdrawn at once. An automatically installed app is
  written with each organization, and only a server command revokes
  it.
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
- **Permissions are existing actions.** An action is requestable when
  its manifest lists `Iam::Installation` and `Iam::Instance` as
  principals. Actions over apps, installations, instances, policies,
  permits, consents and `iam:bot.host` are never requestable. An
  instance holds what its installation holds and nothing of its own,
  and no grant crosses the organization.
- **A token reaches** the app system's own machine routes, the machine
  operations the record lists and nothing else on that listener, the
  browser-facing API only where the record says so, and the bot-token
  exchange only for an instance whose installation holds
  `iam:bot.host`.
- **Delivery.** `pull` reads a per-installation event log by long
  poll, kept 30 days. `webhook` posts each event as a JWS under garam's
  issuer key, retried for 24 hours and then recorded as failed. An
  app's own protocol, such as the connector's declarations feed,
  stays its own routes.
- **Actions** replace `iam:cluster.*`: `iam:app.*`,
  `iam:installation.*` and `iam:instance.register`, `.read`,
  `.update`, `.reissue` and `.delete`, each named and none granted by
  wildcard. Placing a bot on an instance is the bot domain's
  `iam:bot.place`.

## Rationale

- Why one system and not a kind per connected thing, and why the
  connector is an ordinary app: the owner's direction recorded in
  ADR-0192 §Context.
- Why an installation is named by its app's slug: a service derives
  its principal from the organization with no lookup, as ADR-0129's
  did.
- Why installing is the owner's: an install writes `permit`s
  (ADR-0145 item 4).
- Why webhooks are signed by the issuer key and not a shared secret:
  garam already publishes that key set (ADR-0141).

## Open questions

- The order of building is #2079 §Order. Until its core step lands, no
  route, action, table or environment variable is added under a
  cluster's name.
