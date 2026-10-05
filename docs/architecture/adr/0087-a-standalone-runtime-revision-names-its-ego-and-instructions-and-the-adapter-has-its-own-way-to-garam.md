# ADR 0087: A standalone runtime's revision names its ego and instructions, and the adapter has its own way to garam

> Status: accepted
> Date: 2026-10-05

Drafted for #1180's second part, which reserved this number. It
refines
[ADR-0085](./0085-a-standalone-supervisor-owns-each-generation-as-three-containers-on-a-rootless-docker-daemon.md)
and supersedes nothing. The PM accepted the three inputs on the
dispatch on 2026-10-05, and ruled that the revision rule is a decision
of its own. Facts about Sherlock hold at `v0.2.0`
(`b3c05c24bc2a346521251556935bfe0dc70f8cd1`).

## Context

ADR-0085's runner gives the runtime one file, its configuration, and
one address for garam, the one the supervisor dials. Two facts make
that too little for a runtime that holds a real conversation:

- Sherlock takes its ego and the operator's instructions only from
  `--ego-file` and `--instructions-file`, never from the configuration
  file. Its configuration's `revision` is "the desired configuration
  this file is, ego included", and its status echoes it.
- #1169's decision of 2026-10-04 17:04 requires a standalone host's
  configuration to carry the same `garam-message.v1` reply instruction
  a managed agent's does, under the host's own ownership.

Under a rootless daemon the host's loopback is not reachable from a
container, and a container's bridge address is not reachable from the
host. So the supervisor and the adapter beside the runtime may need
different addresses for the one machine listener.

## Decision

**`garam standalone run` takes three more inputs, each optional.**

- `--ego-file` and `--instructions-file` are copied into the runtime
  beside its configuration, owned by the runtime's account and
  readable by it alone, and named to Sherlock by its own flags. With no
  ego file, the runtime runs Sherlock's default ego.
- `--adapter-machine-url` is garam's machine listener as the adapter
  reaches it from inside the runtime's network namespace. It defaults
  to `--machine-url`, which stays the address the supervisor's own
  calls use.

**The configuration's `revision` names the ego and the instructions it
runs with, as well as the configuration.** Whoever writes the three
files writes a new revision when any of them changes. The supervisor
compares revisions and never the files, so a changed ego under an
unchanged revision is not a new generation, and the runtime's status
could not tell the two apart.

## Consequences

- A standalone host carries the garam reply instruction through
  `--instructions-file`. `tests/standalone` runs it in that form, with
  an explicit ego and with Sherlock's default.
- Changing a runtime's ego or instructions is a replacement, as
  changing its configuration is, and only through its revision.

## Rejected alternatives

- **Hashing the three files into the revision in the supervisor.** It
  would replace a value the host chose with one it did not, and the
  runtime's status would echo a revision nobody wrote.
- **Reaching garam from the adapter through the host.** Under a
  rootless daemon that needs a published route the host does not
  otherwise have.
