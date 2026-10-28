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

| Today | Under ADR-0130 |
|-------|----------------|
| IAM-style documents, matched by `platform/internal/authz` | Cedar policies, evaluated by the `cedar-policy` crate and validated against `policy/schema/garam.cedarschema` |
| `CheckRequest` carries collected statements, `subject_groups` and `subject_org` | `Evaluate(subject, action, resource, context)` in AuthZEN's shape; the service reads memberships and ancestors itself |
| Roles are org data the api server turns into statements | A role is a system-tier template, and an assignment is a policy linking it |
| A policy attaches to a user, an agent or a group | A grant names its principal; a guardrail is a `forbid` |
| An organization's scope is checked by `subject_org` | Tenant isolation is a system `forbid` no policy overrides |
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
      "Action": ["org:read", "project:list"],
      "Resource": ["grn:acme:*"]
    }
  ]
}
```

- `Effect`: `Allow` | `Deny`.
- `Action`: `<service_type>:<verb>` strings; `*` and `?` wildcards may
  appear anywhere and repeat (`org:*`, `*`). Constants are owned by
  the defining domain (`platform/internal/org`,
  `platform/internal/user`, …). The engine matches strings only.
- `Resource`: GRN patterns with the same wildcard rules
  (`grn:acme:*` scopes to an org).
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
| org membership role | `Role.Actions()` | the org GRN and `grn:<org>:*` |
| subject's direct roles | `org.ActionsFor(user.Roles())` | the org GRN and `grn:<org>:*` |
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
| the caller's own GRN | `message:send` | `grn:<caller org>:*` |

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
`policy:create`, `policy:read`, `policy:update`, `policy:attach`,
`policy:detach` and `policy:delete` constants live in
`platform/internal/org`, because the api server enforces them and may
not import the engine that stores policies. `owner` and `admin` grant
`policy:*`; `member` grants none, so reading a policy is an admin
action. The addressed policy's GRN is the object of the check, and its
organization segment is the org in the path, so a policy of another org
is not addressable.

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

**Unbuilt (#1664).**
[ADR-0129](./adr/0129-cowork-acts-in-each-organization-as-a-principal-the-platform-issues-with-it-proving-one-deployment-key.md)
gives each organization a cowork principal on the machine listener
([machine-identity.md](./machine-identity.md) §cowork's principal).
No such subject reaches the engine today.

- **Decided as a machine sender.** Its `postMessage` asks `message:send`
  with its GRN as subject and the recipient as object, on the
  identity-derived Allow over its own organization (§Identity-derived
  statements). A policy attached to it applies, so a Deny narrows
  which agents cowork may deliver to; an Allow widens nothing.
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
