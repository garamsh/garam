# ADR 0147: A message between two consenting organizations is tenant isolation's one exception

> Status: accepted; not yet built. The send path still refuses a
> cross-organization recipient before the engine is asked, and tenant
> isolation has no exception
> Date: 2026-11-12

Drafted for #1799, under #1796, which holds the owner's decisions of
2026-11-12 and reserves this number. It supersedes in part
[ADR-0035](./0035-a-cross-organization-message-needs-both-organizations-consent.md)
(its send-path precondition, which the consent relation decided before
the engine) and
[ADR-0024](./0024-policy-decides-what-an-agent-sends.md) (the
check it left on the send path), and builds on
[ADR-0137](./0137-tenant-isolation-is-membership-and-the-policy-engine-fails-closed-on-any-evaluation-error.md)'s
tenant isolation and
[ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md).
garam is read at `85739f0026e01ade88f7fe6f926faabc005c7ed7`; a claim
below that names no revision holds for it.

## Context

- **ADR-0035's terms.** A message crosses an organization boundary
  only where a relation of two halves stands: one organization's half
  toward the other and the other's toward it. Each organization grants
  and withdraws its own half, by a member holding `iam:consent.grant`
  or `iam:consent.withdraw`, and the relation lives while both halves
  do. A refused send comes back to the sender as an inbound message
  (ADR-0013).
- **Where it is decided today.** The send path refuses a recipient of
  another organization before the engine is asked, unless the platform's
  consent relation stands. The engine is never asked a cross-
  organization question, and so no policy of either organization is
  read for one: the earlier evaluator reads what is attached to the
  sender and the consent relation is not policy.
- **Tenant isolation has no exception.** ADR-0137 item 1 is a system
  `forbid` that denies any request whose principal is not a member of
  the resource's organization, and the engine reads policies for the
  resource's organization alone. Once the platform asks the policy
  service for every decision (ADR-0146 item 1), a cross-organization
  send is denied unless something lets it through.
- **Policy cannot carry it.** An organization's `permit` is scoped
  inside the organization (ADR-0144) and a policy naming another
  organization's entity is refused at the write (ADR-0138 §9).

## Decision

**A message between two agents of two organizations is allowed where
each organization consents to the other, as one exception written into
tenant isolation, and every other action across organizations stays
denied.**

1. **The capability stays.** What a user can do does not change: a
   member grants, withdraws and reads consent by the same actions, and
   a refusal comes back as an inbound message. The platform's relation
   stays the fact; what the service holds is a copy of it.
2. **Each organization's consents are an attribute of its entity.**
   `Org` gains `consents: Set<Org>`, the organizations toward which
   this one has granted its half. The platform owns `Org` already and
   feeds it through the outbox (ADR-0140): a grant or a withdrawal
   marks the organization that made it, and deleting an organization
   marks each organization that held a half toward it. The manifest
   (`policy/schema/iam.cedarschema`) and the feed change with the
   implementation, which this ADR does not do.
3. **Tenant isolation gains one exception and no other.** The system
   policy becomes:

   ```
   forbid (principal, action, resource)
   unless {
     principal in resource.org.members ||
     (action == Iam::Action::"message.send"
       && principal is Iam::Agent && resource is Iam::Agent
       && principal.org.consents.contains(resource.org)
       && resource.org.consents.contains(principal.org))
   };
   ```

   The exception is the action that sends, between agents, and both
   halves. It lifts the isolation `forbid` only. A send still needs a
   `permit`, which the system tier's `agent-message-send` policy
   supplies, so consent alone allows nothing that policy does not.
4. **Both organizations' guardrails are read for such a send.** Today
   neither is, as the context says. After, the engine reads the
   recipient organization's policies as it does for any resource, and
   for a cross-organization `message.send` it also reads the sender
   organization's guardrails, its `forbid`s alone, never its role links
   or `permit`s. The entity slice for the request holds both `Org`
   entities. Either organization can therefore forbid the send, and
   neither can widen it past the other's consent.
5. **A withdrawal denies the next send.** The attribute loses the
   organization when the platform feeds the change, and the withdrawing
   request is not answered as done before the service decides with it
   (ADR-0146 item 5), so the send that follows the answer is denied. A
   message stored before the withdrawal stays delivered: consent
   gates a send and does not recall one.

### Checked against ADR-0035

- **Who consents.** Each organization, for itself: the half is on its
  own `Org`, written by the platform on its own member's action, and no
  organization's entity names another's half.
- **What a withdrawal does.** Either half ends the exception, because
  the `unless` needs both, which is the relation's "lives while both
  halves do".
- **What the relation was and is.** The row keeps ADR-0035's audit
  property, one query answers who may write to an organization; the
  attribute is read back from it and rebuilt from it.

### Security

- **The narrow form: one action, between agents, both halves.** No
  other action crosses an organization, a user does not, and an agent
  reads nothing across.
- **A `permit` cannot widen past consent.** The isolation `forbid`
  stays for any pair without both halves, whatever either organization
  writes.
- **Left open: a wider consent.** A half per agent, a half in one
  direction only, or consent for another action such as reading a
  channel, are not decided; each widens what a half permits and needs
  its own ADR.

## Consequences

- **One decision path for consent.** A guardrail either organization
  writes can forbid a cross-organization send, which the relation
  alone never allowed.
- **The send path loses its precondition.** The consent check before
  the engine goes; the engine's answer carries the refusal, which is
  still returned as an inbound message.
- **A cross-organization send reads two `Org` entities and the
  sender's guardrails.** A same-organization send reads what it did.
- **Unbuilt at merge.** Until the implementation lands, the send path
  decides as [messaging.md](../messaging.md) states.

## Signs the decision was wrong

- An organization needs consent toward one agent and not the whole
  organization, which means the half is too coarse.
- A guardrail of the sender's organization forbids a send the
  recipient's `permit` was meant to allow, and the writers cannot find
  why: `Explain` must name the sender's guardrail, or the two-sided
  read hides a cause.
- A second cross-organization action is wanted, which makes the
  exception a list and the form worth another look.

## Alternatives rejected

- **Removing the capability.** ADR-0035 answered #245 with yes, and
  the owner's decision of 2026-11-12 keeps it; removal would be a
  product change and not a consequence of moving the evaluator.
- **A `permit` each organization writes for the other.** The write
  would name another organization's entity, which ADR-0138 §9 refuses
  and ADR-0144 scopes out; if it were allowed, two writes made apart
  would stand for what ADR-0035 requires to be co-signed, a
  withdrawal would be a delete of a policy and not of a half, and "who
  may write to us" would no longer be one query.
- **Keeping the consent check before the engine.** A second decision
  outside the service that no guardrail can see or forbid, and the
  decision ADR-0146 item 1 removes the platform's right to make.
