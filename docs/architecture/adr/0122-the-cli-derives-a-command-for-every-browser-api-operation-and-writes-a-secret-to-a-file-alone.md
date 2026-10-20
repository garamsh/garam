# ADR 0122: The CLI derives a command for every browser API operation and writes a secret to a file alone

> Status: accepted
> Date: 2026-10-20

Drafted for #1632, for which the Garam PM reserved this number. The
direction is the owner's of 2026-10-20, recorded on #1632: agents
administer garam end to end, with no person at a browser. The PM
accepted derived commands on #1632 the same day, asking that the
measured cost of both ways be recorded and that the generator be Go in
this repository, run by `make gen`. garam is read at
`d2d92ec92dfa801469262e434032abcc13f73a91`; a claim below that names no
revision holds for it.

## Context

- **The CLI reaches a sliver of the API.** `api/openapi.yaml` declares
  80 operations. Before this record the CLI's client covered 12 of
  them, and its commands were written by hand, one per operation.
- **ADR-0120 fixed three things this keeps.**
  - The commands are in the `garam` binary.
  - A person signs in by the device grant.
  - The CLI "calls the browser API alone, through a client generated
    from `api/openapi.yaml`, and decides nothing the API does not".
- **A secret had to be copied off a page.** An operator key was taken
  off the console by a person copying it. Three answers carry a secret
  a command would otherwise print:
  - `issueOperatorKey.privateKeyPem`;
  - `createInvitation.token`;
  - `mintOperationAuthority.authority`.

  `deviceToken` answers session tokens too, but only `garam login`
  calls it.
- **The operations are regular.**
  - Every path parameter is a string alias in the generated code,
    except `withdrawAgentEnrollment`'s UUID.
  - Every request body is flat strings, integers, booleans and string
    arrays. The four exceptions are objects: two policy documents,
    `checkPermissions.checks`, and `approveAgentEnrollment.replaces`.

## Decision

**The CLI derives a command for every browser API operation from
`api/openapi.yaml`, and an answer carrying a secret is written to a
file the caller names, never to stdout.**

1. **Commands are derived, not written per operation.**
   - `internal/cli/cligen`, Go in this repository, reads the
     specification and `internal/cli/commands.yaml` and writes
     `internal/cli/commands.gen.go`. `make gen` runs it, and
     `make check-gen` fails while that file is stale.
   - The generated file is a table, plus one call per operation into
     the client oapi-codegen generates from the same specification, so
     ADR-0120's generated client holds. The commands are built from
     the table by hand-written code, `internal/cli/apicmd.go`.
   - A new operation therefore reaches the CLI with no command code.
     The measured cost of both ways:

     | | Per operation, by hand | Derived |
     |---|---|---|
     | Written once | none | 1,075 lines: `cligen` 554, `apicmd.go` 450, exit codes 71 |
     | The 68 operations not served yet | about 2,400 lines | none; `commands.gen.go` is about 13 generated lines per operation |
     | A new operation | about 35 lines and its test | none, unless it is named, excluded or carries a secret: one line of `commands.yaml` |

     The hand-written figure is read off the six commands that existed
     (`internal/cli/objects.go` at `d2d92ec9`), whose bodies run 24 to
     33 lines each.
2. **The grammar is `garam <noun>... <verb> [arguments]`.**
   - The nouns are the path's static segments.
   - The verb is the operationId's first word.
   - The path parameters are positional arguments, in order.
   - Under `/orgs/{org}/...` the org is the one the command acts in,
     read from `--org` or the saved default (ADR-0120).
   - Query parameters and body properties are flags in kebab case. An
     object property takes JSON, or `@file`, and `--body` sends the
     whole body as JSON or `@file` in place of the flags.
   - `commands.yaml` renames a command whose derived name reads badly,
     such as `search` for `searchOrg`.
   - An argument that begins with `-`, as a base64url key ID may,
     follows `--`.
3. **The output contract.**
   - `-o json` prints the API's own answer, indented, so a program
     reads what the specification declares.
   - The default is a table for people. Its columns are the answer's
     scalar properties in declared order, one level of an object's
     beneath it as `parent.child`. Where an answer object holds one
     list, the list is the table's rows.
   - Messages go to stderr.
4. **Exit codes.** A user command ends with:

   | Code | Meaning |
   |---|---|
   | 0 | success |
   | 1 | any other failure |
   | 2 | a usage error |
   | 3 | not signed in, or the session is no longer live |
   | 4 | permission denied |
   | 5 | not found |
   | 6 | a conflict: exists, precondition, or aborted |
   | 7 | an invalid argument |
   | 8 | unavailable or timed out |

   A server command still ends with 1 on any failure.
5. **A secret goes to a file alone.**
   - `commands.yaml` lists an answer's secret fields. The command then
     requires `--out <file>`.
   - It writes the whole answer there, created at mode 0600, and never
     over an existing file. It prints the answer without those fields,
     and names the file on stderr.
   - Without `--out` nothing is sent.
6. **What the CLI does not serve, and why.**
   - **`login`, `loginWithReturnPath`, `oauthCallback`.** Browser
     redirects. The CLI signs in by the device grant.
   - **`loginPassword`, `registerPassword`.** The CLI never sees a
     password (ADR-0120).
   - **`watchAgent`.** A server-sent event stream the console renders,
     not an answer a command prints.

   `requestDeviceCode` and `deviceToken` are served by `garam login`,
   `logout` by `garam logout` and `me` by `garam whoami`.
7. **Coverage cannot fall behind.** Every operation is derived, served
   by hand, excluded, or pending in `commands.yaml`, and in exactly one
   of those.
   - `cligen` fails on an operation that is none of them, on a name
     that is no operation, and on two operations deriving one command.
   - `TestAPIOperations_PlaceEveryOperationOfTheSpecification` asserts
     the same in `make ci`.
   - `pending` holds the operations whose commands are enabled a
     resource group at a time (#1632). It only shrinks, and the last of
     those pull requests empties it.

## Consequences

- Everything the console does, a program can do through `garam`, with
  `-o json` to read and exit codes to branch on, once `pending` is
  empty.
- A change to `api/openapi.yaml` changes the CLI with it. Renaming a
  path segment or an operationId renames a command, which a reader of
  the specification's diff sees in `commands.gen.go`'s.
- A command's flags are the API's property names in kebab case. A
  friendlier name costs a `commands.yaml` line rather than code.
- The table a person reads is only as good as the answer's schema; a
  command whose answer declares nothing scalar prints `-o json`'s form
  by default.
- The generated client covers all 80 operations, 19,199 lines of
  generated code.

## Signs the decision was wrong

- Commands keep needing behaviour beyond their table entry, such as
  reading a file to send, waiting for a state, or paging: the
  operations were less regular than measured, and some wanted hand
  code after all.
- `commands.yaml`'s `names` grows towards one entry per operation: the
  derived grammar reads badly, and naming every command by hand is the
  per-operation cost again.
- A secret reaches stdout or a log through a field `commands.yaml` did
  not list: secrets wanted marking in the specification itself, where
  the server's author sees them, rather than in a CLI file.
