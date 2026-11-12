# Audit

The record garam keeps of what it decided, held by garam rather than by
either party it decided between. Implemented by
`platform/internal/audit`, whose PostgreSQL writer is
`platform/internal/audit/postgres`.

**Send events and machine credential events are the events built.**
Decision and change events, which this domain was planned for, are
unbuilt in the platform's database, and
[ADR-0148](./adr/0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)
decides them as the policy service's record (§The policy service's
record). Send events are read by the deployment's database holder
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
  `iam:operator.read` or `iam:agent.read`, which that subject's own
  reads already take; no action names audit.

## An agent acting on the browser-facing listener

**Unbuilt.**
[ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
lets an agent call the browser-facing listener as itself, and decides
that every record of it names the agent's GRN and never a person's:

- **Its token's mint is a credential event of its own kind**, beside
  `agent_token`, with the agent as subject and read on `iam:agent.read`
  like the agent's other events.
- **Every event the browser-facing listener writes names the
  authenticated principal as its actor**, so an agent's act is never
  recorded as a person's. That listener writes no decision or change
  event yet (§Open questions), so until one is built an agent's acts
  there are recorded by the request line, which names its GRN, and by
  its tokens' credential events.

## cowork's principal

**Unbuilt (#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
records what cowork does on its own account under the organization's
cowork principal, so each organization's record stays its own:

- **A delivery** is a send event like any other, with the principal as
  sender. The cowork message's author is not a party to it.
- **The principal's token issuances and refusals** are credential
  events with the principal as subject, in its organization.

## The policy service's record

**Unbuilt.**
[ADR-0148](./adr/0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)
decides it. Today the policy service keeps nothing of a decision, and
a stored policy version names its writer and holds no time or outcome
of its own. The platform's database holds none of it, and
`platform/internal/audit` is not where it goes:

- **Kept by the policy service, in its own database,** because every
  decision passes it, cowork's included.
- **Every write of a policy** (created, version added, activated,
  rolled back, removed) is recorded in the transaction of the write and
  kept as long as its organization exists.
- **Every denial** is recorded and removed 90 days after its last
  occurrence. An allowed decision is not recorded, and neither is a
  denied probe: a question a caller states it asked to decide what to
  show, as in filtering a list. A question that does not say is an
  attempt and is recorded.
- **A record belongs to the organization of the resource decided on**
  and goes with it. A denial for an organization the service does not
  hold is not recorded.
- **A record holds** when, the subject's GRN and the calling service,
  the action, the resource, the outcome and the ids of the policies in
  the reasons. It holds no policy text beyond an id and a version, no
  request context and no token.
- **Recording a denial does not decide it.** The answer stands when
  the record cannot be written, and the failure is logged and counted.
- **A caller denying in a loop is bounded.** Identical denials in an
  hour are one record with a count, an organization holds at most
  10,000 live denial records, and past that a per-day overflow record
  counts the denials not stored.
- **Read by an organization's owners and administrators** on
  `iam:audit.read`, which the `Iam` manifest does not declare yet,
  through a read on the policy service's contract that the platform
  calls. A member and another organization's owner do not read it.

## Rationale

[ADR-0106](./adr/0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)
records why garam stays the route every message takes rather than
handing delivery to the agents, why the event shares its outcome's
transaction and is the one write a domain makes in another's, and why
the record is kept until its tenant goes rather than for a window.
ADR-0148 records why decisions and policy changes are the policy
service's record and not the platform's, and why denials are kept for
a window and bounded where a send's record is not.

## Open questions

- **How the platform's decision and change events are recorded.**
  What ADR-0148 records is the policy service's. A decision or change
  the platform makes outside the policy service has no event, and
  whether it shares a table or a writer's shape with send events is
  decided when the first one is built; until then this binds nothing.
- **How the policy service's record is removed with its organization.**
  ADR-0148 states the rule and leaves the mechanism to the
  implementation.
- **Who reads send events through garam.** No route serves them, and
  no action names permission to read them.
