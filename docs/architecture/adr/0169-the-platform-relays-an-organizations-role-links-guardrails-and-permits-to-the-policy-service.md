# ADR 0169: The platform relays an organization's role links, guardrails and permits to the policy service

> Status: accepted; built in `platform/api`, `platform/internal/api`,
> `platform/internal/policyrelay`, `platform/internal/cli` and
> `platform/tests/integration` (#1969)
> Date: 2026-11-17

Drafted for #1969, which reserves this number, under #1796. It builds
item 3 of [ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)
on the browser API: what the platform serves, and what it still keeps.
[authz.md](../authz.md) and [api-surface.md](../api-surface.md) state
the rule beside the code it governs.

## Context

The policy service stores an organization's role links, guardrails and
permits, and decides a write by the writer's action on the
organization, read current (ADR-0146 item 3 and §Security). The
platform had no route to it: its policy endpoints write the removed
engine's documents. Three questions were left to this round.

- **What a route looks like.** The service keeps one policy kind per
  policy, fixed by its first version, and takes one request shape for
  all three. The old endpoints are `/orgs/{org}/policies*`, and they
  stay until round 11.
- **Who names a policy.** The service takes any identifier under the
  organization's policy GRN, and reserves `link-owner-`, `link-admin-`,
  `link-member-` and `link-scoped-` for the platform's own links
  (ADR-0161, ADR-0163).
- **What the platform still refuses.** A writer's link and a
  membership's are the same body. The service tells them apart by the
  identifier alone, so a writer who stored a user's organization role
  under a free identifier would give that user a second, unowned grant
  (#1796 decision 5).

## Decision

- **One resource set, the kind in the body.** `/orgs/{org}/iam-policies`
  and `/{iamPolicy}`, `/versions`, `/activate` and `/rollback` below
  it create, version, activate, roll back, delete, read and list a
  role link, a guardrail or a permit. A body is `{kind, text}` for a
  guardrail or permit and `{kind, roleLink}` for a role link, whose
  principal and resource are GRNs; the platform derives the entity
  types from the GRN catalogue. The old `/policies*` routes are
  untouched.
- **A relay, as the caller.** Each route authenticates the session and
  calls the service naming that user as the writer. The platform asks
  no authorization question of its own and stores no body: the
  service's refusals are the answer, PERMISSION_DENIED as 403,
  INVALID_ARGUMENT as 400, NOT_FOUND as 404, ALREADY_EXISTS and
  FAILED_PRECONDITION as 409 (a concurrent rollback moves the default
  an activation reads), a service that cannot be reached as 503, and
  a call that outlasts its timeout as 504. A reply naming a role,
  scope, kind or status the platform does not know is the service's
  fault, answered 500, and never a value the spec's enums forbid. A
  write refused stores nothing, because the
  service stores in the call that decides. Only an owner writes a
  permit for the same reason: `iam:permit.write` is the service's check.
- **The platform names the policy.** An identifier is generated as
  `p-` and 32 hexadecimal digits, which no reserved prefix begins
  with, so a writer's policy never lands among the platform's links.
- **The platform refuses the membership's shapes.** A role link whose
  principal is a user in organization scope, or whose principal equals
  its resource in team-or-project scope, is answered 400 before the
  service is asked. An agent, a team or a project linked to the
  organization, or a user linked to a team or project, is a writer's:
  the service accepts each and no membership derives it.

## Consequences

- A console or a client writes an organization's policies against one
  set of routes whatever the kind, and the SDKs carry them.
- The platform is the second place that knows what a membership's link
  looks like, beside the service's reserved identifiers. A third shape
  derived from a membership would have to be added here as well as
  there.
- The old `/policies*` routes and the platform's engine remain until
  round 11, so a console has two policy surfaces meanwhile.
- A reader of an organization's policies needs `iam:policy.read`, as
  the service decides it: the platform's own `listPolicies` filter is
  not applied to these routes.

## Signs the decision was wrong

- A client needs the old routes' per-kind differences (a permit's
  form, a guardrail's editor) so often that three route sets would
  have been simpler than one with a kind in the body.
- The service starts to store a field the relay cannot carry, so the
  body shape the platform fixes is narrower than what the service
  keeps.
- A membership shape the platform fails to name is written through
  these routes and gives a user a role its membership does not.
