# ADR 0027: A watch crosses between processes through Postgres

> Status: accepted
> Date: 2026-08-17

## Contents
- Decision
- Context
- Consequences
- Rejected alternatives

## Decision

- **The two ends of a watch are in different processes and Postgres is
  the crossing.** A browser opens its stream on `garam serve api`; an
  adapter forwards turn events to `garam serve machine`. Neither can
  reach the other's memory, and neither may call the other.
- **Events cross by announcement, not by storage.** `LISTEN`/`NOTIFY`
  carries a forwarded event to whichever process holds a stream for that
  agent. Nothing is written down, which is what keeps ADR-0022's "best
  effort, never a record" true at the crossing rather than only at the
  ends.
- **The watcher count crosses as a leased row per open watch.** The
  machine listener answers the adapter's poll from that table. A lease
  rather than a plain row, because the count has to fall when the
  process holding a stream stops without saying so — and an unwatched
  agent that keeps being forwarded to is exactly the cost ADR-0022
  exists to avoid.
- **A row per watch, never per agent.** An agent nobody watches has no
  row, no entry and no announcement reaching anyone.

## Context

ADR-0022 decided that the adapter forwards only while garam says
someone is watching, and that garam says so on the poll the adapter
already holds. It did not say how garam knows — and it could not have,
because it treated garam as one thing.

garam is three processes from one image (ADR-0017), and the two ends of
a watch land on different ones. `serve api` holds the browser's stream;
`serve machine` is where the adapter polls and forwards. ADR-0003 and
ADR-0017 make those independent: they never share a process, and
`internal/api` and `internal/machineapi` may not import each other.

So a mechanism had to be chosen, and the choice is not implied by
anything already decided. It is the kind of gap that gets filled by
whatever the first implementation reached for, which is why it is
written down here rather than left in the implementation that made it.

Postgres is already a dependency of both processes, and the readiness
each reports already fails when it is absent (ADR-0021).

## Consequences

- **A watch now depends on Postgres being reachable from both
  processes**, which was already true of everything else either of them
  does. It adds no dependency; it does widen what a database outage
  takes down to include live views, which are the least costly thing to
  lose.
- **The lease has to be renewed, and something has to expire it.** That
  is machinery this decision creates: a watch whose holder vanishes must
  stop being counted without anyone reporting it, because nobody is left
  to report.
- **An event announced while the announcing process is the only one
  connected reaches nobody**, and that is correct rather than a gap.
  ADR-0022 makes the stream best effort; a crossing that queued would be
  a record, and a record is what that ADR refuses.
- **The count an adapter reads can be briefly stale** — a lease expiring
  between two polls. The cost is bounded and it errs toward forwarding
  slightly longer than needed, which is the safe direction: the opposite
  error drops events a watcher is waiting for.

## Rejected alternatives

- **An in-memory fan-out.** The obvious shape and it cannot work: the
  browser's stream and the adapter's poll are in different processes by
  ADR-0017, so an in-memory registry is correct only in a deployment
  that runs one of each on one host and silently wrong everywhere else.
  Wrong in a way that passes every local test.
- **One process calling the other.** It makes each depend on the other's
  availability and reintroduces exactly the coupling ADR-0003 drew the
  boundary to prevent. It also needs a service address for a call that
  exists only while somebody is looking at a page.
- **A broker — Redis, NATS, anything queue-shaped.** It fits the problem
  and it is new infrastructure a consumer must run, which ADR-0012's
  bring-your-own-operator position and ADR-0020's published contract
  both weigh against. Postgres is already required.
- **Storing events and having the browser poll them.** Durable, simple,
  and it converts a best-effort feed into a record — which ADR-0022
  refuses on the grounds that a live feed which looks complete is read
  as complete, and the platform upstream drops events without marking
  the gap.
- **Counting watchers in the same announcement channel.** One mechanism
  instead of two, and it makes the count a stream of deltas that has to
  be replayed from somewhere after a process restarts. A row that can be
  read is the thing a poll wants; a delta log is not.
