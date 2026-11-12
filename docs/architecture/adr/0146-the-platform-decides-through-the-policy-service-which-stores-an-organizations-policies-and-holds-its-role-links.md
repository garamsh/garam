# ADR 0146: The platform decides through the policy service, which stores an organization's policies and holds its role links

> Status: accepted; not yet built. The platform's listeners still
> decide with `garam serve authz`, and the policy service serves no
> write RPC
> Date: 2026-11-12

Drafted for #1799, under #1796, which holds the owner's decisions of
2026-11-12 and reserves this number. It carries out
[ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
§7, "every grant is the policy service's", and supersedes
[ADR-0140](./0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md)
in part: its feed "off by default" and its outbox of entities alone.
It builds on
[ADR-0138](./0138-a-caller-of-the-policy-service-presents-a-short-lived-signed-token-and-owners-write-entities-through-a-sequenced-rpc.md),
[ADR-0141](./0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md),
[ADR-0144](./0144-an-organization-may-write-permit-policies-over-resources-inside-itself.md)
and
[ADR-0145](./0145-an-action-is-a-type-and-a-verb-joined-by-a-dot.md).
[ADR-0147](./0147-a-message-between-two-consenting-organizations-is-tenant-isolations-one-exception.md)
is its pair for consent. garam is read at
`85739f0026e01ade88f7fe6f926faabc005c7ed7`; a claim below that names
no revision holds for it.

## Context

- **Two evaluators run.** The platform's browser and machine listeners
  decide with `platform/internal/authz` behind `garam serve authz`,
  from statements the callers collect: role-derived at the api server,
  identity-derived at the machine listener, and persisted IAM
  documents attached to a user, an agent or a group
  ([authz.md](../authz.md) §Statement collection, §Policy attachment).
  The policy service decides Cedar over the manifests and serves
  `Evaluate`, `Evaluations` and `ApplyEntityChanges`; cowork asks it
  directly (ADR-0141).
- **The service stores and does not serve the write.** The `grant`
  crate in `policy/crates/grant`
  holds role links, guardrails and `permit`s with their versions,
  activation, roll-back and removal, and no RPC reaches it.
- **The feed carries entities and no link.** The outbox of ADR-0140
  is a set of dirty entities read at delivery under one sequence. A
  role link follows nothing.
- **A write is answered before a decision can see it.** The loop
  delivers about once a second, so a request that changes a
  membership is answered as done while the service still decides with
  the old one.
- **The caller supplies a statement the service must not take.**
  ADR-0130 §8 and ADR-0138 item 7 forbid a caller handing the service
  a grant. One present path does it: the hosted operator's delegation
  (inventory, row 15).

## Decision

1. **Every decision the browser and machine listeners take is the
   policy service's `Evaluate`.** `garam serve authz`, its gRPC
   (`platform/proto/garam/authz/v1`), its tables (`policies`,
   `policy_versions`, `policy_attachments`), its `garam_policy_change`
   notification and `CheckCurrent` are removed. No stored document of
   the earlier format is converted: ADR-0117's rule, and garam is not
   operating. The migration drops the tables with the code that read
   them.
2. **The policy service is a required part of every deployment, and a
   listener that cannot have a request decided refuses it.** Today the
   feed runs only where `GARAM_POLICY_ADDR` is set; the address
   becomes required, and an unreachable service answers the request
   as unavailable and never as allowed.
3. **The service serves the write of an organization's policies to
   the platform.** Role links, guardrails and `permit`s, with their
   versions, activation, roll-back and removal, and the reads the
   console needs. The request names the writer and the service
   evaluates the writer's action on the organization, read current,
   before it stores, so no caller's mistake stores what its writer
   could not write. Storing or making effective a `permit` asks for
   ADR-0144's right. A write and a refused write are recorded as
   [ADR-0148](./0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)
   has them. The narrow form, and what is left open, are under
   §Security below.
4. **A member's role is a role link the platform writes from its
   memberships and removes when one ends,** as the caller `platform`
   and with no writer: the membership is the platform's fact, already
   authorized where it was changed. The link is the policy service's
   copy of that fact, and it follows the membership through the
   outbox, so a restore or a rebuild rewrites it. The same holds for a
   role of a team or a project, which the platform records and the
   link repeats.
5. **A writer reads its own write.** A request that changes an
   entity, a membership or a policy is not answered as done before
   the policy service decides with it. The mechanism is under
   §Reading one's own write below.
6. **Each present capability has a place in the model.** The
   inventory below is that list; every row is read off the code at
   the revision above or off [authz.md](../authz.md).
7. **The console's editor.** A role assignment is a form. A guardrail
   and a `permit` are Cedar text, validated against the manifests
   before they are stored, with the registry's types and actions
   (ADR-0143 item 6) beside the editor. The platform relays the text
   to the service and stores none of it.
8. **cowork asks the policy service alone.** Its path through
   `checkPermissions` goes with the evaluator; the platform's
   `checkPermissions` answers the console and the CLI, for people and
   agents, and is `Evaluations`.

### Inventory

Today is a file or a document section; after is the form the
capability takes. A role link is to a system role group, a system
policy is one under `policy/system-policies/`, and a fed entity is
one ADR-0140's loop delivers.

| # | Capability | Today | After |
| --- | --- | --- | --- |
| 1 | Role of an org member | `platform/internal/org/role.go`, `platform/internal/api/statement.go`; §Statement collection, role-derived | a role link to `role-owner`, `role-admin` or `role-member`, written from the membership (item 4) |
| 2 | A user's direct roles, stored on the user | same files | a role link to the role's system group, written from the user's record |
| 3 | A group's role on a resource | same files | a scoped role link (the `role-scoped-*` templates), the group being the principal, written from the platform's record |
| 4 | A project's roles | same files | the same scoped link on the project's entity |
| 5 | Identity-derived statement of an agent | `platform/internal/machineapi`; §Statement collection, identity-derived | a system policy per action an agent holds on its own organization (`agent-message-send.cedar` exists); the rest are added to the system tier by the implementation |
| 6 | Identity-derived statement of an operator or a supervisor | none found: nothing at the revision builds one and hands it to the evaluator (search below) | nothing to carry; a future one is a system policy |
| 7 | A policy attached to a user, an agent, a group or the organization | `policy_attachments`; §Policy attachment | removed, ADR-0117; an organization's equivalent is a guardrail it writes or a `permit` an owner writes (item 3), and none is converted; an administrator no longer writes an `Allow` |
| 8 | Versions, activation, roll-back and removal of a policy | `policy_versions`; §Policy persistence | the service's store through item 3's RPCs |
| 9 | Refusing to detach an active policy's last attachment (ADR-0031) | the attachment code | removed with attachments; a policy is effective by activation and a link by deletion |
| 10 | A watch grant, `iam:agent.watch`, and an activate grant, `iam:agent.activate`, held by no role | an attached policy; `platform/internal/api/watch.go` | a `permit` of the organization on the agent (ADR-0144), written under `iam:permit.write`, so granting either becomes an owner's act |
| 11 | Tenant check of the subject's organization | `subject_org` on the call (ADR-0038) | the system policy `tenant-isolation.cedar`; the field goes with the evaluator |
| 12 | The decision read past the cache, `CheckCurrent` | `platform/internal/authz` | `Evaluate` with `Read` set to `READ_CURRENT` |
| 13 | Cross-organization consent on a send | `platform/internal/api/consent.go`, the send path | a fed attribute on the organization and one exception in tenant isolation (ADR-0147) |
| 14 | A user actor's half of a delegated operation | `platform/internal/operator/authority_service.go` | `Evaluate` with `READ_CURRENT` for the user, whose roles are links |
| 15 | A hosted operator's half of a delegated operation | `audienceAuthorization` in `platform/internal/cli/controller_proof.go` | a fed fact, no grant on the call: see §A hosted operator's delegation |
| 16 | cowork's principal in `checkPermissions` | §cowork's principal | removed; cowork asks the service (item 8) |
| 17 | Explanation of a decision | `platform/internal/api/permission_explanation.go`; §Explain | an RPC of the service naming the policies that decided, the system tier's among them, named at the implementation |
| 18 | `checkPermissions` | `platform/internal/api/permission_check.go` | `Evaluations`, bounded as today |
| 19 | `listPermissionHolders` | `platform/internal/api/permission_holder.go` | `Evaluations` over the page's candidates, bounded as today |
| 20 | The permission explanation endpoint | `ExplainPermission` | row 17 |
| 21 | Enforcement in a handler, `authorizeOn` | `platform/internal/api` | `Evaluate` |
| 22 | Enforcement on the machine listener | `platform/internal/machineapi/message.go` | `Evaluate` with the agent as principal |
| 23 | Search filtered by the caller's reads | `searchOrg` | `Evaluations` |
| 24 | The HTTP endpoints that create, version, activate and attach a policy document | §Managing policies over HTTP | removed with the documents; endpoints for links, guardrails and `permit`s relay item 3 |
| 25 | The console's permission pages and the CLI's commands over them | `web-app.md`; `garam-cli` | the editor of item 7 and the same endpoints as rows 18 to 24; the CLI follows the endpoints |

The search for row 6 was `grep -rn "Statement{" platform/internal
--include=*.go`, test and generated files excluded. It finds the
sites that build a statement: `api/statement.go` (roles),
`machineapi/message.go` (an agent), `cli/authority.go` (a user actor),
`cli/controller_proof.go` (an operator's delegation) and the
evaluator's own `authz/server.go`. None builds an identity-derived
statement for an operator or a supervisor; the `supervisor` the
search of `platform/internal` also finds is the process supervisor of
`garam serve`, not a principal.

### A hosted operator's delegation

Row 15 is a fact the platform feeds. The operator's entity carries the
operations its current grant holds. The system tier gains one policy
per delegable operation that permits an operator of the resource's
organization holding that operation, and an organization's guardrails
forbid as they do today. The platform still refuses an ended or
expired grant before it asks, since the grant's state is its own, and
nothing is supplied on the call. The manifest (`Iam::Operator` as a
principal of those actions, and the attribute) and the feed change
with the implementation, which this ADR does not do.

### Reading one's own write

An entity or a membership change commits its outbox mark with the
change. After the commit the writer wakes the delivery loop and waits
until the marks it wrote are no longer in the table, up to a bound of 5
seconds; the loop deletes a mark only after the service has applied it.
The bound rests on the loop in `platform/internal/policyfeed`: it polls
every second and a pass is one call, so a healthy delivery lands within
about 2 seconds; a failing pass doubles the wait from the poll, to 2
seconds and then 4, so a service that keeps failing passes the bound,
which is the case the answer is for. An implementation does not choose
its own bound. At the bound the request is answered as unavailable with
the fact that the change is stored and not yet applied, which a retry
may read. A request waiting on another replica's loop waits on the
table, and the wake is an optimization that the polling does not need.

- **Cost.** A request that changes an entity, a membership or a role
  waits about one delivery round trip, and at most the bound. A read
  and a decision cost nothing more. A hot entity marked again while it
  is delivered delays a waiter until the next pass.
- **A policy write.** The service stores and decides on the same
  store and holds no cache, so a write answered by the service is
  read by the next decision. A cache the service adds later must not
  serve a decision older than a write it answered.

### Security

- **A role link a writer writes for another** is refused unless the
  writer holds the role it links: an owner links any of the three, an
  admin links `role-admin` and `role-member` and never `role-owner`, a
  member links none. Holding `iam:policy.*` alone would let an admin
  link `role-owner` to itself, which widens access with no one
  holding the right. The wider form, an admin linking `role-owner`,
  stays open for the owner.
- **A write with no writer is the caller `platform`'s alone,** and
  only for a role link of the three org roles or a scoped role
  template, from a membership. A body of any other kind with no
  writer is refused, and any other caller naming none is refused.
- **Only an owner writes a `permit`,** as ADR-0144 item 7 has it:
  `iam:permit.write` is held by `role-owner` alone. A role other than
  the owner writing one, bounded by what it holds, stays open as
  ADR-0144 lists it.
- **A write the service refuses stores nothing.** The check and the
  store are one step, so a refused write leaves no stored version.
- **The console never relays a body unvalidated.** The service
  validates again and does not take the console's word.

## Consequences

- **One decision path.** The same Cedar, the same system tier and the
  same explanation answer the console, the CLI, the machine listener
  and cowork, and a policy an organization writes is the one that
  decides.
- **The platform loses a store and a process.** The policy tables, the
  evaluator's gRPC and its process leave the platform; the service is
  required, which is one more dependency a deployment must run.
- **Availability is the service's.** A decision that cannot be made is
  refused, so a service outage stops the platform's writes and reads
  that ask it, which is what fail closed means here.
- **A membership change costs a wait.** Item 5 buys that a console
  user is not told a role was changed while the next request is still
  denied.
- **An administrator loses writing an `Allow`.** Against today, where
  an administrator attaches a policy holding an `Allow`, it keeps
  assigning the roles it holds and writing guardrails and no longer
  writes a `permit`. Granting `iam:agent.watch` and
  `iam:agent.activate`, which no role holds, becomes an owner's act.
- **The outbox carries links.** ADR-0140's mark kind gains the role
  link, delivered with the entities under the same sequence; the
  backfill, `garam policy-feed start`, writes links as well.
- **Unbuilt at merge.** Until the implementation lands, the earlier
  evaluator decides as [authz.md](../authz.md) §Evaluation states.

## Signs the decision was wrong

- A change's wait reaches the bound in ordinary operation, which
  means the loop's interval or the batch is too large for the writes
  it carries.
- The role-link rule is worked around by granting an owner role for a
  task an admin needs, which means the wider form is wanted.
- A capability appears that this inventory had no row for, which means
  the search for origins missed one.

## Alternatives rejected

- **Keeping both evaluators behind a setting.** A setting lets two
  decisions of one request differ, and a policy an organization writes
  would decide in one and be ignored by the other. It is the legacy
  path ADR-0117 refuses to carry.
- **The platform authorizing a write before it calls the service.** A
  second copy of the rule would decide what the service then stores,
  so a mistake in either stores what the writer could not write; the
  service evaluating the writer is the only check that cannot be
  skipped.
- **Deriving a role from a fed attribute instead of a link.** The
  roles are system policies keyed on a membership attribute, which
  removes the role link and its write. It makes the role a fact of the
  platform's entity and not a policy: a guardrail could not name it, an
  organization's `Explain` could not show the link that granted, and a
  role of a team or a project, which a link scopes to a resource, would
  need an attribute for each.
- **The platform writing a `permit` as itself for a delegation.** The
  service would store a grant with no writer outside the membership
  case, which is the write with no writer §Security keeps to role
  links; it would also put a platform-held right where ADR-0144 gave
  the right to an owner.
- **The platform deciding the delegation and asking only for
  guardrails.** A second decision outside the service that no guardrail
  can see and `Explain` cannot name, the form ADR-0147 rejects for
  consent.
- **A `min_sequence` read option for item 5.** The sequence is taken
  at delivery, so a writer cannot know the one that will carry its
  change.
- **A synchronous `ApplyEntityChanges` by the writer.** It bypasses
  the loop's lock and order, so two writers of one entity race on the
  sequence, and a failure after the commit leaves a change the service
  never sees. The loop stays the one feeder.
- **Answering at once and asking with `READ_CURRENT`.** It reads
  storage when asked, and the entity store it reads is the one the
  feed fills, so a change not yet delivered is not in it; the window
  stays open.
