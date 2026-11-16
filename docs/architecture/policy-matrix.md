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
| Questions asked | 1723 |
| Agreeing: both decided, the same answer | 1713 |
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
feed, not for lack of a right. Nothing here is fixed, and nothing is
narrowed out of the matrix.

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
- **Excluded.** `iam:agent.activate` and `iam:permit.write` are not
  in the organization actions the check route offers, so they are not
  asked. `iam:message.send` is asked through sends alone. The
  `cowork` actions are the service's and cowork's.

**How the platform is asked.**

- Users through the real `POST /orgs/{org}/permission-checks`, at
  most 50 per call. A check across organizations is refused 403 to a
  non-member; the matrix records it as deny.
- The operator through the grant's own coverage test, within its
  organization, and deny otherwise.
- An agent's non-send actions as deny: no route admits one.
- A send through the real machine listener. A send no consent covers
  is answered 201 with garam's refusal stored as a message from the
  recipient (ADR-0013), so the matrix reads the stored sender: the
  sender asked is allow, the recipient is deny. Pairs: A to its second
  agent, A to B and B to A (mutual consent), and every pair with C,
  which consents to nothing.

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
does not list, and when a listed one is gone.

## Open questions

- Whether the service should hold an `Iam::Policy` entity, or the
  platform should ask it nothing about a policy, is not decided here.
  The matrix reports the difference and decides nothing.
