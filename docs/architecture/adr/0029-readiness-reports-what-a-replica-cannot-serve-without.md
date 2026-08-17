# ADR 0029: Readiness reports what a replica cannot serve without, not the policy listener

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- Consequences
- Rejected alternatives

## Context

ADR-0021 rejected "readiness that reports the policy-change listener"
and named the precondition that would reopen the question: the listener
"never restarts, so one database blip would drain every replica
permanently. Restarting the listener is what has to land first."

It restarts now. `ListenPolicyChanges` in `internal/authz` reopens the
connection instead of returning, backs off between attempts under a
ceiling, drops every cached org as the reopened connection registers
its `LISTEN`, and returns to the shortest delay once one is
established. A stop is transient and self-healing where it used to be
permanent, so the reason for the deferral is gone and the question is
answerable. Nothing else would raise it: a decision deferred behind a
blocker that has since been removed stays invisible until somebody
opens it.

Two facts decide it, both in this repository.

- **A readiness answer is per process and covers every route on it.**
  `internal/health` fails the whole answer as soon as one dependency
  fails, and a scheduler acts on that answer by withdrawing the pod
  from the service's endpoints. There is no partial readiness and no
  per-route one.
- **The listener runs in one process and the other two depend on it
  through the engine.** `garam serve authz` starts it and reports the
  database alone; `serve api` and `serve machine` report the database
  and the channel to the engine (`internal/cli/serve.go`,
  `internal/cli/machine.go`).

One fact from outside sets the scale of the cost rather than the
direction of the decision: the repository that deploys garam reports
node-local storage for Postgres, so an instance cannot reschedule and
is unreachable for the length of a node drain rather than the length of
a promotion. That claim belongs to a repository this one does not
control and cannot be checked from here.

## Decision

- **Readiness does not report the policy-change listener**, on any
  process. This settles the question ADR-0021 deferred rather than
  deferring it a second time: the precondition it named has been met,
  and the answer is still no, for reasons that do not rest on that
  precondition.
- **ADR-0021 stands and is not superseded.** Its decision is unchanged.
  What changes is that one of its rejected alternatives is now rejected
  on a durable reason rather than a temporary one.
- **Readiness reports a dependency the replica cannot serve without.**
  That is the line, and it is what puts the database inside readiness
  and the listener outside it:
  - Database unreachable — nearly every request needs a row, so the
    replica genuinely cannot serve and draining it is honest. ADR-0021
    already accepts that consequence.
  - Listener reconnecting — the cache is stale only for an org whose
    policies were written during the gap, and the staleness ends
    without anyone intervening. Draining the replica refuses every
    decision it would still have made correctly, and takes the
    processes that report the channel to it out with it.
- **A staleness that ever has to be handled is handled at the
  decision, not at the probe.** A policy check that cannot be trusted
  fails closed, refusing exactly the requests whose correctness is in
  doubt; readiness is a pod-level instrument aimed at a decision-level
  problem. Nothing is built for this and nothing needs to be — the
  cache is dropped whole as the connection reopens, and the backoff
  returns to its minimum once one is established. Recording the shape
  is what makes this settled rather than deferred again.

## Consequences

- A replica whose listener is reconnecting stays in rotation and
  answers from statements no notification is invalidating. A decision
  can therefore be stale, for as long as the reopen takes.
- That window is bounded by the dependency readiness already reports.
  While the database is unreachable the engine fails readiness for the
  database's own sake and leaves rotation anyway; while it is
  reachable, reopening is an acquisition against a database that is
  answering. The uncovered case is the narrow one between them.
- Nothing tells a deployment that a replica's cache is not being
  invalidated. The listener writes a log line as it reconnects
  (`internal/authz`), and that is the whole of the signal. Whether it
  also becomes a metric is not decided here.
- The contract ADR-0020 obliges garam to publish states that a replica
  whose policy listener is reconnecting is not expected to leave
  rotation, so a repository writing probe configuration against that
  contract does not add a check of its own.
- The engine's readiness answer stays one dependency, and it is the one
  it cannot serve without: a cache miss reads through to the database.
- The next dependency proposed for readiness is answered by asking
  whether a replica can still serve without it, rather than by arguing
  from this list.

## Rejected alternatives

- **Readiness that reports the policy-change listener** — the
  alternative ADR-0021 deferred, now refused outright. A readiness gate
  on a shared dependency is a kill switch rather than a degradation:
  every engine replica reaches the same database, so a probe gated on
  listener state fails on all of them at the same instant and for the
  same reason. The endpoint list goes to zero and a request fails at
  the network layer, before reaching the code that would have decided
  it. It does not stop at the engine either — `serve api` and `serve
  machine` already report the channel to it, so an engine with no
  endpoints drains them behind it. The trade is not stale decisions
  against correct ones; it is stale decisions against no decisions,
  arriving as a total outage caused by routine maintenance.
- **The same gate behind a long grace period.** It looks like the
  cautious version and is the same outage later: the fault is
  simultaneous across replicas, so the grace expires on all of them at
  the same instant too, and delaying a fleet-wide withdrawal is not
  preventing one. A grace long enough to outlast the drain of the node
  holding the database also reports nothing for the whole window it was
  sized to cover.
- **Building the request-level fail-closed check now.** The engine
  keeps no listener state a check could read, so this means adding
  state to serve a control nothing has yet needed, while the reconnect
  already drops the cache it would protect against. The shape is
  recorded above so that the question is closed rather than left for
  the next reader to reopen; building it waits for a failure that
  justifies it.
