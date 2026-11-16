# ADR 0171: The browser listener's enforcement, permission checks and holders list ask the policy service, and delivered state suffices

> Status: accepted; built in `platform/internal/api`,
> `platform/internal/cli`, `platform/internal/policydecision/mocks` and
> `platform/tests/integration` (#1974)
> Date: 2026-11-17

Drafted for #1974, which reserves this number, under #1796. It builds
the browser listener's share of
[ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md):
what a handler's own enforcement, `checkPermissions` and the holders
list ask. [authz.md](../authz.md) and
[api-surface.md](../api-surface.md) state the rule beside the code it
governs.

## Context

Until this round the browser listener decided every org-scoped request
with the platform's engine over gRPC (`Evaluator`), fed the statements a
role grants. The policy service already decides a message send
(ADR-0168), delegated authority and the controller proof (ADR-0146
items 14 and 15), and holds the organization's role links, guardrails
and permits (ADR-0169). Three questions were left to this round.

- **Delivered or current.** The service answers either from what the
  platform has delivered to it, or read current.
- **Which question is a probe.** ADR-0148 item 2 records a denied
  question as an attempt unless it is marked a probe, which the
  listener does for a question nobody acts on.
- **What a failure answers.** The engine's failure was an internal
  error the platform mapped by kind (a 503 where the engine was
  unreachable); a policy service that cannot be reached has a status
  of its own (ADR-0168 item 7).

## Decision

1. **Enforcement, `checkPermissions` and the holders list ask the
   policy service.** `allows`, and so every handler's enforcement,
   asks one `Evaluate` per pair: the signed-in user, the action and
   the resource the handler addresses. The holders list asks one per
   candidate. Neither calls the platform's engine, and neither
   supplies it statements. Membership of the organization remains a
   precondition the platform checks itself, before the service is
   asked.
2. **They read delivered state, not current.** A decision that
   enforces a write the caller has just made is already ordered by
   ADR-0146 item 5: a writer waits for its marks to be delivered
   before it answers. `READ_CURRENT` stays reserved for the former
   `CheckCurrent` consumers (item 12), whose answer an earlier allow
   must never stand in for. A check or a holders page that lags a
   delivery by moments is what a probe is for, and a handler that
   acts reads the same delivered state a probe does.
3. **A probe is a question nobody acts on.** Each `checkPermissions`
   pair, each holder candidate, the agent read that admits agents among
   the candidates and each kind a search offers is asked with `Probe`
   set. The handler's own enforcement and the holders list's
   `iam:org.member-list` check are attempts: a denial of either is
   recorded, since a caller was refused.
4. **Any error of the service is a 503 `unavailable`.** A service that
   cannot be reached, times out or answers an error the client does
   not read as a denial gives the status of ADR-0168 item 7, whatever
   the error's kind, and never an allow. A forbid stays a 403. The
   caller's text is not the service's.
5. **The question per pair and per candidate.** The platform asks
   `Evaluate` and not `Evaluations`. Batching the holders and the
   list filter belongs to the round that moves them.
6. **What stays on the platform's engine.** `permission_explanation.go`
   (the explanation) and `list_visibility.go` (the list filter) still
   ask `Evaluator`, until rounds 11 and 10. The one change to the
   explanation is that `ExplainPermission` marks its whole context a
   probe: nothing it asks is acted on, and `decidingStatements`'
   read of a deciding policy goes through `allows`, which would
   otherwise record its denial as an attempt. That read takes item 7.
   The rest of the file stays for its round.
7. **A question whose resource is a policy is asked on the policy's
   organization.** The service holds no `Iam::Policy` entity
   (`policy/README.md` §Callers: the type's `@caller` is `none`), so
   asked an `iam:policy.*` action on a policy it denies everyone. It
   decides a write to a policy as the writer's step action on the
   organization, and a read as `iam:policy.read` on it
   (`policy_write.rs`). `allows` therefore passes every resource
   through one seam, `askedOn`, which answers the policy's
   `Iam::Org` for a policy GRN and the resource itself otherwise.
   `checkPermissions`, `authorizePolicy` (every
   `/orgs/{org}/policies/{policy}*` route) and the explanation's read
   of a deciding policy all take it. A client's question names the policy, as the console's page
   does, and is answered as the service decides it. `iam:permit.write`
   stays round 10's.

## Consequences

- The service is the only decider of what a role carries and what a
  guardrail forbids on the browser listener's decisions; an
  organization's guardrail written through ADR-0169's routes now
  refuses an action its role allows.
- A policy attached through the old `/policies*` routes no longer
  reaches enforcement; the routes remain until round 11.
- Built as first written, which asked the service about the policy
  itself, the owner and every admin were refused `iam:policy.*` on any
  policy: the integration double granted a role's actions on every
  resource of the organization, which the service never does, so no
  test saw it. Item 7 is the repair, and a real-service case asks a
  policy-resource pair.
- A request costs one service call per question where it cost one
  engine call. A `checkPermissions` page of `n` pairs costs `n`.
- Every browser request that enforces needs the service up. The
  engine's failure was already mapped by kind; the service's is a 503
  a client can retry.
- A decision may lag a write by the delivery interval for a caller
  that did not make the write. The writer is not behind (item 2).

## Signs the decision was wrong

- A user reports a refusal of an action it was just granted, from a
  page another user wrote, often enough that a probe or an attempt
  should read current.
- The `n` calls of a `checkPermissions` page make it slow enough that
  `Evaluations` batching cannot wait for round 10.
- The explanation's reads of a deciding policy (item 6) show in the
  audit as refusals nobody met, which the probe mark rules out.
