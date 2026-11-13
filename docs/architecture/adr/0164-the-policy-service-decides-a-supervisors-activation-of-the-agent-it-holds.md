# ADR 0164: The policy service decides a supervisor's activation of the agent it holds

> Status: accepted; built in `policy/` (#1905) for the policy service's
> half; the platform does not yet feed a supervisor or ask the question,
> and still decides the activation itself. Corrects row 6 of
> [ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)'s
> inventory
> Date: 2026-11-14

Drafted for #1905, which reserves this number, under #1796 (decision
1 of its plan). It records the policy service's half: the shape of the
entity a supervisor is, who feeds it, and the one system policy that
decides what it may do.
[authz.md](../authz.md) §Every present capability has a place states
the result as built.

## Context

ADR-0146's inventory of the statements the platform hands the
evaluator ends its row 6, "identity-derived statement of an operator or
a supervisor", with "none found ... nothing to carry; a future one is a
system policy". That row is wrong for the supervisor. A standalone
host's supervisor activates the agent it holds, and the platform
decides that in `lifecycleAuthorization.supervising`
(`platform/internal/cli/execution.go`): the registration names the
agent, the supervisor holds the assignment, and the operation is
activation. It then asks the evaluator with an Allow of its own making.
That is a decision the policy service does not hold.

What the policy service holds of a supervisor before this decision:

- `Iam::Supervisor` is an entity type with `org` and parent `Iam::Org`,
  `@caller("none")`, so no caller can write it, and it is a principal
  of no action.
- Tenant isolation forbids every request unless the principal is in
  the resource's `org.members`, so an entity that is in no
  `Iam::OrgMembers` is denied everything. Operators and agents are
  `in [OrgMembers, Org, ...]` for that reason.
- A decision composes the organization's `permit`s beside its
  guardrails, except for an `Iam::Operator`, whose reach is its
  delegation alone.

## Decision

1. **The policy service decides a supervisor's activation.** The
   supervisor is an entity the platform feeds, and it carries the agent
   it holds.
2. **The entity is `Iam::Supervisor in [OrgMembers, Org] { org: Org,
   agent?: Agent }`.** `agent` is an attribute and not a parent: a
   parent would hand the supervisor the agent's teams and projects and
   whatever a `permit` or role link names them. It is optional because
   a supervisor can exist before it holds an agent, and a supervisor
   with none decides nothing: the policy reads `principal has agent`
   first, so the request is a plain no-match and not an evaluation
   error. The manifest's strict validation requires that guard.
3. **`@caller("platform")`.** Only the platform feeds a supervisor; a
   scoped caller writing one refuses its whole request, as for an
   operator. `ApplyEntityChanges` gains `agent` on `EntityUpsert`
   (`EntityRef agent = 6`), refused for any type but `Iam::Supervisor`
   with `Error::Attribute("agent")`, and the SDK's generated code is
   regenerated.
4. **It stands in `OrgMembers`.** Without that tenant isolation denies
   it every request, the permit below included. The manifest therefore
   declares it `in [OrgMembers, Org]`, as an operator is. Standing there
   also keeps tenant isolation's refusal of an agent of another
   organization: a supervisor fed an agent of another organization is
   denied by tenant isolation, not by the policy.
5. **`iam:agent.activate` gains `Supervisor` as a principal and no
   other action does.** There is no new action. A request that asks a
   supervisor for any other action does not fit the schema and is
   refused.
6. **One system policy, `supervisor-agent-activate`:**

   ```cedar
   permit (
     principal is Iam::Supervisor,
     action == Iam::Action::"agent.activate",
     resource
   ) when {
     principal has agent &&
     principal.agent == resource
   };
   ```

   The operator policies also compare `principal.org == resource.org`.
   This one does not: tenant isolation denies a resource of another
   organization already, so the comparison would decide nothing a test
   could tell apart.
7. **No `permit` of an organization reaches a supervisor.** A decision
   for an `Iam::Supervisor` composes the organization's guardrails and
   none of its `permit`s, as for an operator (`NO_PERMIT` in
   `Engine::compose_for`). Without it an owner's `permit` on
   `iam:agent.activate` for another agent would let a supervisor
   activate an agent it does not hold. A guardrail still forbids a
   supervisor as it forbids any principal.
8. **Row 6 of ADR-0146's inventory is corrected.** There is an
   identity-derived statement of a supervisor, and it is this system
   policy. ADR-0146's body is frozen; its status line says so.

## Consequences

- The platform's round has to feed an `Iam::Supervisor` per host
  supervisor, in its organization's `Iam::OrgMembers` and `Iam::Org`,
  with `org` and `agent` set to the agent it holds, and to ask
  `iam:agent.activate` of that agent in place of its own Allow. It
  stays to decide the registration and the assignment it reads today
  until then; the policy only compares the agent.
- A supervisor reassigned to another agent is one upsert of the
  entity.
- Nothing checks that a supervisor's `org`, its GRN and its
  `OrgMembers` parent name one organization, as nothing does for an
  operator. The platform feeds them consistently. A supervisor fed
  an agent of another organization, or one the store does not hold,
  is stored, and a decision on it is denied: by tenant isolation for
  the first, by its failed evaluation for the second.
- Nothing else changes: no existing decision, action or policy is
  touched, and the supervisor reaches no action but this one.
- The solver test of the tier now allows exactly two things: sending
  to a delegated operation and a supervisor's activation.

## Signs the decision was wrong

- A second supervisor action is wanted. Then the attribute, and not an
  extra system policy per action, is the question: a set of held
  agents or operations as an operator carries.
- A supervisor holds more than one agent.
- An organization needs a `permit` to reach a supervisor, which item 7
  forbids.
- The platform finds `agent` is not enough to decide from, such as a
  registration it must also consult.
