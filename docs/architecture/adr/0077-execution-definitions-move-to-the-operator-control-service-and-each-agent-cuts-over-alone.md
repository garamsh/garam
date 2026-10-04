# ADR 0077: Execution definitions move to the Operator control service, and each agent cuts over alone

> Status: accepted
> Date: 2026-10-04

Decided by the PM under the owner's explicit delegation of 2026-10-04,
recorded in #1155, whose review holds the full design. Facts about
other repositories hold at Garam Agent Operator `a4033bc` and Sherlock
`07aa5c4`.

## Context

Today garam holds what an agent should be. A definition carries the
operator and the values (ADR-0026), the operator it names polls and
claims it (ADR-0033), and templates are garam's resource, copied once
into a definition (ADR-0075). The operator applies a definition when it
first constructs an agent and not afterwards: its poller returns early
once the agent's credential is placed
([`internal/garam/poller.go`](https://github.com/garamsh/garam-agent-operator/blob/a4033bc/internal/garam/poller.go#L136)).

The product goal asks for services with one owner per domain that
upgrade independently, a cluster controller that only dials out, and a
standalone host that runs an agent without a cluster. Garam owning
runtime values means garam owns execution delivery, retry and schema it
cannot validate, and every change to what Sherlock accepts becomes a
garam change. Existing agents carry GRNs, workload names and volumes
whose continuity is the user's memory, so whatever moves must move
without a new identity or a fresh volume.

## Decision

**Execution belongs to a separately deployed Operator control service
with its own store, and garam keeps identity and collaboration.**

| Owner | Authoritative for |
|---|---|
| garam | organization, membership, teams, policies, an agent's stable GRN and name, delegation, assignment and activation (ADR-0078), collaboration messages |
| Operator control service | execution definitions and templates, desired revisions, allowed deployment profiles, the GRN-to-host and storage binding, reconciliation status |
| Standalone host | its local desired configuration and storage binding |
| Sherlock | applied revision and generation, and the agent's memory, which it alone writes |

- **garam holds no desired runtime value, CRD schema or learned memory
  in the target.** A secret stays a reference (ADR-0036).
- **The control service is the only writable managed desired source.**
  Cluster custom resources are projections of it. The cluster
  controller pulls from it over outbound mTLS, under a credential
  distinct from the hosted service's own, so garam and the hosted
  service still dial into no cluster (ADR-0012).
- **The console keeps Teams, People and an agent's identity pages.**
  Its execution forms call the Operator API through a thin integration;
  the Operator owns those fields' schema, validation and applied-state
  detail, and the console persists none of them and imports no CRD
  type. Copying a template once and naming an agent (ADR-0075) carry
  over unchanged; only the template's owner moves.
- **Managed creation asks garam for the identity.** The control service
  calls garam under an active delegation (ADR-0078) with a stable
  request identifier bound to the organization, actor and payload.
  garam registers the agent and its first assignment in one
  transaction; a retry answers the same GRN and a changed payload under
  the same identifier conflicts.
- **Existing agents cut over one GRN at a time, with one active
  source.** Inventory every definition, value, template, GRN,
  assignment, credential and storage binding, keeping unknown values
  for an explicit disposition. Import them inactive and keyed by the
  existing GRN, freeze the old writer, verify the import is still
  current, then switch the controller's source for that GRN. A stopped
  attempt resumes from its recorded stage. No dual write, and no new
  StatefulSet or volume.
- **An ownership-only switch keeps the certificates.** A runtime that
  actually restarts activates a new generation (ADR-0078).
- **Rollback before new writes restores the former source** under the
  same single-writer gate. After new writes or a storage move it is an
  explicit reverse migration, never a replay of a stale snapshot.
- **Garam's obsolete values are retired only after verification**, as
  a separately approved destructive action.

## What this does not decide

- The control service's layout, image and convention changes. They are
  Garam Agent Operator's, under its own PM (#1155 D1).
- The control API's schema. It is authoritative in the artifact that
  defines it once built; no responsibility document here restates it.
- Any migration number. None is allocated by this decision.

## Consequences

- **None of this is built.** garam still serves definitions, values
  and templates, and the operator still claims them, until the cutover
  above runs for a GRN.
- ADR-0026's "garam holds what an agent should be" is superseded for
  the runtime values; garam still holds the identity and name.
- ADR-0033's machine read stops being the controller's source for a
  GRN once that GRN cuts over, and creation replaces the claim.
- ADR-0075's template ownership moves; its copy-once rule, its names
  and the console's separation of requested from reported stand.
- A garam outage stops collaboration and new privileged changes, not a
  running runtime; an Operator outage stops lifecycle changes, not the
  running runtime.

## Rejected alternatives

- **A garam relay for execution.** It reuses infrastructure and makes
  garam own delivery and retry state for a domain it does not
  validate.
- **A reverse tunnel, or a public endpoint at the customer.** The first
  couples every request to cluster reachability; the second breaks the
  outbound-only constraint.
- **Custom resources as the managed authority.** It needs a console in
  the cluster, or a command channel with receipts.
- **Two writable stores, or dual write during cutover.** Two
  authorities need conflict resolution that nothing here would own.
- **Re-registering agents, or starting them on fresh volumes.** Either
  makes identity and memory continuity false.
- **A new repository for the control service now.** It adds
  coordination before a second owner exists.
