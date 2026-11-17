# Authorization

Org-scoped permission decisions, served by the policy service
(`policy/`, a Rust service deciding Cedar policies, see
[ADR-0130](./adr/0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)).
The platform's earlier engine, `garam serve authz`, is removed
([ADR-0177](./adr/0177-the-earlier-policy-engine-its-serve-command-its-protobuf-module-and-its-tables-are-removed.md)).

## Contents
- Decided and not yet built
- Policy document
- Evaluation
- Statement collection
- Policy persistence
- Managing policies over HTTP
- Policy attachment
- An agent on the browser-facing listener
- cowork's principal
- Components
- Roles vs policies
- Explain
- Constraints

## Decided and not yet built

[ADR-0130](./adr/0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
replaced the platform's earlier engine with `policy/`, a Rust service
of its own (#1689), and the engine is removed (ADR-0177). The sections
from Policy document to Policy attachment say what the engine did and
where each part went.

`policy/` holds the contract, the schema, the system tier, its stores
of grants and entities, the access report and the binary.
[ADR-0138](./adr/0138-a-caller-of-the-policy-service-presents-a-short-lived-signed-token-and-owners-write-entities-through-a-sequenced-rpc.md)
states how a caller authenticates and how an owner writes, and
`ApplyEntityChanges` serves that write. The platform signs its own
calls with a minimal signer, and
[ADR-0141](./adr/0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
decides the issuer: garam is an OIDC issuer whose exchanged token a
service verifies offline, and each service calls the policy service
directly under its own caller token, owning a Cedar namespace and
feeding its own entities. Nothing of that issuer is built.
[ADR-0140](./adr/0140-owners-feed-the-policy-service-from-a-dirty-set-of-entities-read-at-delivery-and-a-caller-is-a-closed-list-of-services.md)
closes who may call: `platform` alone until cowork does, every other
`sub` refused, and each caller writing only the types it owns. The list
is `policy/README.md` §Callers.
ADR-0141 supersedes in part its §2: the platform no longer asks for
cowork's checks, since cowork asks the policy service itself, and the
list stays closed and static.

The platform has a client that asks the policy service for a decision
(`platform/internal/policydecision`, #1915): `Evaluate` and
`Evaluations` as the caller `platform`, `probe` where its caller says
so, the reasons returned as a set, and a service that cannot be
reached answered with an error a caller reads as a denial. It reads
one refusal of the service as a denial itself
([ADR-0167](./adr/0167-the-policy-service-names-a-refusal-for-a-principal-the-action-does-not-apply-to-and-the-client-reads-it-as-a-denial.md)):
a question whose principal type the action does not apply to, which
the service refuses as `INVALID_ARGUMENT` with the reason
`PRINCIPAL_NOT_APPLICABLE` in a `google.rpc.ErrorInfo` detail, so a
hosted operator asked an `iam` action it holds no route to is denied.
Every other refusal, an unknown action, a resource of the wrong kind
and a context the schema does not name among them, stays an error. It
decides what both listeners ask it: `serve machine` every message send
([ADR-0168](./adr/0168-the-machine-listeners-send-is-the-policy-services-decision-consent-included.md)),
and each listener a user's delegated operation authority and a hosted
operator's controller proof, read current. `serve api` does not start
without it. Its policy endpoints are removed
([ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md)).

The browser listener asks it too
([ADR-0171](./adr/0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)):
a handler's own enforcement (`allows`), each `checkPermissions` pair and
each candidate of the holders list are one `Evaluate` of the signed-in
user or candidate, the action and the resource, over the state the
platform has delivered and not read current, since a writer waits for
its marks to be delivered (ADR-0146 item 5). Which is a probe is stated:
each `checkPermissions` pair, each holder candidate, the agent read that
admits agents among them and each kind a search offers are asked as
probes, and the handler's enforcement and the holders list's
`iam:org.member-list` check are attempts. Any error of the service is a
503 `unavailable`, never an allow; a forbid is a 403. Membership of the
organization is checked by the platform before the service is asked. A
list and the search ask it too
([ADR-0172](./adr/0172-the-lists-and-search-filter-through-the-policy-services-evaluations-as-probes-and-checkpermissions-answers-permit-write.md)):
`list_visibility.go` puts one probe on each object of a page, on the
object's own read and through `askedOn`, in one `Evaluations` call (as
many as it takes past the service's 1000), and keeps those allowed; an
error of the service is a 503 `unavailable`. `checkPermissions` answers
`iam:permit.write` on the organization the same way, and the four policy
writes (`attach`, `detach`, `update`, `delete`) on the organization or
on a policy. The service's `Evaluate` and `Evaluations` take
`without_permits`, which leaves the organization's `permit`s out of the
decision as the write of a `permit` does, through the one function
`PermitRight::ask` also uses
([ADR-0175](./adr/0175-the-policy-services-evaluate-and-evaluations-take-a-without-permits-option-deciding-as-a-permit-write-does.md)).
`checkPermissions` asks it for `iam:permit.write` and for no other
action (`actionRules`' `withoutPermits`), so a `permit` naming the
action does not make the check allow what the write refuses. The
explanation (`permission_explanation.go`) asks the service's `Evaluate`
as a probe and names the policies its reasons give
([ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md)),
so nothing on this listener asks an evaluator of its own.
`searchPolicies` (`search.go`) and the count the organization deletion
makes (`org.go`) read the organization's policies through the relay's
`List` (#1990): the search matches the needle on each name or
identifier, case aside, in the platform, since the service has no search
of its own, and then filters by `iam:policy.read` as above. The engine,
its store and its tables are removed (ADR-0177), and so is the
`PolicyManager` seam. The parity matrix that reported where the two
engines differed is retired.

**Two further decisions are taken, both partly built.** The policy
service loads `policy/schema/iam.cedarschema` and
`policy/schema/cowork.cedarschema` as one schema, and the platform's
role table and console write `iam:<type>.<verb>`. An organization
writes role links, `forbid` guardrails and `permit` policies, the last
only in the policy service: nothing sends one yet.

- **A service is declared by one manifest**
  ([ADR-0143](./adr/0143-a-service-is-declared-by-one-manifest-that-is-its-cedar-schema-and-garam-serves-what-the-manifests-say.md)).
  It is the service's Cedar schema file,
  `policy/schema/<service>.cedarschema`, declaring one namespace, `Iam`
  or `Cowork`, with annotations for the service's name, an entity
  type's GRN type and the caller that writes it. The GRN catalogue
  ([grn-model.md](./grn-model.md) §Types by owning service) is generated
  from the manifests; the issuer's audience list and the closed list of
  callers are checked against them; and which caller writes which type
  is read from the annotations. An action is `<service>:<type>.<verb>`
  ([ADR-0145](./adr/0145-an-action-is-a-type-and-a-verb-joined-by-a-dot.md),
  which replaced ADR-0143's hyphen, so a name parses one way) and every
  action moves: the platform's `<type>:<verb>` became
  `iam:<type>.<verb>`, so `iam:org.delete` is
  `Iam::Action::"org.delete"`, in the roles, the console
  and the system tier, and a type's group `iam:<type>.*` holds that
  type's actions and no other's. A role is the `Iam` namespace's, and a
  service's action joins one by naming its group in its own manifest.
  The platform serves a read-only registry of services, types and
  actions, answered from the catalogue generated from the manifests.
  Adding a service is one pull
  request adding one manifest, with the proofs the ADR lists.
- **An organization may write `permit` policies**
  ([ADR-0144](./adr/0144-an-organization-may-write-permit-policies-over-resources-inside-itself.md)).
  Its resource scope is inside the organization and the write refuses
  one that is not; tenant isolation stays above it; it names only what
  the manifests declare, and `Explain` names it as any policy. It still
  cannot touch the system tier or permit across organizations. Writing
  one is the action `iam:permit.write`, held by `role-owner` alone and
  outside `iam:policy.*`, which whatever serves the write asks for
  first; another role writing one, bounded by what it holds, is open.
  The console's editor and the platform's switch are #1689 slice 4.
  Built (#1780): `grant::Body::permit` takes a `PermitRight`, which
  only an allowed evaluation of `iam:permit.write` for the writer on
  the organization's `Org` makes, so a `permit` is stored only through
  a path that asked; the right's organization and writer are written
  into the stored body and the store refuses a mismatch. The right
  comes from the owner role alone: it is asked with the organization's
  `permit`s left out, so no `permit` confers it and a guardrail can
  still take it away. Making a stored `permit` version effective,
  `activate` or `roll_back` to it, takes a `PermitRight` too. The write
  admits `resource == E`, `resource in E` and `resource is T in E`,
  where `E` is an entity of the organization or its `Org`, and refuses
  `resource` alone, `resource is T` alone and an `E` of another
  organization. The principal scope is not read. A `when` or `unless`
  is bounded as a guardrail's is. A decision composes the
  organization's `permit` policies beside its guardrails, and
  `access-report` lists one by its id, and a `permit` never reaches an
  operator or a supervisor: a decision for an `Iam::Operator` or an
  `Iam::Supervisor` composes the organization's guardrails and none of
  its `permit`s. An RPC writes
  one (item 3 of ADR-0146). Not built: a check of an entity literal in
  a `permit`'s principal scope or condition.

**Built of ADR-0143** (items 1, 2, 3, 4 and 5): the two manifests, the
`manifest` crate that reads them, `lib/grn`'s generated catalogue, the
`make -C policy check-gen` that holds the catalogue, the audience list
and the caller list to the manifests, the types a caller may write read
from `@caller`, qualified types and `<service>:<type>.<verb>` actions
on the wire with the unqualified form refused, the `Iam` roles, and
`iam:permit.write` declared in `role-owner` alone. The platform's
outbox sends `Iam::` types under migration 000070. Item 4 renamed the
platform's own actions, in `role.go`, the console, the
CLI and the SDKs, and migrations 000071 and 000072 rewrote the
operations stored in delegation grants and authorities, the second to
the dot form of ADR-0145; no stored policy document was converted, and
`platform/internal/org/action_manifest_test.go` holds the platform's
names to `grn.Actions()`. Item 6, the registry, is built (#1788):
`getRegistry` and `describeGrn` ask for a signed-in caller and no
permission, since they answer what the manifests declare and read no
entity. **Not built:** item 7's proofs for a new service.

**codehub as a service is built in the policy service (#1815).**
[ADR-0149](./adr/0149-codehub-is-a-service-of-the-policy-service-whose-repositories-are-entities-and-whose-paths-are-context.md)
decides a `Codehub` manifest with the entity type `Codehub::Repo`
(`grn:<org>/codehub/repo/<id>`, codehub's immutable id) and the actions
`codehub:repo.read`, held by members too, `repo.write`, `repo.create`
and `runner.administer`, held by owners and administrators, and
`org.use` on an organization in place of `create_organisation`. A path
is asked as the exact UTF-8 context key `path` and is no entity.
The manifest, `Caller::Codehub` (scoped to its organization), the
declared `path` context on both transports and the HTTP binding's
type `grn` are built, and the platform's role table holds the five
actions. A resource's properties are still refused. **Not built:**
codehub's outbox with a sequence per entity, which feeds its
repositories, and the runner as an `Iam::Agent` that codehub registers
and that holds no role by default.

**The platform's outbox, its delivery and its signer are built.** Under
ADR-0140 triggers mark each entity a row change reaches, in a set
holding one row per entity and no document, and a loop reads each
entity's state at delivery under one shared sequence. It is off by
default, so a deployment with no policy service writes no rows and runs
no loop, and a later one starts by a backfill from the owners' tables,
which `garam policy-feed start` runs and `stop` undoes. Restoring the
platform's database rewinds that sequence, so it means rebuilding the
entity store.

**The switch is decided, and built only in the policy service's write.**
[ADR-0146](./adr/0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md) has the
platform's browser and machine listeners decide through the policy
service alone; of what follows, only what a bullet says is built is
true of the platform.

- **One evaluator.** `garam serve authz`, its gRPC, its policy tables,
  its change notification and `CheckCurrent` are removed (ADR-0177),
  and no stored document of the earlier format is converted. The
  policy service is required in every deployment, and a listener that
  cannot have a request decided refuses it.
- **The service serves the write.** Built: seven gRPC RPCs write and
  read role links, guardrails and `permit`s, with versions,
  activation, roll-back and removal, and the HTTP binding gains no
  write route. Only the platform calls them; a scoped caller is
  refused whole. The request names the writer and the service
  evaluates the writer's action on the organization, read current,
  before it stores: `iam:permit.write` for a `permit`, the policy
  actions the Iam manifest declares for the rest. A writer links only
  a role it holds, so an admin cannot link `role-owner`. A write with
  no writer is accepted for a role link alone, written or removed from
  a membership. A policy keeps the kind of its first version.
- **A member's role is a role link.** The platform writes it from the
  membership, as the caller `platform` and with no writer, and the
  outbox carries it, so a rebuild rewrites it. Built for an
  organization membership (#1878), by the rules of §Role links under
  Delivery. A group's and a project's roles are written the same way
  as scoped links (#1896, ADR-0162), and a user holds no role of its
  own: `users.roles` is removed.
- **A writer reads its own write.** A request that changes an entity, a
  membership or a policy waits, up to a bound of 5 seconds, until
  the outbox marks it wrote are applied, and is answered as
  unavailable if they are not. Built for the four requests that write
  an organization membership: creating an organization, accepting an
  invitation, registering by one, and deleting an organization, and
  for the two requests that write and end an operator's delegation
  (§Operator operations under Delivery), and for creating, updating
  and deleting a group or a project, which wait on its scoped link's
  mark (§Role links). A request that changes any other entity does not
  wait, except those §Consents names, and the policy write the platform
  relays (§Managing policies over HTTP) waits for nothing: the service
  stores it in the call that decides it. cowork's creates of a
  channel, a page, a task and an artifact, and its delete of a page,
  wait on the written entity's mark
  ([ADR-0166](./adr/0166-a-cowork-create-or-delete-answers-after-the-policy-service-holds-the-change.md)),
  where its policy client is configured.
- **Every present capability has a place.** The ADR's inventory gives
  each origin of a statement and each surface that reads a decision
  its form. A hosted operator's delegation, handed to the earlier
  engine as a per-call Allow, becomes an attribute of the operator's
  entity that a system policy per delegable operation reads. The
  policy service holds this half (#1805): `Iam::Operator` carries
  `operations`, joins the principals of the ten delegable actions
  alone and sits in its organization's `Iam::OrgMembers`, and a system
  policy per operation permits an operator of the resource's
  organization that holds it. The platform feeds the attribute and the
  operator's place among its organization's members (#1891, §Operator
  operations under Delivery), and still decides an operator's authority
  itself until its own round: the policy service holds the operations
  and no request asks it about them yet. A standalone host's
  supervisor is the second such identity
  ([ADR-0164](./adr/0164-the-policy-service-decides-a-supervisors-activation-of-the-agent-it-holds.md)):
  `Iam::Supervisor` is an entity the platform feeds, carrying the
  optional `agent` it holds, a principal of `iam:agent.activate` alone,
  and a system policy permits it that action where the resource is that
  agent. The policy service holds this half (#1905), correcting row 6
  of ADR-0146's inventory. The platform feeds the supervisor and the
  agent it holds (#1917, §Supervisors under Delivery), and still
  decides its activation itself in
  `lifecycleAuthorization.supervising`: the policy service holds the
  entity and no request asks it about it yet. Only an
  owner writes a `permit`, so an administrator keeps assigning the
  roles it holds and writing guardrails and no longer writes an
  `Allow`.
- **cowork asks the service alone,** and its path through
  `checkPermissions` goes. Built in cowork (#1907): it asks the
  platform for `me` and the organization list only, and refuses to
  start without the four policy settings. The platform's
  `checkPermissions` keeps cowork's kinds until its own round
  (#1784).
- Built in the policy service (#1785), not yet relayed by the
  platform: every denial and every write of a policy is recorded by
  the policy service for the organization of the resource, and an
  organization's owners and administrators read it on
  `iam:audit.read`
  ([ADR-0148](./adr/0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)).
  A write is recorded in its own transaction, so one that cannot be
  recorded is not made. A denial is recorded after the service decides
  it, and is queued for a worker that records it off the answering
  path. One whose record cannot be written, or that meets a full queue
  or a stop with it still queued, is answered anyway, the failure
  logged and counted as lost, and read on `/metrics` of the service's
  health listener. Not built: the platform's relay of
  `ListAuditRecords`.
- **cowork states its probes.** Each question cowork asks to filter a
  list or to decide whether a connection may write carries `probe` on
  its own entry of the batch, so a denied one is not recorded; every
  question a person or an agent made states nothing and is an attempt
  (ADR-0148 item 2). The platform's own questions are not asked of the
  policy service yet (#1784), and the round that asks them states the
  same of its filters and controls before the record is deployed
  with them.

[ADR-0147](./adr/0147-a-message-between-two-consenting-organizations-is-tenant-isolations-one-exception.md) makes a
message between two agents of two consenting organizations tenant
isolation's one exception: `Org` gains `consents`, the platform feeds
it, the isolation `forbid` gains one `unless` branch for `message.send`
between agents where each organization's `consents` holds the other,
and both organizations' guardrails are read for such a send. The
policy service holds all of this (#1805) and the platform feeds
`consents` (§Consents under Delivery); the send path asks the service
and no longer refuses before it, as [messaging.md](./messaging.md)
states (ADR-0168). The sender's
guardrails are read as `forbid`s alone and under ids prefixed by its
organization, and the exception is read by no other action, since
an `unless` branch names `message.send` and two agents.

**What a decision's reasons name.** The platform explains a permission
by recomputing the identifiers of the caller's membership links
([ADR-0161](./adr/0161-the-platform-alone-writes-under-the-identifiers-of-a-members-role-links-and-only-that-members-link.md)
`grant::link_policy`), and of its teams' and projects' scoped links
([ADR-0163](./adr/0163-the-platform-alone-writes-under-the-identifier-of-the-scoped-link-it-derives-from-a-groups-or-a-projects-roles.md)
`grant::scoped_link_policy`), and matching them against the
`policy_id` of each `Reason` an `Evaluate`, or an entry of
`Evaluations`, returns; no explanation call is asked of the service
(#1796). The reasons are those of the policies that decided, and a
test through the server (`reason_cases`) holds each row:

| decision | the reasons name |
| --- | --- |
| allow by a member's link | that link's reserved identifier |
| allow by a scoped link | that link's reserved identifier |
| allow by several policies | every one that permits, and no forbid |
| deny by a guardrail | the guardrails that forbid, not the link |
| deny by isolation | `tenant-isolation` alone |
| deny, nothing permits | no reason |
| deny, a policy failed | the policies that failed, each with its error |
| deny, the question refused | one reason, no `policy_id`, an error |

- A policy is named by its own identifier: a link, a `permit` or a
  guardrail by its GRN, not by its template, and a system policy by its
  file's name. The row for a link names the stored policy, which is why
  a reserved identifier tells it from a writer's `permit`.
- A deny names its forbids alone, so a link that would have allowed
  is not named.
- The order of the reasons is not stated. Cedar returns the
  determining policies as a set and the service does not sort them,
  so a caller compares them as a set.
- A sender's guardrail that denies a `message.send` to another
  organization is named `<organization GRN>/<policy GRN>`, as in
  `grn:root/iam/org/acme/grn:acme/iam/policy/<slug>`. The
  recipient's is named by its GRN, so two organizations naming a
  guardrail alike are two reasons.
- A question refused inside `Evaluations` is a deny with one reason
  whose `policy_id` is empty. The same question to `Evaluate` is an
  error status and carries no reason.
- What a reason does not carry: a statement's `sid`, and the
  attachment of a policy to a caller or a team. The platform's
  explanation is the policy's identifier and no finer, and a finer
  one is a decision of its own.

### Delivery

`serve api` runs the loop when `GARAM_POLICY_ADDR` is set, and only
then. It then requires `GARAM_POLICY_CA_FILE`, the PEM root the
service's certificate is verified against;
`GARAM_POLICY_SIGNING_KEY_FILE`, the platform's RSA key; and
`GARAM_POLICY_TOKEN_ISSUER`, the `iss` the service's
`POLICY_TOKEN_ISSUER` names. Setting the address without the rest
refuses to start. With none set nothing runs, and the process logs an
error at start if the feed is on, since nothing then delivers it and
the outbox grows with every write.

A pass is one transaction (`platform/internal/policyfeed/postgres`):

1. Lock the `policy_feed` row with `FOR UPDATE SKIP LOCKED`, so one
   replica delivers and the others skip. A feed that is off ends the
   pass.
2. Read up to 500 marks that are not set aside (see §Role links) with
   their generations, then each entity's
   state through `policy_entity_state`. No row is an entity the owners
   no longer hold, which goes out as a delete.
3. Take the next value of `policy_feed_sequence` (migration 000068) and
   send every entity in one `ApplyEntityChanges` call under it, with a
   30 second deadline.
4. Delete each mark still at the generation read, and commit. A write
   landing mid-pass raised its mark's generation, so the mark
   survives and goes out at a higher sequence.

A failed call rolls the pass back and leaves every mark. The loop
polls every second, goes straight on after a full batch, and after a
failing pass waits twice as long each time up to 30 seconds. It logs
once when delivery starts failing and once when it recovers, and is not
part of readiness (ADR-0029).

**A refused entity is deleted.** The service refuses a change for its
own fault, as one that breaks a limit of the schema. The pass sends
that entity's delete under a second sequence, logs an error naming the
entity and the reason, and deletes its mark. A bad entity then denies
rather than decides on what it last said (ADR-0137). Nothing retries
it: a later write to the entity marks it again. The log line is the
only record of it, since its mark is gone; a refused link keeps its
mark and is read as §Role links says.

#### Reading the feed's standing

`/metrics` on `serve api` carries the feed's standing, from a
collector that counts the outbox at each scrape
(`platform/internal/policyfeed/metrics.go`), so each replica reports
the whole feed:

- `garam_policy_feed_outbox_marks{state="pending"}` is the marks the
  next pass reads. It grows while delivery fails or nothing delivers
  it, and a value that stays above zero is a feed that is not
  catching up.
- `garam_policy_feed_outbox_marks{state="set_aside"}` is the links the
  service refused for good. Any value above zero is a membership whose
  link the service does not hold; the rows say which.
- `garam_policy_feed_last_success_timestamp_seconds` is the Unix time of
  the last pass of this replica's loop that ended without a failure. A
  pass that found another replica holding the feed ends without one, so
  it counts, and the metric is absent on a replica with no loop and
  until the first pass. `time() - last_success` growing beside a
  growing `pending` is a feed that is failing, which the log's one
  "policy feed delivery failing" line also says.

A scrape that cannot read the outbox fails rather than reading zero.
With `GARAM_POLICY_ADDR` unset the two mark series are still served,
which is how an outbox that nothing delivers is seen to grow.

#### Role links

[ADR-0159](./adr/0159-role-links-follow-the-entities-in-one-pass-the-platforms-tables-win-a-failed-write-and-a-request-finishes-its-work-before-it-waits.md)
records the four decisions below. An organization membership is marked
as `Iam::RoleLink` (ADR-0159 item 4), whose id is the organization's
GRN, a space and the member's GRN (migration 000075). A
trigger marks it on every insert, delete and change of the role of an
`org_members` row, so a route written later is covered without a line of
its own, and the user's own mark on insert and delete stays. The
cascade of a deleted user or organization marks it as well.
`policy_role_link_state` reads the role a mark's membership holds, and
no row is a link to remove.

A pass sends its entities as above, then its links in the same
transaction, then deletes every mark (ADR-0159 item 1, which replaces
ADR-0146's "under the same sequence"). The policy service's link calls
carry no sequence, so a link is ordered by its name instead: one policy
per member and role, `grn:<org>/iam/policy/link-<role>-<hex of the
first 16 bytes of the SHA-256 of the member's GRN>`, since a policy's
identifier is a slug of at most 64 characters. A role held is
`CreatePolicy` (ALREADY_EXISTS is a link written already) and
`ActivatePolicy`, which is idempotent, with no writer and the
organization as resource and `Iam::User` as principal. The member's
other two roles' policies are then `DeletePolicy` (NOT_FOUND is a link
removed already). The role is written before the others go, so a role
that changes is never absent, and a failure between the two leaves the
member holding both until the next pass. A membership that ended is
all three deleted. A role changes by writing the new one and deleting
the old, since a policy with no writer refuses a new version.

**A request reads its own write.** After its last write and with no
transaction open, a request that wrote a membership wakes the loop
and reads the outbox every 50 milliseconds until no `Iam::RoleLink`
mark of that member is left (of any member of the organization, for
its deletion), then answers. At 5 seconds it answers 503 `unavailable`
with the reason `change_not_applied`, which the API discloses on a 503
for this code alone: the change is stored and is not yet applied
(ADR-0159 item 3). The mark stays and the loop applies it when the
service answers. Until then the **platform's tables win** (ADR-0159
item 2): the service holds the links it last
had, so a member added is not yet allowed and a member removed is not
yet denied, and a role changed is the old one's or both. The wait is on
the table, so another replica's loop ends it. With `GARAM_POLICY_ADDR`
unset there is no gate and no request waits, and nothing is fed, as for
the entities.

Three of the four requests cannot be repeated, so each finishes its own
work before it waits (ADR-0159 item 3), and the 503 names the
organization in `resource` (the GRN of the one created, joined,
registered into or deleted). At the bound:

- **Creating an organization** has created it and made the caller its
  owner. The client reads the organization from `resource` and does
  not create another: the identifier is random, so a repeat would
  make a second organization.
- **Accepting an invitation** has spent it and added the member. A
  repeat answers 409 `invitation_accepted`; the client reads the
  membership of `resource`.
- **Registering by an invitation** has created the account, its
  credential and its membership, spent the invitation and started the
  session, which the answer carries as `garam_session`. The person is
  signed in and goes on from `resource`; a repeat would meet a spent
  invitation.
- **Deleting an organization** has deleted it. The client does not
  repeat the delete, which would find it gone.

**A link the service refuses for good is set aside** (ADR-0160). A
write the service answers with `PERMISSION_DENIED` or
`INVALID_ARGUMENT` is not retried, because the request is a function
of the membership alone and is judged the same each time. Those two
codes are the whole list
(`platform/internal/policyfeed/grpc/links.go`); each rests on
`policy/crates/server/src/status.rs`:

- `PERMISSION_DENIED` (lines 115 to 121 and 167 to 170): the caller
  `platform` may not write this link, as where its identifier is held
  by a policy that is not the member's link (`Reserved`, below).
- `INVALID_ARGUMENT` (lines 130 to 138 and 183 to 197): the request or
  the policy is one the service refuses by its own rules.

Every other code, `UNAVAILABLE`, `INTERNAL`, `DEADLINE_EXCEEDED`,
`FAILED_PRECONDITION`, `UNAUTHENTICATED` and `NOT_FOUND` among them, is
a fault that may clear, so it fails the pass as before and the pass is
retried. A permanent refusal first removes the member's links of all
three roles, so a link the service would not write leaves the member
with none and not with the role it held before (a removal the service
refuses for the same reason leaves that link as it was). The mark is
then kept and recorded in the columns `set_aside_code`,
`set_aside_reason` and `set_aside_at` of `policy_outbox` (migration
000076), the pass goes on with the entities and links of the other
organizations, and the loop logs an error naming the member, the code
and the reason once. No pass reads a set-aside mark again. A later
write of the membership, and `garam policy-feed start`, mark it anew
and clear the record, so a repaired link is delivered again;
`stop` empties the outbox as for any mark. An operator reads the
record from the table:

```sql
SELECT entity_id, set_aside_code, set_aside_reason, set_aside_at
FROM policy_outbox WHERE set_aside_at IS NOT NULL;
```

A request waiting for a set-aside link does not wait out the bound: the
gate reads the mark's record and answers at once, 503 with the reason
`change_refused` (`policyfeed.ErrLinkSetAside`) and the same `resource`
as the bound's. It is a reason of its own because `change_not_applied`
says the loop goes on delivering the change and here it does not: the
client has nothing to wait for and does not repeat the request, and an
operator reads the set-aside link. The change is stored, and the
service holds no role for the member, who is denied there until the
membership is written again or an operator restarts the feed. This
fails closed, and it has a cost: a fault of the platform's own
credentials or configuration that the service answers as
`PERMISSION_DENIED`, or one that makes every link write malformed,
sets every link aside and so removes every member's role in the
policy service. `set_aside` in §Reading the feed's standing shows it,
and `garam policy-feed start` repairs it once the fault is mended
([ADR-0160](./adr/0160-a-role-link-the-policy-service-refuses-for-good-is-set-aside-and-the-members-links-are-removed.md)).

**The identifiers of a member's links are the platform's alone**
([ADR-0161](./adr/0161-the-platform-alone-writes-under-the-identifiers-of-a-members-role-links-and-only-that-members-link.md)).
The policy service reserves every identifier of an organization's
policies that opens `link-owner-`, `link-admin-` or `link-member-`.
The caller `platform` with no writer creates there a role link of the
identifier's role in organization scope, whose principal is the
`Iam::User` whose GRN's hash is the identifier's tail and whose
resource is the organization, and nothing else. It activates or
deletes there only a policy every version of which is that link. A
write naming a writer, the owner's included, and a scoped caller's,
is refused for every step. Each refusal is `PERMISSION_DENIED`, so
the platform's create of a link over a policy that is not it reads
`ALREADY_EXISTS` as written and its activation is refused, which
sets the link aside as above and leaves the member with no role. A
writer's refusal is recorded as a denial of the step's action
(`iam:policy.attach`, `iam:policy.detach` for a removal), and the
platform's is not. A policy that holds such an identifier from before
is neither changed nor deleted by the service, and is removed by an
operator from the database.

**The identifier of a team's or a project's scoped link is the
platform's alone as well**
([ADR-0163](./adr/0163-the-platform-alone-writes-under-the-identifier-of-the-scoped-link-it-derives-from-a-groups-or-a-projects-roles.md)).
A group's or a project's roles are held on its own row
(`groups.roles`, `projects.roles`) and grant their actions to its
members on its own GRN, so the link the platform derives from them
has the team (`Iam::Team`) or the project (`Iam::Project`) as
principal and as resource, in team-or-project scope. The service
reserves every identifier of an organization's policies opening
`link-scoped-`, beside the three above. The identifier is
`grn:<org>/iam/policy/link-scoped-<role>-<hex>`, with `<hex>` the
first 16 bytes of the SHA-256 of five fields in this order: the
role's word, the principal's type and id, the resource's type and id,
each as its length in bytes (8 bytes big-endian) and then its bytes,
so two principals or two resources never share one. `grant` computes
it in one function, `scoped_link_policy`, and the platform recomputes
it. The caller `platform` with no writer creates there only a link
whose principal and resource are one team or one project and whose
identifier is that computation; it activates or deletes there only a
policy every version of which is such a link. Every other rule of the
paragraph above holds unchanged: a write naming a writer is refused
for every step, each refusal is `PERMISSION_DENIED` and a writer's is
recorded as a denial. A link in a team's or a project's scope that a
person writes through the console, with a user, an agent or another
team as principal, stays a named writer's under an identifier that
does not open `link-scoped-`.

**A group's and a project's roles are scoped links the platform
writes** ([ADR-0162](./adr/0162-a-groups-and-a-projects-roles-reach-the-policy-service-as-scoped-links-the-platform-writes-and-a-users-direct-roles-are-removed.md),
migration 000079). The outbox's third kind of mark,
`Iam::ScopedRoleLink`, has as its id the GRN of the group or the
project, which is the link's principal and its resource; the kinds are
the entity types, `Iam::RoleLink` and this one, each link kind read
through its own state function (`policy_scoped_link_state` returns the
owner's roles, each once, those the platform knows, or no row for an
owner the tables no longer hold). A trigger marks it when a group or a
project is inserted or deleted and when an update changes `roles`; a
write of the name marks nothing, and the entity's own mark is not
raised for a change of roles alone. A write that reorders the roles or
repeats one marks, though the state is the same; it is rewritten as
the same policies. A pass writes, after the member
links and by name under no sequence, the policy ADR-0163 names for each
role held, as `CreatePolicy` (`ALREADY_EXISTS` is a link written
already) and `ActivatePolicy` with no writer and team-or-project scope,
then deletes the policy of each of the three roles not held
(`NOT_FOUND` is a link removed already). A role removed, a role
changed and a deleted group or project are that one operation, and no
past state is read. A link refused for good is set aside as a member's
is, its owner's links of all three roles are removed, and the waiting
request is answered `change_refused`.

Creating and deleting a group or a project wait for its mark as the
membership routes do (`Gate.AwaitScoped`, the same bound and the same
two reasons). An update waits only when its body wrote the roles; one
that did not is answered at once, whatever mark of its owner is set
aside, since it asked nothing of the policy service. A write of the
roles is answered by what the service then says.

Every other request that changes a fed entity waits as well, on the
marks its own write raised (ADR-0165): `Gate.AwaitMarks` reads each
by its kind's state function under one bound and answers at once for
none. These wait: creating an organization, deleting an organization
(on every mark its cascade raised, collected from its own transaction
by `policyfeedpg.Record` as the group's and the project's deletions
are), creating and deleting a group or a project, adding and removing
a member of either, registering and deleting an operator, writing and
ending an operator's delegation, granting and withdrawing a
consent, registration, accepting an invitation, the first sign-in
that created the user, revoking a standalone host's supervisor
(`Iam::Supervisor`), and on the machine listener creating a managed
agent, admitting an enrollment of a new agent, admitting one that
replaces an agent a supervisor holds (on that supervisor's mark) and
enrolling a standalone agent, new, replaced or replayed, on the new
supervisor's mark and also on the agent's when it is new. A request
that raised several marks names them in one list, role links and
scoped links included, and waits under one bound. A repeat sign-in, an
admission that replaces an operator's agent (the store says which an
enrollment was by `EnrollmentResult.Replaced`), a rename, a withdrawal
of an approval no host has spent, and every other write raise no mark
and answer at once, whatever older mark is set aside. A first sign-in
that created the user waits up to the bound and then redirects as on
success, logging the mark still pending: its session is set and a
redirect cannot carry a 503, and the first request that needs the user
in the service waits on the mark again. Registration and accepting an
invitation keep the 503. The machine process holds a gate with no loop (`NewObservingGate`) over the
same outbox, installed whether or not the feed is on: it answers when
the api's loop has delivered, at once with the feed off, and with the
feed on and no deliverer every create answers `503` at the bound for
good. The api's loop polls every second and backs off up to 30
seconds, so after a recovery a creation can still answer `503` for up
to that long. With the feed off no route waits, and an api with no
policy address has no gate; the machine listener's gate is always
there.

`garam policy-feed start` marks every membership, group, project and
supervisor registration, ended or not, with the rest of the backfill,
and so
do migrations 000075, 000079 and 000080 for a feed already on, each
raising a mark's generation if it is there, so running them again
rewrites the same policies and no second one. Migration 000079's down
leaves the scoped links already written, as 000075's leaves a member's.

**What stays when the feed is turned off and on.** `stop` empties the
outbox and the triggers mark nothing while the feed is off, and
`start` marks the entities and memberships that exist then. So a
member added, or whose role changed, while the feed was off is
written by the `start` that follows. A membership that ended while it
was off is not: nothing marks it and `start` enumerates live rows
only, so its link stays in the policy service and the member stays
allowed there. Nothing in the platform lists or removes such a link.
Before `stop`, end the memberships that are to end, and let the outbox
empty; after a `start` that follows a period off, an operator deletes
each link left by `DeletePolicy` as the platform with no writer, under
the id `grn:<org>/iam/policy/link-<role>-<hex of the first 16 bytes of
the SHA-256 of the member's GRN>` for each of the three roles (a
`NOT_FOUND` is a link not held). Migration 000075's down leaves the
links already written in the same way.

#### Consents

An organization's consents are an attribute of its `Iam::Org`, fed as
ADR-0147 item 2 states (migration 000077). `policy_entity_state`
returns `consent_ids`, the organizations toward which the
organization holds a half of `message_consents` that stands, and the
pass sends them as `EntityUpsert.consents`. The relation stays the
fact: the attribute is read back from it at each delivery and holds no
state of its own.

No mark kind is added, because the attribute belongs to the entity
the outbox already holds. A trigger on `message_consents` marks the
`Iam::Org` of each organization whose half starts or stops standing:

- A grant that creates the row, or sets a half that was down, marks
  the granting organization. A grant that only re-stamps a half
  already standing marks nothing, so repeating one costs the policy
  service nothing.
- A withdrawal marks the withdrawing organization, whether its update
  clears the half beside the other's or its delete drops the row it
  held alone (the two statements `Withdraw` runs).
- Deleting an organization deletes the rows it is a side of through
  the foreign key, and the deletion marks the other organization that
  held a half toward it, beside the organization's own marks.

A grant is thereby the upsert of the organization with the attribute
and a withdrawal the upsert without the organization, both from the
transaction that wrote them. `garam policy-feed start` takes
`message_consents` among the tables it locks and marks every `Iam::Org`,
and migration 000077 marks each organization holding a half for a feed
that is already on, so a consent that existed before is delivered.

The grant and the withdraw routes of the browser-facing listener read
their own write as §Role links says, on the `Iam::Org` mark of the
organization that wrote (ADR-0159 item 3): they wake the loop, wait
until the outbox holds no mark of that entity, and answer 503
`unavailable` with `change_not_applied` and the organization in
`resource` at 5 seconds. The half is stored then, and a repeat changes
nothing. A refused `Iam::Org` is deleted and logged as any entity is,
so no wait ends in `change_refused`: that reason is a role link's
alone. The wait is on the entity's mark and does not read which write
raised it, so a later write that marks the same organization, such as
another consent, keeps it waiting until the pass that delivers it.

The send path no longer reads the platform's relation: the machine
listener asks the policy service, which reads the attribute
(ADR-0168).

#### Operator operations

A hosted operator's delegation is an attribute of its `Iam::Operator`,
fed as ADR-0146 §A states (migration 000078). `policy_entity_state`
returns `operations`, the operations of the operator's current grant
(the `operator_delegations` row with no `ended_at`), and the pass sends
them as `EntityUpsert.operations`. The relation stays the fact: the
attribute is read back from it at each delivery and holds no state of
its own.

The names are the wire form already. `operator_delegations.operations`
holds the ten `iam:` names of the policy service's Iam manifest
(`iam:agent.create` and the rest, since migration 000072), which are
the actions `Iam::Operator` is principal of, so nothing maps one to the
other. A test holds the platform's names to the manifest's list of ten.
A name stored that the service does not list as delegable is refused
whole by the service: the operator's entity is then deleted and logged
as any refused entity is (ADR-0140 item 11), and it denies.

An operator also sits in its organization's `Iam::OrgMembers`, beside
its `Iam::Org` parent. Tenant isolation permits a principal that is in
the resource's `org.members`, and the Operator schema declares the
operator in `OrgMembers` for that reason, as an agent is. No policy
grants anything by that membership, so it adds no authority.

No mark kind is added, because the attribute belongs to the entity the
outbox already holds. A trigger on `operator_delegations` marks the
`Iam::Operator` the row names on an insert, a delete and a change of
`ended_at`, which are the whole life of a grant: writing one, ending
one, replacing one (the end of the old and the insert of the new,
in one transaction) and the cascade that deletes them with an operator
or an organization. A delegation is thereby delivered as the operator's
`operations`, and an end as what then stands, none or the new set, from
the transaction that wrote it.

**Expiry is not fed.** `expires_at` is a moment, so a grant lapsing
writes no row and marks nothing, and the fed operations are those the
row holds whether or not it has lapsed. The platform still refuses an
ended or lapsed grant before it asks (ADR-0146 §A), because the
grant's time is its own: a lapsed grant that was not ended is in the
service with its operations until it is ended or replaced, which is
why no decision may rest on the attribute alone until the platform's
own round.

`garam policy-feed start` takes `operator_delegations` among the tables
it locks and marks every `Iam::Operator`, and migration 000078 marks
every operator for a feed that is already on, since each gains its
parent, so a delegation that existed before is delivered.

The write route and the end route of the browser-facing listener read
their own write as §Role links says, on the `Iam::Operator` mark of the
operator whose delegation changed: they wait until the outbox holds no
mark of that entity and answer 503 `unavailable` with
`change_not_applied` and the operator's organization in `resource` at 5
seconds. The grant is stored then, and a repeat with the same request
id answers as it did. The other writes of an operator (its details and
its control endpoint) do not wait. A later write that marks the same
operator keeps a wait going until the pass that delivers it.

An operator's authority is decided from that attribute: the minting of
an operation authority and the controller proof ask the policy service
read current, and the platform refuses an ended or expired grant before
it asks ([operators.md](./operators.md) §Delegated operation
authority).

#### Supervisors

A standalone host's supervisor is fed as ADR-0164 states (migration
000080): one `Iam::Supervisor` per registration in
`agent_supervisors`, with parents `Iam::OrgMembers` and `Iam::Org`,
the `org` attribute, and `agent`, the agent it holds.
`policy_entity_state` reads all of it back from the relations at each
delivery, and the pass sends `agent` as `EntityUpsert.agent`.

**What `agent` is.** The agent the registration names
(`agent_supervisors.agent_grn`), while the registration is current
(`ended_at` is null) and `agent_assignments` assigns that agent to this
supervisor. A current registration whose agent is assigned elsewhere
(an operator took it by a replacement) is fed with no `agent`, so the
policy service allows it nothing. This is the test
`lifecycleAuthorization.supervising` applies, which still decides: no
request asks the policy service about a supervisor yet, and its tests
are unchanged. A registration that has ended has no state, so its
entity is deleted. The organization comes from the agent's row.

No mark kind is added. A trigger on `agent_supervisors` marks the
registration's supervisor on an insert, a delete and a change of
`ended_at` or `agent_grn`: enrolling, revoking, replacing and the
cascade that deletes registrations with an agent or an organization.
A trigger on `agent_assignments` marks the supervisor that is the
assignee, the old one and the new one, on an insert, a delete and a
change of `assignee_grn`, so an agent moved off a supervisor, or onto
one, is delivered. Any other column (a host's name) marks nothing, and
neither does withdrawing an approval no host has spent, which writes
no registration.

**The routes wait on their own marks.** Revoking a supervisor waits on
its mark, and so do enrolling a standalone agent, a replay and a
replacement included, and an admission that replaces an agent a
supervisor holds, as §Delivery says, with the same 503
`change_not_applied` at 5 seconds. ADR-0165 item 5 says a replacement
raises no mark; that held while only an agent and its memberships were
fed, and no longer holds for a replaced supervisor, which item 1
covers: wait on the marks the write raised. An organization delete
records the supervisors it takes as it records the rest.

**What stays when the feed is turned off and on.** `start` marks every
registration, so a supervisor enrolled or moved while the feed was off
is written, and one revoked while it was off is deleted: an ended
registration has no state and is delivered as a delete, which the
service takes as the removal of an entity it may not hold. What stays
is only what has no row to enumerate, a registration deleted by the
cascade from a deleted agent or organization while the feed was off
(ADR-0140's case, as for a membership): its entity stays in the
policy service as it was when last delivered. Delete agents and
organizations only with the feed on, and let the outbox empty.

Migration 000080 takes `agent_supervisors` and `agent_assignments`
in share mode and marks every registration for a feed that is already
on, raising a mark's generation if it is there. Its down
removes the triggers and the supervisor marks and leaves the entities
already written.

### The signer

`platform/internal/signer` signs the token each call carries under
ADR-0138's profile: RS256, `sub` `platform`, `aud` `policy`, `iss` from
`GARAM_POLICY_TOKEN_ISSUER`, a header of `alg`, `kid` and `typ`, and a
lifetime of at most 300 seconds. The delivery signs one per call with a
lifetime of 60 seconds. The key is PKCS#8 or PKCS#1 RSA of 2048 to 4096
bits, and its `kid` is its RFC 7638 thumbprint.

Loading a key, signing and producing the public JWKS are separate calls
(`LoadKey`, `Sign`, `JWKS`), so an issuer endpoint can reuse them. None
is built: the service reads its JWKS from a file, and nothing in the
platform writes it yet. `lib/policy-sdk-go` is the client of the
service's API, generated from `policy/proto` (ADR-0130).

[ADR-0137](./adr/0137-tenant-isolation-is-membership-and-the-policy-engine-fails-closed-on-any-evaluation-error.md)
revises ADR-0130 on isolation's form and the templates' scopes, and
makes any evaluation error a deny.

## Policy document

Removed. The earlier engine read IAM-style documents of `Effect`,
`Action` and `Resource` and matched strings alone; no document of that
format is read or converted now
([ADR-0177](./adr/0177-the-earlier-policy-engine-its-serve-command-its-protobuf-module-and-its-tables-are-removed.md)).
An organization's policies are the policy service's: role links,
`forbid` guardrails and `permit` policies, written in Cedar and
validated against the manifests under `policy/schema/`.

## Evaluation

The policy service evaluates. A decision is Cedar's: a matching
`forbid` denies, else a matching `permit` allows, else the answer is a
deny, and any evaluation error is a deny
([ADR-0137](./adr/0137-tenant-isolation-is-membership-and-the-policy-engine-fails-closed-on-any-evaluation-error.md)).

## Statement collection

No process collects statements now. The service reads memberships,
ancestors and role links itself from what the platform delivers
(§Delivery).

### Role-derived statements

The api server collects no statements. It once derived them per
request from the org tables and the target's roles; it now delivers
what those tables say to the policy service as entities and role links
(§Delivery) and asks the service, which holds the decision
([ADR-0171](./adr/0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md),
[ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md)).
What each role grants is `Role.Actions()` and `org.ActionsFor`, which
the role links carry.

Group and project roles reuse the org role vocabulary (`owner`,
`admin`, `member`), are assigned through the `roles` field of the
group and project create and patch endpoints, and are narrowed by
resource: a group `admin` administers that group and nothing else,
while an org `admin` administers everything the org owns. Unknown role
names grant nothing. Org membership stays the precondition for every
check: a group grant does not admit a non-member of the org. A user
holds no role of its own: the column that stored one, `users.roles`,
is removed
([ADR-0162](./adr/0162-a-groups-and-a-projects-roles-reach-the-policy-service-as-scoped-links-the-platform-writes-and-a-users-direct-roles-are-removed.md)).

### Identity-derived statements

Removed. A machine identity's grant on its own organization, once a
statement the machine listener collected
([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)), is a
system policy of the policy service, and a send is asked of the service
about the sender and the recipient (ADR-0168). The listener refuses a
recipient of another organization before it asks, and the service's
tenant isolation holds the same boundary behind it.

## Policy persistence

The platform stores no policy. The engine's `policies`,
`policy_versions` and `policy_attachments` tables, the notification
`garam_policy_change` and `CheckCurrent` are removed (migration
000081). The policy service stores an organization's role links,
guardrails and `permit`s with their versions, activation, roll-back and
removal, and the platform relays the writes (§Managing policies over
HTTP). The down migration restores the three tables empty; it does not
restore a row.

## Managing policies over HTTP

**Writes of links, guardrails and permits go to the policy service.**
`/orgs/{org}/iam-policies` creates, versions, activates, rolls back,
deletes, reads and lists an organization's role links, guardrails and
permits ([ADR-0169](./adr/0169-the-platform-relays-an-organizations-role-links-guardrails-and-permits-to-the-policy-service.md)).
Each route authenticates the session and relays the call to the policy
service naming the caller as the writer, and the platform stores none
of the body: the service decides, read current, whether the writer may,
and its refusal is the answer (403, 400 or 503). A write it refuses
stores nothing. Only an owner writes a permit, because the service asks
`iam:permit.write` and `role-owner` alone holds it. The platform refuses
before asking a role link it derives from a membership, a user in
organization scope or a team or project linked to itself, so a user's
organization role stays the membership's.

**The earlier engine's policy endpoints are removed.**
`/orgs/{org}/policies` and its versions, activation, rollback and
attachment operations are gone from the browser listener, the
specification and the SDKs
([ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md)),
and so are the `policy_document_*` reasons they raised. The engine, its
store and its tables are removed too (ADR-0177). The search of policies
and the deletion's count read the policy service's list through the
relay (#1990).

**The audit record is read the same way.**
`/orgs/{org}/audit-records` relays the policy service's record of
policy writes and denials naming the caller as the reader, and the
service decides `iam:audit.read` on the organization, which `owner`
and `admin` hold and `member` does not
([ADR-0176](./adr/0176-the-platform-relays-the-policy-services-audit-record-to-the-console-and-the-cli.md),
[audit.md](./audit.md) §The policy service's record).

The `iam:policy.create`, `iam:policy.read`, `iam:policy.update`,
`iam:policy.attach`, `iam:policy.detach` and `iam:policy.delete`
constants live in `platform/internal/org`. `owner` and `admin` grant
`iam:policy.*`; `member` grants none, so reading a policy is an admin
action. The policy service holds no entity for a policy, so the
platform asks it an `iam:policy.*` question on the policy's
organization, as the service decides a write to a policy, and a
caller's question naming the policy is answered that way
([ADR-0171](./adr/0171-the-browser-listeners-enforcement-permission-checks-and-holders-list-ask-the-policy-service-delivered-state-suffices.md)
item 7).

The console reads and manages an organization's policies through the
relay to the policy service, and manages no attachment and creates no
old-engine document
([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)
is superseded in part). [web-app.md](./web-app.md) §What it serves
states what the console presents. No operation answers the policies
naming one subject.

## Policy attachment

Removed. A policy attached to a user, an agent or a group, and the
obligation that a caller establish the subject's organization before
asking, belonged to the earlier engine (ADR-0008, ADR-0025, ADR-0038).
In the policy service a grant names its principal, a guardrail is a
`forbid`, and tenant isolation is a system `forbid` that membership of
the resource's organization lifts, so no caller establishes it
(ADR-0137, ADR-0146 item 7).


## An agent on the browser-facing listener

**Unbuilt.** The api server admits a session alone today and refuses
a non-member before it asks.
[ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
admits an agent presenting a browser token
([machine-identity.md](./machine-identity.md) §Browser tokens), and
decides it in the earlier engine's terms, which the policy service
replaces:

- **Its authority is the policies that reach it**: the grants naming
  it or a team holding it. An agent no policy reaches can do nothing
  there.
- **The membership check becomes an organization check.** The path's
  org must be the one the agent's GRN names.
- **A floor no policy lifts.** Before the service is asked, an agent
  is refused every operation outside its own organization, and
  `issueOperatorKey`, `createInvitation`, `mintOperationAuthority`
  and `approveAgentEnrollment` inside it. Policy writes, group
  membership and deleting the organization are left to policy, which
  a `forbid` naming the agent bounds. The floor is a system `forbid`
  under ADR-0130.

## cowork's principal

**Built in cowork (#1758); the principal's issue is unbuilt
(#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
gives each organization a cowork principal on the machine listener
([machine-identity.md](./machine-identity.md) §cowork's principal).
[ADR-0141](./adr/0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
supersedes in part ADR-0127 §5: cowork asks the policy service
itself, under a caller token from `exchangeClientAssertion` with
`audience` `policy` and `sub` its principal, and not through the
platform. cowork builds that route (#1758) and asks nothing else
(#1907): it refuses to start without its five settings
(`COWORK_POLICY_ADDR`, `COWORK_POLICY_CA_FILE`,
`COWORK_MACHINE_TOKEN_URL`, `COWORK_IDENTITY_ISSUER`,
`COWORK_DEPLOYMENT_KEY_FILE`), naming the
missing ones, asks one `Evaluations` per batch under the token of the
organization asked about, and feeds its channels, pages, tasks and
artifacts through an outbox, one organization at a time under that
organization's token. It asks the platform for who a credential is
and which organizations it has, and for no decision. The platform's
own `checkPermissions` path for cowork's kinds goes with #1784.

Where the route is built, a create, and a page's delete, answer after
the policy service holds the change
([ADR-0166](./adr/0166-a-cowork-create-or-delete-answers-after-the-policy-service-holds-the-change.md),
#1919): without it, the read that follows is denied as a `fault` of
`tenant-isolation` until the loop has run. The request wakes the loop
and waits up to 5 seconds on the entity's own mark, and answers 503
`change_not_applied` naming the stored entity's GRN at the bound. A
page's delete waits on the mark of each page it removed, those beneath
it too. Updates, reads and writes through a page's sync connection do
not wait: the service holds an entity's type, identifier and
organization, and none of them changes there.

- **Decided as a machine sender.** Its `postMessage` asks
  `iam:message.send` of the policy service with its GRN as subject and
  the recipient as object.
- **A floor before the service.** A recipient that is not an agent of
  its organization is refused, and no consent relation carries it
  across. Its token reaches no other operation.
- **A revoked principal stops cowork for its organization.** While the
  deployment holds an organization's principal revoked, the api server
  refuses every `cowork:` action on that organization before it asks.
- **Under ADR-0130** it is a `Service` entity, its sending a system
  policy, and the floor and the revocation are system `forbid`s.

## Components

The platform holds no decision component. `platform/internal/authz` is
removed, and with it `Statement`, `Evaluator`, `PolicyCache`, the
store, the change listener and the two gRPC servers (ADR-0177). What
decides is the policy service, and what the platform holds of it is a
client, the delivery and the relay:

| Component | Responsibility |
|-----------|----------------|
| policy decision client | enforcement, `checkPermissions`, the holders list, the lists and the explanation ask the service through it (ADR-0171, ADR-0172, ADR-0174); wired by `platform/internal/cli/serve.go` and, for a send, by `platform/internal/cli/machine.go` (ADR-0168) |
| `currentDecision` (`platform/internal/cli/authority.go`) | delegated operation authority and the controller proof, asked read current, sending the actor, the action and the object and nothing else (ADR-0146 items 14 and 15) |
| the outbox and its loop | deliver entities and role links (§Delivery) |
| the relay | writes of policies and the audit record (§Managing policies over HTTP) |

`list_visibility.go` asks the service for each object of a list in one
`Evaluations` call, as ADR-0115 asked the earlier engine's `CheckBatch`.

## Roles vs policies

| Concept | Storage | Update cadence | Where it runs |
|---------|---------|----------------|---------------|
| Role | Postgres, `org` tables, delivered as role links | frequent | the policy service decides on the links |
| Policy | the policy service's store | rare; audited | evaluated per request by the service |

Roles are data. Policies are rules. Keep them on separate update
channels.

## Explain

The explanation is the policy service's `Evaluate` asked as a probe,
and its reasons name the policies that decided
(`permission_explanation.go`,
[ADR-0174](./adr/0174-the-explanation-comes-from-the-policy-service-the-old-policy-endpoints-go-and-the-parity-matrix-is-retired.md),
[api-surface.md](./api-surface.md) §What decides the rest). The earlier
engine's `Explain` RPC is removed.

## Constraints

- Resource-based policies (attaching a policy to a resource) are
  deferred; identity-based only (ADR-0008).
- The earlier Rego/OPA direction is superseded (ADR-0002), and no Rego
  source is in this repository. The WIP that ADR calls reference
  material is at a path outside it, so nothing here preserves it.
