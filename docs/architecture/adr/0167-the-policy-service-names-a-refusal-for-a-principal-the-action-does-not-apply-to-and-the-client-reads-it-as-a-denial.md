# ADR 0167: The policy service names a refusal for a principal the action does not apply to, and the client reads it as a denial

> Status: accepted; built in `policy/crates/decision`,
> `policy/crates/server` and `platform/internal/policydecision` (#1933)
> Date: 2026-11-16

Drafted for #1933, which reserves this number, under #1796. It settles
the open question of [policy-matrix.md](../policy-matrix.md) §Refused
(#1915): what an operator's decision is on an `iam` action the schema
does not apply to an operator. [authz.md](../authz.md) §Decided and
not yet built states the rule beside the client it governs.

## Context

The policy service refuses a question it cannot fit to the schema, and
the platform's client returns that refusal as an error. Of the 1723
questions the matrix asks, 129 are refused: the hosted operator asked
an `iam` action that `policy/schema/iam.cedarschema` does not declare
`Operator` a principal of. The platform denies each, because no route
admits an operator to the action. A caller of
`platform/internal/policydecision` cannot read the refusal as a denial
unless a rule says so, and that rule is needed before any operator
decision moves to the service.

Two ways to make the refusal a decision were measured and rejected:

- **Declaring `Operator` a principal of those actions.**
  `manifests.delegable` is derived from `Operator`'s actions, so every
  `iam` action would become delegable, which rewrites ADR-0146's ten
  operations.
- **Reading the refusal's text in the client.** The service gives one
  text, "the evaluation does not fit the schema", for an operator
  outside an action's principals, an unknown action, a resource of the
  wrong kind and a context attribute the schema does not name. Mapping
  that text to a denial would turn a mistyped action into one.

Cedar tells the first case apart: a request whose principal type is
declared but not valid for the action fails validation with
`RequestValidationError::InvalidPrincipalType`, a variant of its own
apart from `UndeclaredAction`, `InvalidResourceType` and
`InvalidContext` (`cedar-policy` 4.14.0, `src/api/err.rs`).

## Decision

1. **The decision crate gives the case its own error.**
   `Engine::request` returns `Error::PrincipalNotApplicable` where
   Cedar names `InvalidPrincipalType`, and `Error::Evaluation` for
   every other refusal of a question, as before. The cause is read
   from Cedar's validation of the request against the schema the
   service already holds, not from a second copy of it.
2. **The service refuses the question as it did and names the cause on
   the wire.** The status stays `INVALID_ARGUMENT`, so a caller that
   reads the code alone is unchanged (ADR-0164 item 5 holds: the
   question is still refused). It gains a `google.rpc.ErrorInfo` among
   the status's details, in `grpc-status-details-bin`, with reason
   `PRINCIPAL_NOT_APPLICABLE` and domain `garam.policy.v1`. No other
   refusal carries that reason.
3. **The client maps that one cause to a denial.**
   `platform/internal/policydecision/grpc` returns the zero-reason deny
   for a question whose refusal carries that reason, from `Evaluate`
   and from `Evaluations` alike, and returns every other refusal as
   the error it returned before. The reading is the platform's own
   rule: no route admits an operator to an action the schema does not
   apply to it, so the answer is a deny.
4. **A batch entry names the cause by asking again.** `Evaluations`
   answers a refused entry as a deny with a reason holding the fault's
   text and no policy, and `Reason` has no field for a cause. The
   client asks each such entry again through `Evaluate`, with its own
   `probe`, and reads the cause from that refusal. An entry whose
   refusal is for any other cause stays the reason it was.
5. **The schema and ADR-0146's delegable set do not change.**

## Consequences

- **The matrix reads no refusal for the operator class.** The 129
  questions move from refused to agreeing, since the platform denies
  each and the service now does as well, and
  `platform/tests/integration/testdata/policy_matrix_refused.txt`
  lists none. A question refused for any other reason still fails the
  suite.
- **A refused entry of a batch costs one more call.** Only an entry
  the service refused is asked again, and an entry refused for another
  cause is asked and keeps its reason.
- **A caller sees no change in code.** `INVALID_ARGUMENT` is what
  `Evaluate` answered, and the detail is ignored by a caller that does
  not read it, as the HTTP binding does.
- **Other principal types are covered at once.** The cause is the
  principal type's, so a supervisor asked any action but
  `iam:agent.activate` (ADR-0164 item 5) reads as a denial too.
- **A service that gives no detail is read as before.** A refusal with
  no `ErrorInfo` of this reason stays an error, so a client newer than
  its service fails closed.

## Signs the decision was wrong

- A batch holds many refused entries and the second call is its cost.
  The remedy is a field of `Reason` naming the cause, which changes the
  contract and the generated SDK, and which this decision left alone to
  keep to one wire form.
- A second refusal wants to be a denial. Then the question is whether
  the service names a second reason, one per cause, or the client's
  list of denials is the wrong shape.
- A caller needs to tell a denial for this cause from a denial a policy
  gave. The deny carries no reason, as a question nothing matched does,
  so it cannot.
- A mistyped action is read as a denial. That would mean `Evaluation`
  and `PrincipalNotApplicable` were mixed in the engine.

## Alternatives rejected

- **A distinct status code.** The gRPC codes are the platform's error
  kinds (`stack-rust.md` §4), so a code for one cause would invent a
  kind.
- **A string in the message.** A text is for a person, and the client
  would read it as the rule the context above rejects.
- **A custom header or trailer.** `ErrorInfo` is what gRPC's error
  model gives a cause: a stable reason, a domain naming whose reason it
  is, read by any client with the standard detail types, which
  `google.golang.org/genproto/googleapis/rpc/errdetails` already is in
  the platform's module.
- **A `cause` field on `Reason`.** It answers a batch entry in one call
  and is a contract change, left until a sign above is met.
