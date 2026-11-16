# ADR 0174: The explanation comes from the policy service, the old policy endpoints go, and the parity matrix is retired

> Status: accepted; built in `platform/internal/api`,
> `platform-web`, both SDKs and `garam-cli` (#1987); supersedes
> [ADR-0171](./0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)
> in part (item 6's reservation of the explanation to the platform's
> engine)
> Date: 2026-11-17

Drafted for #1987, which reserves this number, under #1796. It builds
the last browser-listener share of
[ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md):
after it nothing on that listener calls the platform's engine.
[authz.md](../authz.md), [api-surface.md](../api-surface.md) and
[web-app.md](../web-app.md) state the rules beside what they govern.

## Context

`explainPermission` was the one caller of the platform's engine left on
the browser listener. It answered from the engine's `Explain`, with the
statements the engine matched, so the decision `checkPermissions` took
from the policy service and the grounds the explanation named came from
two engines. The ten operations that created, versioned, activated,
rolled back, deleted and attached policies in the platform's own tables
had been replaced by the relay
([ADR-0169](./0169-the-platform-relays-an-organizations-role-links-guardrails-and-permits-to-the-policy-service.md),
[ADR-0173](./0173-the-console-writes-role-links-guardrails-and-permits-through-the-relay.md))
and nothing in the console called them. The parity matrix
(`policy_matrix_test.go`, `policy-matrix.md`) measured where the two
engines differ, to decide whether the platform's could be left.

The matrix's last report, taken before it was removed, asked 1881
questions: 1871 agreed and 10 differed, each an organization owner or
admin against a policy of the organization on one of the five
`iam:policy` actions, which the platform's engine allowed from the role
alone and the service denied for want of an entity of the resource. The
service was the one the stack enforces, so the ten were the engine's
differences: 10 in the platform engine, none in the service.

## Decision

1. **The explanation is the service's `Evaluate`, as a probe, read as
   the check reads it.** `explainPermission` asks the pair the way
   `checkPermissions` does, so its `decision` is the one the check
   answers. Its grounds are the service's `reasons`, with no new RPC:
   a role link, named by its reserved identifier and the path it
   reaches the caller by (the organization, or a team or project); a
   guardrail or a permit, named by its GRN; a system policy, named by
   its file name; or an error of evaluation. A deny that nothing
   forbids names no grounds. A guardrail or a permit is named with its
   name and text only where the caller holds `iam:policy.read` on the
   organization, and by its identifier alone otherwise. The basis is
   `policy-service`; an action decided past the check, and an action
   garam does not define, keep their own bases and name no grounds.
2. **The ten old policy operations are removed.** `createPolicy`,
   `listPolicies`, `listPolicyVersions`, `addPolicyVersion`,
   `activatePolicy`, `deletePolicy`, `rollbackPolicy`,
   `listPolicyAttachments`, `attachPolicy` and `detachPolicy` leave the
   OpenAPI document, the handlers, the CLI and both SDKs. The platform's
   tables, store and engine stay until round 14.
3. **The parity matrix is retired.** Its make target, test, test data
   and `policy-matrix.md` are removed. The real-service suites stay in
   CI, under the target `policy-service` and the build tag
   `policyservice`.

## Consequences

- Nothing on the browser listener calls `s.evaluator`.
- The decision an explanation gives and the grounds it names come from
  one evaluation, so they cannot disagree.
- A caller refused `iam:policy.read` is told that a guardrail forbade
  an action, and which one by identifier, and nothing of its content.
- Nothing measures the platform's engine against the service any more;
  the engine leaves in round 14.
- The console's Your access names a role link by its role and path,
  and a stored policy by its name where the caller may read it.

## Signs the decision was wrong

- A person denied an action cannot tell from the explanation what to
  change, because a service reason names a policy they may not read.
- The service's `reasons` omit a ground people need named, and the
  fix would be a new RPC.
- A difference between the service's answer and another decider
  appears after the matrix is gone, with nothing to measure it.
