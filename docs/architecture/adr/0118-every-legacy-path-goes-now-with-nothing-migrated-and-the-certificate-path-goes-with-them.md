# ADR 0118: Every legacy path goes now with nothing migrated, and the certificate path goes with them

> Status: accepted
> Date: 2026-10-19

Drafted for #1593's fifth pull request, for which the Garam PM reserved
this number on 2026-10-19. The direction is the owner's of that day,
"레거시 전부 없애라고" ("remove all legacy"), recorded by the PM on
#1593 at 2026-10-19T11:20:00Z and on #1494 at 2026-10-19T11:20:04Z.
The PM decided this record's placement on #1593 at
2026-10-19T11:22:30Z, and the constraints on what the certificate
path's removal keeps on #1494 at 2026-10-19T11:21:54Z. garam is read
at `57c46e8a9638b2c3c555ea5f8d57c78b8423406e`; a claim below that
names no revision holds for it.

## Context

- **ADR-0117 gated legacy execution mode on aurora's move.** Its item
  5 removed legacy mode, the staged cut-over, `garam
  cutover-inventory` and legacy definitions only after #1594 moved
  aurora's one running agent,
  `grn:garam:default:agent:144460a71649d1a1`, to a new managed agent
  with its memory copied, and after that agent was accepted.
- **The certificate path's removal was gated on seven days.** The
  migration plan, `docs/plans/2026-10-09-machine-identity-migration.md`,
  and [ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
  ask for seven days with no old-authenticated request from every
  known cohort before G4 removes it.
- **What aurora's evidence showed.** The PM read
  `machine_authentication_evidence`
  ([ADR-0109](./0109-each-machine-callers-last-authentication-by-each-path-is-kept-in-postgres-for-the-g3-gate.md))
  on aurora read-only and recorded it on #1494: every caller has
  authenticated by `token` since 2026-10-19T00:44:37Z, the
  `certificate` path was last used at 00:43:57Z, and aurora runs no
  supervisor. The window would have ended at 2026-10-26T00:44:37Z.
- **The owner directed that all legacy go now and nothing be
  migrated.** Before any deletion, the PM took a full read-only
  `pg_dump` of aurora's garam database and holds it privately. The
  agent's state volumes are `Retain`, so its memory outlives the agent.
- **Two things outside garam read what the certificate path's removal
  touches**, as the operator's PM reviewed it on #1494: the Operator's
  v0.2.0 manager refuses an agent-token mint answer whose
  `serverRootPem` is empty, and it logs and retries, without failing
  readiness, when the machine API's definition feed is gone.
- **The authorities' private keys sit sealed in Postgres.** Each row
  of `certificate_authorities` holds a key sealed under the
  key-encryption key. A dropped row cannot be made again, and the
  key-encryption key stays in use for message signing
  ([ADR-0104](./0104-message-signing-keys-may-be-held-by-an-openbao-transit-provider-and-the-file-key-encryption-key-stays-the-default.md)).

## Decision

**Every legacy path goes now, with nothing migrated, and the
certificate path goes with them.** Two pull requests carry it, in this
order.

1. **#1593's fifth pull request runs now.** It no longer waits on
   #1594. It removes:
   - legacy execution mode, so every agent's writes wait for an
     activation, and the execution fence admits none without the
     activation's token;
   - the staged cut-over and `garam cutover-inventory`;
   - the machine API's definition feed and claim (`GET /definitions`
     and `POST /definitions/{agent}/claim`), `createAgent`
     (`POST /agents`) and `issueAgentCertificate`
     (`POST /agents/{agent}/certificate`);
   - every agent definition, with the browser's definition surface:
     the definition reads and their rename, `agent:rename`, the
     search's match on a definition's name, and the deletion preview's
     count of definitions. garam then holds no name for an agent, and
     the console shows an agent by its identifier.

   Migration 000058 deletes the agents whose execution is in legacy
   mode, with their definitions, and the rows that depend on them go
   with them by cascade. Messages stay. The migration refuses, with a
   named exception, a definition or a cut-over of an agent not in
   legacy mode, and a delegation holding no operation but the
   cut-over. On aurora it deletes `144460a71649d1a1`, and nothing
   replaces it.
2. **G4 (#1494) runs after it, without waiting for the seven days.**
   It removes client-certificate authentication on the machine
   listener, garam's certificate authorities and their signing,
   `garam issue-certificate`, the issuance and renewal of operators',
   supervisors' and agents' certificates, the v1 certificate
   contracts, and the certificate path in what writes
   `machine_authentication_evidence`. A supervisor keeps the token
   path.
3. **G4 keeps three things.**
   - **The listener's own server TLS and its serving chain.**
     `GARAM_MACHINE_TLS_CERT_FILE`, `GARAM_MACHINE_TLS_KEY_FILE` and
     `GARAM_MACHINE_SERVER_ROOT_FILE` stay as they are.
   - **`serverRootPem`**, in the agent-token mint answer and wherever
     else a holder pins garam by it. Only `issuerPem` and the issued
     certificate itself go.
   - **The table of sealed authority keys and its rows**, in place and
     unread, with a comment saying why. Dropping them is irreversible,
     and that is the owner's separate decision. The key-encryption key
     stays.
4. **G4 implements this record** and needs no record of its own.

This supersedes:

- [ADR-0117](./0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)
  in part: item 5's gate, the move of aurora's agent to a managed agent
  and its acceptance before the removal;
- [ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)
  in part: the seven days without old authentication that gate the
  certificate path's removal;
- [ADR-0015](./0015-operator-identity.md) in part: an operator
  obtaining an agent's certificate from garam;
- [ADR-0016](./0016-ca-key-custody.md),
  [ADR-0032](./0032-garam-ships-the-command-that-mints-an-operators-first-certificate.md)
  and
  [ADR-0046](./0046-an-operator-renews-its-certificate-by-presenting-the-one-it-holds.md),
  already superseded by
  [ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md),
  in the certificates they decided, which go from #1593's fifth pull
  request on;
- [ADR-0026](./0026-the-console-defines-an-agent.md) and
  [ADR-0033](./0033-an-operator-claims-the-agent-a-definition-mints.md),
  in what ADR-0077, ADR-0090 and ADR-0100 left of them: the definition
  garam holds, its read and its claim;
- [ADR-0045](./0045-an-organization-registers-an-operator.md) in part:
  the definition naming a registered operator;
- [ADR-0070](./0070-an-organization-deletes-an-operator-and-the-record-it-withdraws-is-what-that-operator-authenticated-on.md)
  in part: the refusal to delete an operator a definition names;
- [ADR-0075](./0075-an-agent-is-started-from-a-template-copied-once-and-carries-a-name-of-its-own.md)
  in part: the agent's own name and its rename.

ADR-0100 stands: retiring the authorities is what G4 performs, and the
sealed rows it leaves are the owner's to drop. ADR-0109 stands: its
record is what the decision read.

## Consequences

- garam holds one execution mode and no agent definition, and an agent
  is created only through a control service or a standalone host.
- Aurora loses its one running agent. It is not replaced, its Agent
  resource and StatefulSet are removed outside garam, and its memory
  stays on its retained volumes.
- An agent carries no name in garam. The console names none, and an
  agent is told from another by its identifier.
- A deployment still authenticating by certificate loses the listener
  at G4 with no window. garam knows of none, and promises nothing across
  versions
  ([ADR-0049](./0049-neither-published-specification-promises-anything-across-versions.md)).
- The authorities' sealed keys stay in Postgres until the owner decides
  their drop, so a database dump still carries them under the
  key-encryption key.
- Until G4 lands, the responsibility documents describe the
  certificate path as live, with its removal decided and pending.

## Signs the decision was wrong

- A caller authenticates by certificate between this record and G4's
  release: the evidence read on 2026-10-19 was a day old, and the seven
  days were what would have caught it.
- Aurora's agent is wanted back with its GRN: the deletion is undone
  only from the PM's dump, which is what the managed-agent move kept
  from being needed.
- An operator or a person needs a name for an agent to tell agents
  apart: the name went with the definition and wanted a home of its
  own first.
- A tool outside garam pins garam by `issuerPem` and not by
  `serverRootPem`, and fails at G4.
