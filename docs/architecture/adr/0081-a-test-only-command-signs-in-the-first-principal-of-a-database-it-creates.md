# ADR 0081: A test-only command signs in the first principal of a database it creates

> Status: accepted — §Decision's "no binary or image Garam ships
> carries it" is superseded in part by
> [ADR-0092](./0092-a-test-image-is-published-to-ghcr-from-dev-and-no-release-carries-it.md),
> whose test image carries it
> Date: 2026-10-05

Drafted for #1176; the PM required the record on 2026-10-05, since a
versioned interface another repository relies on is something this
repository publishes, test-only or not.

## Context

Tests in `garamsh/garam-agent-operator` need to call Garam's public
API as a signed-in user. The first user of a database cannot be made
through that API: Google sign-in cannot run unattended, and password
registration needs an invitation, which only a signed-in administrator
can issue (ADR-0041). `web/e2e/seed.ts` solves the same problem for the
browser suite by writing rows with SQL, which ties every caller to the
schema.

## Decision

**`tests/testprincipal` is a command that creates a fresh database on a
Postgres server on the same machine, migrates it, and signs one user in
through Garam's own stores.** It prints the identifiers and the session
a caller needs, under a contract version the caller must name.

- **Test-only.** `cmd/garam` does not import it, so no binary or image
  Garam ships carries it; the deployed sign-in is unchanged.
- **It owns the database it seeds.** It refuses a server reached other
  than over loopback or a unix socket, never seeds or migrates an
  existing database, and drops only a database whose name it generated
  and that carries its mark.
- **Only the first principal.** Everything after it — organizations,
  operators, further users, delegations, operation authorities — goes
  through public routes, so the routes stay the subject of the
  consumer's test. A route a consumer needs and cannot reach is
  reported as a gap, not written by the fixture.
- **Versioned and pinned.** A caller names contract `1`; any other is
  refused. The command is built from the Garam checkout the consumer
  pins, with that checkout's migrations.
- `tests/testprincipal/README.md` is the contract and the invocation.

## Consequences

- Another repository can test against a real Garam without its
  schema, its SQL, or a Google account.
- A change to the output or the refusals is a new contract version.
- `web/e2e/seed.ts` is not moved onto it: the browser suite seeds into
  the database the running stack already uses, which this command
  refuses by construction.

## Rejected alternatives

- **A development sign-in in the server.** ADR-0044 removed one; an
  environment flag would put a bypass in the deployed binary.
- **Seeding a database the caller names.** A flag would then be all
  that stands between the command and a database it does not own.
- **Fixture-written grants and authorities.** They would replace the
  decisions the consumer's test exists to exercise.
