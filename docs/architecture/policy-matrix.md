# Policy matrix

Where the platform's engine and the policy service answer the same
authorization question differently, as measured by the suite in
`platform/tests/integration/policy_matrix_test.go` (#1915).

## Contents
- Result
- Differences
- Refused
- What the matrix asks
- How both engines reach one state
- Non-agent senders on the machine listener
- Running it
- Open questions

## Result

| | Count |
|---|---|
| Questions asked | 1716 |
| Agreeing: both decided, the same answer | 1706 |
| Differing: both decided, different answers | 10 |
| Refused by the service: no decision | 0 |

The three classes sum to the total. A question agrees when both
engines answer allow or both answer deny. The platform's `unknown`
answer (24 questions) is read as the allow the check returns for it;
the service allowed each of them. A refused question is in neither of
the other classes, whatever the platform answered: a refusal is not a
decision. The 129 questions that were refused until #1933 are agreeing
now (§Refused).

The 72 questions asked again one at a time with `Evaluate`, as
`probe` true and as `probe` false, answered as they did in the batch:
the differing ones and a sample of the rest.

## Differences

All ten are one case: an organization's owner or admin against a
policy of the organization, on the five `iam:policy` actions.

| Question | Platform | Service |
|---|---|---|
| owner, `iam:policy.read` on the policy | allow | deny |
| owner, `iam:policy.update` on the policy | allow | deny |
| owner, `iam:policy.attach` on the policy | allow | deny |
| owner, `iam:policy.detach` on the policy | allow | deny |
| owner, `iam:policy.delete` on the policy | allow | deny |
| admin, the same five actions | allow | deny |

The service's reason in every case is `tenant-isolation` with the
error "entity `Iam::Policy::"<grn>"` does not exist".

**Which rule explains it.**

- The platform: `Role.Actions` (`platform/internal/org/role.go`)
  grants the owner and admin roles `iam:policy.*` on the
  organization's resources, and its engine matches that pattern
  against the question without reading an entity of the resource. The
  policy in the suite exists in the platform's store but is a draft
  no statement of the engine reads.
- The service's `tenant-isolation` system policy is a `forbid` unless
  the principal is in `resource.org.members`. It reads the resource's
  `org`. The service holds no `Iam::Policy` entity: the type's
  `@caller` is `none` (`policy/README.md` §Callers), so no caller
  writes one, and evaluating the policy over an absent entity is an
  error, which the service answers as deny.

The subjects who are denied the actions by both engines (member,
group admin, project member) agree. So does every question about a
policy of another organization.

The platform's answer for a resource that exists only in the platform
is the finding: asked about a policy, the service denies for lack of a
feed, not for lack of a right. The matrix still asks the service about
the policy. The routes no longer do: since ADR-0171 they ask it on the
policy's organization, as the service decides a write to a policy, so
the ten differ here and not in what a route answers.

## Refused

No question is refused now. Until #1933, 129 were, all of one
subject, the operator, asked an action the service's schema does not
apply to an operator. The service still refuses each: the batch entry
answers `INVALID_ARGUMENT`, "the evaluation does not fit the schema",
and the same question asked alone answers that status with a
`google.rpc.ErrorInfo` of reason `PRINCIPAL_NOT_APPLICABLE`, which
[ADR-0167](./adr/0167-the-policy-service-names-a-refusal-for-a-principal-the-action-does-not-apply-to-and-the-client-reads-it-as-a-denial.md)
gives that case alone. The decision client reads that cause as a
denial, and asks a refused batch entry again alone to find it.

- **Subject.** The hosted operator of the first organization.
- **Actions.** Every `iam` action the matrix asks except
  `iam:agent.create` and `iam:agent.configure`: the agent, audit,
  consent, group, operator, org, policy and project actions, on every
  resource kind each admits, in all three organizations.
- **Rule of the service.** `policy/schema/iam.cedarschema` declares
  `Operator` a principal of a few actions only (ADR-0146), so Cedar
  cannot evaluate the others for it.
- **Rule of the platform.** No route admits an operator to them, and
  the matrix records deny, which the client's reading of the cause now
  matches.

`platform/tests/integration/testdata/policy_matrix_refused.txt` lists
the questions refused, and holds none. The suite fails when a question
is refused that the list does not hold, which is where a refusal for
any other reason would show, and when a listed one is decided or no
longer asked.

## What the matrix asks

Every action of the `iam` manifest the platform enforces, for each
subject, on each resource kind the action admits, inside the
organization and across it.

- **Subjects.** An organization owner, an admin, a member, a group
  admin, a project member, a member of each of two other
  organizations, a hosted operator with a grant, and agents.
- **Resource kinds.** An action admits a kind when the platform's own
  route accepts it. The suite probes that off the route as an owner:
  a 200 means admitted, a 400 means not.
- **Inside and across.** Each subject is asked about the resources of
  every one of the three organizations.
- **Excluded.** `iam:agent.activate` is not in the organization
  actions the check route offers, so it is not asked.
  `iam:permit.write` is, since ADR-0172, and is asked on the
  organization. `iam:message.send` is not asked of the platform: the machine
  listener has no platform decision for a send (ADR-0168), so the
  matrix asks the service through the listener instead (§What a send
  is asked). The `cowork` actions are the service's and cowork's.

**How the platform is asked.**

- Users through the real `GET /orgs/{org}/permission-explanation`,
  one pair per call, because it decides with the platform's engine
  until round 11 (ADR-0171). `POST /permission-checks` asks the policy
  service itself since ADR-0171, so comparing the service with it
  would compare the service with itself. A question across
  organizations is refused 403 to a non-member; the matrix records it
  as deny.
- The operator through the grant's own coverage test, within its
  organization, and deny otherwise.
- An agent's non-send actions as deny: no route admits one.

**How the service is asked.** `Evaluations` in batches of 500, as the
caller `platform`.

## How both engines reach one state

The suite writes through the platform's own stores and services: the
organizations, users, groups, projects, memberships, agents,
the operator and its grant, and the consent. The platform's feed
delivers every outbox entry to the stack's policy service until the
outbox is empty, with no entry refused or set aside. The service is
the stack's own, not a fake.

The stack publishes the service on loopback under a certificate that
names `localhost` and `127.0.0.1`, so the suite dials the published
address and overrides no authority. The CA and the signing key come
from `POLICY_LOCAL_DIR`, where the stack leaves them
(`deploy/local/README.md` §policy).

## What a send is asked

Since ADR-0168 the machine listener's send is the policy service's
decision, so there is no platform answer to compare and the matrix
holds no send row. The suite drives the real machine listener against
the real service instead and reads each answer off the message the
listener stores: one stored to the recipient is an allow, and one
stored to the sender from the recipient is garam's refusal.

- A send inside the organization to a second agent is stored.
- A send across the boundary is stored where both organizations'
  halves of the consent stand, and refused where one organization
  consents to nothing.
- A send to an agent the service does not hold is refused across the
  boundary and stored inside it, where garam admits it without asking
  as the routing failure ADR-0013 keeps storable.
- A send to an organization is refused across the boundary.
- A refusal carries one text for every case, so a missing consent and
  an absent agent read alike.
- After one organization withdraws its half, the same send is refused.

## Non-agent senders on the machine listener

No non-agent sender exists on the machine listener's send. An
operator's `POST /messages` is answered 401, shown live by the suite
and read in the code: the operator's token carries no serial, and the
listener admits only a holder token, which carries one
(`callerCredential`, `platform/internal/cli/machine_token.go`). An
agent's control is answered 201.

## Running it

`make -C platform stack-up policy-matrix`, which brings up the
backend first. The forge runs it in `make -C platform suites`. The
suite fails when a difference appears that
`platform/tests/integration/testdata/policy_matrix_differences.txt`
does not list, and when a listed one is gone. The same target runs the
cases of `delegated_authority_policy_test.go`, which put the platform's
read-current questions for a member's delegated authority and for a
hosted operator's controller proof to this service: the matrix's rows
and counts do not change with them.

## Open questions

- Whether the service should hold an `Iam::Policy` entity is not
  decided here. Until it does, the platform asks about a policy on the
  policy's organization (ADR-0171 item 7), and the matrix reports the
  difference of the question on the policy itself.
