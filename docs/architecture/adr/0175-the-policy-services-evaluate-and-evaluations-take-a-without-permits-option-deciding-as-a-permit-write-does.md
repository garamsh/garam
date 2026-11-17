# ADR 0175: The policy service's Evaluate and Evaluations take a without-permits option, deciding as a permit write does

> Status: accepted; built in `policy/crates/grant`,
> `policy/crates/server` and `policy/proto` (#1983); builds
> [ADR-0172](./0172-the-lists-and-search-filter-through-the-policy-services-evaluations-as-probes-and-checkpermissions-answers-permit-write.md)
> item 6's deferral
> Date: 2026-11-17

Drafted for #1983, which reserves this number, under #1796. This is
the policy service's half; the platform's half, `checkPermissions`
asking this way, is a later change.
[authz.md](../authz.md) states the rule beside the code it governs.

## Context

A permit write is decided by `policy_write.rs` asking
`PermitRight::ask`: `iam:permit.write` on the organization's `Org`,
against the system tier, the organization's role links, guardrails
and entities, with its `permit`s left out, so the right comes from
the owner role alone (ADR-0144 item 7). `Evaluate` and `Evaluations`
answer with the permits, so a caller asking the same question gets an
allow wherever a `permit` of the organization names `permit.write`,
and the write then refuses it (ADR-0172 item 6). The service gave a
caller no way to ask the question the way the write decides it.

`Read` is no option here: the service has no cache, so every question
reads storage when asked.

## Decision

1. **A question may be asked without permits.** `EvaluateRequest`,
   `EvaluationsRequest` and an `Evaluation` of it carry
   `without_permits`. Set, the service leaves the organization's
   `permit`s out of the decision and nothing else: the system tier,
   role links, guardrails and entities decide as they do for a plain
   question. Unset, nothing changes. An `Evaluation` states it as
   `optional` and takes the request's when unset, as `probe` does.
2. **One function decides both.** `grant::evaluate_without_permits`
   is the decision, and `PermitRight::ask` and a question with the
   option answer through it, so the write and the question cannot
   differ.
3. **It is a way of deciding, not a name of the action.** The service
   applies the option to whatever action it is asked with, and does
   not refuse another. Only `iam:permit.write` is decided this way by
   a write; a write of any other action is decided with the permits.
   A caller asks `iam:permit.write` with it when it wants the write's
   answer.
4. **The HTTP binding does not carry it.** It speaks AuthZEN, which
   has no such field, and sends every question with the permits.
5. **Probe and attempt are untouched.** A denied question is recorded
   unless it is a probe, with or without the option.

## Consequences

- `checkPermissions` can answer `iam:permit.write` as the write does,
  which ADR-0172 item 6 left to this issue. The platform's half does
  it.
- The decision a write takes and the one this option returns share
  code, so changing how the right is decided changes both.
- A caller that sets the option for an action a write decides with
  permits gets an answer no write takes. The contract names the option
  by what it leaves out so that no reader takes it for the write's
  answer to every action.

## Signs the decision was wrong

- A second action is decided without permits by a write, so the option
  has to say which action it follows, or the service has to refuse it
  for any action but one.
- A caller sets it for an action other than `iam:permit.write` and
  relies on the answer as a write's.
