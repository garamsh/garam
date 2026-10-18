# ADR 0114: Every machine credential issuance, renewal and refusal is an audit event, read on the subject's own read

> Status: proposed
> Date: 2026-10-19

Drafted for #1560. The number was not reserved when this was written:
it was the next free one on `dev` at
`a3b0d8e88d16e99daff0406dbd64ca4e3ad5ad61`, and no open pull request
or issue named it. The PM confirms or reassigns it. garam is read at
that revision; a claim below that names no revision holds for it.

## Context

During garam-agent-operator#380 an operator's certificate expired at
2026-10-15T04:00:51Z, and garam could not say whether or when it had
been renewed: `operator_certificates` keeps one row per operator with
no history and no validity, and `RenewIdentityCertificate` logged
neither a renewal nor a refusal. The only trace was a serial that had
changed.

[ADR-0106](./0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)
made `internal/audit` the record garam keeps of what it decided, and
left two things open in §What this does not decide: any event beyond
a message send, and a read route with the action that permits it. A
credential event needs both.

What issues a machine credential is spread across two domains' stores
— the operator domain's certificate, credential and enrollment
records, and the agent domain's standalone registrations and agent
tokens — each with its own transactions, and a refusal writes nothing
at all. ADR-0106's form,
the event written inside its outcome's transaction, would put an audit
write into each of those stores and still leave every refusal without
a transaction to join.

[ADR-0109](./0109-each-machine-callers-last-authentication-by-each-path-is-kept-in-postgres-for-the-g3-gate.md)'s
authentication evidence already hears every exchange and mint on the
machine listener, but it is coalesced to one write per caller and path
a minute and drops a failed write by design, which is the opposite of a
record.

## Decision

- **One event per machine credential garam issues, renews or refuses**,
  in `audit_credential_events` (`migrations/000054`): operator
  certificates, operator credentials, operator enrollment tokens,
  supervisor certificates, supervisor credentials and agent tokens. An
  event names the subject, the credential, `issue` or `renew`, whether
  it was issued or refused, whether an issued answer replayed an
  earlier identical request, the refusal's error kind, and for a
  certificate its serial and validity read off the certificate
  answered, for a token its issue and expiry.
- **No secret material, by shape.** The table has no column for a key,
  a token, a credential, a certificate or any part or hash of one, and
  a refusal carries the class of its failure rather than its text.
- **Recorded by the route that answered, after the outcome and before
  the answer.** The machine listener's and the browser listener's
  handlers write it through a port each declares, implemented by
  `internal/audit/postgres`'s `CredentialStore`. A write that fails is
  logged with the event's fields and the answer stands.
- **A refusal is recorded where it is attributable to a subject.** An
  enrollment whose token or bootstrap secret does not resolve names
  none and is not recorded; a refusal of the member asking, before the
  subject's credential is reached, is the member's.
- **Read on the subject's own read.** `listCredentialEvents` on the
  browser-facing listener pages one subject's events, newest first,
  authorized on `operator:read` on the organization for an operator,
  `agent:read` on the agent for an agent, and `agent:read` on the
  organization for a supervisor. No action is added.

## Consequences

- The question #1560 started from has an answer: when an operator's
  certificate was renewed, with which serial and until when, and every
  renewal refused, readable by whoever may read the operator.
- The record is not transactional with its outcome. A process that
  dies between an issuing store's commit and the event's write leaves
  a credential issued and unrecorded, and a database that refuses the
  write after the issuance does the same, logged. The reverse cannot
  happen: an event is written only once its outcome was decided.
- Failing the answer when the event cannot be written was rejected:
  an operator's renewal does not replay, so withholding the answer
  would leave the operator without the certificate the record says
  nothing about.
- The record grows with issuance, bounded by token lifetimes and the
  renewal windows, and goes with its tenant as ADR-0106's does.
- Agent certificates — the legacy operator route, managed initial
  issuance, recovery and an agent's own renewal — and token exchanges
  are not recorded yet. Adding one is a call at its route.

## Signs the decision was wrong

- An incident needs to know of an issuance the record does not hold,
  and the log shows the event's write failed: the outcome's
  transaction, not the route, was where it had to be written.
- A reader needs to see credential events across subjects — every
  renewal refused in an organization this week — and has to page every
  subject to get it: the read was cut by subject where it should have
  been cut by organization, and governed by an action of its own.
- A member who may read an operator should not see its renewal history:
  the read wanted its own action rather than the subject's.
