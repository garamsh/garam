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
binary carries it, and the image carries only what
`deploy/local/Dockerfile` builds from `./cmd/garam`.
`tests/e2e/principal_fixture_test.go` checks that the built `garam`
has no such command and that `go list -deps ./cmd/garam` names no
package under `tests/`.

## Invocation

Build it from the Garam checkout at the revision the consumer pins,
and run it against a Postgres server on the same machine:

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
