# ADR 0165: Every request that changes a fed entity answers after its delivery, by the marks its own write raised

> Status: accepted; built in `platform/internal/policyfeed`,
> `platform/internal/api`, `platform/internal/machineapi` and
> `platform/internal/org/postgres` (#1909); extends
> [ADR-0159](./0159-role-links-follow-the-entities-in-one-pass-the-platforms-tables-win-a-failed-write-and-a-request-finishes-its-work-before-it-waits.md)
> item 3's wait from the membership routes to every route that raises a
> mark, and
> [ADR-0162](./0162-a-groups-and-a-projects-roles-reach-the-policy-service-as-scoped-links-the-platform-writes-and-a-users-direct-roles-are-removed.md)
> in the same way, with the answers and reasons of
> [ADR-0160](./0160-a-role-link-the-policy-service-refuses-for-good-is-set-aside-and-the-members-links-are-removed.md)
> unchanged
> Date: 2026-11-14

Drafted for #1909, which reserves this number.
[authz.md](../authz.md) §Role links states the result as built.

## Context

ADR-0159 and ADR-0162 make the routes that write a member's role, and
a group's or a project's roles, wait for the policy service to hold the
change, and leave "the other requests that change a fed entity" not
waiting. A request that does not wait answers 2xx while the entity is
not yet in the service, and the person's next request is evaluated
against the old state: a created organization denies its creator until
the loop has run, and a created agent is denied its first call.

What the outbox raises is read from the triggers of migration 000079
(`policy_outbox_track`) and from the stores, not from the handlers:

- `orgs` marks `Iam::Org` and `Iam::OrgMembers`; `groups` and
  `projects` mark `Iam::Team` or `Iam::Project` on insert and delete
  and `Iam::ScopedRoleLink` on insert, delete or a change of `roles`;
  `users` and `agents` mark themselves; `operators` and
  `operator_delegations` mark `Iam::Operator`; `org_members` marks
  `Iam::RoleLink` always and `Iam::User` on insert and delete;
  `message_consents` marks `Iam::Org`; `group_members` and
  `project_members` mark the member's `Iam::User` or `Iam::Agent` when
  the member's service is `iam`.
- A delete of an organization, a group or a project cascades, and the
  marks it raises (every member, agent, operator, team and link under
  it) are not known to the handler before the delete runs.
- A mark can be set aside for good (ADR-0160) only if it is a link
  kind, and an older set-aside mark of one entity must not change the
  answer of an unrelated request: that is why ADR-0162 answers an
  update that did not write `roles` at once.
- The machine listener is a separate process. It has no delivery loop,
  no policy address and no issuer, and its compose service holds none.
  It holds a gate over the outbox whether or not the feed is on
  (`platform/internal/cli/machine.go`).

## Decision

1. **A route waits if its write raises a mark, and by that mark.**
   Each handler that writes a fed entity names the marks its own
   write raised and waits on them with `Gate.AwaitMarks`, one call that
   reads each mark by its kind's state function (an entity, a role link
   or a scoped link) and answers at once for an empty list. It is the
   same bound (5s), the same two answers (`change_not_applied` and
   `change_refused`) and the same stored resource as ADR-0159 and
   ADR-0160; no new reason is added, since a mark that is held or
   set aside is exactly what those two say. A route that raised no mark
   answers at once whatever older mark is present. The routes and what
   each raises are in [authz.md](../authz.md) §Role links, and a test
   reads both API specs and fails on a writing operation the table does
   not decide.
2. **A request that raised several marks waits on all of them, under
   one bound.** The wait is one deadline over the list, not one
   per mark, so a request is answered within the bound however many
   it raised. The role link and scoped link marks are named in that
   same list, by `roleLinkMark` and `scopedLinkMark`, and not by a wait
   of their own before it: CreateOrg, DeleteOrg, CreateGroup,
   DeleteGroup, CreateProject, DeleteProject, RegisterPassword and
   AcceptInvitation each wait once. DeleteOrg's links are among the
   marks its cascade recorded. A write of a group's or a project's
   roles raises no other mark and keeps its one scoped wait. A
   registration that fails after its user is stored
   (`abandonAccount`) deletes the user and raises marks with no wait:
   it answers an error that stores nothing the person acts on.
3. **A cascading delete records what it raised.** `DeleteOrg`,
   `DeleteGroup` and `DeleteProject` collect the marks of their own
   transaction when the request's context carries a collection
   (`policyfeed.WithRaised`), by reading the outbox rows whose `xmin` is
   the transaction's id before commit. A caller with no collection
   reads nothing.
4. **Signing in waits only when it created the user, and at the bound
   redirects.** The OIDC callback waits on the new user's mark up to
   the bound; a repeat sign-in raised none and answers at once, so a
   user's own pending mark from a stuck feed does not lock the person
   out. The session, the spent state and the cookies are stored before
   the wait, and the answer is a redirect a browser follows, which
   cannot carry a 503 it would show. So at the bound the callback
   redirects as on success and logs that the user's mark was still
   pending, and the first request that needs the user in the policy
   service waits on the mark again. Registration and accepting an
   invitation keep the 503, naming the organization, and wait on the
   member's link and the user's mark in one list.
5. **A replacement does not wait, and the store says which it was.**
   The machine routes that replace an agent that exists
   (`admitAgentEnrollment` with `replaces`, `enrollStandaloneAgent`
   with a replacement approval) raise no mark and answer at once; a
   creation, including a replayed one, waits. The standalone route
   decides by `EnrollmentResult.Replaced`, which the store sets from
   the approval it spent (an approval that names an expected assignee
   replaces an agent), and not by `ReplacesActivation`, which is also
   empty where the agent replaced was never activated.
6. **The machine listener waits by observing.** The machine process
   holds a gate over the outbox with no loop to wake
   (`NewObservingGate`), always installed whether or not the feed is on,
   and waits as the API does. With the api's loop running it answers
   when the pass has delivered; with the feed off there are no marks
   and it answers at once; with the feed on in the database and no
   deliverer anywhere, every create answers `503` after the bound for
   good, with no reason, as every other 503 of that surface does
   (`machine.yaml` lists it on the three routes). The api's loop paces
   the wait: it polls every second and backs off up to 30 seconds after
   a failed pass (`platform/internal/cli/policy_delivery.go`), so after
   a recovery a creation can still answer `503` for up to that long.
   It adds no setting and changes no deployment. Two alternatives fail:
   answering without waiting leaves an agent that is denied its next
   call, and delivering from the machine process needs the policy
   address, the CA and the issuer on it, which is a `deploy/` change
   this decision does not make.
7. **With the feed off, routes answer as before; with it not
   configured, the api's do.** With the feed off the triggers raise
   nothing and every wait has no marks. An api with no policy address
   has no gate, so `awaitMarks` returns at once and its handlers do
   not read the outbox. The machine listener is not in that case: its
   gate is always installed (item 6), so a feed on and nothing
   delivering holds its creations to the bound.

## Consequences

- A created organization, group, project, operator or agent, an added
  member, and a first sign-in are in force in the policy service when
  the 2xx is sent.
- `Gate` gains `AwaitMarks`; its fakes gain a method, and
  `upsertUser` reports whether it created the user.
- A request that fails after its own commit can still answer `503`
  with the work done; clients already treat that answer as "stored".
- A request marking many entities (a deleted organization) waits under
  one bound, and a pass reads at most 500 marks, so one that raised
  more answers `change_not_applied` at the bound, as it names a stored
  change. A wait reads each mark still pending once per 50ms tick and
  stops reading one it has seen absent; a batched read over the list
  was not added, since it needs a store method for each of the three
  kinds of read.
- The wait is on a mark's presence, not its generation. A second
  request that raises a mark the first is waiting on holds the first
  to the bound while the loop has not delivered, and releases both once
  it has; a request is never answered early by that.
- A first sign-in at the bound answers a redirect, so a person can land
  in the console before the policy service holds their user, and their
  first request that raises a mark of it waits again.
- The machine listener's creating routes now fail with `503` while the
  api's loop is down and the feed is on, where they answered 201.

## Signs the decision was wrong

- A request answered 2xx whose next request is denied while the
  service is up: a route raises a mark the table does not name.
- `change_refused` answered to a request that raised no link mark: a
  wait is reading a mark that is not its own.
- A deleted organization answering `change_not_applied` while the loop
  is healthy: the cascade raises more marks than a pass delivers within
  the bound.
- A person who cannot sign in while the feed is stuck: a repeat
  sign-in is waiting.
- A cascading delete answering with fewer marks recorded than the
  outbox holds for it, and no error: the recorder reads the rows whose
  `xmin` is the top-level transaction id, and a later `EXCEPTION` block
  in `policy_outbox_track`, or a savepoint in the delete, gives a mark
  a subtransaction id that the read misses.
- The machine listener's creating routes timing out in a deployment
  whose api runs a loop: the observing gate and the loop are reading
  different outboxes.

## Errata

- 2026-11-14 — Decision item 5 says the machine routes that replace an
  agent that exists (`admitAgentEnrollment` with `replaces`,
  `enrollStandaloneAgent` with a replacement approval) "raise no mark
  and answer at once". That held while only an agent and its
  memberships were fed. #1917 feeds a supervisor
  ([ADR-0164](./0164-the-policy-service-decides-a-supervisors-activation-of-the-agent-it-holds.md)),
  and a replacement moves the agent's assignment, which marks the
  supervisor it leaves; an enrollment also writes the new supervisor's
  registration, which marks it. Item 1 stands and the routes wait on
  those marks: `enrollStandaloneAgent` waits on the new supervisor's
  mark in every case, a replacement and a replay included, and
  `admitAgentEnrollment` waits on the replaced supervisor's mark when
  the assignee it replaces is a supervisor. What a replacement still
  does not wait on is the agent's own mark, which it does not raise,
  and an admission replacing an operator's agent raises no mark and
  answers at once. A creation, including a replayed one, still waits
  on the agent's mark. [authz.md](../authz.md) §Supervisors states the
  present routes.
