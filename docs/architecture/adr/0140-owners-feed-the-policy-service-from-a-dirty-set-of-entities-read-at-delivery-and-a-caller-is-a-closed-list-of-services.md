# ADR 0140: Owners feed the policy service from a dirty set of entities read at delivery, and a caller is a closed list of services

> Status: accepted; the policy service's half is built, and the
> platform's outbox and delivery are not
> Date: 2026-10-31

Drafted for #1689, which reserves this number and holds the rulings
this records. It adds to
[ADR-0138](./0138-a-caller-of-the-policy-service-presents-a-short-lived-signed-token-and-owners-write-entities-through-a-sequenced-rpc.md)
items 5 and 7 and supersedes one sentence of it, on when the issuer
lands. garam is read at `7f341cdbd2f19c09968f1bc8ce355881f65de112`; a
claim below that names no revision holds for it.

## Context

- **What a caller may ask was owed.** `crates/server` verified a
  token and kept its `sub` as a string nothing read, so any holder of
  a valid token could ask anything. The review of #1729 owed this to
  slice 3, before a second caller exists.
- **Most of what changes an entity is not a store call.** `DeleteOrg`
  is one statement, and the schema's cascades take the rest
  (`organizations.md`). Users are deleted by another domain, and
  `group_members.member_grn` and `project_members.member_grn` carry no
  foreign key, so a deleted user or agent leaves those rows behind. An
  outbox written from the Go stores would see none of that.
- **An entity's document spans rows.** A `User` is all of that user's
  parents, across `org_members`, `group_members` and
  `project_members`. Two transactions adding one user to two teams
  each read the other's row as absent under `READ COMMITTED`, so a
  document taken in each would hold one team, and the higher sequence
  would erase the other.
- **Every deployment today runs no policy service.** An outbox that
  fills, or a loop that fails, is a defect in all of them, and they
  gain nothing from either.
- **`ApplyEntityChanges` sits behind the token interceptor,** and the
  platform has no signer. ADR-0138 put the issuer in slice 4, so a
  delivery loop in slice 3 could not have called the RPC.

## Decision

**A caller is one of a closed list of services, read from its token's
`sub`, and that list is `platform` alone until cowork calls. The
platform feeds the service from a set of dirty entities with no
payload, written by triggers and off by default. It reads each entity's
state at delivery, under one shared sequence.**

1. **The list.** `sub` is parsed at the boundary into a closed `Caller`
   enum. Any other `sub` is refused with `PERMISSION_DENIED` however
   well its token verifies. Slice 5 adds cowork: its `sub` will be its
   ADR-0129 principal, it will own `Channel`, `Page`, `Task` and
   `Artifact`, and it will ask `cowork:*` actions on resources of that
   organization. That row is not built, having no caller to prove it.
2. **Per-type ownership.** `platform` owns `Org`, `OrgMembers`, `Team`,
   `Project`, `User`, `Agent` and `Operator`, and asks anything, since
   cowork's checks reach the engine through it (ADR-0127 §5). `Policy`
   is no caller's: `grant` writes it, in slice 4. A test fails on a
   schema type that is neither owned nor stated as no caller's.
3. **`ApplyEntityChanges`.** A request holds at most 1000 changes. One
   naming a type the caller does not own refuses the call and writes
   nothing, being the caller's mistake. Any other fault refuses its own
   entry, and the rest are written in one transaction, as `Evaluations`
   answers a question at fault in its own entry. A sequence must be
   above zero, because zero is what an unset field reads as.
4. **A question still carries no entity.** ADR-0138 item 7 refused
   `Resource.properties` until a caller could be shown to own a type.
   Now one can, and the refusal stands: an owner writes through this
   RPC, so a decision reads only what the store holds.
5. **The outbox is a set, not a log.** `policy_outbox` holds one row
   per entity: its type, its id and a generation, with those two as
   the primary key and a write raising the generation. It holds no
   document. Row triggers on `orgs`, `groups`, `projects`,
   `org_members`, `group_members`, `project_members`, `agents`,
   `operators` and `users` mark the entities a row change reaches, in
   the transaction making it, whichever writer that is, a cascade
   included. A role or a name reaches no entity, so an update marks
   nothing.
6. **State is read at delivery, through a declared SQL function,** as
   `agent_execution_admit` is called across domains (ADR-0080). What is
   delivered is what the owners' tables say then, which is what makes
   the race in §Context unreachable.
7. **One Postgres sequence, taken at delivery.** A delivery pass is one
   transaction: lock the feed's row with `FOR UPDATE SKIP LOCKED`, so
   one replica delivers; read up to 500 marks with their generations;
   take the next sequence; read the state; call the RPC under a
   deadline; delete each mark still at the generation read; commit. A
   write landing mid-pass raises its generation, so its mark survives
   and goes out at a higher sequence, and a crash redelivers at a
   higher one. This meets ADR-0138's monotonic sequence, and its
   "per-entity counter" is met by this one shared counter.
8. **Off by default.** `policy_feed` is one row with `enabled` false.
   While it is, each trigger returns before writing, so the outbox
   stays empty and takes no lock. The loop runs in `serve api` only
   when `GARAM_POLICY_ADDR` is set, which then requires
   `GARAM_POLICY_CA_FILE` and the signing key's file. A failing loop
   backs off to a cap, logs on the transition and stays out of
   readiness (ADR-0029).
9. **The bound needs no process.** The primary key holds the outbox to
   the entities written since the feed started, with no sweeper and no
   loop, for an outage of any length. `serve api` logs an error at
   start if the feed is on and nothing delivers it.
10. **A later deployment starts by a backfill.** `garam policy-feed
    start` is one transaction: it takes `SHARE` locks on the source
    tables, which waits out any writer that read the feed as off, sets
    `enabled`, and marks every live entity. `stop` turns the feed off
    and empties the outbox.
11. **An entity the service refuses is deleted from it.** The feed
    sends that entity's delete at a higher sequence and logs an error,
    so a bad entity denies rather than decides on what it last said,
    the direction ADR-0137 wants.
12. **The issuer moves to slice 3's delivery pull request.** It is a
    minimal signer: `sub` `platform`, `aud` `policy`, at most 300
    seconds, under ADR-0138's profile. This supersedes ADR-0138's
    sentence placing it in slice 4, since a loop with no token would
    ship unreachable. The rest of ADR-0138 stands.

The conversion of stored IAM documents is not decided here. Whether a
live deployment holds a policy is unread, and how an `Allow` statement
reaches Cedar under ADR-0130 §4 is the owner's, before slice 4.

## Consequences

- Restoring the platform's database rewinds the sequence, so the store
  would drop every later write as already seen. A restore therefore
  means rebuilding the store.
- `start` emits no deletes. A store that lived through a window with
  the feed off keeps what was deleted in it, so a restart after `stop`
  begins from an emptied `entities` table. The rebuild in the point
  above is the same act.
- `start` blocks writes to the source tables for its enumeration.
- While the feed is on, two transactions marking the same entities in
  opposite orders can deadlock. Postgres aborts one, and that request
  fails. No deployment with the feed off meets this.
- A store is eventually consistent with its owners, by the time a mark
  waits. It fails closed for an addition: a user delivered before its
  team gains nothing until the team arrives, as ADR-0138 item 8 reads
  it. A removal waits for delivery, which is what ADR-0130 §Signs
  watches.
- Intermediate states are never delivered, so the store is no history
  of who belonged where. The owners' tables hold no such history
  either.
- The platform gains a signing key a feature it turns on reads, ahead
  of slice 4, with ADR-0138's costs.
- A token's `sub` is now a fixed string a test fixture and the issuer
  must agree on, and a new caller is a code change here.

## Signs the decision was wrong

- Marks deadlock often enough that requests fail while the feed is on,
  so writes want an append-only log after all.
- `start`'s locks are felt, because the enumeration outgrew a pause
  writers can absorb.
- A store is found stale after a stop or a restore no one rebuilt it
  from, so a rebuild has to be something the feed does for itself.
- A deployment wants a caller's scope to vary, so a closed list keeps
  being rebuilt to change one row.
- A consumer needs the changes themselves rather than the state they
  left, which a set of dirty entities cannot answer.

## Alternatives rejected

- **An outbox of documents, written with each change.** The race in
  §Context, unless a per-entity row lock serialises writers, which then
  deadlock over a cascade's order with a table in every deployment
  whether or not one delivers.
- **An outbox written from the Go stores.** It misses every cascade.
- **An append-only outbox with compaction.** It takes no row lock, and
  needs a process running to hold any bound, where the primary key
  holds one with nothing running.
- **Writing marks whatever the feed's state.** Every deployment would
  pay locks and rows for a service it does not run, and a backfill
  starts a later one anyway.
- **A lease the loop renews instead of a flag.** It bounds a forgotten
  feed, which the primary key already does, and adds a state where the
  feed lapsed by itself, with deletes nobody saw.
- **Disabling the triggers with `ALTER TABLE`.** It reads nothing per
  write, and hides a feature's state in the catalog behind a table
  owner's privilege.
- **A sequence per entity, kept in a counter table.** It is the row
  lock above again, for no property a shared counter under one
  deliverer lacks.
- **A `cowork` row now.** Nothing could prove it (`simplicity.md`).
- **Configuring callers rather than listing them.** A type's owner is
  a fact about the schema in this repository, and no deployment varies
  it.
