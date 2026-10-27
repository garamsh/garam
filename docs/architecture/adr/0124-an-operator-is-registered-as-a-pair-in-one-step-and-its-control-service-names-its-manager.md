# ADR 0124: An Operator is registered as a pair in one step, and its control service names its manager

> Status: accepted
> Date: 2026-10-20

Drafted for #1636, for which the Garam PM reserved this number. The
PM decided its shape on #1636 on 2026-10-20, from the worker's
measurement, and widened it the same day in three steps. After the
owner asked why connecting one Operator takes two registrations, the
pair is registered in one step. After the owner pointed out that the
naming is wrong, the terms below were set. And once aurora's control
service was live, a pair registered before this ADR is linked in place
rather than registered again. garam is read at
`2ba56a6528355506c9c0711b796118a73ff9159b`; a claim below that names
no revision holds for it.

## Terms

This ADR, the console and the documents use three words, set by the PM
on #1636:

- **Operator**: the installed product, one row on the Operators page.
- **Cluster manager**: the identity of the Kubernetes controller in
  the customer's cluster.
- **Control service**: the identity of the server that holds agents'
  desired definitions.

The API keeps its names, which garam-agent-operator and aurora share.
Renaming them is a separate decision, taken with the operator PM if it
is ever taken.

| API name | Console term |
|---|---|
| `Operator` schema, `operators` table, GRN service type `operator` | a cluster manager or a control service, whichever the record is; never the Operator |
| `Operator.manager`, `OperatorRegistration.manager`, `operators.manager_grn` | the cluster manager a control service belongs to |
| `OperatorRegistration.controlService` | the control service registered with its cluster manager |
| `Delegation.controllers` | cluster managers |
| `registerOperator` with `controlService` | connecting an Operator |
| `setOperatorManager` | linking a control service to its cluster manager |

## Context

- **An Operator is two identities.** A hosted Operator runs a cluster
  manager and a control service, and each authenticates as a principal
  of its own
  ([ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
  ADR-0102 keeps them apart for three reasons, which the PM restated on
  #1636. They are two processes. The cluster manager authenticates to
  the control service with a token minted with that control service as
  its audience. And each holds its own key, so a leak of one does not
  grant the other.
- **None of that needs two registrations.** On aurora (v0.5.2,
  2026-10-20), `aurora` and `aurora-control` were registered one at a
  time through "Connect an operator", and were listed as two unrelated
  Operators. The owner asked why the two coexist, and then why one
  Operator takes two registrations.
- **No record pairs them.** An `operators` row carries its GRN, its
  details and its control endpoint, and nothing naming another row.
- **The delegation is not a pair.** A grant names its `controllers`
  (ADR-0078), and a control service's grant naming `aurora` looks like
  one. It is authority, not identity. It exists only once a member
  grants it, lapses within 30 days, may name up to 32 cluster managers,
  and `getOperatorDelegation` reads it one record at a time. A list
  joined over grants would pair whatever a grant happens to name
  today.
- **Names are not identity.** A name is what the organization calls a
  record, and two records may share one
  ([ADR-0074](./0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)),
  so `aurora-control` beside `aurora` pairs nothing.

## Decision

**An Operator is registered as a pair in one step: its cluster manager
and its control service, each a record of its own. The control
service's record names its cluster manager by GRN, at most one control
service names each cluster manager, and `listOperators` answers it.**

1. **One registration records both.** `registerOperator` takes an
   optional `controlService`: an identifier, and optionally a name and
   a description. With it, the registration records two things in one
   transaction: the cluster manager under `identifier`, and the
   control service under `controlService`'s identifier, naming the
   cluster manager. Both are recorded, or neither is. The answer is
   the cluster manager's record. The control service is the record
   under the identifier the caller gave, so the console reads it from
   there. The console's new Operator always registers both, because
   an Operator it connects is the pair; a lone cluster manager stays
   registrable through the API and the CLI, which keep the field
   optional.
2. **The pairing is on the control service's record.**
   `operators.manager_grn` (`migrations/000062`) is nullable and
   unique. It names a record of the same organization other than the
   row itself. A cluster manager names none. On the wire it is
   `Operator.manager`.
3. **Each identity keeps its own key.** Registering issues none
   (ADR-0117). The key route is per record, so `issueOperatorKey` is
   called once for each. The setup that follows the registration
   issues both in one flow and hands over two files. The CLI issues
   each by its own `operators keys issue` call, writing each answer
   to its own file with `--out` (ADR-0122).
4. **A control service still joins an existing cluster manager.**
   `registerOperator` also takes `manager`: the GRN of a cluster
   manager already registered. It records the new identity as that
   cluster manager's control service. `manager` and `controlService`
   are not given together.
5. **A record naming no manager may have it set once.**
   `setOperatorManager` writes `manager` on a record whose `manager` is
   null, and only there. It is authorized on `operator:register`, as
   registering the pair is. After that, `manager` never moves: a second
   set is refused, whether it names the same cluster manager or another,
   and no route clears it. This links a pair registered before this ADR
   without registering it again, which would cost a new key, a Secret
   swap, and the control endpoint and delegation written again. The PM
   decided it on #1636 once aurora's `aurora-control` carried an
   endpoint, a live delegation, and exchanged tokens.
6. **Registering and linking refuse the same things.** Each of these
   is a 409, refused in the transaction that writes:
   - a cluster manager the organization holds no record of;
   - a cluster manager that is itself a control service;
   - a cluster manager another control service already names;
   - a record that some control service already names as its cluster
     manager;
   - for a link, a record that already names its cluster manager.

   A value that is not another record of this organization is a 400.
   An Operator is therefore one cluster manager and at most one
   control service, never a chain.
7. **It names the identity, not a live record.** The column carries no
   foreign key. An identity is configured before it is registered
   (ADR-0045), so a control service still names the cluster manager it
   was configured for after that cluster manager's record is deleted.
   Deleting a cluster manager is not refused for having one:
   `deleteOperator` answers no 409, and none is added. The
   organization's deletion takes both.
8. **The list pairs the two in one read.** The console shows one row
   per Operator. Each record that names no cluster manager is one,
   joined by the record that names it. A control service whose cluster
   manager garam no longer holds is an Operator of its own, whose
   cluster manager is not registered. The console pairs by `manager`
   alone, never by names or grants.
9. **The delegation stays as it is.** Who may act for whom is the
   grant's to say. This record says only which identities make up one
   Operator.

## Consequences

- Connecting an Operator is one registration and one setup, with two
  keys placed: one in each identity's Secret.
- The Operators list answers "which two identities are one Operator"
  from garam's records in one call.
- A pair registered before this is linked in place with
  `setOperatorManager`, keeping its keys, its control endpoint and its
  delegation. aurora's pair is linked that way after the release.
- Issuing the two keys is two writes. A setup interrupted between them
  leaves one identity keyed and the other not, and resumes by issuing
  the key that is missing.
- A record naming a deleted cluster manager keeps naming it.
  Registering that identifier again rejoins the pair, since it is the
  identity the control service was configured for. A pair registered
  under that identifier is refused, because its control service would
  be the second to name it.
- `internal/operator` gains two writes, `RegisterPair` and
  `SetManager`, and no read.
  `ListByOrg` already answers every record, and the pairing is the
  console's join over it.

## Signs the decision was wrong

- An Operator needs a second control service, or a control service
  serves two cluster managers. The uniqueness rule and the no-chain
  rule are what would move.
- A link set by mistake needs undoing often enough that deleting the
  control service and registering it again is the usual way to do it.
  A route that clears or moves `manager` is then owed.
- Setups are often left with one identity keyed and the other not. A
  key route that issues both in one write would then fit better.
- A deleted cluster manager's control service naming it misleads more
  than it tells, for instance when the identifier is registered again
  for an unrelated deployment. A foreign key that clears the name would
  then fit better.
