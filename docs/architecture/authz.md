# Authorization

Org-scoped permission decisions, served by an independent gRPC policy
engine (`garam serve authz`, see
[ADR-0003](./adr/0003-single-cli-topology.md)). Policies are IAM-style
documents (see [ADR-0002](./adr/0002-iam-style-policy-model.md)).

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
replaces this engine with `policy/`, a Rust service of its own (#1689).
Everything below this section describes `garam serve authz` as it runs
today, and stays true until `policy/` serves the platform's checks.

`policy/` holds the contract, the schema, the system tier, its stores
of grants and entities, the access report and the binary. No image
runs it, nothing calls it for a decision and the platform feeds it
entities, so it decides over nothing yet.
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
  `Iam::Action::"org.delete"`, in the evaluator, the roles, the console
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
  operator: a decision for an `Iam::Operator` composes the
  organization's guardrails and none of its `permit`s. An RPC writes
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
platform's own actions, in `role.go`, the evaluator, the console, the
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
  its change notification and `CheckCurrent` are removed, and no
  stored document of the earlier format is converted. The policy
  service is required in every deployment, and a listener that cannot
  have a request decided refuses it. The sections below this one,
  from Policy document to Explain, describe what the removed evaluator
  does today.
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
  Delivery; a role link of a group or a project, and a direct role,
  are not built.
- **A writer reads its own write.** A request that changes an entity, a
  membership or a policy waits, up to a bound of 5 seconds, until
  the outbox marks it wrote are applied, and is answered as
  unavailable if they are not. Built for the four requests that write
  an organization membership: creating an organization, accepting an
  invitation, registering by one, and deleting an organization, and
  for the two requests that write and end an operator's delegation
  (§Operator operations under Delivery). A request that changes any
  other entity does not wait, except those §Consents names, and the
  policy write is not the platform's yet.
- **Every present capability has a place.** The ADR's inventory gives
  each origin of a statement and each surface that reads a decision
  its form. A hosted operator's delegation, handed to the evaluator
  as a per-call Allow today, becomes an attribute of the operator's
  entity that a system policy per delegable operation reads. The
  policy service holds this half (#1805): `Iam::Operator` carries
  `operations`, joins the principals of the ten delegable actions
  alone and sits in its organization's `Iam::OrgMembers`, and a system
  policy per operation permits an operator of the resource's
  organization that holds it. The platform feeds the attribute and the
  operator's place among its organization's members (#1891, §Operator
  operations under Delivery), and still decides an operator's authority
  itself until its own round: the policy service holds the operations
  and no request asks it about them yet. Only an
  owner writes a `permit`, so an administrator keeps assigning the
  roles it holds and writing guardrails and no longer writes an
  `Allow`.
- **cowork asks the service alone,** and its path through
  `checkPermissions` goes.
- Built in the policy service (#1785), not yet relayed by the
  platform: every denial and every write of a policy is recorded by
  the policy service for the organization of the resource, and an
  organization's owners and administrators read it on
  `iam:audit.read`
  ([ADR-0148](./adr/0148-the-policy-service-records-every-policy-write-and-every-denial-for-the-organization-it-concerns.md)).
  A write is recorded in its own transaction, so one that cannot be
  recorded is not made. A denial is recorded after the engine decides
  it, and one whose record cannot be written is answered anyway, the
  failure logged and counted in process. Not built: the platform's
  relay of `ListAuditRecords`, and a metric for the failures.

[ADR-0147](./adr/0147-a-message-between-two-consenting-organizations-is-tenant-isolations-one-exception.md) makes a
message between two agents of two consenting organizations tenant
isolation's one exception: `Org` gains `consents`, the platform feeds
it, the isolation `forbid` gains one `unless` branch for `message.send`
between agents where each organization's `consents` holds the other,
and both organizations' guardrails are read for such a send. The
policy service holds all of this (#1805) and the platform feeds
`consents` (§Consents under Delivery); the send path's refusal before
the engine, as [messaging.md](./messaging.md), stands until its own
round removes it. The sender's
guardrails are read as `forbid`s alone and under ids prefixed by its
organization, and the exception is read by no other action, since
an `unless` branch names `message.send` and two agents.

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

`garam policy-feed start` marks every membership with the rest of the
backfill, and so does migration 000075 for a feed already on, both
raising a mark's generation if it is there, so running them again
rewrites the same policies and no second one.

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

The send path still asks the platform's relation before the engine
(`consentStands`, `platform/internal/machineapi/message.go`) and does
not read the attribute; its own round removes the check.

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

Who decides an operator's authority is unchanged: the `CheckCurrent`
consumers, the minting of an operation authority and the controller
proof read the platform's grant, not the attribute. Their own round
removes that.

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

| Today | Under ADR-0130 |
|-------|----------------|
| IAM-style documents, matched by `platform/internal/authz` | Cedar policies, evaluated by the `cedar-policy` crate and validated against the manifests under `policy/schema/` |
| `CheckRequest` carries collected statements, `subject_groups` and `subject_org` | `Evaluate(subject, action, resource, context)` in AuthZEN's shape; the service reads memberships and ancestors itself |
| Roles are org data the api server turns into statements | A role is a system-tier template, one set for an organization and one for a team or project, and an assignment is a policy linking it |
| A policy attaches to a user, an agent or a group | A grant names its principal; a guardrail is a `forbid` |
| An organization's scope is checked by `subject_org` | Tenant isolation is a system `forbid` no policy overrides, unless the subject is in the resource's organization's `OrgMembers`, which no team or project sits in |
| `CheckCurrent` | A request option reading storage instead of the cache |
| `Explain` | The decision's reasons, and an access report over grants |

## Policy document

**Nothing evaluates a `Condition`.** The key is reserved: a document
carrying one is stored and read back as it was written, and no
evaluation reads it. Every other key below is read.

```json
{
  "Version": "2026-07-25",
  "Statement": [
    {
      "Sid": "AllowOrgRead",
      "Effect": "Allow",
      "Action": ["iam:org.read", "iam:project.read"],
      "Resource": ["grn:acme/*"]
    }
  ]
}
```

- `Effect`: `Allow` | `Deny`.
- `Action`: `<service_type>:<verb>` strings; `*` and `?` wildcards may
  appear anywhere and repeat (`iam:org.*`, `*`). Constants are owned by
  the defining domain (`platform/internal/org`,
  `platform/internal/user`, …). The engine matches strings only.
- `Resource`: GRN patterns with the same wildcard rules
  (`grn:acme/*` scopes to an org).
- `Condition`: reserved, as above.

Reserved is not dropped, and neither is anything else. The parse
boundary refuses a key none of the above names rather than discarding
it, and what it accepts is stored whole: `policy_versions` is
append-only, so a field lost on the way in has nothing left to recover
it from and the version an administrator reads back would not be the
one they wrote.

## Evaluation

```
Evaluator.Evaluate(context, Context)
  1. any matching Deny statement?          → DecisionDeny
  2. else any matching Allow statement?    → DecisionAllow
  3. else                                  → DecisionDeny (default)
```

The engine matches strings only. Matching = action pattern ∩ resource
pattern both match.

## Statement collection

Evaluation input has two origins and the engine unions them
([ADR-0007](./adr/0007-hybrid-statement-collection.md)). Deny-overrides
applies across the union: either origin alone can allow, and a Deny
from either wins.

| Origin | Owner | Resolved |
|--------|-------|----------|
| role-derived statements | api server | per request, from org tables |
| identity-derived statements | machine listener | per request, from the caller's own GRN |
| persisted policy statements | authz engine | from the org's cache; from storage for `CheckCurrent` |

The engine scopes a request to the org that owns the object: an org's
identifier is the organization segment of every resource beneath it
(ADR-0001). Which of that org's policies apply then depends on their
attachments (§Policy attachment).

### Role-derived statements

The api server checks each request against the resource it targets —
the org, or the group or project addressed by the path — and collects
the statements that apply to that resource:

| Origin | Action patterns | Resource patterns |
|--------|-----------------|-------------------|
| org membership role | `Role.Actions()` | the org GRN and `grn:<org>/*` |
| subject's direct roles | `org.ActionsFor(user.Roles())` | the org GRN and `grn:<org>/*` |
| target group's roles, when the subject is a member | `org.ActionsFor(group.Roles())` | the group GRN |
| target project's roles, when the subject is a member | `org.ActionsFor(project.Roles())` | the project GRN |

Group and project roles reuse the org role vocabulary (`owner`,
`admin`, `member`), are assigned through the `roles` field of the
group and project create and patch endpoints, and are narrowed by
resource: a group `admin`
administers that group and nothing else, while an org `admin`
administers everything the org owns. Unknown role names grant nothing.
Org membership stays the precondition for every check — neither a
group grant nor a direct role admits a non-member of the org.

Roles stored on the user itself are direct grants and carry the org
role's scope, so a direct role applies in every org the subject
belongs to. Policies are not collected here at all — the engine owns
them and resolves them from the subject and the groups the request
carries (§Policy attachment).

### Identity-derived statements

A machine identity holds no roles and belongs to no org, so the machine
listener collects what its GRN alone grants it and supplies that in the
same slot
([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)).

| Origin | Action patterns | Resource patterns |
|--------|-----------------|-------------------|
| the caller's own GRN | `iam:message.send` | `grn:<caller org>:*` |

That is why an agent nobody has written a policy for goes on sending
inside its organization. A policy narrows it with a Deny, as a Deny
narrows a role's grant.

What keeps an agent out of another organization is not that pattern.
The listener refuses a recipient of another organization before it
asks, which is how it meets the obligation §Policy attachment states;
the organization segment in the pattern is defence in depth behind it.

The listener sends the sender's groups beside that statement. Group
membership is org data and this surface holds no store, so it reads
them over a seam of its own, which is what makes a policy attached to a
group decide what its members send (§Policy attachment). Membership it
cannot read refuses the message rather than deciding it without: an
unread group and an empty one are the same answer otherwise, and the
difference is every Deny attached to one. The listener also sends
`subject_org` — the GRN of the organization the sender's own GRN
already names — so the engine's scope check (ADR-0038) reads the
boundary the same way it reads any other subject's.

## Policy persistence

Policies are org-managed data in Postgres (ADR-0002): `policies`
carries the metadata, status, and default-version pointer;
`policy_versions` is append-only, enforced by a schema trigger. A
stored version is never rewritten, and versions disappear only with
the policy that owns them. Appending a version leaves the pointer where
it is; rollback moves it to any stored version, older or newer than
the current default; activation changes the status and leaves the
pointer alone. A policy is retired by deleting it, which removes it
with its versions and its attachments in one statement holding its row
lock ([ADR-0117](./adr/0117-garam-keeps-no-legacy-path-no-live-deployment-uses.md)).

| State | Meaning |
|-------|---------|
| `draft` | stored, evaluated by nothing |
| `active` | contributes its default version's statements to checks |

The engine parses each org's active documents once and caches the
resulting statement set. Writes made through the engine drop the org's
entry directly. Writes made by any other process announce the org on
the Postgres `LISTEN`/`NOTIFY` channel `garam_policy_change`, which a
schema trigger raises and the engine's listener consumes.

Losing that connection is ordinary rather than exceptional — a Postgres
failover breaks it by design — so the listener reopens one, backing off
between attempts so a database that is down is not retried into an
outage of its own. A notification raised while it was down is not
repeated, so a reopened connection drops every cached org: what the
engine read before the break is what it cannot know is current. Only
shutting the process down ends the listener, and `garam serve authz`
reports that.

A reconnecting listener does not take the engine out of rotation. The
engine's readiness reports the database and nothing else, because
readiness reports what a replica cannot serve without and one that is
reloading its cache still answers
([ADR-0029](./adr/0029-readiness-reports-what-a-replica-cannot-serve-without.md)).
A decision may therefore be made from statements no notification has
invalidated yet; if that ever has to be refused, the refusal belongs to
the check, which can name the request in doubt, and not to a probe,
which cannot.

**`CheckCurrent` is that refusal**, for the privileged actions that
must be checked against current authority
([ADR-0078](./adr/0078-a-lifecycle-authority-activates-a-runtime-generation-and-old-generations-and-lineages-are-fenced.md)).
Its callers are delegated operation authority on both listeners and
the controller proof on the machine listener
([operators.md](./operators.md) §Delegated operation authority,
`operators.md` §Controller proof).
It decides as
`Check` does, except that the org's active policies and their
attachments are read from storage on every call: it neither reads nor
fills the cache, and storage it cannot read fails the call rather than
falling back to the cache. Only that read is current. The statements,
`subject_groups` and `subject_org` a caller sends are taken as sent,
so a caller needing current membership, roles or groups resolves them
afresh. It is a method of its own and not a field on `CheckRequest`:
an engine predating it answers `UNIMPLEMENTED`, where a field it does
not know would be dropped unread and the call answered from its cache.
`Check` and `Explain` keep the cached behaviour above.

Lifecycle is served by `PolicyService` in
`platform/proto/garam/authz/v1`: create, append a version, list policies
and versions, search policies by name or identifier for `searchOrg`
(api-surface.md), activate, delete, roll back, attach, detach, and list
attachments. Documents are validated at the write boundary, so a stored
document always parses.

## Managing policies over HTTP

Org admins reach that lifecycle under `/orgs/{org}/policies`
(`platform/api/openapi.yaml`). The api server passes each call through
the `PolicyManager` seam — the gRPC client to `PolicyService` — so it
never imports the engine and never parses a document: a document that
does not parse comes back as `invalid_argument` and leaves as 400.

Policy management is authorized like any other action. The
`iam:policy.create`, `iam:policy.read`, `iam:policy.update`,
`iam:policy.attach`, `iam:policy.detach` and `iam:policy.delete`
constants live in `platform/internal/org`, because the api server
enforces them and may not import the engine that stores policies.
`owner` and `admin` grant `iam:policy.*`; `member` grants none, so
reading a policy is an admin action. The addressed policy's GRN is the
object of the check, and its organization segment is the org in the
path, so a policy of another org is not addressable.

The console calls these operations and no others
([ADR-0073](./adr/0073-the-console-manages-policies-and-their-attachments-under-permissions.md)),
and nothing in it changes what one means: the org-wide reach of an
unattached active policy, Deny-overrides, the unread `Condition` and
the last-attachment refusal stay the engine's, and
[web-app.md](./web-app.md) §What it serves states how the console
presents each. No operation answers the policies naming one subject,
so the console assembles that from each policy's attachments and each
team's members, and what it shows is what is stored, not a decision.

## Policy attachment

An active policy is attached to subjects through `policy_attachments`
([ADR-0008](./adr/0008-identity-based-policy-attachments.md)). The
engine matches subject strings and validates no service type, so an
agent GRN is a subject on the same terms as a user
([ADR-0014](./adr/0014-agent-authentication.md)) — it needs no model
change, only a caller that can authenticate as one. The machine
listener is one; the browser-facing listener is to be the other
(§An agent on the browser-facing listener).

| Attachment | Applies to |
|------------|------------|
| none | every subject the caller asks about (see below) |
| a user GRN | that user |
| an agent GRN | that agent |
| a group GRN | the members the request reports for that subject |

A check therefore evaluates four sources: the org's unattached active
policies, the policies attached to the subject, the policies attached
to a group the subject belongs to, and the statements the calling
server collected. Deny-overrides applies across all four.

**An unattached policy's scope is the caller's obligation, not the
engine's**
([ADR-0025](./adr/0025-engine-scope-is-a-caller-obligation.md)) — the
engine did not hold it, and the engine now does. The engine loads the
policies of the organization owning the **object** and applies every
unattached one to whatever subject it was handed; it holds no membership
data and cannot tell whether that subject belongs to that organization.
The caller therefore establishes that the subject belongs to the
object's organization before asking, sends that organization as
`subject_org` on `Check` / `Explain`, and the engine refuses the call
when the field is unset or does not match
([ADR-0038](./adr/0038-the-engine-learns-a-subjects-memberships-and-enforces-its-own-scope.md)).
`platform/internal/api` populates it from the org whose member it
fetched; the machine listener populates it from the sender's own GRN,
after the consent relation has decided the same-organization half. A
caller that hands the engine a subject from another organization is
refused at the engine — the boundary has moved, and the engine is where
the obligation now lives. The groups a caller sends are subjects too,
and the same obligation covers them: a group belongs to the org that
names it in its GRN.

The row above reads "every subject the caller asks about" for that
reason. A caller that hands the engine a subject from another
organization gets that organization's unattached policies applied to
it — only when it also names the right `subject_org`, which is what
the engine now requires.

Detaching an active policy's last attachment is refused, because the
policy would not retire — it would widen back to the org, which for a
Deny turns one subject's restriction into every subject's, and for an
Allow hands that grant to every subject a caller asks about. The
remedies the refusal names are the deliberate acts: delete the policy to
retire it, or attach the next subject before detaching this one. The
org-wide state itself is unchanged
([ADR-0008](./adr/0008-identity-based-policy-attachments.md)): a policy
created and activated without an attachment applies to every subject the
caller asks about, so entering that state stays possible and is never a
side effect of removing an attachment. A draft policy's last attachment
detaches freely, since nothing evaluates it. Deleting is what takes a
policy out of every check, attached or not: nothing of it is left to
widen, so the deletion refuses nothing on attachments.

An attached policy decides what an agent sends
([ADR-0024](./adr/0024-policy-decides-what-an-agent-sends.md)), inside
the sending agent's own org and nowhere else. Every attachment that
decides an agent's message is therefore a Deny: an Allow widens nothing
the identity-derived grant does not already cover, and it buys no
recipient outside the org, which the listener refuses before a policy
is read.

Each caller resolves the subject's groups from the org tables and sends
them as `subject_groups` on `Check` / `Explain`; the engine matches
those GRNs against attachments without knowing what a group is. Each
caller also sends `subject_org` — the GRN of the organization the
subject belongs to — and the engine refuses the call when it is unset
or does not name the object's organization (ADR-0038). An attachment
stays inside the policy's org, enforced by a schema trigger; a
root-owned identity such as a user cannot name an org in its GRN, so
org membership is established by the api server populating `subject_org`
before asking. An agent names its org in its own GRN, so the
machine listener reads it directly.
Attachment writes announce the org on `garam_policy_change` like any
other policy write.

## An agent on the browser-facing listener

**Unbuilt.** The api server admits a session alone today and refuses
a non-member before it asks.
[ADR-0123](./adr/0123-an-agent-calls-the-browser-api-and-the-cli-as-itself-authorized-by-its-own-policies.md)
admits an agent presenting a browser token
([machine-identity.md](./machine-identity.md) §Browser tokens), and
decides it this way:

| Field | For an agent |
|-------|--------------|
| subject | the agent's GRN |
| `subject_groups` | the groups whose members list that GRN |
| `subject_org` | the organization the agent's GRN names |
| collected statements | none: an agent holds no membership, direct, group or project role |

- **Its authority is the policies that reach it**: attached to it,
  attached to a group holding it, and the org's unattached active
  policies. An agent no policy reaches can do nothing there.
- **The membership check becomes an organization check.** The path's
  org must be the one the agent's GRN names.
- **A floor no policy lifts.** Before the engine is asked, an agent
  is refused every operation outside its own organization, and
  `issueOperatorKey`, `createInvitation`, `mintOperationAuthority`
  and `approveAgentEnrollment` inside it. Policy writes, group
  membership and deleting the organization are left to policy, which
  a Deny attached to the agent bounds.
- **Under ADR-0130** the agent is a principal type beside the user,
  decided on the grants naming it or a team holding it, and the floor
  is a system `forbid`.

The engine needs no change for it. It was measured asking `Check` and
`Explain` about an agent and a user carrying the same attachments, a
group holding both included, with no collected statements: every
decision and every matched statement was the same for both
(ADR-0123 §Context).

## cowork's principal

**Built in cowork (#1758); the principal's issue is unbuilt
(#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
gives each organization a cowork principal on the machine listener
([machine-identity.md](./machine-identity.md) §cowork's principal).
No such subject reaches the engine today from the platform's path.
[ADR-0141](./adr/0141-garam-is-an-oidc-issuer-that-exchanges-sessions-for-audience-bound-tokens-and-services-call-the-policy-service-directly.md)
supersedes in part ADR-0127 §5: cowork asks the policy service
itself, under a caller token from `exchangeClientAssertion` with
`audience` `policy` and `sub` its principal, and not through the
platform. cowork builds that route (#1758) behind four settings
(`COWORK_POLICY_ADDR`, `COWORK_POLICY_CA_FILE`,
`COWORK_MACHINE_TOKEN_URL`, `COWORK_DEPLOYMENT_KEY_FILE`): where they
are set it asks one `Evaluations` per batch under the token of the
organization asked about, and feeds its channels, pages, tasks and
artifacts through an outbox, one organization at a time under that
organization's token; where none is set it asks the platform as
before. The platform's path goes with #1784.

- **Decided as a machine sender.** Its `postMessage` asks
  `iam:message.send` with its GRN as subject and the recipient as
  object, on the identity-derived Allow over its own organization
  (§Identity-derived statements). A policy attached to it applies, so a
  Deny narrows which agents cowork may deliver to; an Allow widens
  nothing.
- **A floor before the engine.** A recipient that is not an agent of
  its organization is refused, and no consent relation carries it
  across. Its token reaches no other operation.
- **A revoked principal stops cowork for its organization.** While the
  deployment holds an organization's principal revoked, the api server
  refuses every `cowork:` action on that organization before it asks.
- **Under ADR-0130** it is a `Service` entity, its sending the system
  policy ADR-0024's baseline becomes, and the floor and the revocation
  are system `forbid`s.

## Components

| Component | Responsibility | Depends on |
|-----------|----------------|------------|
| `Statement` / `PolicyDocument` | IAM-style rule model (ADR-0002); the `Effect` type is declared once in `platform/internal/wire` and shared with every surface | `platform/internal/wire` |
| `Evaluator` | statement-set → `Decision` | — |
| `StatementEvaluator` | native in-process `Evaluator` | — |
| `Policy` / `PolicyVersion` | stored policy and its revisions | — |
| `PolicyAttachment` | binds a policy to a subject (ADR-0008) | — |
| `Store` | policy and attachment persistence, implemented by `platform/internal/authz/postgres` (ADR-0069) | `platform/migrations/` |
| `PolicyCache` | per-org parsed policies and their attachments, and the uncached read `CheckCurrent` uses | `Store` |
| `ListenPolicyChanges` (`platform/internal/authz/postgres`) | drops a `PolicyCache` entry as `garam_policy_change` announces the write | `PolicyCache` |
| `GRPCServer` | `Check` / `CheckBatch` / `CheckCurrent` / `Explain` RPCs | `PolicyCache` |
| `PolicyGRPCServer` | policy lifecycle RPCs | `Store`, `PolicyCache` |

Role persistence (subject × role × scope) stays roadmap; roles remain
org data resolved by the api server.

The api server never imports this package; it calls the engine over
gRPC (`Check`, `CheckBatch` and `Explain` in
`platform/proto/garam/authz/v1`) and supplies the role-derived
statements it collected. `CheckBatch` is `Check` asked of many objects
for one subject and one action: the statements that hold for every
object are sent once, each object carries those that reach the subject
through it alone, and the persisted statements are read once for the
call. Each object is decided as `Check` would decide it, and one outside
the subject's organization refuses the call. An org-gated list asks it
once per page to drop the objects the caller may not read
([ADR-0115](./adr/0115-a-list-drops-each-object-its-caller-may-not-read-asked-of-the-engine-in-one-batch.md)).
The machine listener reaches the same engine the same way, over a seam
of its own, and supplies the identity-derived ones. Delegated operation
authority on both reaches `CheckCurrent` (§Policy persistence) through
one adapter, `platform/internal/cli/authority.go`, which reads the
membership, the user's own roles and its groups for each call and sends
the org-scoped statement those roles grant; neither operation targets a
group or project, so no scoped role is collected. Each listener's
`Evaluator` seam has one non-test implementation, the gRPC client, and
that is what both composition roots wire
(`platform/internal/cli/serve.go`, `platform/internal/cli/machine.go`).
The in-process evaluator is the engine's own rather than a test
substitute: `StatementEvaluator` is what `Check` runs on every request
(`platform/internal/authz/server.go`).

## Roles vs policies

| Concept | Storage | Update cadence | Where it runs |
|---------|---------|----------------|---------------|
| Role | Postgres, `org` tables | frequent | api server collects the statements a role grants |
| Policy | Postgres, engine tables | rare; audited | parsed, cached per org, evaluated per request; read from storage for `CheckCurrent` |

Roles are data. Policies are rules. Keep them on separate update
channels.

## Explain

The `Explain` RPC reports which statements matched and which produced
the final decision, across both origins. It decides over the set
`Check` decides over, read from the same cache, so its decision is
`Check`'s. A persisted statement's trace names the policy holding it
and the subjects of the request that policy is attached to — none for
one applying org-wide — so a denied request is reproducible after the
fact down to the attachment that carried it.

Its one caller is `explainPermission` on the browser-facing listener,
which names a policy only to a caller who may read it
([api-surface.md](./api-surface.md) §What decides the rest). No CLI
subcommand asks it, and the machine listener does not.

## Constraints

- Resource-based policies (attaching a policy to a resource) are
  deferred; identity-based only (ADR-0008).
- The earlier Rego/OPA direction is superseded (ADR-0002), and no Rego
  source is in this repository. The WIP that ADR calls reference
  material is at a path outside it, so nothing here preserves it.
- Domain types do not satisfy `authz.Object`; the engine adapts a
  parsed GRN at the transport boundary.
