# ADR 0035: A cross-organization message needs both organizations' consent

> Status: accepted
> Date: 2026-08-19

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

A message from one organization's agent to another's was first open by
omission, and is now refused by construction: the send path checks
`sender.Organization != recipient.Organization` before the engine is
asked, because the engine cannot hold that boundary
([ADR-0025](./0025-engine-scope-is-a-caller-obligation.md)). #245 asked
whether cross-organization messaging should exist, and the owner has
answered: yes, with both organizations' consent.

Two established facts shape the mechanism. Policy cannot express it —
a policy names no principal ([ADR-0008](./0008-identity-based-policy-attachments.md))
and an attachment cannot name a foreign subject (the schema refuses
it), both verified live. And [ADR-0012](./0012-agent-operator-model.md)
fixes that an agent's identifier never changes, so a recipient
organization that wants unwanted inbound to stop has no lever unless
one is built. The shape this project already uses for a binding between
two things is a relation row.

## Decision

**A message may cross an organization boundary only where a relation
permits it, and both organizations hold a side of that relation.**

- The relation is a row: `(sender org, recipient org)`, created only
  when both organizations have agreed. Neither side alone can open the
  channel, and either side can close it by withdrawing its half — the
  row lives only while both halves stand.
- The precondition on the send path changes shape, not kind: from
  "same organization" to "same organization, or a consenting relation
  between the two". The engine is still never asked a cross-organization
  question it cannot scope (ADR-0025 stands untouched).
- A refused post is **not** rejected to the sender. ADR-0013's rule
  holds: what garam cannot route comes back as an ordinary inbound
  message to the sender, so a refusal is observable to the party that
  can act on it and silent to no one.

## Consequences

- An organization that does not know its agents are talking outward has
  lost nothing it did not grant: every cross-boundary channel is a row
  it co-signed.
- The relation is small and enumerable — an org can answer "who may we
  hear from" with one query, which is the audit property the open model
  could not give.
- The duplicate-suppression key (`sender_grn, idempotency_key`) is
  unchanged; the consent check happens before it, so no foreign message
  is ever stored to be suppressed.
- A migration carries the relation table; `docs/architecture/domains.md`
  records it.

## Rejected alternatives

- **Recipient decides alone.** Simple, and the sender's organization
  cannot see where its own agents talk — which is the visibility the
  product claims to give it.
- **Sender decides alone.** The current behaviour with extra steps, and
  the recipient has no lever — the shape #245 exists to prevent.
- **Open by default with a deny-list.** An off switch added later would
  have to decide what happens to traffic already flowing; consent-first
  never owes that answer.
