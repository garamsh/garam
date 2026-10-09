# ADR 0070: An organization deletes an operator, and the record it withdraws is what that operator authenticated on

> Status: accepted — the record read per connection through a seam
> of the trust root's shape is superseded by
> [ADR-0100](./0100-every-machine-caller-authenticates-with-a-garam-issued-credential-and-short-lived-tokens-over-server-tls.md),
> which reads it per request beside the token
> Date: 2026-09-06

## Contents
- Context
- Decision
- What this does not decide
- Consequences
- Rejected alternatives

## Context

[ADR-0045](./0045-an-organization-registers-an-operator.md) refused a
delete route, and named the condition under which one could arrive:

> When revocation is decided, the route that removes a record can
> arrive with the thing that makes it mean something.

Revocation is now decided. The owner directed it on 2026-09-06, and
this ADR is where that reversal is recorded and bounded. What it
reverses is
[ADR-0014](./0014-agent-authentication.md) §Consequences' "Revocation
is by lifetime, not by list", for an operator and for nothing else.

**The cost ADR-0045 accepted has been paid.** Measured against the
running deployment on 2026-09-06, `operators` holds two rows and
`certificate_authorities` holds none. `gagent-dev` holds a certificate,
so `mintOperatorEnrollment` refuses it (ADR-0067); its authority is
gone, so that certificate chains to nothing; and there is no delete. Its
only exit is deleting the organization, which destroys the authority and
every other operator with it. Beside it, `operator_certificates` holds a
row for `gagent-credproof` whose registration is gone — the orphan
ADR-0045's errata predicted and #728 mis-explained.

**Registering is the only act here that a person could not undo.** A
member may register an operator, and may mint it another token where it
holds no certificate (ADR-0067) — which that ADR argued grants nothing,
because it is "what deleting the record and registering the same
identifier again would be". That sentence names a delete nobody could
perform. The permission was granted on the strength of an act the
system did not offer.

## Decision

**An operator authenticates while the organization still holds its
record, and deleting the record stops it.** The registration ADR-0045
created, which that ADR said "grants nothing" and `migrations/000017`
called "not a revocation point", becomes what admits the certificate.
Nothing else about the certificate changes: it is still verified
against the organization's authority, still carries its GRN in a SAN
URI, and is still refused when it expires.

**`DELETE /orgs/{org}/operators/{operator}` is the route**, on the
browser-facing listener, authorized on `operator:delete` — an action of
its own, named rather than reached through a wildcard, for the reason
ADR-0045 names its own actions one at a time. Owners and admins hold
it. It is not the action that registers: registering grants nothing and
this withdraws what an operator runs on, so a member who may add one
does not thereby hold the power to stop one.

**The listener reads the record through a seam of the same shape as
the trust root.** `internal/machine` declares `Registration`, the
composition root satisfies it from `internal/operator`, and
`Service.Authenticate` asks it once per connection after the chain
verifies. There is no revocation list, no responder, no published
artifact and no stored serial — the reversal ADR-0014 refused on the
grounds of new infrastructure is taken without any.

**It reaches an operator and no other identity.** The adapter answers
"held" for every subject whose service type is not `operator`, without
a read. An agent is admitted by the claim its operator made
(ADR-0033) on a certificate this decision leaves alone, and garam holds
no registration for one to withdraw. This is stated here because
`internal/machine` authenticates an agent and an operator on one
listener and does not branch on which (ADR-0015): the branch is in the
adapter, so the package keeps that property and the exemption is a
decision rather than something a reader discovers.

**What the delete takes with the record is stated, and it is
everything the identifier held.** The unspent enrollment token goes by
the cascade `migrations/000023` declared for exactly this route. The
record of the certificate garam last issued has no foreign key to go by
(`migrations/000019`), so the delete removes it in the same
transaction. That row's removal is load bearing rather than tidy:
left standing it would answer `enrolled` against the next registration
of the same identifier, and ADR-0067's refusal would then deny that
operator the token it needs.

**A definition naming the operator refuses the delete, and nothing is
deleted.** `agent_definitions.operator_grn` references `operators` (
`migrations/000018`, `NO ACTION`, `NOT VALID`), so the schema already
refuses it; the store reads that refusal as the domain error it is,
because 23503 on a delete means the opposite of what it means on an
insert. The person points the definitions elsewhere or deletes them
first. This is the answer rather than a cascade or a preview: nothing
is destroyed, so there is nothing for a preview to enumerate, and
`listAgentDefinitions` already names what stands in the way.

### What was measured

The issue named a candidate and said it was not a mandate. Both were
built and measured against real Postgres in this worktree on
2026-09-06. **Cost did not decide, and the renewal window did.**

- **Cost per handshake.** Full TLS 1.3 handshakes, n=400, four runs.
  Today: 1.901, 1.642, 1.405, 1.489 ms. With the record read beside the
  trust root: 2.022, 1.663, 1.677, 1.706 ms. With a recorded-serial
  read instead: 2.134, 1.678, 1.616, 1.638 ms. The spread between runs
  is wider than the spread between the two, so neither is chosen on
  this. What is stable is that a handshake goes from one database read
  to two, and that the added one is a primary-key lookup — `Index Only
  Scan using operators_pkey`, plan cost 8.17, against `Index Scan using
  operator_certificates_pkey`, also 8.17.
- **Reached on a resumed TLS 1.3 handshake.** Yes, measured rather than
  inherited: a second connection reporting `DidResume=true` ran
  `Authenticate` a second time. The check sits inside the call ADR-0064
  §Decision 6 put in `VerifyConnection` for this reason, so it holds on
  resumption. Hung on `VerifyPeerCertificate` it would not, and a
  deleted operator would keep connecting for as long as its tickets
  lasted.
- **ADR-0046's renewal window.** This is what decided it. A caller
  holding the certificate garam has just replaced is answered 200 today
  and 200 with the record read; with the recorded serial read it is
  answered **401**. That reverses ADR-0046 §Consequences — "A
  superseded certificate still authenticates for the rest of its
  life" — as a side effect of adding a delete route, and it turns a
  renewal answer that never arrived from an operator that keeps working
  and cannot renew into one that is down at its next connection. The
  record read moves nothing there: renewing does not touch the
  registration.
- **After the delete.** `DELETE FROM operators` leaves the `operators`
  row gone and the `operator_certificates` row standing, because that
  table carries no foreign key. The record read refuses the certificate
  with no further statement; the serial read would need a second one,
  against the table ADR-0045's errata found already holding an orphan.
- **Where the check runs.** 200 requests over one kept-alive connection
  cost one `Authenticate` call, so a check in the handler would cost
  200 reads for the same traffic.
- **Reading the record through `Store.Get`.** 306µs against 222µs for
  a presence read of its own, plan cost 24.53 against 8.17 — the join
  the enrollment state is derived from, which this has no question
  about. The difference is inside the handshake's own run-to-run
  spread, and a second way to ask whether an organization holds an
  operator is a second answer to keep agreeing with the first.

## What this does not decide

- **Agent certificate revocation.** The mechanism does not reach an
  agent, by the decision above, and no route withdraws one. What an
  agent authenticates on is unchanged.
- **Whether an operator may be suspended rather than deleted.** There
  is one state and it is the record's presence. A withdrawal that could
  be reversed is a different decision, and nothing asks for it.
- **What becomes of the agents a deleted operator provisioned.**
  `agent_assignments` carries no foreign key to `operators`
  (`migrations/000007`), so the assignments stand and the agents keep
  their own certificates. The operator can no longer claim a definition
  or report state, because it can no longer authenticate. Whether an
  assignment should follow its operator is ADR-0033's ground and not
  this one's.
- **The `operator_certificates` foreign key.** It is still not added.
  `migrations/000019` refuses it so that a certificate is recorded for
  a registered operator and an unregistered one alike, which is what
  `garam issue-certificate` relies on, and ADR-0045's errata records
  that the table is deliberately independent of the registry. The
  delete removes the row by naming it rather than by inheriting a
  cascade, so the table stays independent and the route still leaves
  nothing behind. The orphan already on the deployment is not reached
  by this and is not repaired here.
- **Whether a member may mint a second token for an operator that
  already enrolled.** ADR-0067 left it open and this does not close it.
  It does remove one of that ADR's arguments: the delete-and-register
  it called a dance nobody can complete is now a pair of calls anyone
  holding both actions can make.

## Consequences

- **A compromised operator can be stopped.** Until now the only answer
  was deleting the organization, which destroys the certificate
  authority and every other operator with it, unrecoverably
  (ADR-0065). That is no longer the answer to one operator's
  compromise.
- **An operator holding a certificate the organization never registered
  stops authenticating.** This is the consequence a deployment feels on
  upgrade, and it is not confined to the delete: the rule is that the
  record admits, so no record means no admission. ADR-0045
  §Consequences already required such deployments to register their
  operators and made it a one-time act; under this it stops being
  optional. The recovery is registering the identifier, which a member
  may already do and which ADR-0064 permits for a GRN that already
  holds a certificate. On the deployment measured above the row this
  reaches is `gagent-credproof`, which authenticates nothing today
  because its authority is gone.
- **A handshake costs two database reads instead of one.** Every
  connection to the machine listener, resumed ones included, and for an
  agent the second is answered without a read. What that buys is stated
  above; what it costs is inside the variance of the handshake it runs
  on.
- **The console gains a control that stops an operator**, with a
  confirmation that says which of the two consequences is true — a
  certificate refused, or an identifier freed. It replaces the prose
  saying a record cannot be removed rather than standing beside it.
- **`operator:delete` is a new action on the owner and admin roles.**
  No role gained a wildcard, and a member holding `operator:register`
  through a policy does not acquire it.
- **Expiry is no longer the only revocation garam has, and it is still
  the only one for every other identity.** The sentence has to be said
  with its subject from here on, which is why the responsibility
  documents that carried it unqualified are corrected in this pull
  request rather than left to be read as they stand.
- **Nothing detects a second listener reading an identity without the
  record.** The check is one call inside `Service.Authenticate`, and a
  future surface that authenticated a machine identity another way
  would not be refused by anything mechanical. The guard is review,
  which is the class [README.md](../README.md) §What nothing detects
  already describes.

## Rejected alternatives

- **Read the recorded serial instead of the record** — the candidate
  the issue named first, and the one measured against this. It needs no
  new question of the database, since `operator_certificates` already
  holds one row per operator. It is refused on the measurement: it
  refuses the certificate garam has just replaced, which reverses
  ADR-0046 §Consequences as a side effect of a delete route and makes a
  lost renewal answer an outage rather than an inconvenience. It also
  leaves the delete needing a second statement against a table with no
  foreign key, and it decides the theft question ADR-0046 deliberately
  bounded by lineage rather than by refusal.
- **Check in the handler rather than in the handshake.** Fewer moving
  parts, no change to `machine.Service`. It fails the requirement
  outright — the handshake would still authenticate — and it costs 200
  reads where the handshake costs one, measured over one kept-alive
  connection.
- **A revocation list, a CRL, or an OCSP responder.** What ADR-0014
  refused as new infrastructure, and its reasoning is untouched: there
  is nothing to publish, nothing to distribute, and nothing to keep
  fresh. The record the organization already keeps answers the
  question.
- **Cascade the delete to the definitions naming the operator.** One
  call, nothing left to do by hand. It destroys what the person did not
  ask to destroy, which is the shape ADR-0065 exists to refuse, and a
  definition is the organization's statement about an agent rather than
  the operator's property.
- **A deletion preview, as ADR-0065 gives an organization.** The
  precedent is for a cascade: the preview exists because the act
  destroys rows nobody named. This act destroys the record, its token
  and its certificate row, and is refused outright where anything else
  points at it — so the refusal is already the statement, and a preview
  would enumerate a set that is always the same three things.
- **Make the check reach every machine identity.** Symmetric, one rule,
  no branch in the adapter. It refuses every agent, because garam holds
  no registration for one, and building one so the check could pass is
  a decision about agent identity taken to keep a branch out of an
  adapter.
- **Delete the record and leave the certificate row.** The smaller
  statement. It leaves the identifier reading `enrolled` on its next
  registration, so ADR-0067 would refuse that operator the token it
  needs, and it manufactures exactly the orphan ADR-0045's errata
  found.
- **Authorize the delete on `operator:register`.** ADR-0067 argued a
  re-mint grants nothing new because it is the delete-and-register a
  member may already perform. The same argument does not carry here:
  what it names is the act being added, and reading a permission off
  the thing it is about to permit is circular.
