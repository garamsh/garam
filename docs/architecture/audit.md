# Audit

The record garam keeps of what it decided, held by garam rather than by
either party it decided between. Implemented by `internal/audit`,
whose PostgreSQL writer is `internal/audit/postgres`.

Pending removal
([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)):
once the second pull request of #1593 removes the operator credential
and enrollment token, no event of either is recorded again; the events
already recorded stay.

**Send events and machine credential events are the events built.**
Decision and change events, which this domain was planned for, are
unbuilt. Send events are read by the deployment's database holder
alone
([ADR-0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md));
credential events are read through `listCredentialEvents`
([ADR-0114](./adr/0114-every-machine-credential-issuance-renewal-and-refusal-is-an-audit-event-read-on-the-subjects-own-read.md)).

| Type | Responsibility |
|------|----------------|
| `Send` | one message send as garam decided it: the two parties, the decision, and for an authorized send the message it stored — never a body |
| `Decision` | `authorized` or `refused` |
| `Credential` | one issuance, renewal or refusal of a machine credential: the subject, the credential, the outcome, and for a certificate its serial and validity — never the credential |

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
- **No recorder interface for sends.** The send writer is a function
  over the caller's transaction, because no consumer holds it as an
  interface and no test substitutes it.
- **Credential events are recorded by the route that answered, after
  the outcome** (ADR-0114). The issuing stores are the operator and
  agent domains', and a refusal writes nothing to join, so the machine
  and browser listeners' handlers write the event through a port each
  declares, which `postgres.CredentialStore` implements. A write that
  fails is logged and the answer stands, so an issuance can go
  unrecorded and a recorded one always happened.
- **A credential event carries no secret material.**
  `audit_credential_events` has no column for a key, token, credential
  or certificate, and a refusal names its error kind and not its text.
- **A credential event is read on its subject's read.**
  `listCredentialEvents` answers one subject's events on
  `operator:read` or `agent:read`, which that subject's own reads
  already take; no action names audit.

## Rationale

[ADR-0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)
records why garam stays the route every message takes rather than
handing delivery to the agents, why the event shares its outcome's
transaction and is the one write a domain makes in another's, and why
the record is kept until its tenant goes rather than for a window.

## Open questions

- **How decision and change events are recorded.** Whether they share
  a table, a writer's shape, or neither, is decided when the first one
  is built; until then this binds nothing.
- **Who reads send events through garam.** No route serves them, and
  no action names permission to read them.
