# clusterfixture

The server fixture garam supplies to `garamsh/garam-bot-operator`'s
cluster-connection `v1alpha1` server conformance suite
(`interfaces/cluster-connection/v1alpha1/conformance.md` §The server
fixture, at the commit `platform/Makefile`'s `CLUSTER_CONNECTION_PIN`
names). The suite runs it once per action it needs from garam outside
the protocol.

It is not part of garam: `platform/cmd/garam` does not import it, so no
garam binary or image carries it. `platform/tests/e2e` checks that
`go list -deps ./cmd/garam` names no package under `tests/`.

## What it serves

Every subcommand the suite names (#2047). The interface names a
record a cluster; garam keeps it as an instance of the organization's
installation of `garam-bot-connector` (ADR-0192), and the subcommands,
their arguments and what they print keep the interface's names:

| Subcommand | Does | Prints | Through |
|---|---|---|---|
| `mint-token` | installs `garam-bot-connector` where the organization has not, grants the installation bot hosting, creates a record of that installation and answers its registration token | `registrationToken`, `cluster` | `createInstallation`, `grantInstallationBotHosting`, `createInstance` |
| `mint-token --cluster <grn>` | answers a new registration token for that record | `registrationToken`, `cluster` | `reissueInstanceRegistrationToken` |
| `mint-token … --expired` | as above, then moves that token's expiry into the past | `registrationToken`, `cluster` | the database |
| `revoke-keys <grn>` | withdraws every key of the record not yet withdrawn | `cluster` | `listInstanceKeys`, `revokeInstanceKey` |
| `delete-cluster <grn>` | deletes the record | `cluster` | `deleteInstance` |
| `declare <file>` | places the file's object on its record, as a new declaration or the next generation of the one it names | `id`, `generation` | `createInstanceDeclaration`, or `putInstanceDeclaration` where the file names an `id` |
| `tombstone <id>` | tombstones the live declaration `id` on whichever of the organization's records holds it | `id`, `generation` | `listInstances`, `getInstanceDeclaration`, `deleteInstanceDeclaration` |
| `latest-report <grn>` | reads the record's kept report and its `lastContactAt`, each `null` where there is none | `cluster`, `lastContactAt`, `report` | `getInstanceReport`, its `instance` printed as `cluster` |
| `expire-cursors <grn>` | purges the record's log and every cursor answered so far: the purged position becomes the head, the head moves one past it, and the record's held polls are woken | `cluster` | `getInstance`, then the database |

`declare`'s file is one JSON object: `cluster`, the record's GRN;
`id`, optional; and `object`, the declared object.

## Invocation

Build it from the garam checkout under test, and run it beside a
`garam serve api` and `garam serve machine` that share one database:

```sh
go -C platform build -o "$PWD/clusterfixture" ./tests/clusterfixture
GARAM_FIXTURE_API_URL=http://127.0.0.1:8080 \
GARAM_FIXTURE_SESSION_TOKEN=... GARAM_FIXTURE_ORG=<slug> \
GARAM_FIXTURE_DATABASE_URL=postgres://... \
  ./clusterfixture mint-token
```

- `GARAM_FIXTURE_API_URL`: `serve api`'s base URL, its routes at the
  root as the console's `/api/*` rewrite reaches them.
- `GARAM_FIXTURE_SESSION_TOKEN`: the session of a principal holding
  `iam:installation.create`, `.read` and `.update`,
  `iam:instance.register`, `.read`, `.reissue` and `.delete`, and
  `iam:bot.place` on the instance, sent as the `garam_session` cookie.
  Installing and granting bot hosting are the owner's alone.
  `testprincipal prepare` answers one, and that principal owns any
  organization it creates.
- `GARAM_FIXTURE_ORG`: the organization a new record is created in. A
  subcommand naming a record by GRN reaches it in the GRN's own
  organization.
- `GARAM_FIXTURE_DATABASE_URL`: read by `--expired` and
  `expire-cursors` alone, the two writes no browser route makes. It
  must reach a Postgres on this machine (a loopback address,
  `localhost` or a unix socket, every dial checked), and the check runs
  before garam is reached.

`serve machine` refuses past `GARAM_INSTANCE_REGISTRATION_LIMIT`
registrations per address a minute, ten unless set. The suite
registers more often than that from one address, so a run sets it
higher.

## Output

- **Success:** exit 0, one JSON object on stdout.
- **A refusal or a failure:** exit 1, and stdout carries the
  interface's error body (T-5), `{"error", "message"}`. The code is
  read off garam's status: 401 and 403 are `unauthorized`, 404 is
  `not_found`, 429 is `throttled`, 5xx is `internal`, and any other
  status is `invalid_request`. The message is garam's own.
- **A command line it cannot read:** exit 2, and the usage on stderr.

Nothing it prints repeats the session token or the database URL.
