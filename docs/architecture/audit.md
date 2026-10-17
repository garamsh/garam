# Audit

The record garam keeps of what it decided, held by garam rather than by
either party it decided between. Implemented by `internal/audit`,
whose PostgreSQL writer is `internal/audit/postgres`.

**Send events are the only events built, and nothing reads them
through garam.** Decision and change events, which this domain was
planned for, are unbuilt; the deployment's database holder is the only
reader until a read route and the action that governs it are decided
([ADR-0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)).

| Type | Responsibility |
|------|----------------|
| `Send` | one message send as garam decided it: the two parties, the decision, and for an authorized send the message it stored — never a body |
| `Decision` | `authorized` or `refused` |

## Current decisions

- **One event per send garam decides, written in the transaction of
  its outcome.** The messaging domain's store calls
  `postgres.RecordSend` inside the transaction that stores the message,
  stores garam's refusal of a crossing, or — for a policy's denial,
  which stores nothing — holds the event alone after the execution
  fence. A send whose outcome rolls back leaves no event, and a request
  refused before any decision writes none
  ([messaging.md](./messaging.md)).
- **No event carries a body.** `audit_send_events` has no column for
  one.
- **Kept until a tenant goes.** No sweep reaches the table. Each
  party's organization is referenced, and deleting either deletes every
  event naming it; a party whose organization garam does not hold is
  referenced by nothing and still recorded.
- **The event is closed over its fields.** `Authorized` and `Refused`
  are the only constructors, so an authorized send without a message
  and a refused one with a message cannot be built. That is an
  invariant over the fields, which is why this aggregate keeps
  accessors where ADR-0069's data carriers do not.
- **No recorder interface.** The writer is a function over the
  caller's transaction, because no consumer holds it as an interface and
  no test substitutes it.

## Rationale

[ADR-0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)
records why garam stays the route every message takes rather than
handing delivery to the agents, why the event shares its outcome's
transaction and is the one write a domain makes in another's, and why
the record is kept until its tenant goes rather than for a window.

## Open questions

- **How decision and change events are recorded.** Whether they share
  this table, its writer's shape, or neither, is decided when the first
  one is built; until then this binds nothing.
- **Who reads the record through garam.** No route serves it, and no
  action names permission to read it.
