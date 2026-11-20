# ADR 0178: Every workload runs two replicas spread across Nodes, the database stays the coordinator, and an upgrade rolls

> Status: proposed, not built (#2015); builds goal #2012 and
> supersedes the "replica safety above one is not established"
> position of
> [deployment-contract.md](../deployment-contract.md) §Not established
> Date: 2026-11-21

Drafted for #2015 under goal #2012, "with one Node lost, garam keeps
answering". garam is read at
`0b8a5eb189e5b87309fafbc5311792f5d15fe4ac`; a claim below that names no
revision holds for it. Nothing here is built: the chart still renders
`replicas: 1` and `Recreate`, and the client and pool changes are
proposed implementation issues listed in the pull request.

## Contents

- Context
- Decision
- Why a second replica is safe
- The policy-service client after a lost Node
- The lock a lost Node leaves behind
- The per-replica rate limiters
- What "keeps answering" measures
- Alternatives rejected
- Consequences
- Signs the decision was wrong

## Context

Every Deployment of the chart is one pod on a `Recreate` strategy:
`serve api` (`deploy/charts/garam/templates/api.yaml:12,19`), `serve
machine` (`machine.yaml:14,21`), the policy service (`policy.yaml:13,20`),
the console (`web.yaml:13,20`) and the site (`site.yaml:14,21`), each
with a comment that replica safety above one is not established. No
PodDisruptionBudget, spread constraint or affinity exists in the
chart. Losing the Node that carries a pod is therefore an outage of
that workload until the pod is rescheduled, and every upgrade is one.

[deployment-contract.md](../deployment-contract.md) §Not established
says why the chart stops at one: no leader election exists, "whether
that makes a second replica safe or unsafe is not answered". This ADR
answers it from the source, for each place a second replica could
diverge from the first:

- **The policy feed.** One pass holds `FOR UPDATE SKIP LOCKED` on
  `policy_feed`
  (`platform/internal/policyfeed/postgres/deliver.go:35-37`, taken at
  `:91` inside the transaction begun at `:70`), takes the next
  sequence by `nextval` inside that lock (`:47`, `:368-372`), and
  delivers up to three outbound calls under it: `send` (`:337-347`),
  `SendLinks` (`:125`) and `SendScopedLinks` (`:134`), each bounded by
  `CallTimeout`, 30 s (`policyfeed/grpc/grpc.go:26`). The policy service
  applies a change only if `entities.sequence < EXCLUDED.sequence`
  (`policy/crates/entity/src/postgres.rs:149-155`), so a replayed or
  reordered delivery changes nothing.
- **Message claim.** `ClaimMessage` selects with `SKIP LOCKED`
  (`platform/internal/message/postgres/queries.sql:86`) in a transaction
  that also signs (`message/postgres/postgres.go:250-290`); Transit
  calls are bounded at 5 s (`message/transit/transit.go:34-37`).
- **Signing-key rotation.** It takes an advisory lock
  (`queries.sql:239`).
- **The policy service's denial record.** The cap check takes an
  advisory lock and `FOR SHARE`
  (`policy/crates/audit/src/denial.rs:259-300`); the hourly sweep
  is idempotent (`policy/crates/server/src/lib.rs:161,174-190`).
- **Per-process state.** The rate limiters
  (`platform/internal/operator/limiter.go:36-39` states "its bound is
  per replica"), the policy service's in-process denial queue
  (`policy/crates/audit/src/queue.rs:13-22`) and the watch lease, 45 s
  (`platform/internal/watch/service.go:17-21`).

The policy service has no cache: "Every read goes to storage"
(`policy/crates/server/src/policy_service.rs:19-20`), so a second
replica of it reads what the first wrote.

What the source does not give is any bound on what a lost Node leaves
behind. Nothing sets `idle_in_transaction_session_timeout`,
`lock_timeout`, `statement_timeout` or a TCP keepalive: the pool is
`pgxpool.New(ctx, url)` with no settings
(`platform/internal/cli/connect.go:14-24`), and the one gRPC `Dial`
(`platform/internal/policyfeed/grpc/grpc.go:125-130`) sets TLS, a
per-call bearer and nothing else: no keepalive, no service config.

## Decision

Every workload runs two replicas, spread over Nodes, behind a
PodDisruptionBudget, and a Deployment rolls rather than recreates. The
database stays the only coordinator: no leader election is added. The
policy client detects a lost Node by keepalive and balances over the
policy pods; the pool bounds the lock a lost client leaves behind.

| Workload | Replicas | Why it survives losing one Node |
| --- | --- | --- |
| `serve api` | 2 | The survivor carries what the one pod carries today. Every loop and state is serialized in the database. |
| `serve machine` | 2 | Same. Its feed dependence is on one ready `serve api`, which two spread replicas give through the loss. |
| policy service | 2 | It holds no cache and writes through the database with a sequence guard, so both replicas answer alike. |
| console (web) | 2 | Stateless apart from Server Actions, whose skew is bounded to a rollout. |
| site | 2 | Static content. |

Items, each an assertion this ADR makes:

1. **Two, not three.** Three buys survival of two simultaneous
   disruptions, which the goal excludes. Two keeps the survivor's load
   equal to today's single pod, so no capacity claim is added. A
   deployment needs at least two schedulable Nodes to meet it; the
   replica count is a chart value per workload, so a one-Node cluster
   sets one.
2. **The database is the coordinator.** Every loop that can run in two
   replicas is already serialized in it (the section on why a second
   replica is safe). The rule for a loop added later is that it joins
   that coordination (a row lock, an advisory lock or a guard on a
   stored sequence) and is proved by a two-replica test modelled on
   `platform/tests/integration/policy_delivery_test.go:550`.
3. **PodDisruptionBudget `maxUnavailable: 1` on each workload.** Not
   `minAvailable: 1`: with one replica set by a one-Node deployment,
   `minAvailable: 1` would block every drain forever. A budget covers a
   voluntary drain only; it does not cover a lost Node, which the spread
   and the replica count cover.
4. **Spreading by `topologySpreadConstraints`**, `maxSkew: 1`,
   `topologyKey: kubernetes.io/hostname`, `whenUnsatisfiable:
   DoNotSchedule`, selecting the workload's component label. On one
   Node there is one domain and both pods schedule. On two Nodes with
   one lost, the replacement stays Pending and the survivor serves,
   which is the goal. `matchLabelKeys` is not used: the chart does not
   pin a Kubernetes version.
5. **`RollingUpdate`, `maxUnavailable: 0`, `maxSurge: 1`** on every
   Deployment, replacing `Recreate`. `Recreate` stated one reason, that
   replica safety was unestablished; at two replicas it would make every
   upgrade a full outage, which defeats the replica. The price is a
   version-skew rule below.
6. **Readiness is unchanged.** `serve api` and `serve machine` report
   the database alone (`cli/serve.go:150`, `cli/machine.go:149`), so a
   policy outage withdraws neither. The principle of
   [ADR-0029](./0029-readiness-reports-what-a-replica-cannot-serve-without.md)
   stands.
7. **Pool size stays at the pgx default**, and the database's
   `max_connections` for two replicas of each workload is the
   deployment's obligation. It is not measured here.

### Version skew during a rollout

At two replicas with `maxSurge: 1`, two releases run at once. What the
replicas share, so that the previous release must be able to read what
the next writes for one release (expand, then contract):

- the outbox mark kinds and the feed's sequence rows,
- the claim SQL and the lock keys of the claim and the rotation,
- the signing-key rows,
- the policy protobuf.

No test detects a break of this rule; review holds it, and the pull
request raises that as a convention concern. For the console and the
site the skew is Server Actions (about ten `use server` files under
`platform-web/src/features/orgs/api/`) and `_next/static` chunks, on
Next 16.3.1 (`platform-web/package.json:24`,
`site-web/package.json:20`). It is accepted and bounded to the rollout
window, and is unverified: the goal's upgrade exercise measures it. The
fallback, if it shows failures, is `Recreate` for the console alone.

Migrations stay Helm `pre-install,pre-upgrade` hooks
(`migrate.yaml:10`, `policy-migrate.yaml:11`), so a schema change lands
before the new pods and the previous release runs on it: the same
expand-then-contract rule.

## Why a second replica is safe

Measured, not assumed, at the revision above:

| Shared thing | Why two replicas do not diverge |
| --- | --- |
| Feed pass | Row lock with `SKIP LOCKED`: a second pass skips a locked row. The sequence is taken inside the lock, so order follows the sequence. The policy service drops a change at or below the stored sequence. |
| Skipped pass | A skipped pass counts as a pass (`policyfeed/service.go:18-24`), so no replica waits on another. |
| Read your own write | `serve machine` runs no loop and uses an observing gate (`cli/machine.go:134`); the `serve api` loop empties the outbox (`cli/machine.go:208-212`, ADR-0165 item 6). The bound is `Bound` 5 s with a 50 ms poll (`policyfeed/gate.go:14,20`). |
| Claim | `SKIP LOCKED`: two claimers never take one row. |
| Rotation | Advisory lock. |
| Policy denial cap | Advisory lock plus `FOR SHARE`. |
| Policy write | Sequence guard on the entity row. |
| Policy decision | No cache, so no invalidation between replicas. |

What a replica keeps in process is the limiters (next sections), the
denial queue (a denial queued in a lost replica is lost, and counted in
`lost`, `queue.rs:17,154-155`) and the watch lease. A Watcher count
over-reports for up to 45 s after a replica is lost. Per-replica
metrics are per replica, so a deployment sums them. All are accepted as
unchanged.

The API listener turns keep-alives off (`cli/serve.go:168`), so every
request opens a connection and the web to api hop is not pinned to one
replica. The shutdown grace is 10 s (`cli/shutdown.go:16`) against a
claim long-poll of 30 s (`message/service.go:34-39`); the chart sets no
`terminationGracePeriodSeconds`, so Kubernetes' 30 s default applies.
A claim cut by a replica stopping holds no state and its client
retries; the mismatch is accepted.

A policy decision cache stays forbidden unless it brings a
cross-replica invalidation rule in the same decision, since the first
replica to be written is no longer the only one to read.

## The policy-service client after a lost Node

Every caller goes through the one `Dial`
(`policyfeed/grpc/grpc.go:125-130`): the decisions and relay share one
connection (`cli/serve.go:70`, `cli/policy_decisions.go:31-47`),
`serve machine` has one (`cli/machine.go:113`), and the feed has its own
(`cli/policy_delivery.go:88`). The change is inside `Dial`:

1. **Keepalive**: `Time` 10 s (grpc-go's minimum), `Timeout` 3 s,
   `PermitWithoutStream` true. A blackholed TCP connection otherwise
   waits for the operating system, and the feed's connection, idle
   between passes, would never notice. Detection takes about 13 s.
2. **Balancing**: `round_robin` through the default service config,
   with the policy Service made headless (`clusterIP: None`) under the
   same name, so the certificate's names and `GARAM_POLICY_ADDR` are
   unchanged (`Dial` uses `credentials.NewClientTLSFromCert(pool, "")`,
   so the certificate must name the dialed host; the local one names
   `policy`, `policy.garam-local.svc` and
   `policy.garam-local.svc.cluster.local`,
   `deploy/local/minikube/policy-key-material.sh:35`). The target is
   `dns:///`. Today the Service is `ClusterIP`
   (`policy.yaml:149`), which pins a connection to one pod behind
   kube-proxy.
   Cost: `clusterIP` is immutable, so an existing install recreates the
   Service on upgrade; whether cowork depends on the Service as a
   ClusterIP is unverified, since it lies outside the chart.
3. **Retry** on `UNAVAILABLE`, at most 2 attempts, for the read-only
   decision RPCs (`Evaluate`, `Evaluations`) only. The writes and the
   relay's mutating RPCs are attempted once; the feed's idempotence is
   by sequence, not by retry.
4. **Deadline stays 30 s** (`policydecision/grpc/grpc.go:62,111`,
   `policyrelay/grpc/grpc.go:41-170`, `policyfeed/grpc/links.go:199`).
   Keepalive, not the deadline, bounds Node-loss detection; the
   deadline bounds a live but hung service. A shorter one would break
   the feed's 1000-change call and the policy writes.
5. **Reconnect** is grpc-go's: it re-resolves on connection loss, not on
   a timer, which is unverified here. If the after-phase of the run
   shows one policy pod receiving no decisions after a replacement, the
   policy server gets `max_connection_age`, which tonic 0.14.9
   provides (`policy/Cargo.lock:3421-3422`); the policy server sets no
   keepalive today.

### What a test of it must show

With a TCP proxy that blackholes without closing, a manual resolver and
two backends, using shortened injected parameters:

- a call pinned to the blackholed backend fails or retries within about
  keepalive `Time` plus `Timeout` plus a margin;
- the next call goes to the live backend;
- a control with keepalive off hangs to the deadline, so the test fails
  with the change reverted;
- a non-idempotent write is attempted once, and the second backend
  sees no copy.

## The lock a lost Node leaves behind

A Postgres backend idle in a transaction, whose client Node died, holds
its row lock until Postgres notices the dead peer. The feed pass holds
`policy_feed` across up to three 30 s calls, so a lost `serve api` Node
blocks the feed for its replica's pass and the surviving replica's pass
skips (`SKIP LOCKED`), delivering nothing until the lock is released.
The same holds for a claimed message row when a `serve machine` Node is
lost mid-claim. That Postgres notices the peer's death in a bounded time
is unverified, and the source sets no bound.

Decision: set `idle_in_transaction_session_timeout` to 60 s, twice
`CallTimeout`, on every pool connection, as a pgx runtime parameter in
`openPool` (`platform/internal/cli/connect.go`). The timeout counts one
gap between statements, so it covers one gRPC call plus slack. It lives
in code because the database is the deployment's and the repository
cannot read its settings. The implementation checks every `Begin(` for
an outbound call inside the transaction and reports each.

## The per-replica rate limiters

Fixed windows in a Go map, per process
(`platform/internal/operator/limiter.go:36-39`):

| Bound | Per replica | At the balancer with N replicas |
| --- | --- | --- |
| Key issuance, per operator | 3 a minute | 3N |
| Key issuance, per process | 30 a minute | 30N |
| Exchange, per source | 60 a minute | 60N |
| Exchange, per client | 10 a minute | 10N |

Defined at `operator/assertion.go:70-84`, built at
`api/server.go:89-91` and `operator/service.go:73-75`
(also `identity/principals.go:63-73`). Decision: keep them per replica
and state the ceiling at the balancer as N times. The bounds protect a
per-process cost (RSA key generation on CPU), and the documents already
say "one process" ([machine-identity.md](../machine-identity.md),
[operators.md](../operators.md)). With one Node lost the ceiling returns
to 1 times. The implementation corrects the comment in `limiter.go` and
the two documents to say so.

## What "keeps answering" measures

The run is #2012's evidence.

| Phase | What happens | Pass |
| --- | --- | --- |
| Before | At least 60 s, at least 1 call a second per kind | 100% success |
| During | Hard-stop a Node (for example `qm stop`, no graceful FIN) carrying at least one pod of each workload, at T0 | See below |
| After | Wait for replicas Ready | Replicas back at 2 Ready and spread; both policy pods receiving decisions; outbox depth 0; a membership change applied within the 5 s `Bound` |

The kinds are sign-in (a fresh client each iteration, through the route
the console uses; replaying a cached session cookie does not count), an
API read that asks the policy service, and a policy decision
(`checkPermissions`, allow and deny), with a per-policy-pod counter
showing the in-cluster service decided.

During the phase:

- no wrong allow at any moment (fail closed);
- from T0 plus 60 s to the end of the phase every kind succeeds 100%.
  The 60 s rests on Kubernetes' documented default
  `node-monitor-grace-period` of 40 s, owned by the cluster outside
  this repository; naru's value is unverified, so the run reads and
  records the real endpoint-removal time and the longest
  consecutive-failure run per kind;
- within the first 60 s failures are recorded and bounded, and are 5xx
  or `unavailable` only.

A drain is a second, easier run that honors the budget. Machine-listener
behavior under Node loss is outside the pass and is listed as
unmeasured.

## Alternatives rejected

- **Three replicas.** Survives two simultaneous disruptions, which the
  goal excludes;.
- **Leader election for the feed.** The row lock already elects per
  pass; a second mechanism adds a lease to lose and no safety.
- **Running the feed loop in `serve machine`.** More connections and
  waiters for no gain while `serve api` has two replicas (ADR-0165
  item 6 stands). A cross-replica `NOTIFY` wake is rejected for the same
  reason; the worst read-your-write latency is about 2 s plus one call
  inside the 5 s bound ([ADR-0146](./0146-the-platform-decides-through-the-policy-service-which-stores-an-organizations-policies-and-holds-its-role-links.md)
  §Reading one's own write).
- **`minAvailable: 1`.** Blocks every drain on a one-replica
  deployment.
- **`Recreate` kept.** An outage at every upgrade.
- **A shorter gRPC deadline as the detector.** Breaks the long calls
  and still leaves the feed's idle connection undetected.
- **The Service's cluster IP kept, with keepalive alone.** A
  connection to a dead pod is replaced, but both replicas then
  funnel through one pod's connection until it fails.
- **Relying on the database owner's idle-in-transaction setting.** The
  repository cannot read it, so the guarantee would be unverifiable.
- **`SET LOCAL` per pass only.** Misses the claim transaction.
- **Releasing the feed lock before the outbound call.** Breaks the
  ordering by sequence (`deliver.go:47,343-372`).
- **A database-backed limiter.** A write per request on the hot path
  and a new dependency, with no benefit for a per-process CPU bound.

## Consequences

- Losing one Node leaves every workload answering; every upgrade rolls
  without an outage, subject to the skew rule.
- The chart gains values for replica counts, budgets and spread, and
  a headless policy Service. A template edit that changes the render
  moves `Chart.yaml`'s version
  ([ADR-0057](./0057-a-template-edit-moves-the-chart-version-and-a-reviewer-is-what-checks-it.md)).
- The rate bounds at the balancer become N times and the documents say
  so.
- Two policy pods mean the policy service's per-process counters
  (`lost`, queue depth) are per pod and a deployment sums them.
- A deployment needs at least two schedulable Nodes to meet the goal,
  and a database sized for two of each workload.
- Version skew is held by review alone.
- Rules out: a policy cache without invalidation; a feed loop that does
  not join the database coordination.

## Signs the decision was wrong

- The run's during-phase shows 5xx beyond T0 plus 60 s: the detection
  or the balancing is wrong, not the replica count.
- The after-phase shows one policy pod getting no decisions: the
  reconnect assumption failed and `max_connection_age` is owed.
- The feed's lock outlives 60 s after a hard stop: Postgres does not
  release it, and the timeout is not the bound it was taken for.
- A rollout produces console failures from Server Actions skew:
  `Recreate` for the console.
- A migration breaks the previous release: the expand-then-contract
  rule needs a check, not review.
- A cluster with fewer than two Nodes is the common case: the replica
  value defaulting to 2 is wrong.
- The summed limiter ceiling is hit as abuse: a database-backed bound is
  owed.
