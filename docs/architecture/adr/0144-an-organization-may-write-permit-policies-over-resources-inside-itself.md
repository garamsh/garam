# ADR 0144: An organization may write permit policies over resources inside itself

> Status: accepted; partly built. The policy service's half is built
> (#1780): the write, the store and the decision. The console's editor
> and the platform's switch are not.
> Date: 2026-11-12

Drafted for #1775, under #1774, which reserves this number. The
direction is the owner's of 2026-11-12, recorded on #1689 as D3. It
supersedes in part
[ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
(§4, its last clause: "writes no `permit` of its own"). It builds on
[ADR-0137](./0137-tenant-isolation-is-membership-and-the-policy-engine-fails-closed-on-any-evaluation-error.md)'s
tenant isolation,
[ADR-0138](./0138-a-caller-of-the-policy-service-presents-a-short-lived-signed-token-and-owners-write-entities-through-a-sequenced-rpc.md)
§9's refusal of a policy naming another organization's entity, and
[ADR-0143](./0143-a-service-is-declared-by-one-manifest-that-is-its-cedar-schema-and-garam-serves-what-the-manifests-say.md)'s
manifests. garam is read at `cb8f5366132bfe743fb859e12d957a929490d4ef`;
a claim below that names no revision holds for it.

## Context

- **An organization only narrows today.** ADR-0130 §4 has it link the
  system tier's role templates and write `forbid` guardrails, and no
  `permit`. A grant it needs that no role holds, one action on one
  team's resources for one agent, is a template the system tier must
  ship, which is a change to this repository.
- **A guardrail and a link are already refused outside the
  organization.** ADR-0138 §9 refuses a link or a guardrail that names
  another organization's entity.
- **Three things bound a policy to its organization today.**
  `Engine::evaluate` in `policy/crates/decision/src/engine.rs` reads an
  organization's policies by the organization of the resource asked
  about, so a policy stored under one organization is not read for a
  resource of another. Tenant isolation, a system `forbid`, denies any
  request whose principal is not a member of the resource's
  organization (ADR-0137 item 1). The third is the write-time refusal
  of ADR-0138 §9, which a `permit` has not had to meet.
- **A principal may belong to several organizations** (ADR-0137), so
  isolation says who may reach an organization's resource and not
  which organization's policy may grant it.
- **The owner decided that an organization may permit inside
  itself** (D3 on #1689, 2026-11-12).

## Decision

**An organization may write `permit` policies, beside the role links
and `forbid` guardrails it writes today. Each is scoped to the
organization at the write, validated against the manifests, and named
by `Explain` as any policy is.**

1. **An organization may write `permit` policies,** beside the role
   links and the `forbid` guardrails it writes today. This supersedes
   ADR-0130 §4's last clause, and the rest of §4 stands. It is stored
   as an organization's policy is: data, versioned append-only,
   audited and deleted as ADR-0117 deletes a policy (ADR-0130 §5).
2. **A `permit` an organization writes is scoped to it.** Its
   resource scope names an entity inside that organization, or the
   organization itself, and the write refuses a policy whose scope
   could name a resource of another: one naming an entity of another
   organization, and one that constrains the resource by type alone
   or not at all. The organization of an entity is the first segment
   of its GRN, as ADR-0142 item 1 reads it, and an `Org` is its own
   (ADR-0137 item 1). Tenant isolation, the system `forbid`, stays
   above it, so the scope check is the second line and not the only
   one: a `permit` cannot override a `forbid`, so a stray one that
   passed the check would still deny a principal outside the
   organization.
3. **It names only what the manifests declare,** entity types and
   actions by their qualified names under ADR-0143 item 3, and is
   validated at the write against the schema they form, as every
   policy is. A name no manifest declares is refused.
4. **`Explain` names it as it names any policy.** A decision it
   produced lists the policy's id among its reasons, as it lists a
   role link or a guardrail.
5. **Built in the policy service by #1774.** The console's editor and
   the platform's switch to deciding through the policy service are
   #1689 slice 4, which the platform's present evaluator is not
   affected by until then.
6. **What an organization still cannot do.** It cannot touch the
   system tier: tenant isolation, the role templates and an agent's
   sending are the repository's, and no write reaches them. It cannot
   permit across organizations: a resource of another organization
   is refused by item 2 and denied by isolation if it got that far.
7. **Writing a `permit` is an action of its own, `iam:policy-permit`,
   held by `role-owner` and no other role.** It is
   `Iam::Action::"policy-permit"` (ADR-0143 item 3) and sits outside
   the group `iam:policy-*`, which `role-admin` holds, so that
   holding the group does not carry it. An owner already holds every
   action in its organization, so a `permit` an owner writes grants
   nothing its writer lacked. Letting another role write one, bounded
   by what that writer itself holds, is left open. Whatever serves the
   write asks for `iam:policy-permit` before it stores a `permit`: the
   policy service's write path in #1774, and the console in #1689
   slice 4.

This leaves open, for the implementation: how the scope check reads a
resource scope that combines `in` and `is`, and whether a principal
scope is also checked (isolation already requires membership of the
resource's organization). Left open as a decision: a role other than
the owner writing a `permit`, bounded by what it holds.

## Consequences

- **An organization may grant what no role grants,** without a change
  to this repository. The system tier is no longer the only source of
  a `permit`.
- **Writing a policy may widen access, and once it could only narrow.**
  `policy-*` is held by `role-owner` and `role-admin` in
  `policy/schema/garam.cedarschema`, so a `permit` gated by that group
  would let an administrator permit itself an action the owner role
  alone holds, such as `org:delete`. Item 7 gates it by an action only
  the owner holds, so no `permit` is written by a role that lacks what
  it grants. The cost is that an administrator writes guardrails and
  role links and no `permit`, until the open question of item 7 is
  decided.
- **The access report gains a source.** `crates/access-report/` lists
  the policies naming a subject or a resource, and a `permit` names
  them in its scope, so the report lists it by the same lookup.
- **A `permit` that errors stops the request it reaches.** Any
  evaluation error is a deny (ADR-0137 item 2), so a broken `permit` is
  as loud as a broken `forbid`, and the schema validation of item 3 is
  what keeps it rare.
- **The scope check is a second line and a cost.** It is written and
  kept equal with how the engine selects policies, so that a change to
  the selection is read against it.

## Signs the decision was wrong

- An organization's `permit` reaches a resource of another
  organization in a test or a decision, which would show the scope
  check or isolation does not hold.
- Organizations write a `permit` to do what a role link does, and the
  role templates are bypassed in most grants.
- A guardrail is written to narrow a `permit` more often than it
  narrows a role, which would show the two kinds need separate tools.
- A role other than the owner writes a `permit` that grants an action
  its writer lacks, which would show item 7's gate was bypassed or
  that `iam:policy-permit` was put in a group a lesser role holds.

## Alternatives rejected

- **Roles only,** which is ADR-0130 §4 as it stood. An organization's
  grants are limited to the role templates the repository ships, so a
  narrower grant waits for a change here, or is made by linking a
  broader role and narrowing it with a `forbid`.
- **A `permit` with no scope check, resting on tenant isolation
  alone.** Isolation decides who may reach a resource, and what a
  stray policy in one organization names is not isolation's to refuse.
  The write-time property would then hold only while the engine reads
  an organization's policies by the resource's organization, which is
  one function's behaviour, and a link and a guardrail already carry
  the refusal (ADR-0138 §9).
