# Policy matrix

Where the platform's engine and the policy service answer the same
authorization question differently, as measured by the suite in
`platform/tests/integration/policy_matrix_test.go` (#1915).

## Contents
- Result
- Differences
- What the matrix asks
- How both engines reach one state
- Non-agent senders on the machine listener
- Running it
- Open questions

## Result

| | Count |
|---|---|
| Questions asked | 1723 |
| Questions agreeing | 1713 |
| Questions differing | 10 |
| Of the agreeing, refused by the service | 129 |

A question agrees when both engines answer allow or both answer deny.
The platform's `unknown` answer (24 questions) is read as the allow
the check returns for it; the service allowed each of them.

The 129 refusals are all of one subject, the operator, asked an action
the service's schema does not apply to an operator. The service
answers `INVALID_ARGUMENT` ("the evaluation does not fit the schema")
to the batch entry and to the same question asked alone. The platform
denies each, because no route admits an operator to the action, so
the answers agree as denials. They are counted here because a refusal
is not a decision.

The 195 questions asked again one at a time with `Evaluate`, as
`probe` true and as `probe` false, answered as they did in the batch:
the differing ones, the refused ones and a sample of the rest.

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

The platform host reaches the service by its container address,
because the stack publishes neither its port nor its key and CA. The
suite is given the address, the CA and the signing key by path. The
connection names the authority `policy` because the service's
certificate is for that name.

## Non-agent senders on the machine listener

No non-agent sender exists on the machine listener's send. An
operator's `POST /messages` is answered 401, shown live by the suite
and read in the code: the operator's token carries no serial, and the
listener admits only a holder token, which carries one
(`callerCredential`, `platform/internal/cli/machine_token.go`). An
agent's control is answered 201.

## Running it

`make -C platform policy-matrix COMPOSE_LOCAL="..."`, against the
stack from `make -C deploy/local backend-up`. It is in no whole-set
target and no workflow. The suite fails when a difference appears that
`platform/tests/integration/testdata/policy_matrix_differences.txt`
does not list, and when a listed one is gone.

## Open questions

- Whether the service should hold an `Iam::Policy` entity, or the
  platform should ask it nothing about a policy, is not decided here.
  The matrix reports the difference and decides nothing.
