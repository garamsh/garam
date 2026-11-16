# ADR 0172: The lists and search filter through the policy service's Evaluations as probes, and checkPermissions answers iam:permit.write

> Status: accepted; built in `platform/internal/api`,
> `platform/internal/org` and `platform/tests/integration` (#1979);
> supersedes
> [ADR-0115](./0115-a-list-drops-each-object-its-caller-may-not-read-asked-of-the-engine-in-one-batch.md)
> in part (the engine's `CheckBatch` as the list filter's call) and
> [ADR-0171](./0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)
> in part (item 6's list filter, and item 7's reservation of
> `iam:permit.write`)
> Date: 2026-11-17

Drafted for #1979, which reserves this number, under #1796. It builds
the list filter's share of
[ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md),
which ADR-0171 item 6 left on the platform's engine.
[authz.md](../authz.md) and [api-surface.md](../api-surface.md) state
the rule beside the code it governs.

## Context

A list is authorized on the organization first. `visible` in
`list_visibility.go` then drops each object whose own read the caller
is denied. It asked the platform's engine one batch of objects, fed the
statements a role grants, and treated a service-held guardrail as
unknown. After ADR-0171 every handler's enforcement asks the policy
service, so a guardrail refusing `iam:group.read` on one group refused
that group's read and still listed it.

`checkPermissions` could not answer `iam:permit.write`. The action is
the owner's alone (ADR-0145 item 4) and the policy service holds it in
the owner's role link, but `role.go` did not define it, so the manifest
check left it out of `org.Actions()`.

## Decision

1. **A page is one `Evaluations` call.** `visible` asks the policy
   service one question per item of the page, in one `Evaluations`
   call, and keeps the items allowed. The service answers at most 1000
   evaluations a call (`MAX_EVALUATIONS`), so a page longer than that,
   which a list asked without a limit can be, is asked in as many
   calls as it takes. Nothing in `platform/internal/api` calls the
   engine's `EvaluateBatch`; it is removed from `Evaluator`. The
   explanation (`permission_explanation.go`, round 11) calls `Explain`
   and nothing else calls the engine in production; `Evaluate` stays on
   the interface with no caller outside its own test, and leaves with
   the evaluator in round 14.
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
   `iam:policy.read` on the organization, and a page of `n` policies
   asks the same question `n` times.
5. **`role.go` defines `iam:permit.write`, and `checkPermissions`
   answers it.** The action joins `org.Actions()` and the owner's
   actions, and not the admin's, as the service's schema declares it
   (`role-owner` alone). The role table and the service now agree, which
   removes the matrix's difference on it and keeps the explanation, which
   reads the role table, in step with the check. It is declared on the
   organization and answered by `allows`, one `Evaluate` per pair
   (ADR-0171 item 1), as a probe (item 3). The holders list answers the
   action too: it returned no candidate for it before, and returns the
   organization's owners now.
6. **The check is not the write's decision.** A permit write is decided
   by `policy_write.rs` with the guardrails and without the permits
   (`PermitRight::ask`), reading the service's current state. The check
   is a plain `Evaluate`, which honours permits and reads delivered
   state (ADR-0171 item 2). The two agree for the owner, for the roles
   that do not hold the action and for a guardrail forbidding it, which
   the real-service cases hold. They differ where a permit of the
   organization names `permit.write`, which the check allows and the
   write refuses, and where a delivery lags a write. The check can offer
   a write the service then refuses, which is the safe direction.
   Making them agree needs an option of the service's `Evaluate`, which
   is not this change's; #1983 carries it.
7. **A policy write is also asked on the organization.**
   `checkPermissions` accepted `iam:policy.attach`, `.detach`, `.update`
   and `.delete` on a policy only, and answered 400 for the whole batch
   when one was asked on the organization. The service decides every
   policy write on the organization (`Iam::Org`, `policy_write.rs`), and
   a role link or guardrail not yet written can be asked about nowhere
   else, which is the reasoning of `askedOn`. The four actions are
   accepted on the organization beside `iam:policy.create` and
   `iam:permit.write`, and stay accepted on a policy.

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
- A permit naming `permit.write` exists in an organization and a client
  offers a permit write whose request is refused (item 6).
