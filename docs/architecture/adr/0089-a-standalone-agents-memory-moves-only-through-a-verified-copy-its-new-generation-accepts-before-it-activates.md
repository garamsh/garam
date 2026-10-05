# ADR 0089: A standalone agent's memory moves only through a verified copy its new generation accepts before it activates

> Status: accepted
> Date: 2026-10-06

Drafted for #1202, which reserved this number; the PM accepted the
design on the dispatch on 2026-10-06. It answers the item
[ADR-0085](./0085-a-standalone-supervisor-owns-each-generation-as-three-containers-on-a-rootless-docker-daemon.md)
§What this does not decide left open, "An explicit storage handover",
and supersedes nothing. Facts about Sherlock hold at `v0.2.0`
(`b3c05c24bc2a346521251556935bfe0dc70f8cd1`); facts about Docker hold
for Engine 29.8.1 run rootless, as measured on the host the tests ran
on.

## Context

ADR-0078 requires a move to other storage to quiesce the old writer,
take a consistent copy of the database and the outbox, fence the old
writer and verify the restore before the new generation activates.
ADR-0085's supervisor refuses a memory volume it cannot prove and
waits for a person, and no command moved the memory anywhere.

Sherlock states the supported boundary for copying one agent's stored
state in its `docs/architecture/deployment.md` §Snapshot and restore:

- the state is `memory.db` with its `-wal` and `-shm`, and `outbox/`
  beside them; the writer lock `memory.db.lock` is not state;
- a consistent copy is taken only from a writer positively stopped,
  which is two facts: it exited 0 after its drain, and its writer lock
  is free; the copy holds that lock throughout;
- a sidecar that is absent stays absent, and a restore goes to a fresh
  path and replaces nothing;
- not starting a copy while the old writer might still run is the
  lifecycle authority's job, which Sherlock states and does not
  enforce.

Its `snapshot.sh` performs that procedure, but it lives in Sherlock's
repository, in neither image, and needs a shell, coreutils and
`python3`.

## Decision

**`garam standalone run --move-memory-to <volume>` is the one command
that moves an agent's memory, and it does so in one order**: drain the
running generation, observe that its writer stopped, copy, verify, start
the next generation on the copy, and activate it replacing the old
activation. The agent keeps its GRN.

- **The old writer is drained as ADR-0085 drains it**, SIGTERM and no
  deadline, runtime then workspace then adapter, which writes the
  outbox. The drain records on the memory volume's record the status
  the runtime exited with. A move takes only a store whose last
  runtime exited 0; a non-zero or an unobserved exit refuses it before
  anything of the move exists.
- **The move is recorded after that, before anything of it exists**:
  an identifier, a nonce, the volume it moves from, and the new volume
  with a nonce of its own. The supervisor creates the new volume as
  ADR-0085 creates one, so a volume already under that name refuses
  the move.
- **The copy is made under Sherlock's writer lock.** A helper container
  runs `garam copy-memory` from the run's garam image, as the runtime's
  account, with no network and no capability, the old volume mounted
  read-only and the new one writable. It takes the lock with a
  non-blocking `flock` and holds it for the whole copy, so a writer that
  has not stopped refuses it and none can start during it. It copies
  the three store files and the outbox, keeps an absent sidecar
  absent, copies no lock, refuses any entry that is neither a regular
  file nor a directory, and writes only into an empty volume.
- **The copy is verified by content digest.** The supervisor reads the
  old volume through the engine before the copy starts and after it
  ends, and the new one once, and digests each into a manifest of path,
  mode, owner and SHA-256 content, absences included. The old volume
  must not have changed, the new one must match it, and the new one
  must hold nothing else at its top. That the helper exited 0 is not
  the verification.
- **The copy is accepted by Sherlock's own `--require-store`.** A
  verified copy becomes the record's memory, and the next generation's
  runtime starts on it with `--require-store`. The adapter asks for an
  activation only for a runtime reporting `serving` (ADR-0085), so the
  first activation request of that generation is Sherlock having
  opened the copy. The supervisor records the move accepted then, and
  forwards the activation naming the old one as
  `replacesActivationId`, so garam fences the old generation's token
  and lineage from then on (ADR-0078, ADR-0080).
- **The old volume is never written or removed.** It is mounted
  read-only, the drain has stopped every container that mounted it,
  and discarding it stays a person's separate act.
- **Every failure refuses the move and states why.** A non-zero exit,
  a held lock, a digest that differs, an extra entry, a new volume not
  the move's own, or the generation on the copy ending before an
  activation was asked for it settles the move refused with its
  reason. The record's memory is then the old volume, the agent is
  stopped on it, and the run returns the reason. The refused volume
  is kept and never used again, so a move asked to it again refuses.
  Nothing falls back to either volume on its own.
- **A move recorded and not settled holds every run to it.** A run
  without the same `--move-memory-to` refuses rather than start on
  either volume. The same run asked again resumes where the record
  stands: the helper is found by its recorded ID, or by its name where
  its labels, image and volumes confirm the move, as ADR-0085 adopts a
  container.
- **An engine the supervisor cannot reach decides nothing**, as in
  ADR-0085: the move stays where the record says.

## What this does not decide

- The managed path's handover, which is the Operator's and Sherlock's
  (#1171).
- A move across hosts, deleting the old volume, and automatic recovery
  of a writer whose state is unknown.
- The workspace volume, which is not memory and does not move.

## Consequences

- `internal/workload` performs the move. `Engine` gains reading a path
  out of a container as an archive, a container's last log lines, a
  read-only mount, a container with no network and the exit status.
- garam's image carries `garam copy-memory`, so a run's adapter image
  is also its copier, and an image from before this cannot move.
- A runtime that was killed rather than drained cannot be moved until
  a generation runs on its volume and drains cleanly.
- A helper that exited non-zero leaves a partly written volume, which
  is refused and kept; the move then needs another volume.
- `make workload-integration` and `make standalone-e2e` run the move
  against a real daemon, the pinned images and, for the second, a real
  garam and model.

## Rejected alternatives

- **Copying through the engine's archive alone, with no process.**
  Nothing then holds the writer lock, so a writer started outside the
  supervisor's record would not be seen.
- **Running Sherlock's `snapshot.sh`.** Neither image carries it or
  what it needs.
- **A trial start of Sherlock on the copy, apart from the generation.**
  It starts a second runtime to prove what the generation's own start
  proves, and proves it of a process that does not then hold the
  activation.
- **Inferring the stop from the containers being gone.** Sherlock
  defines a drained writer by its exit status, and a killed writer is
  gone too.
- **Writing into a volume that already exists, or one a refused move
  wrote.** Either would mix the copy with what was there.
