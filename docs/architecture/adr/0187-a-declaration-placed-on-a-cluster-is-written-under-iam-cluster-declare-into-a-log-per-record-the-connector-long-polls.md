# ADR 0187: A declaration placed on a cluster is written under iam:cluster.declare into a log per record the connector long-polls

> Status: accepted; superseded in part by
> [ADR-0192](./0192-every-system-that-connects-to-garam-is-an-app-installed-in-an-organization-and-each-running-copy-is-an-instance-with-its-own-key.md)
> (decision 1)
> Date: 2026-11-24

Append-only: once merged, the body below is not rewritten. A fact later
found wrong is corrected in an appended note, not edited out; a revised
decision is a new ADR that supersedes this one.

Drafted for #2048, for which the Garam PM reserved this number. The
action fork was put to the PM with a recommendation and agreed on
2026-11-24, as was moving the refusal read off the latest report to
#2050. garam is read at
`617686ff296df67b22c23699555310949f00861d`, and a claim below that
names no revision holds for it.

## Context

[ADR-0184](./0184-a-connected-cluster-is-a-machine-identity-that-registers-once-with-a-one-time-token-and-a-key-it-generates.md)
decision 8 has garam store the declarations placed on a connected
cluster, their tombstones, and how far those tombstones have been
purged. Its decision 9 lists the record's actions, and none of them
places a declaration.

garam-bot-operator's `interfaces/cluster-connection/v1alpha1/` says
what the connector reads (DECL-1 to DECL-9, at the pin ADR-0186 names):

- a long poll on `GET …/clusters/self/declarations`, held up to 50
  seconds;
- with no cursor, the full set of live declarations, then the changes
  after the position that set began at;
- each change carrying an identifier, a generation one past the last,
  and on an apply the whole object;
- a cursor at or before the position the log was purged through,
  answered 410 `cursor_expired`, with tombstones kept 30 days at least;
- a declared object of four kinds only, naming no image and no
  secret's value.

The write lands on `serve api`, and the poll is held by `serve
machine`, so the wake crosses processes, and replicas of each.
[ADR-0027](./0027-a-watch-crosses-processes-through-postgres.md)
already carries a watch between processes through Postgres's
LISTEN/NOTIFY.

## Decision

1. **Placing, changing and tombstoning a declaration is
   `iam:cluster.declare`, decided on the organization.** The owner and
   the administrator hold it, and a member does not: what a cluster
   runs is the organization's administrative act, as standing the
   record up is. Reading what is placed is `iam:cluster.read`. This
   adds a sixth action to ADR-0184 decision 9's list, under that
   decision's reason for naming each.
2. **Each record keeps a log of its changes, at positions drawn one at
   a time.** A write holds the record's row, draws the next position,
   stores the declaration at it and appends the change, in one
   transaction. Positions therefore commit in the order they were
   drawn, and a cursor is a position: `c.<n>` reads the log after `n`,
   and `f.<base>.<sent>` pages a full set that began at `base`.
3. **A change is kept 30 days, and the record keeps the position it
   was purged through.** Each write deletes its record's changes older
   than that and moves the purged position to the last one deleted. A
   cursor at or before it is answered 410, whether or not a tombstone
   it covered remains. A full set reads the live declarations, which a
   purge never touches.
4. **An identifier names one declaration, once, across every record.**
   garam draws one where the writer names none. A tombstoned
   declaration's identifier is refused 409, and so is one placed on
   another record. The reservation outlives the record and its
   organization, so a report or an object's marker naming an
   identifier can mean no other declaration.
5. **garam refuses at the write what the connector refuses, with
   interface 1's own parser, and stores the object as it admitted
   it.** `manifest.Parse` of `github.com/garamsh/garam-bot/api`, at the
   version `platform/go.mod` pins, is the check, so garam never
   stores or sends an object the connector would hold as invalid
   (DECL-12), a secret's value among them (DECL-8). An object it
   refuses, or one with no namespace, is answered 422 naming the field.
   The object keeps DECL-7's members alone, its numbers as written. Two
   live declarations of a record never name one object.
6. **A write wakes the polls held on its record through Postgres.** The
   write's transaction sends a notification naming the record, and
   each `serve machine` process holds one LISTEN connection and wakes
   the polls it holds on that record. A held poll also reads again
   every five seconds whatever it was told, so a notification lost to a
   reconnect costs at most that long.
7. **Each cluster's polls are bounded, 300 a minute,** keyed on the
   record rather than the address. That allows three pages a second
   through a full set, far past a held poll's pace.

## Consequences

- `serve api` gains five routes under
  `/orgs/{org}/clusters/{cluster}/declarations`, and `serve machine`
  serves `watchDeclarations`, which answered 501 until now.
- The identifier table grows by one row per declaration ever placed,
  and nothing removes a row. An identifier an organization chose names
  it to any organization that picks the same one later, as a 409 that
  says no more than that.
- The refusal of a declaration whose namespace and name the latest
  report shows held by a cluster-owned object needs reports, so it is
  built with them (#2050).
- The console's pages for declarations wait for the console issue
  filed after #2050.

## Signs the decision was wrong

- A record's write rate makes holding its row the bottleneck, which
  would show as declaration writes queueing on one cluster.
- Five-second rechecks across many held polls load the database more
  than the wakes save. Raising the interval is then the first move.
- An organization needs a member to place a declaration without the
  administrator's other actions, which would show as owners granting
  `iam:cluster.declare` by hand.
- The name-based DECL-8 and DECL-9 checks refuse a field interface 1
  defines, or admit one, once that interface is written.

## Rejected alternatives

- **Deciding a declaration on `iam:cluster.update`.** It needs no new
  action, but it would let anyone who may rename a record also decide
  what runs on the cluster, which are two decisions.
- **Identifiers unique per record.** The connector sees only its own
  record, so that would suffice for the wire. It would let an
  identifier mean two declarations over garam's history, and a report
  naming one could then be read against the wrong one.
- **A log alone, the full set derived from it.** It saves a table, but
  a purge would then lose the declarations the full set must still
  answer.
- **Polling the database on every held poll, with no wake.** It needs
  no listener, but every connector would then see a write only as fast
  as the polling interval, and the database would answer every held
  poll on that interval.
