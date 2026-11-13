# ADR 0153: The gaon guest runs as root and its supervisor drops each program to the account it needs

> Status: accepted; built in `gaon-operator/`
> Date: 2026-11-13

Drafted for #1846, which reserves this number. It supersedes
[ADR-0151](./0151-the-gaon-operator-is-a-service-here-that-runs-each-managed-agent-as-one-microvm.md)
item 3 in part: the guest's user, and the drain order that item words.
The rest of item 3 stands: one MicroVM, one composed image whose
entrypoint is a supervisor, the three programs, the credential as a
Secret-sourced file.

## Context

ADR-0151 item 3 says the supervisor runs the three programs "as one
user", and the build issue read that as "user 65532 as in the runner".
That is not the runner's shape. [agents.md](../agents.md) §The host's
runner starts the runtime and the adapter as 65532 with no capability,
and the workspace as root with five capabilities (`SETUID`, `SETGID`,
`CHOWN`, `SYS_ADMIN`, `KILL`) and `SHERLOCK_EXEC_UID` 10001, so an
`Exec`'s child runs as an account that owns nothing. The memory volume
goes whole into the runtime and as the outbox alone into the adapter,
and the workspace volume goes to the workspace only. The model
credential is in the runtime's environment, which agents.md calls "a
limit rather than an isolation".

In one guest with one user, an `Exec` child is that user. The
check made on #1813 before the build found the child could read the
memory store, the outbox, the agent's tokens and possibly the model
credential, which is what the runner's accounts exist to prevent. A
MicroVM is a machine of its own, and the MicroVM, not the user inside
it, is the tenant boundary; inside it the three programs still need
separate accounts from each other and from the executor's children.

Two other statements of item 3 are wrong as written:

- **The drain order.** Item 3 says the programs are "started in that
  order and drained in the reverse order, without force, with the
  adapter last". The reverse of runtime, workspace, adapter ends with
  the runtime, so the sentence contradicts itself. The runner drains in
  start order, so the runtime stops first and the adapter outlives it.
  That the adapter is last is the item's intent.
- **What the composed image pins.** It carries the runtime and the
  workspace from Sherlock 0.10.0 (agent
  `sha256:fe7e426bc2ad7c6d5c31c96daf699fb1b801a6c9c14c3d917e7297a0cbd264cf`,
  workspace
  `sha256:acb6679005542bfd01705ae6a12926dee9a3f7ed839a673b14ec78f29a1fd489`)
  and garam's server 0.7.0
  (`sha256:d0798604733e7a1b7becedfe841add01a7f6f8423e2a1b222e1d6eb20bff1997`).
  `platform/Makefile` pins Sherlock 0.9.0 for the runner's tests, so
  what the guest runs is not what those tests ran.

The token the operator hands the adapter was named wrongly too. The
token minted with the control service as audience is the control token,
`GARAM_ADAPTER_CONTROL_TOKEN_FILE`, which the adapter presents to the
lifecycle authority ([adapter.md](../adapter.md) §Token mode); the
agent token has no audience. The placement token is a third, separate
credential, and where a managed agent's comes from is this:

- adapter.md and agents.md: it is long-lived per placement, sent in
  `Garam-Placement` on activation, and its SHA-256 is a field of the
  activation's request identifier; garam never issues it.
- `garamsh/garam-agent-operator` ADR-0042: the Kubernetes operator
  mints it, 32 random bytes hex-encoded, in its own Secret
  `<agent>-placement`, when that Secret is absent and again at each new
  placement. Registering a placement on the control service's route is
  work that ADR leaves to a later issue there (#218), and its delivery
  by copy is superseded by its ADR-0073, which this ADR did not read.
- The standalone supervisor draws its own 32 random bytes
  ([ADR-0085](./0085-a-standalone-supervisor-owns-each-generation-as-three-containers-on-a-rootless-docker-daemon.md)
  is the standalone supervisor's decision).
- `gaon-operator` mints the agent token and the control token and calls
  no placement route, so it holds no placement token and no route to
  register one. This is an **open dependency**: the adapter requires
  `GARAM_ADAPTER_PLACEMENT_TOKEN_FILE`, and nothing here supplies it.

## Decision

**The MicroVM's `user` is `0:0`.** The guest is a machine of its own,
and the supervisor needs root to give each program its account.

**The supervisor runs as root and starts each program as the account it
needs:**

- the runtime and the adapter as 65532:65532, with no supplementary
  group and so no capability;
- the workspace as root, with its bounding set cut to the runner's five
  capabilities (`setpriv --bounding-set`) and `SHERLOCK_EXEC_UID` 10001.

**What each account holds:**

- the memory directory and the outbox are 65532's at 0700, set by the
  supervisor before it starts anything; a path that is a symbolic link
  is refused;
- the credential files are 65532's at 0400, which the operator's
  `files` entries state as owner;
- the model credential is named by `GUEST_RUNTIME_ENV` and reaches the
  runtime's environment alone.

**A test in the built image shows a command run as 10001 refused each of
these** (`make -C gaon-operator guest-run`): the store, the outbox, each
credential file, and the runtime's and the adapter's environment, with
the same reads as 65532 succeeding so that each refusal is a
permission. It also reads each program's account and effective
capabilities.

**The drain is in start order**, runtime, workspace, adapter, each
awaited before the next, without force, as the runner does. This
corrects item 3's "reverse order" and does not change what the
supervisor did.

**The token minted with the control service as audience is the control
token**, `control-token` in the Secret and
`GARAM_ADAPTER_CONTROL_TOKEN_FILE` in the adapter. `machineapi` names it
so.

**The placement token stays an open dependency**: no operator here
mints or delivers it, and the next issue on the guest takes it with the
command, arguments and environment `render.go` does not yet render.

**The guest kernel for the executor is an open question for gaon**,
named exactly: does gaon's guest kernel admit, to a process running as
root with exactly `SETUID`, `SETGID`, `CHOWN`, `SYS_ADMIN` and `KILL`,
the unprivileged user-namespace and network-namespace `clone` that
`sherlock workspace` probes at start and logs as failed under Docker's
default seccomp profile (`NoNetwork probe failed`, `EPERM`)?

## Consequences

- **The workspace is still root in the same guest as the store.** It
  holds `CHOWN` and `SETUID`, so a workspace process could take the
  store or become 65532. The runner gives memory to the runtime only;
  the guest does not give the workspace that separation. The sign that
  would move it is a second MicroVM for the workspace, which needs
  links between the two that a guest's loopback does not give.
- **The runtime and the adapter share one account.** The runtime can
  read the adapter's tokens, and the adapter can read the whole store,
  where the runner gives it the outbox alone.
- **An `Exec` child, running as 10001, owns and reads nothing the guest
  keeps** by the permissions above. A real `Exec` through the workspace
  under gaon's guest kernel has not been run, and Docker's default
  seccomp refuses the executor's namespaces.
- **The test is in Docker and approximates a MicroVM.** It adds
  `SYS_ADMIN` so that the container's bounding set holds the five, and
  its `docker exec --user 10001` stands in for the executor's child.
- **`machineapi` and the guest rename the placement token to the
  control token**, and `render.go` renders root and 65532-owned files.
  A setuid binary in the image could let a 65532 process regain
  privilege; none was audited.
- **ADR-0151's status says it is superseded in part**, and its item 3 is
  read with this ADR's accounts and drain order.
- **The supervisor does not reap an orphaned `Exec` child that
  reparents to it.** Not built.

## Signs the decision was wrong

- gaon's guest kernel refuses the executor's namespaces for a root with
  these five capabilities, so no shell `Exec` runs. The accounts then
  change, or what gaon admits does.
- A second MicroVM for the workspace becomes possible, which would
  remove the root workspace from the store's guest and make this ADR's
  first consequence a defect.
- The control service's placement route is registered and the operator
  can mint a placement token. This ADR's open dependency then closes by
  another decision.
- A process of 10001 reads one of the paths the test says it is refused,
  in a real guest.
