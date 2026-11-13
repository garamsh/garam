# ADR 0148: The policy service records every policy write and every denial for the organization it concerns

> Status: accepted; built in the policy service (#1785), not in the
> platform's relay or the console's view
> Date: 2026-11-12

Drafted for #1800, under #1796, which holds the owner's decisions of
2026-11-12 this records and reserves this number. It supersedes
nothing: it fills what
[ADR-0130](./0130-the-policy-engine-is-a-rust-service-deciding-cedar-policies-behind-an-authzen-shaped-grpc-contract.md)
§5 calls "audited" and what
[ADR-0106](./0106-every-message-send-is-recorded-as-an-audit-event-in-the-transaction-of-its-outcome-and-garam-stays-the-only-route.md)
and
[ADR-0114](./0114-every-machine-credential-issuance-renewal-and-refusal-is-an-audit-event-read-on-the-subjects-own-read.md)
left open, a record of decisions and changes. garam is read at
`85739f0026e01ade88f7fe6f926faabc005c7ed7`; a claim below that names
no revision holds for it.

## Context

- **The two records garam keeps are not of authorization.**
  `docs/architecture/audit.md` holds message sends (ADR-0106) and
  machine credential events (ADR-0114), in the platform's database.
  Decision and change events were planned and never built.
- **The policy service answers and keeps nothing of the answer.**
  `Evaluate` returns a decision with reasons naming the policies that
  decided (`policy/proto/garam/policy/v1/policy.proto`, `Reason`), and
  nothing is stored of it. A denial leaves no trace an organization
  can read.
- **A policy version names its writer and little else.**
  `Version` in `policy/crates/grant/src/postgres.rs` holds the version
  number, the body and `created_by`. Appending a version, activating
  one, rolling back and removing a policy are methods of the same
  store, and none leaves a record of its own.
- **The policy service is the one place every decision passes.** The
  platform's listeners and cowork each ask it (ADR-0130 item 8,
  #1796), so a record kept anywhere else would miss a caller.
- **An organization will write `permit` policies** (ADR-0144), which
  can widen access where it could once only narrow. Who wrote what, and
  when, is what its owners and administrators need to read.
- **A denial costs nothing to cause.** ADR-0106's Consequences already
  record that a sender retrying a denied post writes a row each time,
  and there the table is bounded by nothing but its tenants' deletion.
  A record of every denial that a caller can provoke is a store a
  caller can grow.
- **garam is not operating** (owner, 2026-11-12), so no record exists
  to convert.

## Decision

**The policy service records every write of a policy and every denial,
in its own database, for the organization of the resource concerned.
Writes are kept as long as the organization exists, denials for 90
days, and an organization's owners and administrators read them.**

1. **Every write of a policy is recorded, and kept as long as its
   organization exists.** A write is one of: a policy created, a
   version added, a version made the default (activation), the default
   moved to an earlier version (roll-back), and a policy removed. It
   covers every kind of policy an organization holds: a role link, a
   guardrail and a `permit`. No sweep reaches a write's record.
   - **The record is written in the transaction of its write.** The
     policy store and the record share one database, which ADR-0106
     and ADR-0114 did not, so a write that committed has its record
     and a record exists only for a write that committed.
   - **A write refused is not a write.** One that fails validation or
     the scope check stores nothing and leaves no write record. A
     write refused for lack of the right to make it, as an
     administrator's `permit` is (ADR-0144 item 7), is a denial of
     `iam:permit.write` and is recorded as item 2 says.
2. **Every denial is recorded and removed 90 days after it was made.
   An allowed decision is not recorded.** A denial is a decision whose
   answer is deny, whatever its reasons: a policy's, tenant isolation's
   and a fail-closed evaluation error (ADR-0137 item 2) alike. An entry
   of `Evaluations` that is denied is a denial of its own. A request
   refused before any decision (a caller the service does not know, a
   caller asking outside its service) is not a decision and is not
   recorded.
   - **A denied probe is not recorded.** A question says whether it is
     an attempt, a request someone made and was refused, or a probe, a
     caller deciding what to show, such as filtering a list or
     enabling a control. An attempt is the default, so a question that
     says nothing is recorded when denied. A denied probe is not
     recorded, and an allowed one never is. The callers are the closed
     list of services (ADR-0140), and which of a caller's questions
     are probes is that caller's to state in its own code, where it is
     reviewed. Without this a list filtered through `Evaluations`, as
     cowork's pages and the console's controls are, would make most of
     the record and reach item 7's cap in ordinary use. The contract
     leaves open only how a request carries the statement.
   - **What removes it.** The policy service itself, on a schedule it
     runs in `policy serve`, deleting every denial record whose
     `last_at` is more than 90 days before the database's clock. The
     read of item 6 answers no denial past that bound whether or not
     the sweep has run, so the bound is a property of the read and the
     sweep reclaims space.
   - **What the bound rests on.** The owner's decision of 2026-11-12:
     a denial is read to investigate what happened lately, a quarter
     covers that, and a denial older than it is the one thing the
     store can lose without losing a fact an organization is owed. It
     is measured on the database's clock, as ADR-0106's events are,
     and on the last denial a record counts (item 7), so a denial
     repeated is not removed while it repeats.
3. **A record belongs to the organization of the resource decided on,
   and is removed with that organization.** The organization is the
   first segment of the resource's GRN (ADR-0142 item 1), and for an
   `Iam::Org` its own. A write's record belongs to the organization of
   the policy.
   - **An organization the service does not hold has no record.** A
     denial whose resource names an organization with no `Iam::Org`
     entity in the service's store is not recorded, and the omission is
     counted. `Iam::Org` is written by the platform alone (`@caller` in
     `policy/schema/iam.cedarschema`, ADR-0143 item 1), so a caller
     cannot make a record by naming one, and item 7's bound
     cannot be multiplied by naming many.
   - **A denial by tenant isolation records the outside subject's
     GRN and nothing else of it.** The subject is not a member of the
     organization, and the record is still the organization's, because
     the attempt was on its resource. The organization reads the
     subject's GRN: a name of the form `grn:<org>/<service>/<type>/<id>`
     (ADR-0142), which already tells it which organization and which
     principal asked. No attribute, membership or policy of the
     subject's own organization is read or stored, since the record
     holds only what item 4 lists. That is acceptable because a GRN is
     a name and not a credential or a secret (ADR-0142), the
     organization is the party whose resource was asked of and has a
     claim to know who asked, and the service's decision never needed
     more than the name to deny.
   - **How its removal is tied to the organization's** is the
     implementation's: the policy service holds the records in its own
     database, so no foreign key joins them to a row of the platform,
     and the rule is that no record outlives the deletion of its
     organization reaching the service.
4. **What a record holds.** One record is one of two kinds, `write` or
   `denial`, and holds:
   - `id`, `kind` and the organization's GRN;
   - when: for a write `at`, and for a denial `first_at` and `last_at`
     and the `count` of item 7, all on the database's clock;
   - who: the subject's GRN and the calling service's `sub` (ADR-0140
     item 1). For a write the subject is the writer the platform
     names, which `created_by` holds today. A write the platform makes
     as itself with no writer, as it writes a member's role link from
     the memberships (#1799, ADR-0146), is recorded with the calling
     service and no subject. Whether the platform names who changed
     the membership is open;
   - the action, in its qualified wire form (`iam:permit.write`,
     ADR-0145);
   - the resource's GRN: for a write, the policy's;
   - the outcome: for a denial, `deny`; for a write, `created`,
     `version-added`, `activated`, `rolled-back` or `removed`;
   - the ids of the policies in the decision's reasons, for a denial,
     and for a write the policy's id with the version written, or the
     version moved from and to. A reason that is an evaluation fault
     is recorded as `fault` and never as its text.
   - **All of it is the organization's to read.** No field is held
     back from its owners and administrators; each is a name, a time
     or an id.
   - **No record holds** a policy's text beyond its id and version,
     anything of a request's `context` or of a resource's properties,
     a token or any part of one, an entity's attributes, or the text of
     a fault. The reason is shape, as ADR-0114's: the store has no
     column for them, so no later change can start recording them by
     writing one more value. The body of a version stays where
     `policy_versions` holds it, readable by what already reads a
     policy.
5. **The policy service keeps it, in its own database.** It is the one
   place every decision passes, cowork's included. The platform's
   database is not used: a record of a decision the platform's
   listeners ask for and one cowork asks for would be written by two
   services and read from one, and the policy service would have to
   call the platform to write what it just decided, against ADR-0130
   item 10.
6. **An organization's owners and administrators read its records,**
   through the console, the API and the CLI. Reading is the action
   `iam:audit.read` (ADR-0145: type `audit`, verb `read`), declared in
   the `Iam` manifest and held by `role-owner` and `role-admin`. A
   member and another organization's owner do not hold it. It is
   asked of the organization, as the other reads on an organization
   are.
   - **How the platform reaches the records.** The policy service's
     contract gains a read of an organization's records, served to the
     platform alone. The platform authorizes the reader on
     `iam:audit.read` through the policy service as it does any act,
     and then asks for the organization's records; the read requires
     the organization and returns nothing of another. The platform's
     browser API, the console and the CLI are that read's callers,
     since they hold the session the policy service does not. The
     RPC's shape, its paging and its filters are the implementation's.
   - **A read is not recorded,** but a read denied is a denial like
     any other, and is recorded as one.
   - **The manifest changes with the implementation,** not with this
     ADR: `policy/schema/iam.cedarschema` carries no `audit.read` now.
7. **Recording does not decide, and it is bounded.** A denial is
   answered whether or not its record could be written, and a failure
   to record is logged and counted. A write is the exception item 1
   states: it shares its transaction, so a write that cannot be
   recorded is not made. What a caller that denies in a loop costs,
   and what bounds it:
   - **Identical denials collapse into one record with a count.** Two
     denials are identical when they share the organization, the
     subject, the action, the resource and the set of policy ids in
     their reasons, and a record covers one hour, the hour of its
     `first_at`. A denial that matches a record of its hour adds one
     to its `count` and moves its `last_at`; it writes no new row. A
     caller that denies one question in a loop costs one record an
     hour, and 24 a day.
   - **An organization holds at most 10,000 live denial records.**
     Varying the subject or the resource makes denials that are not
     identical, so the collapse alone does not bound the store. At
     the cap a denial that matches no record stores no row; the
     service adds one to the `count` of the organization's overflow
     record for that UTC day, a record of its own kind
     (`denial-overflow`) holding the day, the count of denials not
     stored and nothing else. A denial that matches an existing
     record still collapses into it at the cap.
   - **So the store is finite.** Per organization, at most 10,000
     denial records and one overflow record for each day of the
     window, 90, beside the writes. Writes are not capped: each is a
     write an authenticated writer made under `iam:permit.write` or
     the policy right it needs, and its rate is the platform's to
     bound as it bounds any write. Across the deployment the store is
     bounded by the organizations the platform holds times that
     figure, and an outside subject adds none (item 3).
   - **The numbers are decided here.** An hour and 10,000 are what the
     rule is stated with, so that a change of either is a decision an
     ADR makes, and an implementation does not choose its own.

## Consequences

- **An organization can read who wrote which policy, when, and what it
  was denied.** The question "who gave this agent that access" has an
  answer beyond a version's `created_by`.
- **The record is the organization's, and so is an outsider's GRN in
  it.** An organization reads who outside it asked for its resources.
  Nothing more of the outsider is learned, and nothing of the
  organization's is learned by the outsider, who cannot read it.
- **A denial costs a write on the answering path.** Collapse makes the
  common loop one upsert, and whether the answer waits for the write
  is the implementation's, since the answer is the same either way.
- **A list that filters by asking `Evaluations` records nothing for
  the objects it drops,** since its caller states those questions are
  probes (item 2). The record is of attempts, and the cap is not
  spent on what a caller decided to show. A caller that fails to state
  a probe records its denials as attempts, and the collapse and the cap
  bound that.
- **A record can be lost, and a write's cannot.** A denial's write
  that fails, or that the cap refuses, is gone and is counted. A
  policy write and its record commit together.
- **Overflow is visible.** A day on which the cap was reached has a
  record saying how many denials were not stored.
- **The record is the policy service's, so garam's records sit in two
  databases.** The platform's send and credential events stay where
  they are, and no event moves.
- **Nothing is built.** The policy service stores no record, serves no
  read and the `Iam` manifest has no `audit.read`; the platform and the
  console show none. What exists today is the writer's name on a
  version and the reasons an `Evaluate` returns.

## Signs the decision was wrong

- An organization needs to see an allowed decision, which would argue
  for recording more than this does and for a different bound.
- The overflow record appears for an organization in ordinary use, so
  the cap of 10,000 is too small to keep the record useful, or probes
  a caller did not state are filling it with denials no one wants.
- A denial is needed after 90 days to answer an investigation, which
  would argue for a longer window or for keeping denials of one kind.
- A record is found for an organization the service does not hold, or
  one survives its organization's deletion.
- A record holds text of a request's context, a policy body or a token,
  which would show the shape did not hold.
- A denial's record could not be written for lack of the database and
  the failure went unnoticed, which would show the count of item 7 is
  not read.

## Alternatives rejected

- **Recording every decision, allowed ones included.** An allowed
  decision is the common case, and the record would grow with the
  platform's traffic and not with what an organization needs to
  investigate. An allowed decision is what the organization's own
  policies already say; a denial is the event that none of them
  covers. Owner's decision of 2026-11-12, and out of scope in #1796.
- **Recording in the platform's database beside the credential
  events.** The platform would have to be told of each decision cowork
  asks the policy service for, which calls from `policy/` to a service
  (ADR-0130 item 10), or the policy service would return the record to
  its caller to store, which makes the record the caller's to lose. A
  policy write's record could not share the policy store's
  transaction either.
- **No removal of denials.** A denial a caller can provoke would grow
  the store without bound. Removing after a window keeps what an
  investigation reads and lets the store settle.
- **A cap with no collapse.** A caller denying one question in a loop
  would fill an organization's cap in seconds and leave the record of
  every other denial to the overflow count. Collapse keeps the loop to
  one record, and the cap is only the second line.
- **A collapse with no cap.** Identical denials collapse, and a caller
  varying the resource makes none identical, so the store would still
  grow with the caller's effort.
- **A rate limit on the caller in place of a cap.** It bounds a rate
  and not the store: a slow caller still fills it over 90 days, and a
  limit needs a caller identity the outside subject may not have.
- **Silently dropping denials past the cap.** An organization would
  find a gap in its record and no way to tell one from a quiet day.
  The overflow record says that denials happened and how many.
- **Failing the answer when a denial cannot be recorded.** The answer
  is the decision; refusing to give it because a log failed turns a
  database fault into an outage of every deny, and a deny is the
  fail-closed direction already.
