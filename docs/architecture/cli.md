# CLI

What the user commands of the `garam` binary are responsible for: how a
person signs one in, what it keeps between runs, what it calls, and what
it does not decide. The server commands the same binary carries are
[overview.md](./overview.md)'s module list and
[deployment-contract.md](./deployment-contract.md)'s.

## Contents
- What it is
- Signing in
- What it keeps
- What it calls
- What it does not decide
- Not established
- Decisions this rests on

## What it is

`internal/cli` implements it, beside the server commands, in the one
binary garam ships
([ADR-0003](./adr/0003-single-cli-topology.md),
[ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md)).
The user commands sit at the top level:

| Command | What it does |
|---|---|
| `garam login [--no-browser]` | signs the CLI in by the device grant |
| `garam logout` | revokes the CLI's session and forgets its tokens |
| `garam whoami` | names the user the session is for |
| `garam orgs list` | the organizations the user belongs to |
| `garam orgs use <org>` | saves the organization later commands act in |
| `garam operators list` | the organization's operators |
| `garam agents list`, `garam agents get <agent>` | the organization's agents |

Every one of them takes `--endpoint`, `--org` and `--output`
(`-o table`, the default, or `-o json`, which writes the API's own
objects).

## Signing in

`garam login` requests a device code, prints the user code and the page
it is approved on, opens that page unless `--no-browser` is passed, and
polls until the code is decided or lapses. It honours `slow_down` and
the interval the answer carries, and keeps polling through a failed
request until the code's expiry, as RFC 8628 §3.5 has a client do. The
page is answered as the path `/device` and joined to the endpoint the
command talks to; the console serves it
([web-app.md](./web-app.md) §What it serves). What the server does
with each request is [sign-in.md](./sign-in.md) §The CLI's session.

A command presents the access token as a bearer. Where it lapses within
a minute, the command spends the refresh token for a new pair first,
and a refused refresh answers "not signed in; run garam login".
`garam logout` revokes the session on the server and drops the tokens
from the file. A session already revoked elsewhere is treated as signed
out.

## What it keeps

- **The file.** `$XDG_CONFIG_HOME/garam/credentials`, or
  `~/.config/garam/credentials` where that is unset or relative. It
  holds, per endpoint, the session's tokens and their expiries, and the
  organization `garam orgs use` saved.
- **Its permissions.** It is written `0600` in a `0700` directory,
  replaced whole through a file created beside it, and refused when
  anyone but its owner can read it.
- **Concurrent commands.** A read-modify-write holds an exclusive lock
  on `credentials.lock` beside it, so two commands refreshing at once
  spend the refresh token once and the second reads the pair the first
  wrote.
- **Printing.** No command prints a token.
- **Logout keeps the org.** `garam logout` keeps the saved organization.

## What it calls

- **The browser API alone.** The CLI calls the browser API through the
  client `api/client-codegen.yaml` generates from `api/openapi.yaml`,
  limited to the operations a command calls.
- **The endpoint.** It comes from `--endpoint`, else `GARAM_ENDPOINT`,
  else `https://garam.sh`, and is an origin. It is `https`, or `http` to
  a loopback address alone, because a session's tokens cross it. The
  API is reached under it at `/api`, behind the console's rewrite
  ([web-app.md](./web-app.md) §How it reaches garam).
- **The organization.** It comes from `--org`, else the saved one, and a
  command that needs one refuses rather than guessing. `garam orgs use`
  reads the organization before saving it, so what is saved is one the
  session reaches.

## What it does not decide

Authorization is the API's: a command sends what the person asked for
and reports what the API answered. A refusal reaches the person as the
API's message under its kind.

## Not established

- **Writes.** The CLI does not yet create, change or delete anything.
  `agents rename` waits for the agent name of #1610.
- **Other platforms.** The file lock is `flock(2)`, which builds where
  `syscall.Flock` does. No release publishes the binary for a platform
  without it.

## Decisions this rests on

- [ADR-0003](./adr/0003-single-cli-topology.md) — one binary carries
  every command
- [ADR-0005](./adr/0005-server-side-sessions.md) — a session is a row
  revoked by deleting it
- [ADR-0120](./adr/0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md)
  — the CLI signs in by the device grant, holds a session as a bearer,
  and keeps it in one owner-only file
