# ADR 0172: The lists and search filter through the policy service's Evaluations as probes, and checkPermissions answers iam:permit.write

> Status: accepted; built in `platform/internal/api`,
> `platform/internal/org` and `platform/tests/integration` (#1979)
> Date: 2026-11-17

Drafted for #1979, which reserves this number, under #1796. It builds
the list filter's share of
[ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md),
which
[ADR-0171](./0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)
item 6 left on the platform's engine. [authz.md](../authz.md) and
[api-surface.md](../api-surface.md) state the rule beside the code it
governs.

## Context

A list is authorized on the organization first. `visible` in
`list_visibility.go` then drops each object whose own read the caller
is denied. It asked the platform's engine one batch of objects, fed the
statements a role grants, and treated a service-held guardrail as
unknown. After ADR-0171 every handler's enforcement asks the policy
service, so a guardrail refusing `iam:group.read` on one group refused
that group's read and still listed it.

`checkPermissions` could not answer `iam:permit.write`. The action is
the owner's alone (ADR-0145 item 4) and no role's action list carries
it, so the manifest check left it out and the console withheld the
permit form until the platform answered it.

## Decision

1. **A page is one `Evaluations` call.** `visible` asks the policy
   service one question per item of the page, in one `Evaluations`
   call, and keeps the items allowed. The service answers at most 1000
   evaluations a call (`MAX_EVALUATIONS`), so a page longer than that,
   which a list asked without a limit can be, is asked in as many
   calls as it takes. Nothing in `platform/internal/api` calls the
   engine's `EvaluateBatch`; it is removed from `Evaluator`, which
   keeps `Evaluate` and `Explain` for the explanation alone
   (`permission_explanation.go`, round 11).
2. **Each question is a probe.** The question names the signed-in
   user, the list's read action and the object, with `Probe` set: a
   denial that only hides an item is not an attempt a caller made
   (ADR-0148 item 2). The list's own authorization on the organization
   stays the handler's enforcement and is recorded as before
   (ADR-0171 item 3).
3. **An error of the service is a 503 `unavailable`.** Any error of the
   call, whatever its kind, is the status of ADR-0171 item 4. A failed
   call is never an allow and never an empty page.
4. **A policy is asked on its organization.** The object of a policy
   passes through `askedOn` as every other question does
   (ADR-0171 item 7). The service holds no policy entity, so a policy
   cannot be hidden alone: its list is as visible as the caller's
   `iam:policy.read` on the organization.
5. **`checkPermissions` answers `iam:permit.write`.** The action is
   declared on the organization and answered by `allows`, the one
   `Evaluate` per pair of ADR-0171 item 1, with the probe mark of item
   3. It is the manifest's action and appears in `org.Actions()`,
   though no role's list carries it, because the service decides it
   from the owner role's link and a guardrail may forbid it.
6. **The answer is the service's decision of the question, which a
   permit write decides narrower.** `policy_write.rs` decides a write
   of a permit with the guardrails and without the permits
   (`PermitRight::ask`), so no permit grants a permit write. A plain
   `Evaluate` honours permits as well. The two agree for the owner,
   for the roles that do not carry the action and for a guardrail that
   forbids it, which the real-service cases hold. They differ where a
   permit of the organization names `permit.write`: the check allows
   what the write refuses. Closing that is a service change (a probe
   of the write's own question), left to the issue that owns it.

## Consequences

- A guardrail of an organization refuses the read of one object and
  hides it from every list and the search, as it refuses its read.
- A list costs one service call per page (or per 1000 items) where
  ADR-0171 item 5 deferred it, instead of one engine call.
- A list that cannot reach the service answers 503 where it answered
  from the engine's own state.
- The holders list still asks one `Evaluate` per candidate
  (ADR-0171 item 5); batching it is not part of this change.
- The platform's engine is asked only by the explanation, until round
  11 moves it.

## Signs the decision was wrong

- A list page of a thousand items is slow enough that the cap or the
  probe cost needs a different shape.
- A permit naming `permit.write` exists in an organization and the
  console shows a permit form whose write is refused (item 6).
