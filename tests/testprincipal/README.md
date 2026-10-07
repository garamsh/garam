# testprincipal

The contract of the test-only command that prepares a disposable Garam
database holding one signed-in principal (ADR-0081).

## What it is for

A test in another repository that calls Garam's public API needs a
signed-in user, and the first one cannot be made through that API:
an invitation needs a signed-in administrator, and Google cannot run
unattended. This command makes that first user and nothing else.
Organizations, operators, further users, delegations and operation
authorities are made through the public routes the test exercises.

It is not part of Garam: `cmd/garam` does not import it, so no Garam
binary carries it, and no release image does: those carry only what
`deploy/local/Dockerfile` builds from `./cmd/garam`. The test image
below carries it as a file (ADR-0092).
`tests/e2e/principal_fixture_test.go` checks that the built `garam`
has no such command and that `go list -deps ./cmd/garam` names no
package under `tests/`.

## Invocation

Build it from the Garam checkout at the revision the consumer pins,
or copy it out of the test image (§The test image), and run it
against a Postgres server on the same machine:

```sh
go build -o testprincipal ./tests/testprincipal
./testprincipal prepare -contract 1 -migrations ./migrations \
  -server-url 'postgres://garam:garam@localhost:25432/postgres?sslmode=disable'
```

Then start `garam serve authz` and `garam serve api`, built from the
same checkout, with `GARAM_DATABASE_URL` set to `databaseUrl` from the
output and the api's `GARAM_AUTHZ_ADDR` naming authz's
`GARAM_GRPC_ADDR`, and send the session as a cookie. When every
process using the database has stopped:

```sh
./testprincipal drop -contract 1 -server-url "$SERVER_URL" -database "$DATABASE"
```

`go run github.com/garamsh/garam/tests/testprincipal@<revision>` is not
a supported invocation: `-migrations` names a checkout's directory, and
nothing verifies that a module cache copy serves.

## Contract 1

- `-contract` is required and must be `1`. Any other value exits 2
  before anything is read; build from the revision your tests pin.
- `prepare` writes one JSON object to stdout:
  - `contract`: `1`
  - `database`: the database it created
  - `databaseUrl`: the server URL naming that database; it carries the
    password the server URL carried
  - `userGrn`: the principal
  - `session.cookie`, `session.token`: the cookie name and value of the
    principal's session
  - `expiresAt`: when that session ends; `-session-ttl` sets it, one
    hour by default and at most 24
- Exit 0 is success, 2 a refused invocation, 1 a failure. Stderr holds
  one actionable line on failure and nothing on success; it carries no
  password and no session token.

## What it refuses

- **A server off this machine.** Every host the URL names, fallbacks
  and query overrides included, must be a loopback address,
  `localhost` or a unix socket directory, and every dial must reach a
  loopback address.
- **A database it did not create.** `prepare` always creates a new
  database, `garam_principal_` and 16 hex digits, and marks it with a
  comment; it never seeds or migrates an existing one. `drop` refuses a
  name of any other shape, or one without the mark, and does not force
  past open connections.
- **A partial database.** When `prepare` fails after creating its
  database, writing its output included, it drops it again. Where
  that drop fails too, stderr names the database left behind and why.

## The test image

`ghcr.io/garamsh/garam-test` holds this command, `garam` and the
migrations of one Garam commit, for a CI that cannot read this
repository (ADR-0092). It is a file source: copy the paths out and
never run the image.

| Path | What it is |
|---|---|
| `/garam` | `go build ./cmd/garam`, static, linux/amd64 |
| `/testprincipal` | `go build ./tests/testprincipal`, static, linux/amd64 |
| `/migrations/` | the repository's `migrations/`, verbatim |

Its label `org.opencontainers.image.revision` names the full commit.
`make test-image` builds it from `tests/testprincipal/Dockerfile`, and
`make test-image-e2e` checks those paths, the linking and the label.

**Pin the digest, not the tag.** Every push to `dev` publishes one
version tagged with the commit's full hash, and a tag can be pushed
again; the digest cannot name other bytes:

```sh
docker buildx imagetools inspect --format '{{.Manifest.Digest}}' \
  ghcr.io/garamsh/garam-test:<commit>
id=$(docker create ghcr.io/garamsh/garam-test@<digest>)
docker cp "$id:/garam" . && docker cp "$id:/testprincipal" . \
  && docker cp "$id:/migrations" .
docker rm "$id"
```

Then run them as §Invocation says, with `-migrations ./migrations`.

- **Access.** The package is private. A repository reads it from its
  own workflows once the package grants that repository Actions
  access, a setting an organization admin applies; ADR-0092 grants it
  to `garamsh/garam-agent-operator`.
- **Retention.** The 8 newest unpinned versions are kept, and every
  version tagged `pinned-<consumer>`; a version left without a tag is
  deleted. A digest older than 8 `dev` commits is gone unless pinned.
  To hold one, ask the Garam PM, who runs
  `.github/workflows/test-image.yml` by dispatch with the commit and
  your name: that moves `pinned-<consumer>` to it, and your previous
  pin becomes prunable.
- **What it refuses outside a test database.** `/testprincipal` is
  this command at that commit, and refuses everything §What it
  refuses lists: a server off the machine, a database it did not
  create, and a partial database. `/garam` is the ordinary server and
  refuses nothing of its own here; it serves whatever database it is
  given, so give it the `databaseUrl` `prepare` printed.
