# ADR 0124: A control service names its manager on its own record, set when it is registered

> Status: accepted
> Date: 2026-10-20

Drafted for #1636, for which the Garam PM reserved this number. The
PM decided its shape on #1636 on 2026-10-20, from the worker's
measurement. garam is read at
`766d1e5931afce1ac81b9c5d4cf0b86f18597ab1`; a claim below that names
no revision holds for it.

## Context

- **An Operator is two identities.** A hosted Operator runs a manager
  and a control service, and each is registered as an operator of its
  own and issued its own key
  ([ADR-0102](./0102-an-operator-proves-its-identity-by-private-key-jwt-and-agents-share-enrollment.md)).
  The Operators page says "Each connects two identities: its manager
  and its control service."
- **No record pairs them.** An `operators` row carries its GRN, its
  details and its control endpoint, and nothing naming another row.
  On aurora (v0.5.2, 2026-10-20) `aurora` and `aurora-control` are
  listed as two unrelated Operators, and the owner asked why the two
  coexist.
- **The delegation is not a pair.** A grant names its controllers
  (ADR-0078), so a control service's grant naming `aurora` looks like
  one. It is authority, not identity: it exists only once a member
  grants it, lapses within 30 days, may name up to 32 controllers, and
  `getOperatorDelegation` reads it one operator at a time. A list
  joined over grants would pair what a grant happens to name today.
- **Names are not identity.** A name is what the organization calls an
  operator and two may share one
  ([ADR-0074](./0074-an-operator-carries-a-name-and-description-and-the-console-gives-it-a-page-of-its-own.md)),
  so `aurora-control` beside `aurora` pairs nothing.

## Decision

**A control service's operator record names its manager, by GRN. The
name is set when the control service is registered, at most one
control service names each manager, and `listOperators` answers it.**

1. **The record is the control service's.** `operators.manager_grn`
   (`migrations/000062`) is nullable and names an operator of the same
   organization other than the row itself. A manager names none, and
   neither does an identity registered alone. `Operator.manager` on the
   wire is that GRN.
2. **It is set at registration and by nothing after.**
   `registerOperator` takes `manager` and records the identity as that
   manager's control service, so the pair is recorded from the start.
   The store refuses, in the transaction that writes, a manager the
   organization holds no record of, a manager that is itself a control
   service, a manager another control service already names, and a
   registration with a manager of an identity a control service already
   names as its own manager. Each is a 409; a value that is not an
   operator of this organization, or is the registration itself, is a
   400. An Operator is therefore one manager and at most one control
   service, never a chain.
3. **It names the identity, not a live record.** The column carries no
   foreign key. An identity is configured before it is registered
   (ADR-0045), so a control service still names the manager it was
   configured for after that manager's record is deleted, and deleting
   a manager is not refused for having one; `deleteOperator` answers
   no 409 and none is added. The organization's deletion takes both.
4. **The list pairs the two in one read.** The console shows one row
   per Operator: each record naming no manager, joined by the record
   that names it, and a control service whose manager garam no longer
   holds as an Operator of its own whose manager is not registered. It
   pairs by `manager` alone, never by names or grants.
5. **The delegation stays as it is.** Who may act for whom is the
   grant's; this record says only which identities make up one
   Operator.

## Consequences

- The Operators list answers "which two identities are one Operator"
  from garam's records in one call, and the connect dialog registers a
  control service as part of an existing Operator.
- A pair registered before this cannot be linked in place: the field
  is written only at registration, and no update route takes it.
  aurora's `aurora-control` is linked by deleting and registering it
  again with `manager`, which withdraws its key and so needs one
  issued and placed again. An update route would be a decision of its
  own.
- A record naming a deleted manager keeps naming it. Registering the
  manager's identifier again rejoins the pair, which is the identity
  the control service was configured for.
- `internal/operator` gains no read: `ListByOrg` already answers every
  record, and the pairing is the console's join over it.

## Signs the decision was wrong

- An Operator needs a second control service, or a control service
  serves two managers: the uniqueness and the no-chain rule are what
  would move.
- A pair needs changing after registration often enough that deleting
  and registering again, with its key re-issued, is the usual way to
  do it: an update route is then owed.
- A deleted manager's control service naming it misleads more than it
  tells, for instance when the identifier is registered again for an
  unrelated deployment: a foreign key that clears the name would then
  fit better.
