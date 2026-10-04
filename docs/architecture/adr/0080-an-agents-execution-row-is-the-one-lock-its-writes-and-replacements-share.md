# ADR 0080: An agent's execution row is the one lock its writes and replacements share, reached through a declared admission function

> Status: accepted
> Date: 2026-10-05

Decided by the PM in #1161 (the preflight decision of 2026-10-04 and
the boundary correction accepted on 2026-10-05), implementing
ADR-0078's fence.

## Context

ADR-0078 fences an agent's protected writes by the activation of its
current generation and by its credential lineage, per request and on
connections already open. The writes it fences belong to other domains:
claiming, renewing and completing a message, posting one and reading
its state are `internal/message`'s, and announcing a turn event is
`internal/watch`'s. Each of them writes in its own local transaction,
and no domain here reads another's tables, shares a transaction with
it, or takes another's concrete types (ADR-0054).

A check before the write is not a fence. A write admitted by a read
can commit after a replacement committed, so a superseded process
completes a claim as current. The ordering has to come from the
database, at the moment the write commits.

The first cutover of an agent has the same race with a row that does
not exist yet: a legacy write that saw no activation can commit after
the first activation was recorded, and a row that does not exist cannot
be locked.

## Decision

**Every agent has one execution row, and it is the lock.** Migration
000029 creates `agent_executions` with a row for every existing agent,
and every path that creates an agent writes its row in the same
transaction. The row carries the agent's mode, legacy or fenced, its
current activation's token hash and its credential lineage. The mode
is server-owned: the first activation moves it, and nothing a caller
sends selects it.

**Other domains reach it through one declared function.**
`agent_execution_admit` is owned by `internal/agent` and declared by
000029. A domain whose write is protected calls it as the first
statement of its own local transaction. It takes the row `FOR SHARE`
and holds it until that transaction ends, then answers `admitted` or
the reason it refused. No transaction is exported, put in a context, or
shared, and no foreign table is read directly.

**Each consumer declares what it takes, and the transport translates
into it.** `message.Fence` and `watch.Fence` are those domains' own
inputs: the agent, the serial and validity of the certificate its
connection verified, and the hash of the token it carries. The machine
transport builds each from the handshake identity and the
`Authorization` header. Each consumer reads the function's answer into
its own refusal and fails closed on an answer it does not know. The
function is the one place the rule is decided; what the consumers
repeat is the mapping of its answer to a kind.

**What replaces an execution takes the row `FOR UPDATE`.** These are an
activation, the first one included, a deactivation, a recovery and a
reassignment. So a write admitted first commits before the replacement,
and a write admitted after the replacement commits is refused, on any
connection. Every transaction that takes both the execution row and the
assignment takes the execution row first.

**Authority is read before the lock, never under it.** A lifecycle
request reads its grant, its user's operation authority and persisted
policy with no transaction open. Then one short agent transaction locks
the row, rechecks the assignment and every known expiry against
`clock_timestamp()`, and writes. A withdrawal that commits between the
two does not undo the act; the next request is refused. No path holds a
row lock while it waits for a second pool connection or an RPC.

**Credential standing is one predicate.** `agent_credential_current`
answers whether a certificate is inside its validity now and in the
current lineage. It is `STABLE`, so a single statement that also reads
the execution answers facts that were current together.

## What this does not decide

- The wire shape of activation, introspection, recovery and
  deactivation. `api/machine.yaml` is authoritative for it.
- Notification arrival time. A turn event admitted before a replacement
  commits before it, and may still be delivered after it.

## Consequences

- `internal/message` and `internal/watch` depend on a database function
  `internal/agent` owns, and on no Go package of it.
- A protected write holds a share lock on its agent's row for its
  transaction. A replacement waits for writes already admitted.
- A superseded generation's open claim lapses by its lease (ADR-0018).
- A new admission answer is added to the function and to each
  consumer's mapping in the same change; until a consumer maps it, that
  consumer refuses the write as an internal error.

## Rejected alternatives

- **A check before the write.** It orders nothing.
- **Holding the lock on a second connection.** It needs two connections
  per write and deadlocks under pool exhaustion, and a caller that skips
  it bypasses the fence.
- **Passing a transaction across the domain seam.** That is a shared
  transaction framework under another name.
- **Each domain reading `agent_executions` itself.** It spreads one rule
  across every query that needs it, with no surface naming it.
- **Treating an absent row as legacy.** It cannot order the first
  cutover.
- **The agent's own fence record in the consumers' signatures.** It is
  a concrete type crossing a sibling boundary (ADR-0054).
- **A shared fence package below the domains.** It is a new boundary
  for one record, owned by neither side.
- **Raising each refusal as its own SQLSTATE.** It removes the
  consumers' mapping but changes the function's answer and every test
  reading it, for less code and no change in what is decided.
