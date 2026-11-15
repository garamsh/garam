# ADR 0166: A cowork create or delete answers after the policy service holds the change

> Status: accepted; built in `cowork/internal/policyfeed`,
> `cowork/internal/cli`, `cowork/internal/api` and
> `cowork/internal/mcp` (#1919); applies the pattern of
> [ADR-0165](./0165-every-request-that-changes-a-fed-entity-answers-after-its-delivery-by-the-marks-its-own-write-raised.md)
> to cowork, narrowed to the requests that create or delete an entity
> and not those that update one
> Date: 2026-11-15

Drafted for #1919, which reserves this number.
[authz.md](../authz.md) §cowork's principal and
[cowork.md](../../../cowork/docs/cowork.md) §Running it state the
result as built.

## Context

cowork feeds its channels, pages, tasks and artifacts to the policy
service through the outbox of migration 000006, delivered by a loop
that polls every second (ADR-0140). A create answers 2xx as soon as its
row is committed, and the person's next request, a read of what was
created, asks the policy service about an entity it does not yet hold.
The `tenant-isolation` policy faults on an absent entity, and cowork
maps a fault of that policy to "not a member of this organization"
(403). The policy service records the denial as a `fault`. This was
measured on a local stack: a create followed at once by a read is
denied, and the same read a second later is allowed.

What the service holds of a cowork entity is read from the feed, not
from the handlers: an `Upsert` of the entity's type and identifier with
the organization as its only parent, and a `Delete`. The triggers mark
the row's own entity alone, and only while the feed is on.

- An update re-sends an entity identical to the one the service holds,
  because no attribute of it changes with a title, a status, an
  assignee or a page's parent.
- A delete is read-your-writes too: until the service drops the
  entity, a policy still allows an act on it, and ADR-0165 makes the
  platform's deletes wait. A page's delete cascades to its children by
  foreign key, and their marks are raised in the same transaction.
- No route moves a fed entity between organizations: every key is
  `(org, id)`, no query sets `org`, and the only move is a page's
  reparenting within its organization, which is an update here.
- A write through a page's sync connection touches `page_updates` and
  `page_snapshots`, neither of which is a fed table. It answers no
  request.
- Messages, comments and members are not fed tables.
- cowork's four policy settings are optional: where they are unset
  there is no service to wait for.

## Decision

1. **A create waits on the mark of the entity it created.** The
   services of pages, channels, tasks and artifacts are wrapped, where
   a policy client is configured, by one that calls the inner `Create`,
   and, if it succeeded, waits until the outbox holds no mark of the
   created entity. The wait is after the commit and holds no
   transaction. The wrapper sits under both the HTTP routes and the MCP
   tools, so each answers alike. A create that failed answers its error
   at once, and where no client is configured nothing waits.
2. **The wait wakes the loop.** The gate asks the delivery loop to
   start a pass now, and reads the mark every 50 ms up to a bound of
   5 seconds, the bound of ADR-0159. The loop's own poll stays, so a
   request that waits does not become the only thing that delivers.
3. **A delete waits on the marks of every entity it deleted.**
   Deleting a page waits, like a create, on the page's own mark and on
   the mark of each page beneath it, which go with it by foreign key
   and raise marks in the same transaction, as ADR-0165 item 3 has a
   cascading delete wait on what it raised. The page store names the
   pages the delete removed, read under the tree lock the delete
   holds, and `Delete` returns them, the page first. The wrapper waits
   on each in turn with the gate's bound, and the first to pass it
   answers 503 naming that page. It is the one route that deletes a
   fed entity: a channel, a task and an artifact have none, a
   channel's leaving and a task's links are not fed tables.
4. **What does not wait.** An update, a read and a write through a
   page's sync connection answer at once: an update re-sends the
   entity the service holds, and a sync write raises no mark, answers
   no request, and has no caller to wait. This is narrower than
   ADR-0165, which waits for every route whose write raises a mark.
   An entity that gains an attribute the policy reads changes this:
   the route that writes that attribute waits on the entity's mark, as
   ADR-0165 says.
5. **At the bound the caller is answered unavailable, and told what it
   stored.** The answer is 503, kind `unavailable`, reason
   `change_not_applied`, and `resource` the stored entity's GRN. The
   caller reads a created entity and does not create it again; after
   a delete the page is gone and the call is not repeated. `writeError` discloses
   the message, the reason and the resource for this reason alone;
   every other 5xx stays "internal error". An MCP tool answers that
   the entity was created as that GRN, that it may not be readable for
   a moment, and that it is to be read and not created again. No tool
   deletes. The mark
   stays, so the loop goes on delivering the entity.
6. **A refused entity is not set aside.** The service refusing an
   entity has a `Delete` sent and its mark dropped (ADR-0140 item 11),
   so the wait ends with the entity not held and a read is denied.
   cowork has no link to set aside, so it has no `change_refused`
   reason and no table recording a cascade's marks, since the page
   store names the pages itself: its only reason is
   `change_not_applied`.
7. **The mark is read by one query.** `policyfeed.Marks` has one
   method, whether the outbox holds a mark of an entity, and the
   Postgres store implements it beside `Store`. The generation is not
   read: a create's mark is raised once, and an entity is created once.

## Consequences

- A create is answered after the policy service holds its entity, so a
  read that follows it is evaluated against the entity, and a delete
  after the service drops it, so an act on the page is denied. The `fault`
  denial of a read after a create is gone from the audit.
- A create or a delete takes up to one delivery longer, and up to 5 seconds when
  the policy service is down. A request that waited holds a connection
  and no transaction.
- The unavailable answer is the first cowork 5xx that carries a
  reason, and `apigen.Error` gained optional `reason` and `resource`.
  The four create operations and `deletePage` declare a `503`.
- A caller that updated an entity and reads it at once is decided on
  the entity the service already holds, which is the same.
- A page delete waits once per page it removed, each a read of the
  mark that returns at once when the page is delivered, so a large
  subtree costs a query per page and, past the bound, the first page
  not delivered answers.
- An update to a fed entity that gains an attribute, and the platform
  asking the policy service about cowork's entities in #1907, make
  items 4 and 6 the first to re-examine.

## Signs the decision was wrong

- A denial recorded as a `fault` for an entity cowork holds, after an
  update answered 2xx: an attribute the policy reads
  changed with it, and that route must wait.
- A create answered 503 `change_not_applied` while the delivery loop
  is running and the policy service is up: the loop's pass is slower
  than the bound, and the bound or the wake is wrong.
- A deleted page that the policy service still holds after its delete
  answered 204: the wait did not reach the delete.
- A caller creating an entity twice after a 503 answer: the resource
  did not reach it.
- A request that waits and is answered after its entity was refused and
  deleted at the service, with the caller shown the entity: item 6
  needs a set-aside.
