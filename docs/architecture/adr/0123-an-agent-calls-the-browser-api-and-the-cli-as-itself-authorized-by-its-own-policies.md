# ADR 0123: An agent calls the browser API and the CLI as itself, authorized by its own policies

> Status: accepted
> Date: 2026-10-28

Drafted for #1633, which reserves this number. The goal is the
owner's of 2026-10-20, recorded on #1633: an agent administers garam
as itself, and is never recorded as a person. garam is read at
55f9b1474efd373e0867e0b366d52d54f693dae3; a claim below that names no
revision holds for it. No implementation ships in this change.

## Context

- **The browser API admits people alone.** `platform/api/openapi.yaml`
  declares two security schemes, `sessionCookie` and `sessionBearer`,
  and both present a session, a row naming a user
  ([ADR-0005](./0005-server-side-sessions.md),
  [ADR-0120](./0120-a-person-signs-the-cli-in-by-the-device-grant-and-holds-a-server-side-session-as-a-bearer.md)).
  Every org-scoped operation resolves that user's membership first and
  refuses a non-member (`platform/internal/api/org.go` `callerIn`).
- **An agent already holds garam-issued, short-lived tokens.** Its
  assignee — the operator or the supervisor holding it — mints them on
  `mintAgentToken`, and every request reads the token's row joined to
  the agent's assignment and lineage, so a reassignment or a recovery
  refuses it from the next request
  ([ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md)).
  A token carries an audience: garam's machine listener, or a hosted
  control service it is to be shown to. An operator proves its own key
  by `private_key_jwt`
  ([ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
- **What ADR-0100 and ADR-0102 refused stays refused.** An agent holds
  no long-lived secret and no private key; a token cannot mint a
  token; an agent bearer cannot mint its successor or enroll peers.
- **The shortcut is ruled out by the goal.** An agent running under a
  person's CLI session would be recorded as that person, and revoking
  the agent would mean revoking the person's session.
- **cowork will need the same kind of credential.** The owner decided
  on #1655 (comments 6456001127 and 6464161444) that cowork gets one
  principal per organization, proving one deployment key by
  `private_key_jwt` and holding tokens scoped to one organization.
  That is ADR-0129, not yet written.
  [ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
  moves the engine to a Cedar service and is not built.

### Measured: the engine answers an agent as it answers a user

`platform/internal/authz` matches subject strings. `parseSubject`
reads the subject and its groups as GRNs with no service type checked,
`PolicyCache` reaches a policy by comparing attachment GRNs, and the
only scope check is `subject_org` against the object's organization.
No non-test file under it names an agent or a user.

A scratch integration test confirmed it against PostgreSQL 17.11 (the
image `deploy/local/` pins), in a container of its own, through the
gRPC server the suite's `policyEngine` fixture serves
(`platform/tests/integration/fixtures_test.go`). It was not committed.
One organization held a stored agent, a user, and a group holding
both, written by `org/postgres` `AddGroupMember`. Three active
policies were attached to both subjects or to the group: an Allow on
`project:read`, a Deny on one project, and an Allow on `group:read`.
`Check` and `Explain` were each asked, with no caller statements,
about six pairs: `project:read` on two projects, `project:delete`,
`group:read` on the group, `org:delete` and `policy:attach`.

- **The decisions were the same for both subjects in all six**, and so
  were the statements `Explain` reported as matched. The attached
  Allow allowed, the Deny overrode it, the group's policy reached each
  subject through `ListGroupsByMember`, and an action no policy names
  was refused.
- **An object of another organization was refused for both**, with
  the same `subject is not a member of the object's organization`.

The difference between the two is not in the engine. For a user, the
api server collects role statements: membership roles, direct roles,
and group and project roles (`platform/internal/api/statement.go`). An
agent holds no membership
([ADR-0014](./0014-agent-authentication.md)), so the server has no
roles to collect for it, and `callerIn` would refuse it before asking.

## Decision

**An agent calls the browser API with an agent token minted for the
browser API as its audience, presented as a bearer and read on every
request. It is authorized by the engine on its own GRN and the teams
holding it, with no role statements. A floor no policy lifts keeps it
in its own organization and keeps it from turning its token into a
lasting credential. The garam CLI runs as an agent from a token file
its assignee keeps current.**

1. **The credential is an agent token with a browser audience.**
   - The agent's assignee mints it on `mintAgentToken`, as it mints the
     agent's other tokens. The audience is the browser API, a value
     beside garam's machine listener and a control service.
   - A token of one audience is accepted by that audience alone. The
     token the adapter holds for the machine listener reaches no
     browser operation, and a browser token reaches no machine
     operation.
   - It is presented as `Authorization: Bearer <token>`, the scheme a
     CLI session uses. Its shape (`garam_at_` and 64 hex digits) tells
     it from a session's 43-character token. A value of either shape
     is looked up as that shape only. A request carrying an
     `Authorization` header is read by it alone, as now.
   - The mechanism is not specific to agents: a garam-issued machine
     token with the browser audience, resolved per request to a
     subject GRN and the record it rests on. ADR-0129 may admit
     cowork's principal through it, with tokens from its
     `private_key_jwt` exchange. This ADR admits agents alone; an
     operator's and a supervisor's tokens take no browser audience.
2. **Lifetime and revocation are the agent token's.**
   - It lives the configured agent token lifetime: one hour by
     default, at least 20 minutes and at most 24 hours. The assignee
     refreshes it in the same window as the agent's other tokens, and
     a refresh does not void the token it replaces.
   - Every request reads its row joined to the agent's assignment and
     lineage, as the machine listener does. A reassignment, a
     recovery, deleting the operator holding it or deleting its
     organization refuses it from the next request.
   - It carries no execution token. The generation fence keeps two
     runtimes from writing one agent's queue and outbox (ADR-0078).
     Administration through the browser API is decided per request on
     current policy, which is the same whichever runtime asks.
   - No route lets an agent mint or refresh its own browser token.
3. **Authorization is the engine's, on the agent's GRN.**
   - The subject is the agent's GRN, its groups are the teams whose
     members list that GRN, and `subject_org` is the organization its
     GRN names.
   - The api server collects no statements for it: no membership,
     direct, group or project role. An agent holds no role, so its
     authority is exactly the policies that reach it: those attached
     to it, those attached to a team holding it, and the
     organization's unattached active policies.
   - In place of the membership check, the server checks that the
     path's organization is the one the agent's GRN names. The token's
     read already joins the agent's record.
   - The same `Check`, `CheckBatch` and `Explain` decide it, so
     `checkPermissions`, `explainPermission` and filtered lists answer
     an agent as they answer a person.
   - **Under ADR-0130 the rule carries over unchanged**: an agent is
     decided on the grants naming it or a team holding it, by the
     decision a person gets. The agent becomes a principal entity type
     in the schema beside the user. The floor below becomes system
     policies, a `forbid` no organization policy overrides. Nothing
     here waits on ADR-0130.
4. **The floor.** These refusals are made before the engine is asked,
   whatever a policy says:
   - **Its own organization only.** An agent's token reaches only
     operations under `/orgs/{org}` whose `{org}` is the organization
     its GRN names. Signing in, sessions, the device grant, `/me`,
     listing or creating organizations, and reading or accepting an
     invitation are refused: each is about a person's own account or
     membership, which an agent does not have.
   - **No lasting credential from a token.** An agent's token reaches
     no operation whose answer is a secret that admits a principal, or
     that makes such a secret usable: `issueOperatorKey`,
     `createInvitation`, `mintOperationAuthority` and
     `approveAgentEnrollment`. An hour's bearer token that could buy an
     operator key, a membership, an authority to create an agent or a
     supervisor credential would outlive every revocation it rests on,
     which is the credential perpetuating itself that ADR-0100 refused.
   - **No further floor.** Policy writes, team membership and deleting
     the organization are left to policy. Each stays inside the policy
     system and its record: a Deny attached to the agent or a team
     holding it bounds them, and the act is recorded under the agent's
     GRN. A floor on an agent widening its own policies would not hold
     against two agents widening each other, and the engine cannot
     compare two grants to tell widening from narrowing.
5. **Every record names the agent.**
   - The browser API's request line names the agent's GRN, in the
     field the machine listener uses for a machine principal, so a
     reader can tell it from a person.
   - Minting a browser token is a credential event of its own kind,
     beside `agent_token`, with the agent's GRN as subject (ADR-0114).
   - Every audit event the browser API writes names the authenticated
     principal as its actor. No event of an agent's act names a person.
6. **The garam CLI runs as an agent without a sign-in.**
   - `GARAM_TOKEN_FILE` names a file holding the agent's browser token.
     Where it is set, every command reads the file, presents the token
     as a bearer and sends nothing from the credentials file.
   - The file is read per command and never written. The CLI refreshes
     nothing: the assignee replaces the file atomically, as ADR-0100
     has it replace the adapter's token files. A file anyone but its
     owner can read is refused, as the credentials file is.
   - `garam login` and `garam logout` refuse while it is set. A
     refused token exits as not signed in.
   - The organization comes from `--org` as for any caller, and the
     floor refuses any other.

## Consequences

- An agent administers garam under its own identity, with the
  authority its organization attached to it, and stops when its
  assignment or lineage moves.
- An organization's unattached active policies reach agents as they
  reach people. An org-wide Allow written for people becomes an
  agent's grant once an assignee mints it a browser token. A person
  who wants an Allow to stop at people attaches it to them.
- An agent with no policy reaching it can do nothing on the browser
  API, where a person with no policy still holds the member role's
  reads.
- An assignee that does not mint a browser token keeps its agents off
  the browser API whatever their policies say. How an Operator hands
  the token to its agent is the Operator's side (#1633, out of scope).
- An agent cannot issue keys, invite people, mint operation
  authorities or approve enrollments, so an organization that wants
  an agent to stand up operators or agents still needs a person for
  that step.
- The agent's live tokens span three audiences, so the bound on live
  tokens per agent rises from two audiences' worth to three.
- No audit event records a browser act by anyone yet: decision and
  change events are unbuilt ([audit.md](../audit.md)). Until they
  are, an agent's acts are recorded by the request line and by the
  credential events of its tokens.
- The responsibility documents this changes are paired in this pull
  request: [machine-identity.md](../machine-identity.md),
  [authz.md](../authz.md), [api-surface.md](../api-surface.md),
  [cli.md](../cli.md), [agents.md](../agents.md) and
  [audit.md](../audit.md).

## Signs the decision was wrong

- An organization finds an agent holding a grant it wrote for people
  through an unattached policy, often enough that agents should have
  been kept out of org-wide policies.
- Agents are routinely granted `policy:*`, and an agent widening its
  own authority, or two widening each other, is found in the record:
  the floor should have covered self-reaching policy writes.
- Organizations keep asking a person to issue keys or invitations for
  an agent that is otherwise trusted to administer: the credential
  floor is too wide.
- A browser token is found accepted on the machine listener, or a
  machine token on the browser API.
- An agent's act is found recorded under a person's GRN.

## Rejected alternatives

- **An agent running under a person's CLI session.** It records the
  agent as the person and ties the agent's revocation to theirs.
- **The machine token's audience reused for the browser API.** A
  token shown to the adapter for messaging would then administer the
  organization, and an assignee could not give an agent one without
  the other.
- **An agent proving a key of its own by `private_key_jwt`.** It puts
  a long-lived key in the runtime, which ADR-0100 and ADR-0102 kept
  out of it, and lets the agent refresh its own access.
- **An agent given the member role.** It makes the agent a member,
  which ADR-0014 refused, and grants it every member's reads whatever
  its policies say.
- **A floor on policy writes that reach the agent itself.** Refusing
  an agent any policy, attachment or team write that reaches itself.
  It is defeated by two agents widening each other, and it also
  refuses an agent narrowing itself.
- **No floor at all.** It lets an hour's token buy an operator key or
  a membership that outlives it.
