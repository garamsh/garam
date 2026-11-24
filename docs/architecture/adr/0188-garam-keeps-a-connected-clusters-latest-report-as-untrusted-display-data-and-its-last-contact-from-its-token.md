# ADR 0188: garam keeps a connected cluster's latest report as untrusted display data, and its last contact from its token

> Status: accepted
> Date: 2026-11-24

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2050, for which the Garam PM reserved this number. The
forks below were put to the PM with a recommendation and agreed on
2026-11-24. The PM also moved the console's cluster pages to the issue
filed after #2050, and the suite's CI criterion to #2047. garam is read
at `992651208811593f16bf7787c53e4305ecd519dd`, and a claim below that
names no revision holds for it.

## Context

[ADR-0184](./0184-a-connected-cluster-is-a-machine-identity-that-registers-once-with-a-one-time-token-and-a-key-it-generates.md)
decision 8 has garam store a connected cluster's latest report.
garam-bot-operator's `interfaces/cluster-connection/v1alpha1/` says
what that report is and what garam owes it (REP-1 to REP-9, at the pin
[ADR-0186](./0186-the-cluster-connection-routes-are-served-from-the-interfaces-own-specification-vendored-at-a-pinned-commit.md)
names):

- a report is the connector's whole view: its connector, the outcome
  of each declaration it last received, and every own object of a
  declared kind;
- garam answers 204, keeps only the report with the latest
  `observedAt`, and records the time of every authenticated request as
  `lastContactAt`;
- a body over 4 MiB may be answered 413 `report_too_large`;
- a Secret, a ConfigMap or an undeclared kind is refused 400
  `invalid_request`;
- an outcome or object naming a declaration not placed on the reporting
  record is ignored, and nothing of it is recorded;
- a cluster-owned `spec`, every `status` and every `message` are
  display data, never acted on and rendered only as text.

garam-bot-operator's ADR 0089 decision 4 adds a courtesy refusal: garam
refuses a declaration whose namespace and name its latest report shows
held by a cluster-owned object. The connector's own `conflict` is what
holds, since the report may be stale.

The interface leaves open where garam keeps a report, which
identifiers count as placed on a record, how often `lastContactAt` is
written, and what the courtesy refusal answers.

## Decision

1. **A record keeps one report, the one observed latest, as garam
   filtered it.** It is one row per record, holding the report as
   JSON, its `observedAt` and when it arrived. A report replaces the
   row only where its `observedAt` is later. One observed at the same
   instant, or earlier, is answered 204 and changes nothing. The row
   goes with its record.
2. **An identifier counts as placed on a record where a declaration of
   that record holds it, live or tombstoned.** An object the connector
   still reports for a tombstoned declaration therefore stays visible
   to the organization. Before the row is written, garam drops:
   - an outcome naming any other identifier;
   - an object naming any other identifier;
   - a garam-owned object naming no declaration, or whose marker names
     another record.

   The report is still answered 204, as the reference server answers
   it.
3. **What the report says is data that garam keeps and answers, and
   never reads.** garam reads the members it checks and the
   identifiers it filters on. It keeps every `spec` and `status` byte
   for byte, and answers them on `getClusterReport` under
   `iam:cluster.read`. A console renders them as text. A member the
   interface does not name is dropped (T-4).
4. **`lastContactAt` is written when a cluster's token resolves, at
   most once a second per record.** The listener reads every bearer
   token on every request (AUTH-2). The query that reads it also
   upserts the record's contact time, and skips the write while the
   kept time is under a second old. A busy record therefore writes at
   most once a second, and a held long poll records its contact when
   it starts. `lastContactAt` is answered as that time, so it can be
   up to a second behind the latest request.
5. **A report body is bounded at 4 MiB, and each cluster's reports at
   120 a minute.** The machine listener bounds every other body at
   1 MiB. The report route's path alone is bounded at REP-6's 4 MiB,
   and past it is answered 413 `report_too_large`. That status is
   carried in the connection's own table of interface codes, as 410
   `cursor_expired` is (`stack-go.md` §4). The rate bound is two a
   second per cluster, far past REP-4's pace, and past it the answer
   is 429 `throttled`.
6. **The courtesy refusal is 409, read in the declaration's write
   transaction.** A declaration whose kind, namespace and name the
   record's kept report shows held by a cluster-owned object is
   refused with `ErrDeclaredObjectHeld`. That covers one placed under
   an identifier garam draws, one placed under an identifier chosen,
   and a next generation moved onto the name. It reads the kept row,
   so a report arriving meanwhile is not waited for: the refusal is
   the courtesy the interface names, and not a guarantee.

## Consequences

- `serve machine` serves `report` (`POST
  /cluster-connection/v1alpha1/clusters/self/reports`), which answered
  501 until now. Every cluster-connection route is now built.
- `serve api` gains `getClusterReport`, and the fixture gains
  `latest-report`, which reads it.
- Migration 000086 adds `cluster_contacts` and `cluster_reports`.
- Every authenticated cluster request can write one row a second.
- The console's cluster pages wait for the issue filed after this one.

## Signs the decision was wrong

- `lastContactAt`'s one-second window shows up as contention on
  `cluster_contacts` rows, or a reader needs a finer time than that.
- Reports of thousands of objects make the courtesy check's
  containment read slow, which would call for keeping cluster-owned
  objects as rows.

## Rejected alternatives

- **Keeping every report, or a history.** ADR-0184 decision 8 keeps
  the latest, and REP-1 makes each report the whole view, so a history
  holds nothing the latest does not, past what it costs.
- **Writing `lastContactAt` on every request.** A long-polling
  connector would write a row per poll and per page. One write a
  second records the same contact to within that second.
- **Counting only live declarations as placed.** The orphan that
  DECL-13 has the connector report would vanish from garam, which is
  where an organization would see it and tombstone it.
- **Keeping cluster-owned objects as rows of their own.** The
  courtesy check is the one reader that needs them by name, and one
  report per record is small enough to search in place.
