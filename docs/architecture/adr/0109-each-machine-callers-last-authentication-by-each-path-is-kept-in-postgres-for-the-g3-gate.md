# ADR 0109: Each machine caller's last authentication by each path is kept in Postgres for the G3 gate

> Status: proposed
> Date: 2026-10-18

Drafted for #1494, acceptance criterion 1. That issue reserves this
number, and the PM approved the design on 2026-10-18 in the dispatch
that assigned it. Facts about garam hold at
`28754663e27e20dd43c9451d53829ba112ed23ad`.

## Context

- **G3 retires the certificate path once a gate holds.**
  `docs/plans/2026-10-09-machine-identity-migration.md` asks for
  inventory evidence that each caller cohort completed a new-mode
  exchange, a refresh and a restart, then seven days with no
  old-authenticated request. The cohorts it names are registered but
  idle operators, standalone supervisors, hosted services, legacy agents
  and audience tokens. The evidence must not log an assertion, a bearer
  value or a private byte.
- **The metrics cannot name an idle caller.**
  `garam_machine_requests_total` counts requests by authentication
  method and nothing else. `garam_machine_token_exchanges_total` counts
  exchanges by method and outcome. Neither carries a caller, so a
  caller that sent nothing is absent from both, and absence is what the
  gate has to show. A label carrying the caller's GRN would have one
  series per caller, which is unbounded.
- **What the database already held was partial.**
  `operator_keys.last_exchanged_at` (`migrations/000043`) covers an
  operator's private_key_jwt exchanges alone. Token rows are trimmed to
  the newest five and swept once expired, so they are no history. No row
  recorded a request authenticated by certificate.
- **Each authentication path is decided in one of two places.** The
  listener's `identify` (`internal/machine/listener.go`) decides,
  request by request, whether a certificate or a token proves the
  caller. An exchange or a mint is anonymous at the listener, and its
  handler decides who it issues to. Exchanges are already classified by
  route and credential prefix in one middleware,
  `countTokenExchanges` (`internal/cli/machine_token.go`).

## Decision

**garam keeps one Postgres row per machine caller and authentication
path, holding when the path was first and last seen and, for an
issuance, whether a refresh has been seen. A person reads the G3
inventory with one SQL file.**

- **Paths.**
  - `certificate`: a request the listener authenticated by certificate.
    This is the old path.
  - `token`: a request the listener authenticated by token.
  - `exchange_shared_secret`: an operator's or a supervisor's
    shared-secret exchange.
  - `exchange_private_key_jwt`: an operator's assertion exchange.
  - `agent_mint`: an agent token its assignee minted, recorded under
    the agent.
  - `audience_token`: an exchange or mint whose token names an
    audience, recorded under the token's subject in addition to its
    method path.
- **The table is `machine_authentication_evidence`**
  (`migrations/000052`), keyed by caller GRN and path. It has these
  columns: `first_at`, `last_at`, `issued_until` (when the token the row
  last recorded expires), and `last_refresh_at`. No column holds a
  credential, an assertion, a token or any part of one, and nothing is
  logged beyond the path and the write's error.
- **A refresh is an issuance made while the token the row last recorded
  is still accepted.** ADR-0100 has a refresh leave the older token
  good, so this is the only refresh the server can observe. A caller
  that restarts and exchanges again before its old token expires reads
  as a refresh too.
- **A restart is not observable from the server.** A restarted caller
  presents a fresh exchange like any other. Restart evidence stays with
  joint acceptance (#1495), not with this table.
- **Two recording points, one per place a path is decided.**
  - `identify` notes `certificate` or `token` for every request it hands
    an identity, keyed by that identity.
  - `recordIssuances` (`internal/cli/machine_token.go`) notes exchanges
    and mints. It classifies routes with the same prefix reading as
    `countTokenExchanges` and runs as a sibling middleware. It records
    the subject the handler reported through
    `machine.ReportIssuance`, so a refused issuance records nothing.
  - No handler writes evidence: a handler only reports what it issued.
- **Writes are coalesced and best effort.**
  - `machine.Evidence` writes each caller and path at most once a minute
    per process (`evidenceWindow`), synchronously on the request.
  - On the hot path that costs one upsert of one row per caller, path
    and process each minute, bounded at one second. Every other request
    costs a map lookup under a mutex.
  - A failed write is logged at warn, is not retried within the window,
    and never fails the request.
  - The window applies to issuances too. An exchange within a minute of
    the previous one records nothing, which a gate read in days does not
    notice.
- **Retention follows the tenant rule ADR-0045 and ADR-0106 state.**
  `org_grn` references the organization and cascades, so a deleted
  tenant's evidence goes with it. Nothing references the caller's own
  registration. A caller deleted while its organization remains keeps
  its rows, which are the evidence that it stopped. A sighting whose
  organization garam does not hold writes nothing.
- **The read is `internal/machine/postgres/inventory.sql`.**
  - It returns one row per registered operator, hosted service (an
    operator with a control endpoint), current supervisor and agent.
    Agents are split into `agent` and `legacy_agent` by execution mode.
    Deleted callers that left evidence are included.
  - Each row carries the last time of each path and the last refresh. A
    registered row with every path empty is an idle caller.
  - It is run with `psql -f` and needs no garam process. An integration
    test runs it as it stands.
- **Where it lives.** The seam `machine.Recorder`, the coalescer
  `machine.Evidence` and the paths are in `internal/machine`, which
  still imports no driver. The store and the inventory are in
  `internal/machine/postgres`, and the composition root wires them.

## What this does not decide

- **The gate's verdict.** This records evidence; whether G3 holds is
  read off it by the PM, and the removal is #1494's later criteria.
- **A forwarded certificate.** A controller certificate under
  `operation-authority.v1` and an agent certificate under
  `execution-fence.v1` are not recorded under their own GRN. Only a
  caller that authenticated by certificate may send a v1 value
  (`internal/machineapi/contract.go`), so that caller's `certificate`
  row already shows the old path in use.
- **A read route or a command.** No surface serves the inventory.

## Consequences

- The first request of each caller on each path in each minute waits
  on one upsert, and a slow database delays it by at most a second.
- The table grows with the number of callers, not with traffic: at most
  six rows a caller.
- A process that never sees a caller writes nothing for it. The
  inventory is the union of every replica's writes, because the rows
  are shared.
- `last_refresh_at` cannot tell a refresh from a restart that
  exchanged before its old token expired.

## Signs the decision was wrong

- The upsert shows in the listener's latency, which would argue for an
  asynchronous writer.
- G3 needs a distinction the paths do not draw, for example
  shared-secret exchanges by operator versus by supervisor beyond what
  the GRN's service type already shows.
- A reader needs the inventory without database access, which would
  argue for a command or a route.

## Rejected alternatives

- **A metric with the caller's GRN as a label.** Unbounded
  cardinality, and a series that has stopped incrementing still does
  not name a caller that never sent anything.
- **A metric with bounded labels alone**, cohort by path. It cannot
  name an idle caller, which is what the gate exists to find.
- **Reading token tables for the inventory.** They are trimmed and
  swept, and no table records a certificate request.
- **Writing evidence in each handler**, or in each domain's exchange
  transaction. That puts a write in every place that issues instead of
  in the two places a path is decided, and it reaches
  `internal/agent`'s supervisor exchange and agent mint, which are
  outside this change.
- **An asynchronous writer.** It needs its own lifecycle and shutdown
  drain to save a write the window already bounds to once a minute per
  caller.
- **A `garam` command printing the inventory**, like
  `cutover-inventory`. The PM asked for a read a person can run, and a
  SQL file needs no release to change.
