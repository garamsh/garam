# ADR 0106: Every message send is recorded as an audit event in the transaction of its outcome, and garam stays the only route

> Status: proposed
> Date: 2026-10-18

Drafted for #1216, which reserves this number and records the PM's
decision of 2026-10-07, under the owner's delegation, to keep routing
every message through garam and record each send as an audit event.
Facts about garam hold at `af6b783cbcd6664d0d665a944300d946afc84a68`.

## Context

- **Every message passes through garam**
  ([ADR-0013](./0013-agent-messaging.md)), and the machine listener
  authorizes each post when it is made: inside one organization the
  policy engine decides
  ([ADR-0024](./0024-policy-decides-what-an-agent-sends.md)), and
  across a boundary the consent relation alone does
  ([ADR-0035](./0035-a-cross-organization-message-needs-both-organizations-consent.md)).
  A post the policy denies is answered 403 and stores nothing; a post
  no consent carries stores a refusal addressed back to its sender.
- **The only record of a send is the queue's row, and it is kept for
  space.** A completed message keeps a small row without its body, and
  an undelivered one is deleted after thirty days
  ([ADR-0034](./0034-a-delivered-message-keeps-its-key-and-drops-its-body.md)).
  A send the policy denied leaves no row at all. The agents' own stores
  are self-reported, and they go when an agent is deleted or its
  storage moves.
- **There was no audit seam to route a send through.**
  `docs/architecture/domains.md` §Future domains planned
  `internal/audit` for decision and change events, and no package or
  table held one: the tree had no `internal/audit` directory, and no
  file under `migrations/` contained the word audit.
- **A message write by an identity that is not an agent ran outside a
  transaction.** `internal/message`'s Postgres store wrapped a write in
  a transaction only to run the agent execution fence
  ([ADR-0080](./0080-an-agents-execution-row-is-the-one-lock-its-writes-and-replacements-share.md)),
  and ran an unfenced write on the pool.

## Decision

**garam stays the only route a message takes, and every send it
decides writes exactly one audit event, in the transaction that writes
the send's outcome.**

- **What is a send.** One post request garam decides on, by policy or
  by consent. A retry is re-authorized when it is made, so it is a send
  of its own, and its event names the message its key already stores.
  A request that never reaches a decision writes no event: one that is
  malformed, one the fence refuses, one the engine cannot answer.
  Neither does a durable submission its budget refuses
  ([ADR-0083](./0083-a-durable-reply-is-admitted-under-a-fixed-budget-from-its-creation-and-its-key-is-answered-after-it.md)),
  or one whose key holds another message: those store nothing, so there
  is no outcome for an event to describe.
- **Which transaction.** An authorized send's event commits with the
  message it stores or the one its key already stores. A crossing no
  consent carries commits its refused event with the refusal garam
  writes back to the sender. A policy's denial stores nothing, so its
  event is the transaction's only write, run after the fence the post
  would have run. A message write therefore always runs in a
  transaction now, fenced or not.
- **What an event holds.** The sender's GRN, the recipient's GRN, the
  decision (`authorized` or `refused`), the database's clock when the
  event was written, and for an authorized send the identifier of the
  message and when the queue stored it. A refused send names no message:
  the one a crossing stores is garam's refusal, not what was sent. The
  basis of the decision is not stored, because the two GRNs carry it:
  the same organization was decided by policy, two organizations by
  consent.
- **No event carries a body.** The table has no column for one, and
  nothing about body retention changes: a body lives only as long as
  ADR-0034 already keeps it.
- **How long it is kept.** No sweep reaches the record: it exists to
  outlive the agent and the queue's thirty-day window. It does not
  outlive its tenant. Each party's organization is referenced by the
  event, and deleting either one deletes every event naming it — the
  rule ADR-0045 states for operators, "a tenant's rows go with the
  tenant", whose erratum of 2026-09-01 records that it is not a rule
  over every table a tenant keys; it is this table's. A party whose
  organization garam does not hold — the recipient of a refused
  crossing may name any organization — is referenced by nothing, and
  its event is still written, because a reference that refused the
  write would refuse the send garam answers with a message instead
  (ADR-0013).
- **Who can read it.** The deployment's database holder, and nobody
  through garam, until a read route and the action that governs it are
  decided.
- **Where it lives.** `internal/audit` is the audit domain, and it
  holds send events alone. The domain package holds the event and its
  decision and imports no driver
  ([ADR-0069](./0069-the-aggregates-are-data-carriers-so-the-driver-leaves-the-domain-package.md));
  the event's fields are closed, because it holds an invariant over
  them — an authorized send names a message and a refused one does
  not. `internal/audit/postgres` holds the table's writer, which takes
  the caller's transaction rather than a pool. The table is
  `audit_send_events`, named for the only events it holds.
- **The one cross-domain transaction.** `internal/message`'s Postgres
  store calls the audit writer inside the transaction that writes the
  outcome. The message domain reaches audit through the surface audit
  declares
  ([ADR-0047](./0047-a-domain-reaches-a-sibling-through-its-declared-surface.md)),
  holds no audit query of its own, and audit imports nothing of message.
  It is the one exception to each store holding its own transactions,
  because an event committed beside its outcome rather than with it can
  record a send that never happened or miss one that did, and the
  message store already holds the transaction the outcome is written
  in.
- **No recorder port.** No consumer holds a recorder as an interface
  and no test substitutes one, so a port in the domain package would
  hide nothing (`docs/convention/simplicity.md`). It arrives with its
  first consumer: decision or change events, or a read route.

## What this does not decide

- **Decision and change events.** They stay planned, and nothing here
  says which table or shape they take.
- **A read route.** Neither its surface nor the action a policy names
  to permit it.
- **Any change to delivery or to body retention**, both outside #1216.

## Consequences

- A send's history is held by the party that is neither its sender nor
  its recipient, past the agent's deletion and the queue's window, and
  a send the policy denied is on it too.
- The record grows with the send rate and nothing bounds it but its
  tenants' deletion. A row is bounded in size, because it holds no body.
- A policy's denial now costs a write, so a sender retrying a denied
  post writes a row each time.
- The message store's writes depend on audit's schema: a migration
  that breaks `audit_send_events` breaks posting.
- Nothing reads the record through garam, so it answers an operator
  with database access and no one else.

## Signs the decision was wrong

- The table's growth becomes an operating cost a deployment has to
  manage, which would argue for a retention window after all.
- A reader needs to know which policy denied a send, which the
  decision alone does not say.
- A second domain needs its events written in its own transactions the
  same way, which would argue for the recorder port, or for the
  exception becoming the rule.
- An event is found for a send whose outcome was rolled back, or a
  committed send is found without one.

## Rejected alternatives

- **Direct agent-to-agent delivery, with the history read from the
  agents' logs.** Per-send authorization and the cross-organization
  refusals (#1157 scenario 3) depend on one choke point; durable
  delivery is already garam's (ADR-0083); agents have no inbound path;
  and a record the parties hold goes with them.
- **A message-owned events table, written by the message domain's own
  query.** The record is of garam's decisions rather than of the
  queue, and the audit domain was already planned for it; a table
  inside the message domain would be the second mechanism once that
  domain arrived.
- **Writing the event outside the outcome's transaction**, after the
  commit or from the listener. A failure between the two leaves an
  event without a send or a send without an event, which is the record
  being wrong rather than incomplete.
- **A general `audit_events` table with a kind column.** Only send
  events exist, and their columns are a send's; a shape for events
  nobody has decided is speculative.
- **Requiring both organizations to exist.** A send to an organization
  garam does not hold is decided and answered like any other, so a
  reference that refused it would turn a refusal garam answers with a
  message into a failed post.
- **Keeping message rows longer and calling them the record.** ADR-0034
  rejected it: the queue's rows are kept for space, and a body would be
  kept with them.
