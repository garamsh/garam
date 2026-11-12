# gaon-operator

The operator that runs each managed agent as one MicroVM on a gaon
cluster
([ADR-0151](../../docs/architecture/adr/0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)).
How it sits among garam's operators is
[operators.md](../../docs/architecture/operators.md) §The gaon operator.

**Built: the reconciler (#1834), against interfaces and a test fake.
The binary cannot run against garam yet.** `cmd/gaon-operator` reads its
settings and refuses to start, naming what is not wired: garam's machine
client and the control service's feed client. Neither wire client
exists; they are the work after this one (§What is not built).

## Contents
- What is built
- What is not built
- What the tests prove
- Rationale
- Open questions

## What is built

`internal/agent` holds the reconciler. `Service.Reconcile` is one
level-triggered pass: it reads the whole feed, lists the objects it
labelled in gaon, and writes what differs. It keeps no state a restart
loses.

- **Objects.** An agent yields one `Secret`, two `Volume`s (memory and
  workspace) and one `MicroVM`, named `agent-` plus 16 hex digits of a
  SHA-256 of the agent's GRN (`-memory`, `-workspace` on the Volumes).
  Labels are `garam.sh/role=agent` and 32-hex digests of the
  organization and agent; the GRN, epoch and revision are annotations,
  because a label value cannot hold a GRN.
- **No credential in a spec.** The tokens and the two trust roots live
  in the `Secret`. The MicroVM's files take them with
  `valueFrom.secretKeyRef`, mode 0400, owner 65532; no file carries
  inline content.
- **Mint once.** A Secret that exists is never minted over. One that is
  missing is minted again, which is what makes a restart lose nothing.
- **Report.** `observe` maps a MicroVM to `provisioned`, `ready` or
  `failed` by the ADR's table, reading the phase through
  `EffectivePhase` so a status of another `bindingEpoch` counts as
  pending. `failed` needs a last exit. Nothing is reported where nothing
  was read, and each change is reported once. A stale epoch refused by
  garam is dropped and logged.
- **Halt, never delete.** A `stopped` agent, and one that left the feed,
  has its MicroVM's `runStrategy` set to `Halted`. Nothing is deleted:
  the Volumes are the agent's memory.
- **Settings.** `internal/config` reads addresses and file paths from
  the environment and names each missing variable, never a value.

Garam's side (`Garam`) and the control service's feed (`Feed`) are
interfaces `internal/agent` declares. gaon's side is `lib/api` at
`github.com/garamsh/gaon` commit
`4d950576aa75baab497f327e0b3bb5381488c740`.

## What is not built

- The wire client of garam's machine listener (exchange,
  `mintAgentToken`, `reportProvisioningState`), which waits on a
  generated client for `platform/api/machine.yaml`.
- The wire client of the control service's feed.
- Replacing a MicroVM of another revision, and the long poll on the
  feed.
- The image `garam/gaon-operator` and its deployment.

## What the tests prove

They run the reconciler against stand-ins for the feed and for garam and
against `tests/gaonfake`, an `httptest` server of gaon's REST paths
built on `lib/api`'s types and validation. The fake enforces
compare-and-swap on `resourceVersion`, the API's validation and
label-selector lists. It does not prove: RBAC selector rules, garbage
collection, the Node authorizer, a get refused for lack of labels,
`bindingEpoch` as gaon writes it, or watches.

## Rationale

The reconciler declares its two outside dependencies because the wire
clients are separate work with separate owners; the interface is the
seam they fill. It deletes nothing because nothing in the feed says an
agent is gone for good.

## Open questions

- Whether the MicroVM should carry an owner reference to its Secret and
  Volumes, which waits on gaon's garbage collector being verified.
