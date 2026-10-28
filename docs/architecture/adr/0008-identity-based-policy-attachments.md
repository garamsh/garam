# ADR 0008: Identity-based policy attachments

> Status: accepted — the detach transition is narrowed by [ADR-0031](./0031-detaching-an-active-policys-last-attachment-is-refused.md);
> attachments are superseded by [ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
> Date: 2026-08-02

## Context

[ADR-0002](./0002-iam-style-policy-model.md) chose an IAM-style policy
model with identity-based attachment first, and
[ADR-0007](./0007-hybrid-statement-collection.md) left per-subject
attachment as future work: until it landed, an active policy was an
org-wide grant, so a policy could not be aimed at one user or one
group.

AWS IAM attaches identity-based policies to users and groups and
evaluates the union of everything applicable, deny-overrides. Group
membership is org data ([domains.md](../domains.md)) and the engine is
domain-free ([ADR-0003](./0003-single-cli-topology.md)), so the engine
cannot resolve a subject's groups itself.

## Decision

A policy is attached to subjects through `policy_attachments
(policy_grn, subject_grn)`; a subject is a user or a group GRN.

- An active policy with no attachment keeps applying org-wide, which
  is the behavior ADR-0007 shipped. Attaching narrows it to the
  subjects it names.
- `Check` / `Explain` carry `subject_groups`: the api server resolves
  the subject's groups from the org tables, as it already does for
  role-derived statements, and the engine matches those GRNs against
  attachments without knowing what a group is.
- Evaluation input is the union of org-wide active policies,
  subject-attached active policies, group-attached active policies, and
  the caller's role-derived statements. Deny-overrides applies across
  the union.
- Attachments live in the same per-org cache entry as the policies they
  belong to, and an attachment write invalidates that entry over the
  existing `garam_policy_change` channel.
- An attachment stays inside the policy's org, enforced by the schema.
  An org-owned subject carries the org identifier in its organization
  segment ([ADR-0001](./0001-grn-segments.md)), so the segments must
  agree. A root-owned identity — a user exists before any org
  membership — cannot name an org in its GRN, so the schema admits it
  and org membership stays the api server's precondition, as it already
  is for every check.

## Consequences

- A policy can be aimed at one user or one group without duplicating
  its document per org.
- The engine learns one more wire fact: a request carries the subject's
  group GRNs. It still matches strings and does not validate that a
  subject GRN names a user or a group.
- Attachments to a subject the api server never reports (a group the
  subject has left, an identity from another domain) are inert rather
  than an error; the subject list on the request is what a check sees.
- Resource-based policies (`Principal` in statements), condition
  evaluation, and an IAM-style version cap stay out of scope.
